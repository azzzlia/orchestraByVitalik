package app

import (
	"claude-squad/config"
	"claude-squad/session"
	"claude-squad/session/tmux"
	"claude-squad/ui"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

// fakeClaude imitates Claude Code's screen. Mode "mcp": stuck on an MCP call
// with a spinner until Esc. Mode "neterr": idle with a connection error.
// With --resume (or after the first phase) it shows an input box and logs every
// line it receives.
const fakeClaude = `#!/bin/bash
D="$(cd "$(dirname "$0")" && pwd)"
box() { printf '\n────────────────\n❯ \n────────────────\n'; }
listen() { while IFS= read -r line; do echo "$1: $line" >> "$D/got.log"; printf '\n❯ %s\n\n● ок\n' "$line"; box; done; }
if [[ "$*" == *--resume* ]]; then printf 'resumed\n'; box; listen resumed; fi
mode="$(cat "$D/mode")"
if [ "$mode" = neterr ]; then
  printf '❯ проверь задачу\n\n● Bash(curl https://git.example.com)\n  ⎿ curl: (6) Could not resolve host: git.example.com\n'; box; listen first
fi
tool='gitlab - get_merge_request (MCP)(id: 5)'
[ "$mode" = bash ] && tool='Bash(npm test)'
stty raw -echo
i=0
while true; do
  printf '\033[H\033[2J❯ проверь задачу\r\n\r\n⏺ %s\r\n\r\n✻ Pondering… (%ds · esc to interrupt)\r\n────────────────\r\n❯ \r\n────────────────\r\n' "$tool" $i
  # Whole seconds: macOS bash 3.2 rejects fractional read timeouts (and never reads).
  if IFS= read -r -t 1 -n 1 c && [ "$c" = $'\e' ]; then break; fi
  i=$((i+1))
done
stty sane
printf '\033[H\033[2J❯ проверь задачу\n\n⏺ gitlab - get_merge_request (MCP)(id: 5)\n  ⎿ Interrupted · What should Claude do instead?\n'; box; listen first
`

func netTestHome(t *testing.T, mode string) (*home, *session.Instance, string) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	bin := filepath.Join(t.TempDir(), "claude")
	require.NoError(t, os.WriteFile(bin, []byte(fakeClaude), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(bin), "mode"), []byte(mode), 0644))

	state := config.LoadState()
	storage, err := session.NewStorage(state)
	require.NoError(t, err)
	h := &home{ctx: context.Background(), appConfig: config.DefaultConfig(), storage: storage, appState: state, state: stateDefault}
	h.appConfig.ConductorIntervalMinutes = -1
	h.tabbedWindow = ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane())
	h.menu = ui.NewMenu()
	sp := spinner.New()
	h.list = ui.NewList(&sp, false)

	title := "vpn-" + mode
	inst, err := session.NewInstanceInFolder(session.SessionRequest{Title: title, Dir: t.TempDir()}, bin)
	require.NoError(t, err)
	h.list.AddInstance(inst)()
	require.NoError(t, inst.Start(true))
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", "="+tmux.TmuxNameFor(title)).Run() })
	// The notification hook would record the conversation id; restart needs it.
	dir := filepath.Join(homeDir, ".claude-squad", "claude-sessions")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, tmux.TmuxNameFor(title)), []byte("0123abcd-1111\n"), 0600))
	return h, inst, filepath.Join(filepath.Dir(bin), "got.log")
}

func tick(h *home) {
	results := collectMetadata(h.snapshotActiveInstances(), nil)
	h.mu.Lock()
	h.processMetadata(results)
	h.mu.Unlock()
}

func runUntil(t *testing.T, h *home, d time.Duration, cond func() bool) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		tick(h)
		if cond() {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	var b strings.Builder
	for _, inst := range h.list.GetInstances() {
		out, _ := exec.Command("tmux", "capture-pane", "-p", "-t", "="+tmux.TmuxNameFor(inst.Title)+":").Output()
		n, mcp := inst.TurnNetworkError()
		b.WriteString(fmt.Sprintf("%s status=%v input=%v net=%v mcp=%v waits=%v draft=%q\n%s\n", inst.Title, inst.Status, inst.InputReady(), n, mcp, inst.WaitsOnNetwork(), inst.Draft, out))
	}
	t.Fatalf("condition not reached in %s\n%s", d, b.String())
}

func shortNetTimers(t *testing.T) {
	g, s, e := netGrace, netStuckAfter, netEscWait
	netGrace, netStuckAfter, netEscWait = time.Second, 2*time.Second, 15*time.Second
	t.Cleanup(func() { netGrace, netStuckAfter, netEscWait = g, s, e })
}

