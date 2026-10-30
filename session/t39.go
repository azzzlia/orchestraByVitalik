package session

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// The official score from t39, shown next to the local estimate. Squad does not
// know how to reach t39 itself: it runs the user's own command — t39_command, or
// the status-line command that was there before `cs statusline --install` — and
// takes the number after "плт"/"плотность" from its output.

type t39Cache struct {
	Value float64   `json:"value"`
	OK    bool      `json:"ok"`
	At    time.Time `json:"at"`
}

var t39NumberRe = regexp.MustCompile(`(?i)пл(?:отность|т)\D{0,12}?(\d+(?:[.,]\d+)?)`)

// ParseT39 takes the score from a command's output.
func ParseT39(out string) (float64, bool) {
	text := StripANSI(out)
	if m := t39NumberRe.FindStringSubmatch(text); m != nil {
		return ParseDensity(m[1])
	}
	return 0, false
}

// ownStatuslineRe: Squad's own status line ("cs statusline", ".../claude-squad statusline").
var ownStatuslineRe = regexp.MustCompile(`(^|[/\s])(cs|claude-squad)\s+statusline`)

// t39SourceCommand is t39_command, else the previous status-line command.
func t39SourceCommand(configured string) string {
	if configured != "" {
		return configured
	}
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".claude-squad", "statusline-backup.json"))
	if err != nil {
		return ""
	}
	var b struct {
		StatusLine struct {
			Command string `json:"command"`
		} `json:"statusLine"`
	}
	// Our own status line must never be run from here: it would call itself.
	if json.Unmarshal(data, &b) != nil || ownStatuslineRe.MatchString(b.StatusLine.Command) {
		return ""
	}
	return b.StatusLine.Command
}

// CachedT39 reads only the t39 cache (Squad refreshes it every 10 minutes); for the
// status line, which must not run the t39 command on every redraw.
func CachedT39() (float64, bool) {
	path, err := configPath("t39-cache.json")
	if err != nil {
		return 0, false
	}
	var c t39Cache
	if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &c) == nil && time.Since(c.At) < time.Hour {
		return c.Value, c.OK
	}
	return 0, false
}

// T39Density returns the official score, refreshed at most every 10 minutes.
func T39Density(configured string) (float64, bool) {
	path, err := configPath("t39-cache.json")
	if err != nil {
		return 0, false
	}
	var c t39Cache
	if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &c) == nil && time.Since(c.At) < 10*time.Minute {
		return c.Value, c.OK
	}
	cmdline := t39SourceCommand(configured)
	if cmdline == "" {
		return 0, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", cmdline)
	cmd.Stdin = strings.NewReader("{}")
	out, _ := cmd.Output()
	c = t39Cache{At: time.Now()}
	c.Value, c.OK = ParseT39(string(out))
	if !c.OK && len(out) == 0 {
		c.At = time.Now().Add(-8 * time.Minute) // failed to run: try again in ~2 minutes
	}
	if data, err := json.Marshal(c); err == nil {
		writeAtomic(path, data)
	}
	return c.Value, c.OK
}
