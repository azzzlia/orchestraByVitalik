package app

import (
	"claude-squad/session"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNotifyConductorWaiting(t *testing.T) {
	var got []string
	defer func(f func(string, string)) { notifyUser = f }(notifyUser)
	notifyUser = func(title, msg string) { got = append(got, title+" | "+msg) }

	m := &home{}
	c := &session.Instance{Title: "Дирижёр", Conductor: true, Number: 1, Status: session.Ready}
	t0 := time.Now()

	c.Interrupted = true // no hook for this — announced at once
	m.notifyConductorWaiting(c, t0)
	require.Len(t, got, 1)
	require.Contains(t, got[0], "Дирижёр ждёт тебя")
	m.notifyConductorWaiting(c, t0.Add(5*time.Minute))
	require.Len(t, got, 1, "the same wait — not again")
	m.notifyConductorWaiting(c, t0.Add(11*time.Minute))
	require.Len(t, got, 2)
	require.Contains(t, got[1], "всё ещё ждёт")
	m.notifyConductorWaiting(c, t0.Add(30*time.Minute))
	require.Len(t, got, 2, "one reminder only")

	c.Interrupted = false // a moment out of the wait (a digest typed in) …
	m.notifyConductorWaiting(c, t0.Add(31*time.Minute))
	c.Interrupted = true // … and back: the same wait, no new notification
	m.notifyConductorWaiting(c, t0.Add(31*time.Minute+20*time.Second))
	require.Len(t, got, 2)
	c.Interrupted = false // answered for good: a minute later the wait is over
	m.notifyConductorWaiting(c, t0.Add(33*time.Minute))
	m.notifyConductorWaiting(c, t0.Add(35*time.Minute))
	c.Interrupted = true
	m.notifyConductorWaiting(c, t0.Add(36*time.Minute))
	require.Len(t, got, 3, "a new wait after a real break is announced")
}
