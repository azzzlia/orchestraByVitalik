package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUsageTrackerCountsEachMessageOnceAndReadsIncrementally(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "projects", "-Users-x-repo")
	require.NoError(t, os.MkdirAll(dir, 0700))
	now := time.Now().UTC().Format(time.RFC3339)
	old := time.Now().Add(-6 * time.Hour).UTC().Format(time.RFC3339)
	line := func(id, ts, model string, in, out int) string {
		return `{"type":"assistant","timestamp":"` + ts + `","message":{"id":"` + id + `","model":"` + model +
			`","usage":{"input_tokens":` + itoa(in) + `,"output_tokens":` + itoa(out) + `,"cache_read_input_tokens":1000}}}` + "\n"
	}
	path := filepath.Join(dir, "conv-1.jsonl")
	// msg a is written twice (two content blocks), old msg c is outside 5h.
	content := line("a", now, "claude-sonnet-5-5", 100, 10) + line("a", now, "claude-sonnet-5-5", 100, 10) +
		`{"type":"user","message":{"content":"hi"}}` + "\n" + line("c", old, "claude-sonnet-5-5", 100, 0)
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))

	tr := NewUsageTracker()
	tr.Refresh()
	by, total := tr.Since(time.Now().Add(-Window5h))
	require.InDelta(t, 100+50+100, total, 0.01) // in + 5*out + 0.1*cache_read
	require.InDelta(t, 250, by["conv-1"], 0.01)

	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	_, _ = f.WriteString(line("b", now, "claude-opus-5-5", 300, 0))
	f.Close()
	tr.Refresh()
	_, total = tr.Since(time.Now().Add(-Window5h))
	require.InDelta(t, 250+(300+100)*5.0/3.0, total, 0.01)
	_, week := tr.Since(time.Now().Add(-WindowWeek))
	require.Greater(t, week, total)
}

func itoa(n int) string { return FormatUnits(float64(n)) }
