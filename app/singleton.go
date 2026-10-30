package app

import (
	"claude-squad/config"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// claimSingleInstance makes sure only one Squad window runs: two of them would both
// send digests to the conductor and act on the same cs ctl requests. It records our
// pid in ~/.claude-squad/squad.pid and refuses to start while another live Squad
// holds it. The returned func releases the file on exit.
func claimSingleInstance() (func(), error) {
	dir, err := config.GetConfigDir()
	if err != nil {
		return func() {}, nil
	}
	path := filepath.Join(dir, "squad.pid")
	if data, err := os.ReadFile(path); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid != os.Getpid() && squadAlive(pid) {
			return nil, fmt.Errorf("Squad уже запущен в другом окне (pid %d) — открой то окно (cs ctl focus) или закрой его; вторая копия слала бы дирижёру двойные сводки", pid)
		}
	}
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0600)
	return func() {
		if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) == strconv.Itoa(os.Getpid()) {
			_ = os.Remove(path)
		}
	}, nil
}

// squadAlive: the process exists and is a cs/Squad binary (pids get reused).
func squadAlive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return false
	}
	name := filepath.Base(strings.TrimSpace(string(out)))
	return name == "cs" || strings.Contains(name, "claude-squad")
}
