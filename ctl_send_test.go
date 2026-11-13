package main

import (
	"claude-squad/session"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The same fake Claude Code as in session/send_test.go: an unfocused session draws
// typed text dim, the first N Enters are swallowed, sent messages go to a log.
const fakeAgentCtl = `import os, sys, tty
swallow = int(sys.argv[1]); buf = sys.argv[2]; log = sys.argv[3]
fd = sys.stdin.fileno(); tty.setraw(fd)
def draw():
    sys.stdout.write("\x1b[2J\x1b[H\r\n" + "─" * 60 + "\r\n❯ \x1b[2m" + buf + "\x1b[0m\r\n" + "─" * 60 + "\r\n"); sys.stdout.flush()
draw()
while True:
    ch = os.read(fd, 1)
    if not ch: break
    if ch == b"\r":
        if swallow > 0: swallow -= 1
        elif buf:
            open(log, "a").write(buf + "\n"); buf = ""
    elif ch == b"\x7f": buf = buf[:-1]
    elif ch == b"\x05": pass
    else:
        b = ch
        while True:
            try:
                buf += b.decode(); break
            except UnicodeDecodeError:
                b += os.read(fd, 1)
    draw()
`

func ctlFake(t *testing.T, swallow, preset string) (log string) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("нет tmux")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	script := filepath.Join(home, "agent.py")
	log = filepath.Join(home, "sent.log")
	if err := os.WriteFile(script, []byte(fakeAgentCtl), 0o644); err != nil {
		t.Fatal(err)
	}
	name := "ctlsendtest_" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
	_ = exec.Command("tmux", "kill-session", "-t", "="+name).Run()
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-x", "120", "-y", "30", "python3", script, swallow, preset, log).CombinedOutput(); err != nil {
		t.Fatalf("tmux: %v %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", "="+name).Run() })
	snap := session.StatusSnapshot{UpdatedAt: time.Now(), Sessions: []session.StatusEntry{{Number: 7, Title: "Тест", TmuxName: name}}}
	b, _ := json.Marshal(snap)
	_ = os.MkdirAll(filepath.Join(home, ".claude-squad"), 0o700)
	if err := os.WriteFile(filepath.Join(home, ".claude-squad", "status.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)
	return log
}

func ctlSend(force bool, text string) error {
	ctlForce, ctlWait = force, 15
	defer func() { ctlForce = false }()
	return ctlSendCmd.RunE(ctlSendCmd, []string{"7", text})
}

func logText(p string) string { b, _ := os.ReadFile(p); return strings.TrimSpace(string(b)) }

// Enter swallowed once (the agent was still starting) — sent on a retry.
func TestCtlSendRetriesSwallowedEnter(t *testing.T) {
	log := ctlFake(t, "1", "")
	if err := ctlSend(false, "прогони тесты и покажи итог"); err != nil {
		t.Fatal(err)
	}
	if logText(log) != "прогони тесты и покажи итог" {
		t.Fatalf("ушло: %q", logText(log))
	}
}

// Bug 10.11: a dim leftover blocked the message even with --force.
func TestCtlSendForceErasesDimLeftover(t *testing.T) {
	log := ctlFake(t, "0", "висящий старый текст")
	err := ctlSend(false, "срочное задание")
	if err == nil || !strings.Contains(err.Error(), "НЕ отправлено") || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("без --force ждали честный отказ с подсказкой, получили: %v", err)
	}
	if logText(log) != "" {
		t.Fatalf("без --force что-то ушло: %q", logText(log))
	}
	if err := ctlSend(true, "срочное задание"); err != nil {
		t.Fatalf("--force: %v", err)
	}
	if logText(log) != "срочное задание" {
		t.Fatalf("ушло %q, ждали только новое", logText(log))
	}
}

// Our own earlier, undelivered message left in the box is erased without --force.
func TestCtlSendErasesOwnLeftover(t *testing.T) {
	log := ctlFake(t, "0", "старое задание от дирижёра")
	rememberSent("ctlsendtest_testctlsenderasesownleftover", "старое задание от дирижёра")
	if err := ctlSend(false, "новое задание"); err != nil {
		t.Fatal(err)
	}
	if logText(log) != "новое задание" {
		t.Fatalf("ушло %q", logText(log))
	}
}
