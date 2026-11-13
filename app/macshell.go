package app

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"

	"claude-squad/log"
	"claude-squad/session"
)

// «Терминал Мака»: a plain login shell in its own tmux session, not an agent — the
// conductor and the lists never see it. The page streams its screen like a session's
// and types into it, so the Mac can be driven from the phone (over Tailscale, with the
// web key). "web_shell": "off" in config.json turns it off.

const macShellName = "squad_macshell"

func (m *home) macShellOn() bool { return m.appConfig.WebShell != "off" }

// ensureMacShell starts the shell session if it is not running; cols/rows size it.
func ensureMacShell(cols, rows int) error {
	if exec.Command("tmux", "has-session", "-t", "="+macShellName).Run() == nil {
		if cols >= 20 && rows >= 5 {
			_ = exec.Command("tmux", "resize-window", "-t", "="+macShellName+":", "-x", strconv.Itoa(cols), "-y", strconv.Itoa(rows)).Run()
		}
		return nil
	}
	if cols < 20 || rows < 5 {
		cols, rows = 100, 32
	}
	home, _ := os.UserHomeDir()
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	args := []string{"new-session", "-d", "-s", macShellName, "-x", strconv.Itoa(cols), "-y", strconv.Itoa(rows), "-c", home, shell, "-l"}
	if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("tmux: %v: %s", err, out)
	}
	_ = exec.Command("tmux", "set-option", "-t", "="+macShellName, "history-limit", "20000").Run()
	_ = exec.Command("tmux", "set-option", "-t", "="+macShellName, "window-size", "manual").Run()
	return nil
}

func clientHost(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func (m *home) webShellStream(w http.ResponseWriter, r *http.Request) {
	if !m.macShellOn() {
		http.Error(w, "терминал Мака выключен (\"web_shell\": \"off\")", http.StatusForbidden)
		return
	}
	if err := ensureMacShell(formInt(r, "cols"), formInt(r, "rows")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	session.AppendEvent(session.Event{Kind: "status", Text: "терминал Мака открыт из веба (" + clientHost(r) + ")"})
	streamPane(w, r, macShellName)
}

func (m *home) webShellKeys(w http.ResponseWriter, r *http.Request) {
	if !m.macShellOn() {
		http.Error(w, "терминал Мака выключен", http.StatusForbidden)
		return
	}
	data := r.FormValue("data")
	if data == "" || len(data) > 16384 {
		http.Error(w, "пусто или слишком длинно", http.StatusBadRequest)
		return
	}
	if err := ensureMacShell(0, 0); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := shellType(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	kickScreen(macShellName)
	writeJSON(w, map[string]bool{"ok": true})
}

// shellType sends bytes to the shell as they are (control keys, escape sequences, text).
func shellType(data string) error {
	b := []byte(data)
	for len(b) > 0 {
		n := len(b)
		if n > 1024 {
			n = 1024
		}
		args := []string{"send-keys", "-t", "=" + macShellName + ":", "-H"}
		for _, c := range b[:n] {
			args = append(args, hex.EncodeToString([]byte{c}))
		}
		if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("tmux: %v: %s", err, out)
		}
		b = b[n:]
	}
	return nil
}

// webShellAction: resize, restart (kill and start fresh).
func (m *home) webShellAction(w http.ResponseWriter, r *http.Request) {
	if !m.macShellOn() {
		http.Error(w, "терминал Мака выключен", http.StatusForbidden)
		return
	}
	switch r.FormValue("do") {
	case "resize":
		_ = ensureMacShell(formInt(r, "cols"), formInt(r, "rows"))
	case "restart":
		_ = exec.Command("tmux", "kill-session", "-t", "="+macShellName).Run()
		time.Sleep(100 * time.Millisecond)
		if err := ensureMacShell(formInt(r, "cols"), formInt(r, "rows")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.InfoLog.Printf("web: mac shell restarted from %s", clientHost(r))
	default:
		http.Error(w, "нет такого действия", http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
