package app

import (
	"claude-squad/config"
	"claude-squad/session"
	"claude-squad/ui"
	"claude-squad/ui/overlay"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Status history, the morning report ("что было", w) and quick opening of a
// session's QA report (O) and folder (f). None of it costs tokens.

// statusStable is how long a status must hold before it is written to the log
// (agents flicker between "working" and "waiting" between ticks).
const statusStable = 10 * time.Second

type statusTrack struct {
	label   string
	kind    session.StatusKind
	since   time.Time
	written string
}

// recordStatuses writes status changes to events.log. Runs with m.mu held.
func (m *home) recordStatuses() {
	if m.statusLog == nil {
		m.statusLog = map[*session.Instance]*statusTrack{}
		session.TrimEvents(60 * 24 * time.Hour)
	}
	now := time.Now()
	for _, inst := range m.list.GetInstances() {
		if !inst.Started() {
			continue
		}
		label, kind := inst.StatusLabel()
		tr := m.statusLog[inst]
		if tr == nil {
			tr = &statusTrack{}
			m.statusLog[inst] = tr
		}
		if label != tr.label {
			tr.label, tr.kind, tr.since = label, kind, now
		}
		if tr.written != tr.label && now.Sub(tr.since) >= statusStable {
			tr.written = tr.label
			session.AppendEvent(session.Event{At: tr.since, Kind: "status", Number: inst.Number,
				Title: inst.Title, Label: tr.label, Status: int(tr.kind)})
		}
	}
}

func reportStampPath() string {
	dir, err := config.GetConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "last-report")
}

// maybeMorningReport shows "что было" once a day from 5:00 — also when Squad ran
// all night (it is checked every tick, not only at start).
func (m *home) maybeMorningReport() {
	now := time.Now()
	if m.state != stateDefault || m.overview || now.Hour() < 5 {
		return
	}
	if m.morningCheckedDay == now.Format("2006-01-02") {
		return
	}
	m.morningCheckedDay = now.Format("2006-01-02")
	m.morningPending = true // shown on the first key press: the user is back
}

// showMorningReport shows "что было" since the last report; false when nothing to show.
func (m *home) showMorningReport() bool {
	now := time.Now()
	last := time.Time{}
	if data, err := os.ReadFile(reportStampPath()); err == nil {
		last, _ = time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	}
	if !last.IsZero() && last.Format("2006-01-02") == now.Format("2006-01-02") {
		return false
	}
	since := last
	if since.IsZero() || now.Sub(since) > 72*time.Hour {
		since = now.Add(-16 * time.Hour)
	}
	text := m.buildReport(since)
	if text == "" {
		return false
	}
	m.showReport(text)
	return true
}

func (m *home) showReport(text string) {
	_ = os.WriteFile(reportStampPath(), []byte(time.Now().Format(time.RFC3339)), 0600)
	m.showText(text)
}

var journalDateRe = regexp.MustCompile(`^\s*[-*]?\s*(\d{2})\.(\d{2})(?:\.(\d{2,4}))?\s+(\d{1,2}):(\d{2})`)

// journalSince returns conductor journal lines written after since.
func journalSince(since time.Time) []string {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".claude-squad", "conductor", "journal.md"))
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		mm := journalDateRe.FindStringSubmatch(line)
		if mm == nil {
			continue
		}
		var d, mo, h, mi int
		fmt.Sscanf(mm[1]+" "+mm[2]+" "+mm[4]+" "+mm[5], "%d %d %d %d", &d, &mo, &h, &mi)
		t := time.Date(since.Year(), time.Month(mo), d, h, mi, 0, 0, time.Local)
		if t.After(time.Now().Add(24 * time.Hour)) { // December entries read in January
			t = t.AddDate(-1, 0, 0)
		}
		if t.After(since) {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return out
}

