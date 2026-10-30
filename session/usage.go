package session

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Token usage from Claude Code's own logs (~/.claude/projects/*/<session>.jsonl).
// Counting costs no tokens. Usage is weighted roughly by price (output and
// cache writes cost more, cache reads much less; Opus more than Sonnet), which is
// close to how plan limits are consumed. Percentages need a budget calibrated
// against what /usage shows (cs usage --calibrate).

type usageEvent struct {
	at    time.Time
	units float64
}

type usageFile struct {
	offset int64
	events []usageEvent
	seen   map[string]bool // message ids already counted (one message spans many lines)
}

// UsageTracker reads Claude Code logs incrementally.
type UsageTracker struct {
	mu    sync.Mutex
	dir   string
	files map[string]*usageFile // by conversation id
	keep  time.Duration
}

// NewUsageTracker tracks ~/.claude/projects.
func NewUsageTracker() *UsageTracker {
	home, _ := os.UserHomeDir()
	return &UsageTracker{dir: filepath.Join(home, ".claude", "projects"), files: map[string]*usageFile{}, keep: 8 * 24 * time.Hour}
}

type usageLine struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			Input       float64 `json:"input_tokens"`
			Output      float64 `json:"output_tokens"`
			CacheCreate float64 `json:"cache_creation_input_tokens"`
			CacheRead   float64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func modelWeight(model string) float64 {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "opus"):
		return 5.0 / 3.0
	case strings.Contains(m, "haiku"):
		return 1.0 / 3.0
	default:
		return 1
	}
}

// weightedUnits converts one response's usage into Sonnet-input-token equivalents.
func weightedUnits(l *usageLine) float64 {
	u := l.Message.Usage
	return (u.Input + 1.25*u.CacheCreate + 0.1*u.CacheRead + 5*u.Output) * modelWeight(l.Message.Model)
}

// Refresh reads what was appended to the logs since the last call.
func (t *UsageTracker) Refresh() {
	t.mu.Lock()
	defer t.mu.Unlock()
	cutoff := time.Now().Add(-t.keep)
	paths, _ := filepath.Glob(filepath.Join(t.dir, "*", "*.jsonl"))
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || info.ModTime().Before(cutoff) {
			continue
		}
		id := strings.TrimSuffix(filepath.Base(p), ".jsonl")
		f := t.files[id]
		if f == nil {
			f = &usageFile{seen: map[string]bool{}}
			t.files[id] = f
		}
		if info.Size() < f.offset { // rewritten
			f.offset, f.events, f.seen = 0, nil, map[string]bool{}
		}
		if info.Size() == f.offset {
			continue
		}
		t.readFrom(p, f, cutoff)
	}
	for id, f := range t.files {
		k := 0
		for _, e := range f.events {
			if e.at.After(cutoff) {
				f.events[k] = e
				k++
			}
		}
		f.events = f.events[:k]
		if len(f.events) == 0 && f.offset == 0 {
			delete(t.files, id)
		}
	}
}

func (t *UsageTracker) readFrom(path string, f *usageFile, cutoff time.Time) {
	fh, err := os.Open(path)
	if err != nil {
		return
	}
	defer fh.Close()
	if _, err := fh.Seek(f.offset, io.SeekStart); err != nil {
		return
	}
	r := bufio.NewReaderSize(fh, 256*1024)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			break // a partial last line is read again next time
		}
		f.offset += int64(len(line))
		if !strings.Contains(string(line), `"usage"`) {
			continue
		}
		var l usageLine
		if json.Unmarshal(line, &l) != nil || l.Type != "assistant" || l.Message.Usage == nil || l.Timestamp.Before(cutoff) {
			continue
		}
		if l.Message.ID != "" {
			if f.seen[l.Message.ID] {
				continue
			}
			f.seen[l.Message.ID] = true
		}
		f.events = append(f.events, usageEvent{at: l.Timestamp, units: weightedUnits(&l)})
	}
}

// Since sums usage after since: per conversation id, and in total.
func (t *UsageTracker) Since(since time.Time) (byConv map[string]float64, total float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	byConv = map[string]float64{}
	for id, f := range t.files {
		for _, e := range f.events {
			if e.at.After(since) {
				byConv[id] += e.units
				total += e.units
			}
		}
	}
	return byConv, total
}

// UsageWindows are the windows plan limits are counted in.
const (
	Window5h   = 5 * time.Hour
	WindowWeek = 7 * 24 * time.Hour
)

// FormatUnits prints usage compactly (1.2M, 340k).
func FormatUnits(u float64) string {
	switch {
	case u >= 1e6:
		return strings.TrimSuffix(strconv.FormatFloat(u/1e6, 'f', 1, 64), ".0") + "M"
	case u >= 1e3:
		return strconv.FormatFloat(u/1e3, 'f', 0, 64) + "k"
	default:
		return strconv.FormatFloat(u, 'f', 0, 64)
	}
}
