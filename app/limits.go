package app

import (
	"claude-squad/log"
	"claude-squad/session"
	"fmt"
	"strings"
	"time"
)

// When the plan's usage limit is hit, agents stop with an error and do not go on
// by themselves once it resets. Squad remembers them and, after the reset, asks
// each one to continue. It also watches that the conductor reacts to digests.

const limitContinueMsg = "[squad] Лимит токенов сбросился. Продолжай задачу с того места, где остановился."

type limitTrack struct {
	nextAt   time.Time
	tries    int
	lastSeen time.Time // the session was last seen on a limit; the track is forgotten
	// only after 2 hours without one (a "continue" that hits the limit again must not
	// reset the try counter and the notification)
	active bool // on a limit right now; false once the session got past it
}

// limitNext is when to ask a limited session to continue: 2 minutes after the reset
// time it printed, or in 20 minutes when it did not print one.
func limitNext(inst *session.Instance, now time.Time) time.Time {
	if !inst.LimitReset.IsZero() && inst.LimitReset.After(now.Add(-time.Hour)) {
		return inst.LimitReset.Add(2 * time.Minute)
	}
	return now.Add(20 * time.Minute)
}

func (m *home) handleLimits(now time.Time) {
	if m.limitTracks == nil {
		m.limitTracks = map[*session.Instance]*limitTrack{}
	}
	var fresh []string
	alive := map[*session.Instance]bool{}
	for _, inst := range m.list.GetInstances() {
		alive[inst] = true
		tr := m.limitTracks[inst]
		if !inst.LimitHit || !inst.Started() || inst.Paused() || inst.Assistant {
			if tr != nil {
				if !inst.LimitHit {
					tr.active = false
				}
				if now.Sub(tr.lastSeen) > 2*time.Hour {
					delete(m.limitTracks, inst)
				}
			}
			continue
		}
		if tr != nil {
			tr.lastSeen = now
			if !tr.active {
				// Hit the limit again after getting past it: wait for the new reset,
				// do not nudge at the old (already passed) time.
				tr.active = true
				tr.nextAt = limitNext(inst, now)
			}
		}
		if tr == nil {
			tr = &limitTrack{nextAt: limitNext(inst, now), lastSeen: now, active: true}
			m.limitTracks[inst] = tr
			fresh = append(fresh, fmt.Sprintf("#%d", inst.Number))
			continue
		}
		if now.Before(tr.nextAt) || tr.tries >= 6 {
			continue
		}
		if inst == m.attached || inst.Status == session.Running || inst.NeedsInput || inst.Draft != "" || !inst.InputReady() {
			continue
		}
		tr.tries++
		tr.nextAt = now.Add(20 * time.Minute) // still limited? try again later
		if err := inst.ReplaceInput(limitContinueMsg); err != nil {
			log.WarningLog.Printf("continue after limit %q: %v", inst.Title, err)
			continue
		}
		session.AppendEvent(session.Event{Kind: "recover", Number: inst.Number, Title: inst.Title, Text: fmt.Sprintf("#%d продолжил после лимита", inst.Number)})
	}
	for inst := range m.limitTracks {
		if !alive[inst] {
			delete(m.limitTracks, inst)
		}
	}
	if len(fresh) > 0 {
		notifyUser("orchestraByVitalik: лимит", "Упёрлись в лимит: "+strings.Join(fresh, ", ")+". Когда он сбросится, оркестр сам попросит их продолжить.")
	}
}

// watchConductor notifies the user when the conductor has not reacted to a
// digest for a while (it did not start working on it).
func (m *home) watchConductor(now time.Time) {
	if m.digestSentAt.IsZero() || m.digestAcked {
		return
	}
	var conductor *session.Instance
	for _, inst := range m.list.GetInstances() {
		if inst.Conductor {
			conductor = inst
		}
	}
	if conductor == nil {
		return
	}
	// Typing the digest itself changes the screen; count only what happens later.
	if now.Sub(m.digestSentAt) > 5*time.Second && (conductor.Status == session.Running || conductor.UpdatedAfter(m.digestSentAt.Add(5*time.Second))) {
		m.digestAcked = true
		return
	}
	if now.Sub(m.digestSentAt) > 10*time.Minute && !m.digestWarned {
		m.digestWarned = true
		notifyUser("orchestraByVitalik: дирижёр молчит", fmt.Sprintf("Сводка висит 10 минут без ответа — загляни в #%d.", conductor.Number))
	}
}