// buildReport describes what happened since `since`: sessions, conductor, VPN, tokens.
func (m *home) buildReport(since time.Time) string {
	events := session.ReadEvents(since)
	var b strings.Builder
	fmt.Fprintf(&b, "Что было с %s\n\n", since.Format("02.01 15:04"))

	// Sessions: how many steps each finished, and where it stands now.
	stats := session.ComputeStats(events, time.Now())
	type row struct {
		inst *session.Instance
		st   *session.SessionStats
	}
	var rows []row
	for _, inst := range m.list.GetInstances() {
		if st := stats[inst.Title]; st != nil && (st.Finished > 0 || st.Working > 0) {
			rows = append(rows, row{inst, st})
		}
	}
	sort.Slice(rows, func(a, c int) bool { return urgencyRank(rows[a].inst) < urgencyRank(rows[c].inst) })
	if len(rows) > 0 {
		b.WriteString("Сессии:\n")
		for _, r := range rows {
			label, _ := r.inst.StatusLabel()
			if st := r.inst.Stage(); st != "" {
				label += " · " + st
			}
			fmt.Fprintf(&b, "  #%d %s — сейчас: %s; работал %s, закончил шагов: %d\n",
				r.inst.Number, r.inst.Title, label, fmtDur(r.st.Working), r.st.Finished)
		}
		b.WriteString("\n")
	}
	var waiting, forConductor []string
	for _, inst := range m.list.GetInstances() {
		if inst.Conductor || inst.Assistant {
			continue
		}
		label, kind := inst.StatusLabel()
		switch kind {
		case session.StatusNeedsInput, session.StatusWaiting:
			item := fmt.Sprintf("#%d", inst.Number)
			if strings.Contains(label, " · ") {
				item += " (" + label[strings.Index(label, " · ")+len(" · "):] + ")"
			}
			waiting = append(waiting, item)
		case session.StatusConductor:
			forConductor = append(forConductor, fmt.Sprintf("#%d", inst.Number))
		}
	}
	if len(waiting) > 0 {
		fmt.Fprintf(&b, "Ждут тебя: %s\n", strings.Join(waiting, ", "))
	}
	if len(forConductor) > 0 {
		fmt.Fprintf(&b, "Ждут дирижёра: %s\n", strings.Join(forConductor, ", "))
	}
	if len(waiting)+len(forConductor) > 0 {
		b.WriteString("\n")
	}
	if line := m.densityDayLine(lastWorkday(time.Now())); line != "" {
		b.WriteString(line + "\n\n")
	}
	if wipes := session.ReadRecentWipes(time.Since(since)); len(wipes) > 0 {
		var titles []string
		for t := range wipes {
			titles = append(titles, t)
		}
		sort.Strings(titles)
		fmt.Fprintf(&b, "Стёрты забытые черновики: %s (тексты — wiped-drafts.log)\n\n", strings.Join(titles, ", "))
	}

	if j := journalSince(since); len(j) > 0 {
		b.WriteString("Дирижёр без тебя:\n")
		if len(j) > 12 {
			fmt.Fprintf(&b, "  … ещё %d записей раньше (journal.md)\n", len(j)-12)
			j = j[len(j)-12:]
		}
		for _, l := range j {
			b.WriteString("  " + strings.TrimLeft(l, "-* ") + "\n")
		}
		b.WriteString("\n")
	}

	// VPN outages and what Squad did about them.
	var outages int
	var downFor time.Duration
	var downAt time.Time
	var recovered []string
	for _, e := range events {
		switch e.Kind {
		case "vpn_down":
			outages++
			downAt = e.At
		case "vpn_up":
			if !downAt.IsZero() {
				downFor += e.At.Sub(downAt)
				downAt = time.Time{}
			}
		case "recover":
			recovered = append(recovered, e.Text)
		}
	}
	if outages > 0 {
		fmt.Fprintf(&b, "VPN отваливался %d раз (всего %s).", outages, fmtDur(downFor))
		if len(recovered) > 0 {
			b.WriteString(" Squad растормошил: " + strings.Join(recovered, "; "))
		}
		b.WriteString("\n\n")
	}

	if m.usage.tracker != nil {
		_, total := m.usage.tracker.Since(since)
		if total > 0 {
			fmt.Fprintf(&b, "Токенов за это время: %s (%s)\n\n", session.FormatUnits(total), m.usageBanner())
		}
	}

	if len(rows) == 0 && outages == 0 && len(waiting) == 0 {
		return ""
	}
	b.WriteString("Статистика за неделю: cs stats.   (любая клавиша — закрыть)")
	return b.String()
}

