package session

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDensityEstimateCountsParallelBranches(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := filepath.Join(home, ".claude", "projects", "-Users-x-repo")
	require.NoError(t, os.MkdirAll(filepath.Join(proj, "s1", "subagents"), 0700))
	now := time.Now()
	base := time.Date(now.Year(), now.Month(), now.Day(), 0, 30, 0, 0, now.Location())
	if now.Before(base.Add(2 * time.Hour)) {
		t.Skip("needs two hours of today behind us")
	}
	write := func(path string, from, to time.Duration) {
		var s string
		for d := from; d <= to; d += time.Minute {
			s += fmt.Sprintf(`{"type":"assistant","timestamp":"%s","message":{"content":"секрет"}}`+"\n", base.Add(d).UTC().Format(time.RFC3339Nano))
		}
		require.NoError(t, os.WriteFile(path, []byte(s), 0600))
	}
	// main session works 0..60 min; a second session 0..60; a subagent 0..30.
	write(filepath.Join(proj, "s1.jsonl"), 0, 60*time.Minute)
	write(filepath.Join(proj, "s2.jsonl"), 0, 60*time.Minute)
	write(filepath.Join(proj, "s1", "subagents", "agent-a.jsonl"), 0, 30*time.Minute)
	tr := NewDensityTracker()
	tr.Refresh(now)
	est := tr.Estimate()
	require.True(t, est.HasActivity)
	require.Equal(t, 3, est.Branches)
	require.InDelta(t, 1.0, est.AIHours, 0.01)
	require.InDelta(t, 2.5, est.AgentHours, 0.01)
	require.InDelta(t, 2.5, est.Value, 0.01)

	// A gap longer than 10 minutes splits a branch: idle time does not count.
	f, _ := os.OpenFile(filepath.Join(proj, "s2.jsonl"), os.O_APPEND|os.O_WRONLY, 0600)
	fmt.Fprintf(f, `{"timestamp":"%s"}`+"\n", base.Add(90*time.Minute).UTC().Format(time.RFC3339Nano))
	fmt.Fprintf(f, `{"timestamp":"%s"}`+"\n", base.Add(100*time.Minute).UTC().Format(time.RFC3339Nano))
	f.Close()
	tr.Refresh(now)
	est = tr.Estimate()
	require.InDelta(t, 1.0+10.0/60, est.AIHours, 0.01)
	require.InDelta(t, 2.5+10.0/60, est.AgentHours, 0.01)
}
