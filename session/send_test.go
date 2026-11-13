package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-squad/session/tmux"
)

// fakeAgent imitates Claude Code in an unfocused terminal: the input box under a
// "───" rule, typed text drawn dim (like a greyed-out suggestion), the first N
// Enters swallowed (still starting), sent messages listed above the box.
const fakeAgent = `import os, sys, tty, termios
swallow = int(sys.argv[1]); preset = sys.argv[2] if len(sys.argv) > 2 else ""
log = sys.argv[3] if len(sys.argv) > 3 else ""
fd = sys.stdin.fileno(); tty.setraw(fd)
buf = preset; sent = []
def draw():
    out = "\x1b[2J\x1b[H"
    for m in sent[-5:]:
        out += "> " + m + "\r\n"
    out += "\r\n" + "─" * 60 + "\r\n"
    out += "❯ \x1b[2m" + buf.replace("\n", " ") + "\x1b[0m\r\n" + "─" * 60 + "\r\n"
    sys.stdout.write(out); sys.stdout.flush()
draw()
while True:
    ch = os.read(fd, 1)
    if not ch: break
    if ch == b"\r":
        if swallow > 0:
            swallow -= 1
        elif buf:
            sent.append(buf)
            if log:
                open(log, "a").write(buf.replace("\n", " ") + "\n")
            buf = ""
    elif ch == b"\x7f":
        buf = buf[:-1]
    elif ch == b"\x05":
        pass
    else:
        b = ch
        while True:
            try:
                buf += b.decode(); break
            except UnicodeDecodeError:
                b += os.read(fd, 1)
    draw()
`

func startFake(t *testing.T, swallow int, preset string) (*Instance, string) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("нет tmux")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("нет python3")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "agent.py")
	if err := os.WriteFile(script, []byte(fakeAgent), 0o644); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "sent.log")
	name := "sendtest" + strings.ReplaceAll(t.Name(), "/", "_")
	ts := tmux.NewTmuxSession(name, "python3 "+script+" "+strconv.Itoa(swallow)+" '"+preset+"' "+log)
	if err := ts.Start(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ts.Close() })
	time.Sleep(500 * time.Millisecond)
	return &Instance{Title: name, started: true, tmuxSession: ts}, log
}

func sentLog(t *testing.T, p string) string {
	b, _ := os.ReadFile(p)
	return strings.TrimSpace(string(b))
}

// Bug 09.11: the first task stayed in the box — typed text is drawn dim in an
// unfocused session, so Squad did not see it, took it for sent and never retried Enter.
func TestSendPromptDimTextSwallowedEnter(t *testing.T) {
	inst, log := startFake(t, 1, "")
	msg := "Проверь задачу 782059 и покажи план до аренды стенда\n\nНе используй меню выбора"
	if err := inst.SendPrompt(msg); err != nil {
		t.Fatalf("SendPrompt: %v", err)
	}
	if got := sentLog(t, log); !strings.Contains(got, "Проверь задачу 782059") || !strings.Contains(got, "меню выбора") {
		t.Fatalf("агент не получил сообщение: %q", got)
	}
}

// Enter never works: an honest error, not «sent».
func TestSendPromptEnterAlwaysLost(t *testing.T) {
	inst, log := startFake(t, 9, "")
	if err := inst.SendPrompt("короткое задание"); err == nil {
		t.Fatal("ждали ошибку: Enter не сработал")
	}
	if sentLog(t, log) != "" {
		t.Fatal("сообщение ушло, хотя Enter глотался")
	}
}

// Bug 10.11: a leftover text (dim, unfocused) blocked the message; ReplaceInput
// erases it and sends the new one alone.
func TestReplaceInputErasesDimLeftover(t *testing.T) {
	inst, log := startFake(t, 0, "старый неотправленный текст")
	if err := inst.ReplaceInput("новое сообщение"); err != nil {
		t.Fatalf("ReplaceInput: %v", err)
	}
	if got := sentLog(t, log); got != "новое сообщение" {
		t.Fatalf("ушло %q, ждали только новое сообщение", got)
	}
}

func TestBoxHas(t *testing.T) {
	long := strings.Repeat("а", 50) + " середина " + strings.Repeat("б", 50)
	if !BoxHas(strings.Repeat("б", 50), long) { // the head scrolled out of sight
		t.Fatal("хвост длинного текста не узнан")
	}
	if BoxHas("", long) || BoxHas("Try \"fix lint\"", "fix the bug") {
		t.Fatal("ложное совпадение")
	}
}
