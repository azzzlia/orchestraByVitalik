package session

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"claude-squad/config"
)

// Worker permissions: workers the conductor starts run with --settings
// ~/.claude-squad/worker-settings.json, so a worker commits, pushes its own branch and
// creates branches at night without stopping on a permission prompt. Force-push, a push
// to master/main, pushes that delete refs and deletions still ask (ask beats allow in
// Claude Code), so they wait for the user. Squad writes the file once; after that it is
// the user's to edit and Squad never overwrites it.

// WorkerSettingsFile is the name of the file in the config dir.
const WorkerSettingsFile = "worker-settings.json"

var workerAllow = []string{
	"Bash(git add *)", "Bash(git add)",
	"Bash(git commit *)",
	"Bash(git push)", "Bash(git push *)",
	"Bash(git checkout -b *)", "Bash(git switch -c *)", "Bash(git switch --create *)",
	"Bash(git fetch *)", "Bash(git fetch)", "Bash(git pull --rebase *)", "Bash(git pull)",
	"Bash(git status *)", "Bash(git status)", "Bash(git diff *)", "Bash(git diff)",
	"Bash(git log *)", "Bash(git log)", "Bash(git show *)", "Bash(git branch --show-current)",
	"Bash(git rev-parse *)", "Bash(git stash *)",
	// build leftovers in the worker's own worktree (the cwd): exact commands only
	"Bash(rm node_modules)", "Bash(rm -f node_modules)", "Bash(rm -rf node_modules)", "Bash(rm -rf ./node_modules)",
	"Bash(unlink node_modules)", "Bash(rm -rf dist)", "Bash(rm -rf ./dist)", "Bash(rm -rf coverage)", "Bash(rm -rf ./coverage)",
}

var workerAsk = []string{
	// force-push in any spelling
	"Bash(git push *--force*)", "Bash(git push *-f)", "Bash(git push *-f *)", "Bash(git push *+*)",
	// pushes to the main branches
	"Bash(git push *master*)", "Bash(git push *main*)", "Bash(git push *HEAD:master*)",
	// pushes that delete remote refs ("--delete", "-d", "origin :branch")
	"Bash(git push *--delete*)", "Bash(git push *-d *)", "Bash(git push * :*)", "Bash(git push *--mirror*)", "Bash(git push *--prune*)",
	// local deletions and history rewrites
	"Bash(git branch -d *)", "Bash(git branch -D *)", "Bash(git branch --delete *)",
	"Bash(git reset --hard*)", "Bash(git clean *)", "Bash(git checkout -- *)", "Bash(git restore *)",
	"Bash(git rebase *)", "Bash(git tag -d *)", "Bash(git stash drop*)", "Bash(git stash clear*)",
}

// Any other rm/rmdir/unlink is not allowed, so Claude Code asks about it anyway (an
// "ask: rm *" rule here would also override the exact node_modules rules above).

// legacyDefaults are the files earlier versions wrote: one still unchanged by the
// user is replaced with the current default.
var legacyDefaults = [][2][]string{
	{ // 1.15.0
		{"Bash(git add *)", "Bash(git add)", "Bash(git commit *)", "Bash(git push)", "Bash(git push *)",
			"Bash(git checkout -b *)", "Bash(git switch -c *)", "Bash(git switch --create *)",
			"Bash(git fetch *)", "Bash(git fetch)", "Bash(git pull --rebase *)", "Bash(git pull)",
			"Bash(git status *)", "Bash(git status)", "Bash(git diff *)", "Bash(git diff)",
			"Bash(git log *)", "Bash(git log)", "Bash(git show *)", "Bash(git branch --show-current)",
			"Bash(git rev-parse *)", "Bash(git stash *)", "Bash(git restore --staged *)"},
		{"Bash(git push *--force*)", "Bash(git push *-f)", "Bash(git push *-f *)", "Bash(git push *+*)",
			"Bash(git push *master*)", "Bash(git push *main*)", "Bash(git push *HEAD:master*)",
			"Bash(git push *--delete*)", "Bash(git push *-d *)", "Bash(git push * :*)", "Bash(git push *--mirror*)", "Bash(git push *--prune*)",
			"Bash(git branch -d *)", "Bash(git branch -D *)", "Bash(git branch --delete *)",
			"Bash(git reset --hard*)", "Bash(git clean *)", "Bash(git checkout -- *)", "Bash(git restore *)",
			"Bash(git rebase *)", "Bash(git tag -d *)", "Bash(git stash drop*)", "Bash(git stash clear*)",
			"Bash(rm *)", "Bash(rmdir *)", "Bash(unlink *)"},
	},
}

func settingsJSON(allow, ask []string) []byte {
	data, _ := json.MarshalIndent(map[string]any{
		"permissions": map[string]any{"allow": allow, "ask": ask},
	}, "", "  ")
	return append(data, '\n')
}

// WorkerSettingsPath is the file passed to workers, "" when it is missing.
var WorkerSettingsPath string

// EnsureWorkerSettings writes the default worker-settings.json if there is none and
// sets WorkerSettingsPath. A broken file the user edited is not passed to workers.
func EnsureWorkerSettings() {
	dir, err := config.GetConfigDir()
	if err != nil {
		return
	}
	path := filepath.Join(dir, WorkerSettingsFile)
	data, err := os.ReadFile(path)
	write := os.IsNotExist(err)
	if err == nil {
		for _, l := range legacyDefaults {
			if bytes.Equal(data, settingsJSON(l[0], l[1])) {
				write = true // an old default the user never edited
			}
		}
	}
	if write {
		writeAtomic(path, settingsJSON(workerAllow, workerAsk))
		data, err = os.ReadFile(path)
	}
	var probe map[string]any
	if err != nil || json.Unmarshal(data, &probe) != nil {
		WorkerSettingsPath = ""
		return
	}
	WorkerSettingsPath = path
}

// workerSettingsArg is " --settings '<path>'" for a worker, or "".
func workerSettingsArg(program string) string {
	if WorkerSettingsPath == "" || strings.Contains(program, "--settings") || strings.Contains(WorkerSettingsPath, "'") {
		return ""
	}
	return " --settings '" + WorkerSettingsPath + "'"
}
