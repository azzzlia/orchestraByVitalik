package tmux

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A session started again (woken) gets the size sessions are shown at, not tmux's 80x24.
func TestRestartedSessionGetsRememberedSize(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	defer func() { lastW, lastH = 0, 0 }() // other tests expect no remembered size
	dir := t.TempDir()
	s := NewTmuxSession("sizetest-a", "cat")
	require.NoError(t, s.StartWithCommand(dir, "cat"))
	defer s.Close()
	RememberSize(200, 50)
	require.NoError(t, s.SetDetachedSize(200, 50))

	w := NewTmuxSession("sizetest-b", "cat") // as after waking: a new session
	require.NoError(t, w.StartWithCommand(dir, "cat"))
	defer w.Close()
	time.Sleep(300 * time.Millisecond)
	out, err := exec.Command("tmux", "display-message", "-p", "-t", "="+w.sanitizedName+":", "#{window_width}").Output()
	require.NoError(t, err)
	require.Equal(t, "200", strings.TrimSpace(string(out)))
}

func TestStatusLineIsNotWork(t *testing.T) {
	a := "⏺ Bash(php x.php)\n\nDo you want to proceed?\n❯ 1. Yes\n────────────────\n❯ \n────────────────\n  (•_•) «Ну-у» плт 3,0\n"
	b := "⏺ Bash(php x.php)\n\nDo you want to proceed?\n❯ 1. Yes\n────────────────\n❯ \n────────────────\n  (^ᴗ^) «Ура» плт 3,1\n"
	require.Equal(t, withoutStatusLine(a), withoutStatusLine(b))
	require.NotEqual(t, withoutStatusLine(a), withoutStatusLine(strings.Replace(b, "php x.php", "php y.php", 1)))
}
