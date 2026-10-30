package session

import (
	"fmt"
	"os"
	"strings"
)

// IdleFreesPlaceAfter: a worker idle this long (finished its step, waits for someone)
// no longer takes a place — it costs no tokens; Squad may put it to sleep.
const IdleFreesPlaceAfter = 30

// DiskLowGB / DiskStopGB: free disk space under which new clones and sessions are a
// risk (a bx clone takes 4+ GB) and under which nothing new should be started.
const (
	DiskLowGB  = 20
	DiskStopGB = 10
)

// Capacity says whether there is room to take another task: free session slots
// and enough of the token limits left. Computed from the snapshot, no tokens.
type Capacity struct {
	Busy, Max, Free  int
	BusyList         []string // "#4 Подсказки… — в работе": the sessions that take places
	Usage5h, UsageWk float64
	DiskFreeGB       float64  // free space in the home folder, -1 unknown
	IdleList         []string // finished workers that do not take a place
	OK               bool
	Reason           string
}

// ComputeCapacity counts busy worker sessions (working, waiting by itself, or
// waiting for the user) against maxActive, and checks the limits.
func ComputeCapacity(snap *StatusSnapshot, maxActive int) Capacity {
	c := Capacity{Max: maxActive, Usage5h: snap.Usage5hPct, UsageWk: snap.UsageWeekPct}
	for _, s := range snap.Sessions {
		if s.Conductor || s.Assistant || s.Status == "спит" || s.Status == "на паузе" {
			continue
		}
		background := strings.HasPrefix(s.Status, "в ожидании") || strings.HasPrefix(s.Status, "упёрся в лимит")
		if s.IdleMinutes >= IdleFreesPlaceAfter && !background {
			c.IdleList = append(c.IdleList, fmt.Sprintf("#%d %s — %s, %d мин", s.Number, s.Title, s.Status, s.IdleMinutes))
			continue
		}
		c.Busy++
		c.BusyList = append(c.BusyList, fmt.Sprintf("#%d %s — %s", s.Number, s.Title, s.Status))
	}
	c.Free = c.Max - c.Busy
	c.DiskFreeGB = -1
	if home, err := os.UserHomeDir(); err == nil {
		c.DiskFreeGB = DiskFreeGB(home)
	}
	switch {
	case c.DiskFreeGB >= 0 && c.DiskFreeGB < DiskStopGB:
		c.Reason = fmt.Sprintf("на диске %.0f ГБ свободно — новые клоны и сессии не запускать, нужна уборка (решает пользователь)", c.DiskFreeGB)
	case snap.VPNDown:
		c.Reason = "VPN лежит"
	case c.Free <= 0:
		c.Reason = fmt.Sprintf("все %d мест заняты", c.Max)
	case c.Usage5h >= 60:
		c.Reason = fmt.Sprintf("лимит 5ч уже %.0f%%", c.Usage5h)
	case c.UsageWk >= 70:
		c.Reason = fmt.Sprintf("недельный лимит уже %.0f%%", c.UsageWk)
	default:
		c.OK = true
		c.Reason = fmt.Sprintf("свободно мест: %d из %d, лимит 5ч %.0f%%, неделя %.0f%%", c.Free, c.Max, c.Usage5h, c.UsageWk)
		if c.DiskFreeGB >= 0 && c.DiskFreeGB < DiskLowGB {
			c.Reason += fmt.Sprintf("; диск: %.0f ГБ — клон окружения (bx clone, ~4 ГБ) только один", c.DiskFreeGB)
		}
	}
	return c
}
