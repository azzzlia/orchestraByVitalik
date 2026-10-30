package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Event is one line of ~/.claude-squad/events.log: what happened to sessions,
// written by Squad without any tokens. The morning report and `cs stats` read it.
type Event struct {
	At     time.Time `json:"t"`
	Kind   string    `json:"ev"` // status | vpn_down | vpn_up | recover | report
	Number int       `json:"n,omitempty"`
	Title  string    `json:"title,omitempty"`
	Label  string    `json:"label,omitempty"`  // status label ("ждёт тебя")
	Status int       `json:"status,omitempty"` // StatusKind
	Text   string    `json:"text,omitempty"`
}

func eventsPath() (string, error) { return configPath("events.log") }

// AppendEvent writes one event (best effort).
func AppendEvent(e Event) {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	path, err := eventsPath()
	if err != nil {
		return
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}

// ReadEvents returns events after since, oldest first.
func ReadEvents(since time.Time) []Event {
	path, err := eventsPath()
	if err != nil {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.At.After(since) {
			out = append(out, e)
		}
	}
	return out
}

// TrimEvents drops events older than keep (called at startup).
func TrimEvents(keep time.Duration) {
	path, err := eventsPath()
	if err != nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) < 4<<20 { // only bother when the log got big
		return
	}
	cutoff := time.Now().Add(-keep)
	var b strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		var e Event
		if json.Unmarshal([]byte(line), &e) == nil && e.At.After(cutoff) {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, []byte(b.String()), 0600) == nil {
		_ = os.Rename(tmp, path)
	}
}

// SessionStats is time spent per status for one session over a period.
type SessionStats struct {
	Number      int
	Title       string
	Working     time.Duration
	WaitingUser time.Duration // "ждёт тебя" + "нужно разрешение" + "прерван"
	OnHold      time.Duration
	Finished    int           // times the agent finished a step and handed over to the user
	LongestWait time.Duration // the longest the user kept it waiting
}

// ComputeStats replays status events into per-session time totals.
func ComputeStats(events []Event, until time.Time) map[string]*SessionStats {
	type cur struct {
		kind  int
		since time.Time
	}
	stats := map[string]*SessionStats{}
	open := map[string]cur{}
	close := func(title string, at time.Time) {
		c, ok := open[title]
		if !ok {
			return
		}
		d := at.Sub(c.since)
		if d < 0 {
			return
		}
		s := stats[title]
		switch StatusKind(c.kind) {
		case StatusWorking:
			s.Working += d
		case StatusWaiting, StatusNeedsInput:
			s.WaitingUser += d
			if d > s.LongestWait {
				s.LongestWait = d
			}
		case StatusOnHold, StatusConductor:
			s.OnHold += d
		}
	}
	for _, e := range events {
		if e.Kind != "status" || e.Title == "" {
			continue
		}
		s := stats[e.Title]
		if s == nil {
			s = &SessionStats{Title: e.Title}
			stats[e.Title] = s
		}
		if e.Number > 0 {
			s.Number = e.Number
		}
		prev, had := open[e.Title]
		close(e.Title, e.At)
		if had && StatusKind(prev.kind) == StatusWorking &&
			(StatusKind(e.Status) == StatusWaiting || StatusKind(e.Status) == StatusNeedsInput || StatusKind(e.Status) == StatusConductor) {
			s.Finished++
		}
		open[e.Title] = cur{kind: e.Status, since: e.At}
	}
	for title := range open {
		close(title, until)
	}
	return stats
}
