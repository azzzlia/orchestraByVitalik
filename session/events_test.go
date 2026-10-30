package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestComputeStatsAndEventLog(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t0 := time.Now().Add(-3 * time.Hour)
	ev := func(min int, kind StatusKind) {
		AppendEvent(Event{At: t0.Add(time.Duration(min) * time.Minute), Kind: "status", Number: 3, Title: "a", Status: int(kind)})
	}
	ev(0, StatusWorking)
	ev(30, StatusWaiting) // finished a step, waits 60 min for the user
	ev(90, StatusWorking)
	ev(100, StatusOnHold)
	ev(120, StatusNeedsInput) // waits until now (60 min)
	AppendEvent(Event{At: t0.Add(5 * time.Minute), Kind: "vpn_down"})

	events := ReadEvents(t0.Add(-time.Minute))
	require.Len(t, events, 6)
	st := ComputeStats(events, t0.Add(180*time.Minute))["a"]
	require.Equal(t, 40*time.Minute, st.Working)
	require.Equal(t, 120*time.Minute, st.WaitingUser)
	require.Equal(t, 20*time.Minute, st.OnHold)
	require.Equal(t, 1, st.Finished)
	require.Equal(t, 60*time.Minute, st.LongestWait)
	require.Equal(t, 3, st.Number)
	require.Len(t, ReadEvents(t0.Add(100*time.Minute)), 1)
}

func TestComputeCapacity(t *testing.T) {
	snap := &StatusSnapshot{Sessions: []StatusEntry{
		{Title: "c", Conductor: true}, {Title: "a", Assistant: true},
		{Title: "w1", Status: "в работе"}, {Title: "w2", Status: "ждёт тебя"}, {Title: "s", Status: "спит"},
	}}
	c := ComputeCapacity(snap, 4)
	require.True(t, c.OK)
	require.Equal(t, 2, c.Busy)
	require.Equal(t, 2, c.Free)
	snap.Usage5hPct = 65
	require.False(t, ComputeCapacity(snap, 4).OK)
	snap.Usage5hPct, snap.VPNDown = 10, true
	require.Contains(t, ComputeCapacity(snap, 4).Reason, "VPN")
	snap.VPNDown = false
	require.Contains(t, ComputeCapacity(snap, 2).Reason, "заняты")
}