func fmtDur(d time.Duration) string {
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dч %02dм", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dм", int(d.Minutes()))
	default:
		return "<1м"
	}
}

// qaReport finds the newest QA report (report.html) in the session's folder.
func qaReport(inst *session.Instance) string {
	dir := inst.GetWorktreePath()
	if dir == "" {
		return ""
	}
	// The recap usually names the report exactly.
	if mm := regexp.MustCompile(`(/\S+?report\.html)`).FindStringSubmatch(inst.Recap); mm != nil {
		if _, err := os.Stat(mm[1]); err == nil {
			return mm[1]
		}
	}
	matches, _ := filepath.Glob(filepath.Join(dir, ".ai", "qa-screens", "*", "report.html"))
	newest, newestAt := "", time.Time{}
	for _, p := range matches {
		if info, err := os.Stat(p); err == nil && info.ModTime().After(newestAt) {
			newest, newestAt = p, info.ModTime()
		}
	}
	return newest
}

func openPath(p string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("открыть можно только на macOS: %s", p)
	}
	return exec.Command("/usr/bin/open", p).Start()
}

// showText shows a text window (closed by any key).
func (m *home) showText(text string) {
	m.textOverlay = overlay.NewTextOverlay(text)
	m.textOverlay.SetWidth(int(float32(m.winW) * 0.8))
	m.state = stateHelp
}

// lastWorkday is the previous weekday before now (Friday on a Monday).
func lastWorkday(now time.Time) time.Time {
	d := now.AddDate(0, 0, -1)
	for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
		d = d.AddDate(0, 0, -1)
	}
	return d
}

// densityDayLine: "Плотность за пт 24.10: 5 ч из 6 · пустые рабочие часы: 09, 12".
func (m *home) densityDayLine(day time.Time) string {
	cmdline := m.appConfig.DensityCommand
	if cmdline == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "/bin/sh", "-c", cmdline)
	c.Env = append(os.Environ(), "COMMIT_DENSITY_DAY="+day.Format("2006-01-02"))
	out, err := c.Output()
	if err != nil {
		return ""
	}
	v, ok := session.ParseDensity(string(out))
	if !ok {
		return ""
	}
	return formatDensityDay(day, v, string(out))
}

var weekdayShort = []string{"вс", "пн", "вт", "ср", "чт", "пт", "сб"}

func formatDensityDay(day time.Time, v float64, out string) string {
	have := map[int]bool{}
	if i := strings.Index(out, "hours="); i >= 0 {
		for _, h := range strings.Split(strings.Fields(out[i+len("hours="):] + " ")[0], ",") {
			if n, err := strconv.Atoi(h); err == nil {
				have[n] = true
			}
		}
	}
	var empty []string
	for h := 7; h < 20; h++ {
		if !have[h] {
			empty = append(empty, fmt.Sprintf("%02d", h))
		}
	}
	line := fmt.Sprintf("Плотность за %s %s: %s ч из %d", weekdayShort[day.Weekday()], day.Format("02.01"),
		ui.FormatDensity(v), densityGoalHours)
	if v >= densityGoalHours {
		line += " ✓"
	}
	if len(empty) > 0 && len(empty) < 13 {
		line += " · пустые рабочие часы: " + strings.Join(empty, ", ")
	}
	return line
}
