package app

import (
	"strings"
	"testing"
	"time"

	"claude-squad/session"

	"github.com/stretchr/testify/require"
)

// A session showing a network error while the VPN has long been up gets one
// «go on» — not again on every tick — and one more only after guardPingEvery.
func TestGuardPingsSessionStuckOnNetwork(t *testing.T) {
	shortNetTimers(t)
	h, inst, got := netTestHome(t, "neterr")
	runUntil(t, h, 10*time.Second, func() bool { return inst.Status == session.Ready && inst.InputReady() && inst.NetworkIssue })
	h.net.set(true, time.Now().Add(-10*time.Minute))
	tick(h)
	runUntil(t, h, 10*time.Second, func() bool { return strings.Count(readLog(got), "[squad] Сеть") == 1 })
	h.mu.Lock()
	h.guardAt = time.Time{}
	h.mu.Unlock()
	tick(h)
	time.Sleep(500 * time.Millisecond)
	require.Equal(t, 1, strings.Count(readLog(got), "[squad] Сеть"), "no second ping within guardPingEvery")
	h.mu.Lock()
	rec := h.guard[inst]
	require.NotNil(t, rec)
	require.True(t, rec.restarted.IsZero(), "first ping is a message, not a restart")
	h.mu.Unlock()
}

// While the VPN is down, or the session sleeps, the guard does nothing.
func TestGuardLeavesAloneWhenVPNDown(t *testing.T) {
	shortNetTimers(t)
	h, inst, got := netTestHome(t, "neterr")
	runUntil(t, h, 10*time.Second, func() bool { return inst.Status == session.Ready && inst.InputReady() && inst.NetworkIssue })
	h.mu.Lock()
	h.net.set(false, time.Now().Add(-10*time.Minute))
	h.guardNetStuck(time.Now())
	h.mu.Unlock()
	time.Sleep(300 * time.Millisecond)
	require.Empty(t, readLog(got))
}

func TestStaleWaitNote(t *testing.T) {
	h, inst, _ := netTestHome(t, "neterr")
	runUntil(t, h, 10*time.Second, func() bool { return inst.Status == session.Ready && inst.InputReady() })
	h.mu.Lock()
	defer h.mu.Unlock()
	old := session.ConductorActive
	session.ConductorActive = true
	defer func() { session.ConductorActive = old }()
	inst.NetworkIssue = false
	inst.IdleSince = time.Now().Add(-25 * time.Minute)
	inst.IdleTurn = session.ConductorTurns
	if label, _ := inst.StatusLabel(); label != "ждёт дирижёра" {
		t.Skipf("fixture is %q, not waiting for the conductor", label)
	}
	note := h.staleWaitNote(time.Now())
	require.Contains(t, note, "[сторож] ждут дирижёра дольше 20 мин")
	require.Contains(t, note, "(25 мин)")
	h.lastStaleNoteAt = time.Now()
	require.Empty(t, h.staleWaitNote(time.Now()), "at most every guardStaleEvery")
	inst.IdleSince = time.Now().Add(-5 * time.Minute)
	h.lastStaleNoteAt = time.Time{}
	require.Empty(t, h.staleWaitNote(time.Now()), "a short wait is not stale")
}
