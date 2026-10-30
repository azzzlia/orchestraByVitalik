package app

import (
	"claude-squad/config"
	"claude-squad/log"
	"claude-squad/session"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The morning brief: once a day at morning_brief_at (09:00) a short text — who waits
// for the user, open MRs, what the conductor did at night, VPN — goes out as a Mac
// notification and, through the conductor, to the «Избранное» chat, so it reaches the
// phone. It costs the conductor one short turn.

const briefDeadline = 2 * time.Hour // conductor busy all this time: Mac notification only

func briefStampPath() string {
	dir, err := config.GetConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "morning-brief")
}

// buildBrief is the brief text for the night since `since`.
func (m *home) buildBrief(now, since time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Оркестр, утро %s.\n", now.Format("02.01"))
	queue := session.UserQueue(m.list.GetInstances())
	if len(queue) == 0 {
		b.WriteString("• Ждут тебя: никто.\n")
	} else {
		fmt.Fprintf(&b, "• Ждут тебя (%d):\n", len(queue))
		for n, it := range queue {
			if n == 6 {
				fmt.Fprintf(&b, "  … и ещё %d — клавиша u в Squad\n", len(queue)-n)
				break
			}
			text := strings.Join(strings.Fields(it.Text), " ")
			if r := []rune(text); len(r) > 90 {
				text = string(r[:90]) + "…"
			}
			fmt.Fprintf(&b, "  #%d %s — %s: %s\n", it.Inst.Number, it.Inst.Title, it.KindLabel(), text)
		}
	}
	var open []string
	for _, inst := range m.list.GetInstances() {
		for _, r := range inst.OpenMRs() {
			open = append(open, fmt.Sprintf("#%d %s", inst.Number, r))
		}
	}
	if len(open) > 0 {
		if len(open) > 8 {
			open = append(open[:8], "…")
		}
		fmt.Fprintf(&b, "• Открытые MR: %s\n", strings.Join(open, ", "))
	}
	working := 0
	for _, inst := range m.list.GetInstances() {
		if !inst.Conductor && !inst.Assistant && inst.Status == session.Running {
			working++
		}
	}
	if working > 0 {
		fmt.Fprintf(&b, "• Сейчас в работе: %d\n", working)
	}
	if j := journalSince(since); len(j) > 0 {
		fmt.Fprintf(&b, "• Дирижёр за ночь: %d записей в журнале, последняя — %s\n", len(j), trimRunes(j[len(j)-1], 120))
	}
	outages := 0
	for _, e := range session.ReadEvents(since) {
		if e.Kind == "vpn_down" {
			outages++
		}
	}
	if outages > 0 {
		fmt.Fprintf(&b, "• VPN отваливался %d раз, Squad поднимал воркеров сам\n", outages)
	}
	b.WriteString("Подробно — клавиша w в Squad.")
	return b.String()
}

func trimRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// maybeMorningBrief sends the brief once a day after morning_brief_at.
func (m *home) maybeMorningBrief(now time.Time) {
	h, mi, ok := m.appConfig.MorningBriefTime()
	if !ok {
		return
	}
	at := time.Date(now.Year(), now.Month(), now.Day(), h, mi, 0, 0, now.Location())
	if now.Before(at) {
		return
	}
	today := now.Format("2006-01-02")
	if m.briefDay == today || now.Sub(m.briefTriedAt) < 30*time.Second {
		return
	}
	m.briefTriedAt = now
	path := briefStampPath()
	if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) == today {
		m.briefDay = today
		return
	}
	since := now.Add(-14 * time.Hour)
	text := m.buildBrief(now, since)
	if !m.briefNotified {
		m.briefNotified = true
		first := strings.SplitN(text, "\n", 3)
		msg := "утренний отчёт"
		if len(first) > 1 {
			msg = first[1]
		}
		notifyUser("orchestraByVitalik: утро", strings.TrimPrefix(msg, "• "))
	}
	done := func() {
		m.briefNotified, m.briefDay = false, today
		_ = os.WriteFile(path, []byte(today), 0600)
	}
	var conductor *session.Instance
	for _, inst := range m.list.GetInstances() {
		if inst.Conductor {
			conductor = inst
		}
	}
	if m.appConfig.BriefChat == "" || conductor == nil || !conductor.Started() || now.Sub(at) > briefDeadline {
		done()
		return
	}
	if conductor.Status == session.Running || conductor.DraftBlocks() || conductor.NeedsInput || conductor.Paused() || !conductor.InputReady() || conductor == m.attached {
		return // try again on a later tick
	}
	msg := "[squad] Утренний отчёт для пользователя. Отправь его одним сообщением в чат «" + m.appConfig.BriefChat + "» как есть (¶ — перенос строки), ничего не добавляя и не делая больше ничего: " +
		strings.ReplaceAll(text, "\n", " ¶ ")
	if err := conductor.ReplaceInput(msg); err != nil {
		log.WarningLog.Printf("morning brief: %v", err)
		return
	}
	m.digestSentAt, m.digestAcked, m.digestWarned = now, false, false
	done()
}
