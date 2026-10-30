package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The user's work density score ("плотность"), shown by the overview pet.
// Stored in ~/.claude-squad/density by `cs density` or the density_command.

// ParseDensity reads "10,4" or "10.4" (the first number in s).
func ParseDensity(s string) (float64, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	start := -1
	for i, r := range s {
		if (r >= '0' && r <= '9') || (r == '-' && start < 0) {
			if start < 0 {
				start = i
			}
		} else if start >= 0 && r != '.' {
			s = s[start:i]
			start = 0
			break
		}
	}
	if start > 0 {
		s = s[start:]
	}
	v, err := strconv.ParseFloat(strings.TrimRight(s, "."), 64)
	return v, err == nil
}

// ReadDensity returns the saved score and when it was saved.
func ReadDensity() (float64, time.Time, bool) {
	path, err := configPath("density")
	if err != nil {
		return 0, time.Time{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, time.Time{}, false
	}
	v, ok := ParseDensity(string(data))
	info, _ := os.Stat(path)
	var at time.Time
	if info != nil {
		at = info.ModTime()
	}
	return v, at, ok
}

// WriteDensity saves the score.
func WriteDensity(v float64) error {
	path, err := configPath("density")
	if err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	return os.WriteFile(path, []byte(strconv.FormatFloat(v, 'f', -1, 64)+"\n"), 0600)
}

// ClearDensity removes the score set by hand.
func ClearDensity() error {
	path, err := configPath("density")
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// The current score is cached for fast readers (the Claude Code status line).
type densityCache struct {
	Value float64   `json:"value"`
	OK    bool      `json:"ok"`
	At    time.Time `json:"at"`
}

// WriteDensityCache stores the score Squad currently shows.
func WriteDensityCache(v float64, ok bool) {
	path, err := configPath("density-cache.json")
	if err != nil {
		return
	}
	data, _ := json.Marshal(densityCache{Value: v, OK: ok, At: time.Now()})
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	writeAtomic(path, data)
}

// writeAtomic writes through a unique temp file (several status lines may write at once).
func writeAtomic(path string, data []byte) {
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(f.Name())
		return
	}
	_ = os.Chmod(f.Name(), 0600)
	if os.Rename(f.Name(), path) != nil {
		_ = os.Remove(f.Name())
	}
}

// CachedDensity reads only the cache Squad keeps fresh (for the status line, which
// runs on every Claude Code redraw and must not scan logs or run commands).
func CachedDensity(maxAge time.Duration) (float64, bool) {
	if path, err := configPath("density-cache.json"); err == nil {
		if data, err := os.ReadFile(path); err == nil {
			var c densityCache
			if json.Unmarshal(data, &c) == nil && time.Since(c.At) < maxAge {
				return c.Value, c.OK
			}
		}
	}
	if v, at, ok := ReadDensity(); ok && at.Format("2006-01-02") == time.Now().Format("2006-01-02") {
		return v, true
	}
	return 0, false
}

// CurrentDensity returns the score for display: the cache if it is fresh, else
// a score set by hand today, else a fresh estimate from the logs (cached).
func CurrentDensity(maxAge time.Duration) (float64, bool) {
	if path, err := configPath("density-cache.json"); err == nil {
		if data, err := os.ReadFile(path); err == nil {
			var c densityCache
			if json.Unmarshal(data, &c) == nil && time.Since(c.At) < maxAge {
				return c.Value, c.OK
			}
		}
	}
	if v, at, ok := ReadDensity(); ok && at.Format("2006-01-02") == time.Now().Format("2006-01-02") {
		WriteDensityCache(v, true)
		return v, true
	}
	t := NewDensityTracker()
	t.Refresh(time.Now())
	est := t.Estimate()
	WriteDensityCache(est.Value, est.HasActivity)
	return est.Value, est.HasActivity
}