func readLog(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

// A session hanging on an MCP call through the outage is interrupted, restarted
// with the same conversation and asked to continue.
func TestVPNRecoveryRestartsSessionStuckOnMCP(t *testing.T) {
	shortNetTimers(t)
	h, inst, got := netTestHome(t, "mcp")
	runUntil(t, h, 10*time.Second, func() bool { return inst.Status == session.Running && inst.WaitsOnNetwork() })

	h.net.set(false, time.Now())
	for k := 0; k < 3; k++ {
		tick(h)
		time.Sleep(200 * time.Millisecond)
	}
	require.True(t, h.netDown)
	require.Contains(t, h.netRecover, inst)

	h.net.set(true, time.Now())
	runUntil(t, h, 40*time.Second, func() bool { return strings.Contains(readLog(got), "resumed: [squad] Сеть/VPN") })
	require.NotContains(t, readLog(got), "first:", "the hung step must not get the message typed into it")
	require.Empty(t, h.netRecover)
}

// An idle session that stopped on a network error gets a nudge, no restart.
func TestVPNRecoveryNudgesSessionStoppedOnNetworkError(t *testing.T) {
	shortNetTimers(t)
	h, inst, got := netTestHome(t, "neterr")
	runUntil(t, h, 10*time.Second, func() bool {
		n, mcp := inst.TurnNetworkError()
		return inst.Status == session.Ready && inst.InputReady() && n && !mcp
	})
	h.net.set(false, time.Now())
	tick(h)
	h.net.set(true, time.Now())
	runUntil(t, h, 20*time.Second, func() bool { return strings.Contains(readLog(got), "first: [squad] Сеть/VPN") })
	require.NotContains(t, readLog(got), "resumed")
	require.Empty(t, h.netRecover)
}

// While the VPN is down, sessions are left alone: nothing is typed anywhere.
func TestVPNDownLeavesSessionsAlone(t *testing.T) {
	shortNetTimers(t)
	h, inst, got := netTestHome(t, "neterr")
	runUntil(t, h, 10*time.Second, func() bool { return inst.InputReady() })
	h.net.set(false, time.Now())
	for k := 0; k < 10; k++ {
		tick(h)
		time.Sleep(300 * time.Millisecond)
	}
	require.Empty(t, readLog(got))
}

// Long local work (tests) that does not need the network is never interrupted.
func TestVPNRecoveryLeavesLocalWorkAlone(t *testing.T) {
	shortNetTimers(t)
	h, inst, got := netTestHome(t, "bash")
	runUntil(t, h, 10*time.Second, func() bool { return inst.Status == session.Running })
	h.net.set(false, time.Now())
	tick(h)
	h.net.set(true, time.Now())
	for k := 0; k < 25; k++ { // well past netStuckAfter
		tick(h)
		time.Sleep(300 * time.Millisecond)
	}
	require.Empty(t, readLog(got))
	require.Contains(t, h.netRecover, inst, "still watched until it ends")
}

// A notification click (cs ctl focus) selects the session and presses Enter in Squad.
func TestFocusRequestOpensSession(t *testing.T) {
	h, inst, _ := netTestHome(t, "neterr")
	runUntil(t, h, 10*time.Second, func() bool { return inst.InputReady() })
	_, err := session.SubmitSessionRequest(session.SessionRequest{Title: inst.Title, Focus: true, At: time.Now()})
	require.NoError(t, err)
	tick(h)
	require.Equal(t, inst, h.focusTarget)
	cmd := h.takeFocus()
	require.NotNil(t, cmd)
	require.Equal(t, tea.KeyMsg{Type: tea.KeyEnter}, cmd())
	require.Equal(t, inst, h.list.GetSelectedInstance())
	require.Nil(t, h.focusTarget)

	// A click from long ago is ignored.
	_, err = session.SubmitSessionRequest(session.SessionRequest{Title: inst.Title, Focus: true, At: time.Now().Add(-time.Hour)})
	require.NoError(t, err)
	tick(h)
	require.Nil(t, h.focusTarget)
}

// Status changes land in the event log and show up in "что было".
func TestStatusHistoryAndReport(t *testing.T) {
	h, inst, _ := netTestHome(t, "neterr")
	runUntil(t, h, 10*time.Second, func() bool { return inst.InputReady() })
	h.statusLog = nil
	h.recordStatuses()
	h.statusLog[inst].since = time.Now().Add(-time.Minute) // pretend it has been stable
	h.recordStatuses()
	evs := session.ReadEvents(time.Now().Add(-time.Hour))
	require.NotEmpty(t, evs)
	require.Equal(t, inst.Title, evs[len(evs)-1].Title)
	session.AppendEvent(session.Event{Kind: "vpn_down", At: time.Now().Add(-30 * time.Minute)})
	session.AppendEvent(session.Event{Kind: "vpn_up", At: time.Now().Add(-20 * time.Minute)})
	report := h.buildReport(time.Now().Add(-time.Hour))
	require.Contains(t, report, "VPN отваливался 1 раз")
	require.Contains(t, report, "#1 vpn-neterr")
}

// Restart requests run even while a window is open in Squad; a request for the
// session the user is inside waits in the queue instead of being lost.
func TestControlRequestsRunWithWindowOpen(t *testing.T) {
	h, inst, got := netTestHome(t, "neterr")
	runUntil(t, h, 10*time.Second, func() bool { return inst.InputReady() })

	h.attached = inst
	_, err := session.SubmitSessionRequest(session.SessionRequest{Title: inst.Title, Restart: true})
	require.NoError(t, err)
	tick(h)
	_, done := session.ReadCtlResult(inst.Title, time.Now().Add(-time.Minute))
	require.False(t, done, "must wait while the user is inside the session")

	h.attached = nil
	h.state = stateHelp // a window is open in Squad
	start := time.Now()
	runUntil(t, h, 10*time.Second, func() bool {
		res, ok := session.ReadCtlResult(inst.Title, start)
		return ok && res == "ок"
	})
	runUntil(t, h, 15*time.Second, func() bool { return strings.Contains(readLog(got), "") && inst.InputReady() })
	require.Empty(t, session.TakeSessionRequests(), "the queue is empty")
}
