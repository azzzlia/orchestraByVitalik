package git

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// prepareDiff readies the worktree for a diff. It returns false when no diff should
// be computed. For external worktrees (the user's own folders) it is strictly
// read-only: their git index is never touched, and plain folders are skipped.
func (g *GitWorktree) prepareDiff(stats *DiffStats) bool {
	if g.isExternal {
		if _, err := os.Stat(filepath.Join(g.worktreePath, ".git")); err != nil || g.baseCommitSHA == "" {
			return false
		}
		return true
	}
	// -N stages untracked files (intent to add), including them in the diff
	if _, err := g.runGitCommand(g.worktreePath, "add", "-N", "."); err != nil {
		stats.Error = err
		return false
	}
	return true
}

// mergeBaseTTL: how long the merge-base is reused (it changes only on a fetch or merge).
const mergeBaseTTL = 2 * time.Minute

// diffBase is what the session's changes are counted from: the merge-base of HEAD
// with the remote default branch (origin/HEAD, origin/master, origin/main). A branch
// that merged fresh master then shows only its own lines, not the whole master.
// Without such a branch (no remote), the commit the session started from.
func (g *GitWorktree) diffBase() string {
	g.baseMu.Lock()
	defer g.baseMu.Unlock()
	if g.mergeBase != "" && time.Since(g.mergeBaseAt) < mergeBaseTTL {
		return g.mergeBase
	}
	base := g.baseCommitSHA
	for _, ref := range g.defaultRemoteRefs() {
		if out, err := g.runGitCommand(g.worktreePath, "merge-base", "HEAD", ref); err == nil {
			if mb := strings.TrimSpace(out); mb != "" {
				base = mb
				break
			}
		}
	}
	g.mergeBase, g.mergeBaseAt = base, time.Now()
	return base
}

func (g *GitWorktree) defaultRemoteRefs() []string {
	var refs []string
	if out, err := g.runGitCommand(g.worktreePath, "rev-parse", "--abbrev-ref", "origin/HEAD"); err == nil {
		if r := strings.TrimSpace(out); r != "" && r != "origin/HEAD" {
			refs = append(refs, r)
		}
	}
	return append(refs, "origin/master", "origin/main")
}

// DiffStats holds statistics about the changes in a diff
type DiffStats struct {
	// Content is the full diff content
	Content string
	// Added is the number of added lines
	Added int
	// Removed is the number of removed lines
	Removed int
	// Error holds any error that occurred during diff computation
	// This allows propagating setup errors (like missing base commit) without breaking the flow
	Error error
}

func (d *DiffStats) IsEmpty() bool {
	return d.Added == 0 && d.Removed == 0 && d.Content == ""
}

// Diff returns the git diff between the worktree and the base branch along with statistics
func (g *GitWorktree) Diff() *DiffStats {
	stats := &DiffStats{}

	if !g.prepareDiff(stats) {
		return stats
	}

	content, err := g.runGitCommand(g.worktreePath, "--no-pager", "diff", g.diffBase())
	if err != nil {
		stats.Error = err
		return stats
	}
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			stats.Added++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			stats.Removed++
		}
	}
	stats.Content = content

	return stats
}

// DiffNumstat returns the added/removed line counts between the worktree and the
// base branch without loading the full diff content into memory. Use this when
// only the summary counts are needed (e.g. for unselected instances in the list).
func (g *GitWorktree) DiffNumstat() *DiffStats {
	stats := &DiffStats{}

	if !g.prepareDiff(stats) {
		return stats
	}

	out, err := g.runGitCommand(g.worktreePath, "--no-pager", "diff", "--numstat", g.diffBase())
	if err != nil {
		stats.Error = err
		return stats
	}

	stats.Added, stats.Removed = parseNumstat(out)
	return stats
}

// parseNumstat sums the added/removed columns from `git diff --numstat` output.
// Each line is formatted as <added>\t<removed>\t<path>. Binary files report
// "-\t-\t<path>" and are ignored for line totals.
func parseNumstat(out string) (added int, removed int) {
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) < 2 {
			continue
		}
		a, aerr := strconv.Atoi(fields[0])
		r, rerr := strconv.Atoi(fields[1])
		if aerr != nil || rerr != nil {
			continue
		}
		added += a
		removed += r
	}
	return added, removed
}
