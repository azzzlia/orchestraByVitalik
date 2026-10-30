package app

import (
	"claude-squad/log"
	"claude-squad/session"
	"context"
	"errors"
	"fmt"
	"time"
)

// Token limits: Squad reads Claude Code's logs (no tokens spent), shows how full
// the 5-hour and weekly limits are, warns at 70% and wakes the conductor less
// often from 85%.

type usageState struct {
	tracker      *session.UsageTracker
	refreshing   bool
	lastAt       time.Time
	pct5h, pctWk float64
	units5h      float64
	unitsWk      float64
	warned       bool
	// plan: the real limits from Anthropic (as /usage shows them), asked every
	// 3 minutes; planErr — why the last ask failed (then the estimate is shown).
	plan     session.PlanUsage
	planAt   time.Time
	planErr  string
	planLogd string
}

// planFresh: the real limits are known and not older than 15 minutes.
func (u *usageState) planFresh() bool {
	return !u.plan.At.IsZero() && time.Since(u.plan.At) < 15*time.Minute
}

// handleUsage runs every monitor tick with m.mu held; the log reading itself
// happens in the background once a minute.
func (m *home) handleUsage() {
	u := &m.usage
	if u.tracker == nil {
		u.tracker = session.NewUsageTracker()
	}
	if time.Since(u.lastAt) < time.Minute || u.refreshing {
		return
	}
	u.refreshing = true
	u.lastAt = time.Now()
	tracker := u.tracker
	askPlan := !m.appConfig.PlanUsageOff && time.Since(u.planAt) >= 3*time.Minute
	if askPlan {
		u.planAt = time.Now()
	}
	go func() {
		tracker.Refresh()
		var plan session.PlanUsage
		var planErr error
		if askPlan {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			plan, planErr = session.FetchPlanUsage(ctx)
			cancel()
		}
		m.mu.Lock()
		if askPlan {
			if planErr == nil {
				m.usage.plan, m.usage.planErr = plan, ""
			} else {
				m.usage.planErr = planErr.Error()
				if errors.Is(planErr, session.ErrKeychainDenied) {
					m.usage.planAt = time.Now().Add(100 * 365 * 24 * time.Hour) // the user said no: no more prompts
				}
				if m.usage.planLogd != m.usage.planErr {
					m.usage.planLogd = m.usage.planErr
					log.WarningLog.Printf("usage: real limits unavailable: %v", planErr)
				}
			}
		}
		defer m.mu.Unlock()
		m.applyUsage()
		m.usage.refreshing = false
	}()
}

func (m *home) applyUsage() {
	u := &m.usage
	now := time.Now()
	_, u.units5h = u.tracker.Since(now.Add(-session.Window5h))
	_, u.unitsWk = u.tracker.Since(now.Add(-session.WindowWeek))
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	today, _ := u.tracker.Since(midnight)
	for _, inst := range m.list.GetInstances() {
		inst.UsageToday = today[inst.ClaudeConversationID()]
	}
	u.pct5h, u.pctWk = 0, 0
	if b := m.appConfig.TokenBudget5h; b > 0 {
		u.pct5h = 100 * u.units5h / b
	}
	if b := m.appConfig.TokenBudgetWeek; b > 0 {
		u.pctWk = 100 * u.unitsWk / b
	}
	if u.planFresh() {
		u.pct5h, u.pctWk = u.plan.FiveHour, u.plan.Week
	}
	session.SnapshotExtra.Usage5hPct, session.SnapshotExtra.UsageWeekPct = u.pct5h, u.pctWk
	warn, _ := m.appConfig.UsageThresholds()
	top := u.pct5h
	if u.pctWk > top {
		top = u.pctWk
	}
	switch {
	case top >= warn && !u.warned:
		u.warned = true
		notifyUser("orchestraByVitalik: лимит", fmt.Sprintf("Лимит заполнен на %.0f%% (5ч %.0f%%, неделя %.0f%%). Дирижёр будет просыпаться реже.", top, u.pct5h, u.pctWk))
	case top < warn-10:
		u.warned = false
	}
	m.list.SetBanner(m.usageBanner())
	m.maybeNotifyCapacity()
}

// usageThrottled reports whether the conductor should be woken less often.
func (m *home) usageThrottled() bool {
	_, throttle := m.appConfig.UsageThresholds()
	return m.usage.pct5h >= throttle || m.usage.pctWk >= throttle
}

func (m *home) usageBanner() string {
	u := &m.usage
	vpn := ""
	if m.vpnDown() {
		vpn = "VPN ✗ · "
	}
	if u.planFresh() {
		return fmt.Sprintf("%sлимит 5ч %.0f%% · нед %.0f%%", vpn, u.plan.FiveHour, u.plan.Week)
	}
	if m.appConfig.TokenBudget5h <= 0 && m.appConfig.TokenBudgetWeek <= 0 {
		if u.unitsWk == 0 {
			return vpn
		}
		return fmt.Sprintf("%s5ч %s · нед %s", vpn, session.FormatUnits(u.units5h), session.FormatUnits(u.unitsWk))
	}
	return fmt.Sprintf("%sлимит 5ч ~%.0f%% · нед ~%.0f%%", vpn, u.pct5h, u.pctWk)
}

// maybeNotifyCapacity tells the user when there is room to take a task from the
// team board (TeamBoard in the config): free slots and limits left. Costs no tokens; the
// assistant does the rest when asked.
func (m *home) maybeNotifyCapacity() {
	if m.appConfig.CapacityNotifyOff {
		return
	}
	now := time.Now()
	if now.Weekday() == time.Saturday || now.Weekday() == time.Sunday || now.Hour() < 10 || now.Hour() >= 19 {
		return
	}
	if time.Since(m.lastCapacityNote) < 2*time.Hour || m.usage.unitsWk == 0 && m.appConfig.TokenBudgetWeek > 0 {
		return
	}
	snap := &session.StatusSnapshot{Usage5hPct: m.usage.pct5h, UsageWeekPct: m.usage.pctWk, VPNDown: m.vpnDown()}
	for _, inst := range m.list.GetInstances() {
		if inst.Started() {
			snap.Sessions = append(snap.Sessions, inst.SnapshotEntry())
		}
	}
	c := session.ComputeCapacity(snap, m.appConfig.MaxActive())
	if !c.OK || c.Free < 2 {
		return
	}
	m.lastCapacityNote = now
	if board := m.appConfig.TeamBoard; board != "" {
		notifyUser("orchestraByVitalik: есть ресурс", "Можно взять задачу из "+board+" ("+c.Reason+"). Спроси помощника, что взять.")
	} else {
		notifyUser("orchestraByVitalik: есть ресурс", "Можно взять ещё задачу ("+c.Reason+").")
	}
}
