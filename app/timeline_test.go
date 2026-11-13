package app

import (
	"testing"
	"time"

	"claude-squad/session"

	"github.com/stretchr/testify/require"
)

func TestBuildTimeline(t *testing.T) {
	to := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	from := to.Add(-3 * time.Hour)
	at := func(min int) time.Time { return from.Add(time.Duration(min) * time.Minute) }
	ev := func(min int, title string, n int, k session.StatusKind) session.Event {
		return session.Event{At: at(min), Kind: "status", Title: title, Number: n, Status: int(k), Label: "x"}
	}
	events := []session.Event{
		ev(-30, "a", 3, session.StatusWorking), // started before the window
		ev(60, "a", 3, session.StatusConductor),
		ev(90, "a", 3, session.StatusWorking),
		ev(150, "a", 3, session.StatusWorking), // same kind: merged
		ev(10, "gone", 9, session.StatusWorking),
		ev(40, "gone", 9, session.StatusOnHold), // closed afterwards: lane ends here
		ev(-100, "old", 8, session.StatusWorking),
		ev(5, "c", 1, session.StatusWaiting),
		{At: at(20), Kind: "vpn_down"},
	}
	live := map[string]tlLive{
		"a":   {number: 3, kind: int(session.StatusWorking)},
		"c":   {number: 1, cond: true, kind: int(session.StatusWaiting)},
		"new": {number: 12, kind: int(session.StatusStarting)},
	}
	res := buildTimeline(events, from, to, live)
	titles := []string{}
	for _, l := range res.Lanes {
		titles = append(titles, l.Title)
	}
	// conductor first, then live by number, closed ones last; "old" closed before the window
	require.Equal(t, []string{"c", "a", "new", "gone"}, titles)
	a := res.Lanes[1]
	require.Len(t, a.Segs, 3)
	require.Equal(t, from.UnixMilli(), a.Segs[0].From)
	require.Equal(t, int(session.StatusConductor), a.Segs[1].Kind)
	require.Equal(t, to.UnixMilli(), a.Segs[2].To)
	require.Equal(t, int64(150*60*1000), a.Working)
	require.Equal(t, int64(30*60*1000), a.WaitCond)
	gone := res.Lanes[3]
	require.False(t, gone.Live)
	require.Len(t, gone.Segs, 1)
	require.Equal(t, at(40).UnixMilli(), gone.Segs[0].To)
	require.Equal(t, 2, res.Now) // "a" works, "new" is starting
	require.Equal(t, 120, len(res.Parallel))
	require.LessOrEqual(t, res.Peak, 3)
}

func TestTimelineOrder(t *testing.T) {
	to := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	from := to.Add(-3 * time.Hour)
	at := func(min int) time.Time { return from.Add(time.Duration(min) * time.Minute) }
	ev := func(min int, title string, n int, k session.StatusKind) session.Event {
		return session.Event{At: at(min), Kind: "status", Title: title, Number: n, Status: int(k)}
	}
	events := []session.Event{
		ev(-60, "sleepy", 2, session.StatusPaused),
		ev(0, "early", 5, session.StatusWorking), ev(30, "early", 5, session.StatusOnHold),
		ev(0, "recent", 9, session.StatusOnHold), ev(150, "recent", 9, session.StatusWorking), ev(170, "recent", 9, session.StatusConductor),
		ev(0, "waits", 3, session.StatusConductor),
	}
	live := map[string]tlLive{
		"sleepy": {number: 2, kind: int(session.StatusPaused)},
		"early":  {number: 5, kind: int(session.StatusOnHold)},
		"recent": {number: 9, kind: int(session.StatusConductor)},
		"waits":  {number: 3, kind: int(session.StatusConductor)},
	}
	res := buildTimeline(events, from, to, live)
	titles := []string{}
	for _, l := range res.Lanes {
		titles = append(titles, l.Title)
	}
	require.Equal(t, []string{"recent", "early", "waits", "sleepy"}, titles)
	require.True(t, res.Lanes[3].Asleep)
}
