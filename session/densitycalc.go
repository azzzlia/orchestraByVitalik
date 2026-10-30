package session

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// Local estimate of the company's "плотность": how many agent branches (Claude
// sessions and their subagents) work at the same time, on average, while the
// user works with AI during the last 24 hours. sum(branch active time) / union(active time).
// Only the "timestamp" fields of Claude Code's logs are read, never the text.

// densityWindow: the score covers the last day (a rolling 24 hours).
const densityWindow = 24 * time.Hour

// densityGap: records closer than this belong to one stretch of work.
const densityGap = 10 * time.Minute

type densityInterval struct{ from, to time.Time }

type densityBranch struct {
	offset    int64
	last      time.Time
	intervals []densityInterval
}

// DensityTracker reads Claude Code logs incrementally.
type DensityTracker struct {
	mu     sync.Mutex
	roots  []string
	cutoff time.Time
	files  map[string]*densityBranch
}

// NewDensityTracker tracks ~/.claude/projects and ~/.claude-*/projects.
func NewDensityTracker() *DensityTracker {
	home, _ := os.UserHomeDir()
	roots := []string{filepath.Join(home, ".claude", "projects")}
	if extra, _ := filepath.Glob(filepath.Join(home, ".claude-*", "projects")); len(extra) > 0 {
		roots = append(roots, extra...)
	}
	return &DensityTracker{roots: roots, files: map[string]*densityBranch{}}
}

var timestampRe = regexp.MustCompile(`"timestamp"\s*:\s*"([^"]+)"`)

// Refresh reads what was appended since the last call (records of the last 24 hours).
func (t *DensityTracker) Refresh(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cutoff := now.Add(-densityWindow)
	t.cutoff = cutoff
	for _, root := range t.roots {
		var paths []string
		for _, pat := range []string{"*/*.jsonl", "*/*/subagents/*.jsonl", "*/*/subagents/*/*.jsonl"} {
			m, _ := filepath.Glob(filepath.Join(root, pat))
			paths = append(paths, m...)
		}
		for _, p := range paths {
			info, err := os.Stat(p)
			if err != nil || info.ModTime().Before(cutoff) {
				continue
			}
			b := t.files[p]
			if b == nil {
				b = &densityBranch{}
				t.files[p] = b
			}
			if info.Size() < b.offset {
				*b = densityBranch{}
			}
			if info.Size() > b.offset {
				b.read(p, cutoff)
			}
		}
	}
	// Forget stretches of work that fell out of the window.
	for p, b := range t.files {
		k := 0
		for _, iv := range b.intervals {
			if iv.to.After(cutoff) {
				b.intervals[k] = iv
				k++
			}
		}
		b.intervals = b.intervals[:k]
		if len(b.intervals) == 0 && b.last.Before(cutoff) {
			delete(t.files, p)
		}
	}
}

func (b *densityBranch) read(path string, cutoff time.Time) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(b.offset, io.SeekStart); err != nil {
		return
	}
	r := bufio.NewReaderSize(f, 256*1024)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return // a partial last line is read again next time
		}
		b.offset += int64(len(line))
		head := line
		if len(head) > 4096 {
			head = head[:4096] // the timestamp sits near the start of a record
		}
		m := timestampRe.FindSubmatch(head)
		if m == nil {
			if m = timestampRe.FindSubmatch(line); m == nil {
				continue
			}
		}
		ts, err := time.Parse(time.RFC3339Nano, string(m[1]))
		if err != nil || ts.Before(cutoff) {
			continue
		}
		b.add(ts)
	}
}

func (b *densityBranch) add(ts time.Time) {
	if n := len(b.intervals); n > 0 && !ts.Before(b.last) && ts.Sub(b.last) <= densityGap {
		b.intervals[n-1].to = ts
	} else if n == 0 || !ts.Before(b.last) {
		b.intervals = append(b.intervals, densityInterval{ts, ts})
	} // records out of order are ignored
	if ts.After(b.last) {
		b.last = ts
	}
}

// DensityEstimate is the last day's estimate and what it is made of.
type DensityEstimate struct {
	Value       float64
	AIHours     float64 // union of active time
	AgentHours  float64 // sum over branches
	Branches    int
	HasActivity bool
}

// Estimate computes the density of the last 24 hours from what Refresh has read.
func (t *DensityTracker) Estimate() DensityEstimate {
	t.mu.Lock()
	defer t.mu.Unlock()
	var all []densityInterval
	var est DensityEstimate
	var sum time.Duration
	for _, b := range t.files {
		active := false
		for _, iv := range b.intervals {
			if iv.from.Before(t.cutoff) {
				iv.from = t.cutoff
			}
			if d := iv.to.Sub(iv.from); d > 0 {
				sum += d
				all = append(all, iv)
				active = true
			}
		}
		if active {
			est.Branches++
		}
	}
	if len(all) == 0 {
		return est
	}
	sort.Slice(all, func(i, j int) bool { return all[i].from.Before(all[j].from) })
	var union time.Duration
	cur := all[0]
	for _, iv := range all[1:] {
		if !iv.from.After(cur.to) {
			if iv.to.After(cur.to) {
				cur.to = iv.to
			}
			continue
		}
		union += cur.to.Sub(cur.from)
		cur = iv
	}
	union += cur.to.Sub(cur.from)
	if union <= 0 {
		return est
	}
	est.HasActivity = true
	est.AIHours = union.Hours()
	est.AgentHours = sum.Hours()
	est.Value = sum.Hours() / union.Hours()
	return est
}
