package app

import (
	"claude-squad/config"
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/ui"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// refreshDensity keeps the pet's score current. Priority: a score set by hand
// today (cs density 10,4), then density_command, then the local estimate from
// Claude Code's logs (sessions and subagents working at the same time).
func (m *home) refreshDensity() {
	if m.densityRunning || time.Since(m.densityRunAt) < time.Minute {
		return
	}
	m.densityRunning = true
	m.densityRunAt = time.Now()
	if m.densityTracker == nil {
		m.densityTracker = session.NewDensityTracker()
	}
	tracker, cmdline, t39cmd := m.densityTracker, m.appConfig.DensityCommand, m.appConfig.T39Command
	// Every 2 minutes (the "этот час ✓" mark should follow a commit soon), and
	// right after the hour changes.
	runCmd := cmdline != "" && (time.Since(m.densityCmdAt) >= 2*time.Minute || m.densityCmdHour != time.Now().Format("2006-01-02 15"))
	if runCmd {
		m.densityCmdAt = time.Now()
	}
	go func() {
		v, ok := 0.0, false
		if mv, at, has := session.ReadDensity(); has && at.Format("2006-01-02") == time.Now().Format("2006-01-02") {
			v, ok = mv, true // set by hand today
		} else if cmdline != "" {
			if runCmd {
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				out, err := exec.CommandContext(ctx, "/bin/sh", "-c", cmdline).Output()
				cancel()
				if err != nil {
					log.WarningLog.Printf("density_command: %v", err)
				} else if cv, cok := session.ParseDensity(string(out)); cok {
					m.mu.Lock()
					m.densityCmdValue, m.densityCmdOK = cv, true
					m.densityCmdNow = parseNowFlag(string(out))
					m.densityCmdHour = time.Now().Format("2006-01-02 15")
					m.mu.Unlock()
				}
			}
			m.mu.Lock()
			v, ok = m.densityCmdValue, m.densityCmdOK
			m.mu.Unlock()
		} else {
			tracker.Refresh(time.Now())
			if est := tracker.Estimate(); est.HasActivity {
				v, ok = est.Value, true
			}
		}
		session.WriteDensityCache(v, ok)
		tv, tok := session.T39Density(t39cmd)
		var phrases ui.PetPhrases
		if dir, err := config.GetConfigDir(); err == nil {
			phrases = ui.LoadPetPhrases(filepath.Join(dir, "pet-phrases.json"))
		}
		m.mu.Lock()
		m.t39, m.hasT39 = tv, tok
		m.petPhrases = phrases
		m.density, m.hasDensity = v, ok
		m.densityRunning = false
		m.mu.Unlock()
	}()
}

// petOverlay draws the pet over the overview: see-through, walking along the bottom.
func (m *home) petOverlay(bg string, width, height int) string {
	return ui.OverlayPet(bg, m.petConfig(), m.appConfig.PetStill || m.appConfig.PetBig, width, height, time.Now())
}

// petConfig is what the pet shows.
func (m *home) petConfig() ui.PetConfig {
	return ui.PetConfig{
		Kind: m.appConfig.Pet, CustomDir: m.appConfig.PetDir,
		Density: m.density, HasValue: m.hasDensity, T39: m.t39, HasT39: m.hasT39,
		Goal: m.densityGoal(), HourEmpty: m.hourEmptyNow(time.Now()), HourCommit: m.hourCommit(),
		Big: m.appConfig.PetBig, Phrases: m.petPhrases,
	}
}

const densityGoalHours = 6

// densityGoal is the daily goal in hours, shown only for the commit-hours score
// (density_command); the local estimate is another unit.
func (m *home) densityGoal() float64 {
	if m.appConfig.DensityCommand == "" || !m.densityCmdOK {
		return 0
	}
	return densityGoalHours
}

// hourCommit: 1 — there is a commit this hour, -1 — not yet, 0 — unknown (no
// commit-density command, it does not say, or it ran in the previous hour).
func (m *home) hourCommit() int {
	if m.appConfig.DensityCommand == "" || !m.densityCmdOK || m.densityCmdHour != time.Now().Format("2006-01-02 15") {
		return 0
	}
	switch m.densityCmdNow {
	case 1:
		return 1
	case 0:
		return -1
	}
	return 0
}

// parseNowFlag reads "now=0|1" from commit-density output (-1 when absent).
func parseNowFlag(out string) int {
	switch {
	case strings.Contains(out, "now=1"):
		return 1
	case strings.Contains(out, "now=0"):
		return 0
	}
	return -1
}

// hourEmptyNow: a weekday working hour (07–20) without a commit that is nearly
// over (from :40) — the last chance to make it count.
func (m *home) hourEmptyNow(now time.Time) bool {
	if !m.densityCmdOK || m.densityCmdNow != 0 || m.appConfig.DensityCommand == "" {
		return false
	}
	return workHourEnding(now)
}

func workHourEnding(now time.Time) bool {
	wd := now.Weekday()
	return wd != time.Saturday && wd != time.Sunday && now.Hour() >= 7 && now.Hour() < 20 && now.Minute() >= 40
}

// emptyHourNote is a line for the conductor digest, once per empty hour.
func (m *home) emptyHourNote() string {
	now := time.Now()
	key := now.Format("2006-01-02 15")
	if !m.hourEmptyNow(now) || m.emptyHourSent == key {
		return ""
	}
	m.emptyHourSent = key
	return fmt.Sprintf("час %02d:00 пока без коммита — если у кого-то из воркеров есть законченный кусок, пусть закоммитит локально (без пустых коммитов)", now.Hour())
}
