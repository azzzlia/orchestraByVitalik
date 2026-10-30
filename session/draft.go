package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// StaleDraftAfter: text left in an input box and not touched for this long is a
// forgotten draft (a few stray characters). Squad and cs ctl send erase it so the
// conductor and workers still get their messages; it is saved to wiped-drafts.log.
const StaleDraftAfter = 2 * time.Minute

// DraftIsStale reports whether the input box holds a draft nobody touched lately.
func (i *Instance) DraftIsStale() bool {
	return i.Draft != "" && !i.DraftSince.IsZero() && time.Since(i.DraftSince) >= StaleDraftAfter
}

// DraftBlocks reports whether a draft in the box should stop automatic messages:
// only a fresh one (the user may be typing right now).
func (i *Instance) DraftBlocks() bool {
	return i.Draft != "" && !i.DraftIsStale()
}

// SaveWipedDraft appends an erased draft to ~/.claude-squad/wiped-drafts.log.
func SaveWipedDraft(title, draft string) {
	path, err := configPath("wiped-drafts.log")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	clean := strings.NewReplacer("\n", " ⏎ ", "\t", " ").Replace(draft)
	fmt.Fprintf(f, "%s\t%s\t%s\n", time.Now().Format(wipeTimeLayout), strings.ReplaceAll(title, "\t", " "), clean)
}

const wipeTimeLayout = "2006-01-02 15:04:05"

// WipedDraft is a draft erased from a session's input box.
type WipedDraft struct {
	At   time.Time
	Text string
}

// ReadRecentWipes returns the last erased draft per session title, newer than within.
func ReadRecentWipes(within time.Duration) map[string]WipedDraft {
	out := map[string]WipedDraft{}
	path, err := configPath("wiped-drafts.log")
	if err != nil {
		return out
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	if len(data) > 64*1024 {
		data = data[len(data)-64*1024:]
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		at, err := time.ParseInLocation(wipeTimeLayout, parts[0], time.Local)
		if err != nil || time.Since(at) > within {
			continue
		}
		out[parts[1]] = WipedDraft{At: at, Text: parts[2]}
	}
	return out
}
