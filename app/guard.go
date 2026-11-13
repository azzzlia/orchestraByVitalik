package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"claude-squad/log"
	"claude-squad/session"
)

// The guard picks up sessions that got stuck without anyone noticing:
//   - a session that shows a network error (or reports in its recap that the
//     network/VPN was down) while the VPN is up gets «the network is back, go on»,
//     at most every guardPingEvery; one still stuck on its screen error after
//     guardRestartAfter is restarted (same conversation, MCP servers connect again);
//   - after guardMaxPings tries it gives up on the session (until it recovers);
//   - workers waiting for the conductor longer than guardStaleWait are listed for
//     the conductor in its digest, at most every guardStaleEvery.
// It never touches sleeping or paused sessions, the assistant, sessions waiting
// for the user, or a draft the user is typing.
const (
	guardPingEvery    = 20 * time.Minute
	guardRestartAfter = 40 * time.Minute
	guardStaleWait    = 20 * time.Minute
	guardStaleEvery   = 30 * time.Minute
	guardTick         = 30 * time.Second
	// guardMaxPings: a session still on a network error after this many tries has
	// another problem (a stand that is down, say) — the guard stops spending tokens.
	guardMaxPings = 3
)

type guardRec struct {
	since, pinged, restarted time.Time
	recapPinged              string
	pings                    int
}

// guardNetStuck runs every monitor tick with m.mu held.
func (m *home) guardNetStuck(now time.Time) {
	if now.Sub(m.guardAt) < guardTick {
		return
	}
	known, up, upSince := m.net.get()
	if !known || !up || len(m.netRecover) > 0 || now.Sub(upSince) < 2*time.Minute {
		return // no VPN check, VPN down, or the outage recovery is still at work
	}
	m.guardAt = now
	if m.guard == nil {
		m.guard = map[*session.Instance]*guardRec{}
	}
	alive := map[*session.Instance]bool{}
	for _, inst := range m.list.GetInstances() {
		alive[inst] = true
		if !guardMayTouch(inst) {
			continue
		}
		screen := inst.NetworkIssue
		told := !screen && inst.Stage() == "нет сети / VPN"
		if !screen && !told {
			delete(m.guard, inst)
			continue
		}
		rec := m.guard[inst]
		if rec == nil {
			rec = &guardRec{since: now}
			m.guard[inst] = rec
		}
		if told && rec.recapPinged == inst.Recap {
			continue // already told it about this recap; wait for a new one
		}
		if now.Sub(rec.pinged) < guardPingEvery || rec.pings >= guardMaxPings {
			continue
		}
		restart := screen && now.Sub(rec.since) >= guardRestartAfter && now.Sub(rec.restarted) >= guardRestartAfter
		action := m.nudge(inst, restart)
		if action == "" {
			continue // the input box is not ready: next tick
		}
		rec.pinged, rec.recapPinged = now, inst.Recap
		rec.pings++
		if restart {
			rec.restarted = now
		}
		log.InfoLog.Printf("guard: %q stuck on the network while the VPN is up — %s", inst.Title, action)
		session.AppendEvent(session.Event{At: now, Kind: "status", Text: fmt.Sprintf("сторож: #%d «%s» висела на сети — %s", inst.Number, inst.Title, action)})
	}
	for inst := range m.guard {
		if !alive[inst] {
			delete(m.guard, inst)
		}
	}
}

// guardMayTouch: the guard leaves alone what is not its business.
func guardMayTouch(inst *session.Instance) bool {
	if inst.Assistant || !inst.Started() || inst.Paused() || inst.Status == session.Running ||
		inst.Status == session.Loading || inst.NeedsInput || inst.MarkedFor == "user" ||
		inst.DraftBlocks() || inst.Menu != nil {
		return false
	}
	return true
}

// staleWaitNote lists workers that have been waiting for the conductor too long, for
// the digest; empty when there are none or the last note is recent.
func (m *home) staleWaitNote(now time.Time) string {
	if now.Sub(m.lastStaleNoteAt) < guardStaleEvery {
		return ""
	}
	type item struct {
		n    int
		text string
	}
	var items []item
	for _, inst := range m.list.GetInstances() {
		if inst.Conductor || inst.Assistant || !inst.Started() || inst.Paused() || inst.IdleSince.IsZero() {
			continue
		}
		label, kind := inst.StatusLabel()
		if kind != session.StatusConductor || label != "ждёт дирижёра" {
			continue
		}
		if wait := now.Sub(inst.IdleSince); wait >= guardStaleWait {
			title := []rune(inst.Title)
			if len(title) > 30 {
				title = title[:30]
			}
			items = append(items, item{inst.Number, fmt.Sprintf("#%d «%s» (%d мин)", inst.Number, string(title), int(wait.Minutes()))})
		}
	}
	if len(items) == 0 {
		return ""
	}
	sort.Slice(items, func(a, b int) bool { return items[a].n < items[b].n })
	parts := make([]string, len(items))
	for k, it := range items {
		parts[k] = it.text
	}
	return fmt.Sprintf("[сторож] ждут дирижёра дольше %d мин: %s — подними их или дай конкретное следующее действие",
		int(guardStaleWait.Minutes()), strings.Join(parts, "; "))
}
