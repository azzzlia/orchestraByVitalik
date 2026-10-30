package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestTmuxNameKeepsASCIITitles(t *testing.T) {
	if got := toClaudeSquadTmuxName("my session.v2"); got != "claudesquad_mysession_v2" {
		t.Fatalf("ascii title changed: %s", got)
	}
}

func TestTmuxNameSurvivesTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	for _, title := range []string{"Demo: сценарии 766710", "UserFactory::getColor", "Поиск по эмодзи", "Мерж фиксов 259648/259650"} {
		// A suffix of its own: on the user's machine a real session may have this very
		// title, and the test must never touch (or kill) it.
		name := toClaudeSquadTmuxName(fmt.Sprintf("%s squadtest%d", title, os.Getpid()))
		if err := exec.Command("tmux", "new-session", "-d", "-s", name, "sleep 5").Run(); err != nil {
			t.Fatalf("%s: %v", title, err)
		}
		out, err := exec.Command("tmux", "has-session", "-t", "="+name).CombinedOutput()
		_ = exec.Command("tmux", "kill-session", "-t", "="+name).Run()
		if err != nil {
			t.Fatalf("tmux lost session for %q (%s): %s", title, name, strings.TrimSpace(string(out)))
		}
	}
	if toClaudeSquadTmuxName("Поиск по эмодзи") == toClaudeSquadTmuxName("Поиск по чатам") {
		t.Fatal("different titles must give different names")
	}
}
