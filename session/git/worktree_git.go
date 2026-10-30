package git

import (
	"claude-squad/log"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// MaxBranchSearchResults is the maximum number of branches returned by SearchBranches.
const MaxBranchSearchResults = 50

// FetchBranches fetches and prunes remote-tracking branches (best-effort, won't fail if offline).
func FetchBranches(repoPath string) {
	cmd := exec.Command("git", "-C", repoPath, "fetch", "--prune")
	_ = cmd.Run()
}

// SearchBranches searches for branches whose name contains filter (case-insensitive),
// ordered by most recently updated first. Returns at most MaxBranchSearchResults.
// If filter is empty, returns all branches up to the limit.
func SearchBranches(repoPath, filter string) ([]string, error) {
	cmd := exec.Command("git", "-C", repoPath, "branch", "-a",
		"--sort=-committerdate",
		"--format=%(refname:short)")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %s (%w)", output, err)
	}

	worktrees, _ := ListWorktrees(repoPath)

	seen := make(map[string]bool)
	var branches []string
	lower := strings.ToLower(filter)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "HEAD") {
			continue
		}
		name := strings.TrimPrefix(line, "origin/")
		if seen[name] {
			continue
		}
		seen[name] = true
		if filter != "" && !strings.Contains(strings.ToLower(name), lower) {
			continue
		}
		branches = append(branches, name)
		if len(branches) >= MaxBranchSearchResults {
			break
		}
	}

	// Branches that already have their own worktree folder go first:
	// picking one of them attaches the session to that folder.
	if len(worktrees) > 0 {
		var withTree, rest []string
		for _, b := range branches {
			if _, ok := worktrees[b]; ok {
				withTree = append(withTree, b)
			} else {
				rest = append(rest, b)
			}
		}
		branches = append(withTree, rest...)
	}
	return branches, nil
}

// ListWorktrees returns a map of branch name -> worktree path for every worktree
// of the repository (the main checkout included) that has a branch checked out.
func ListWorktrees(repoPath string) (map[string]string, error) {
	cmd := exec.Command("git", "-C", repoPath, "worktree", "list", "--porcelain")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %s (%w)", output, err)
	}
	result := make(map[string]string)
	current := ""
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "worktree "):
			current = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch ") && current != "":
			branch := strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
			result[branch] = current
		case line == "":
			current = ""
		}
	}
	return result, nil
}

// FindWorktreeForBranch returns the path of an existing worktree that has
// branchName checked out, skipping worktrees managed by Claude Squad itself.
func FindWorktreeForBranch(repoPath, branchName string) (string, bool) {
	worktrees, err := ListWorktrees(repoPath)
	if err != nil {
		log.WarningLog.Printf("could not list worktrees: %v", err)
		return "", false
	}
	path, ok := worktrees[branchName]
	if !ok {
		return "", false
	}
	if managedDir, err := getWorktreeDirectory(); err == nil {
		if rel, err := filepath.Rel(managedDir, path); err == nil && !strings.HasPrefix(rel, "..") {
			return "", false
		}
	}
	return path, true
}

// runGitCommand executes a git command and returns any error
func (g *GitWorktree) runGitCommand(path string, args ...string) (string, error) {
	baseArgs := []string{"-C", path}
	cmd := exec.Command("git", append(baseArgs, args...)...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git command failed: %s (%w)", output, err)
	}

	return string(output), nil
}

// PushChanges commits and pushes changes in the worktree to the remote branch
func (g *GitWorktree) PushChanges(commitMessage string, open bool) error {
	if g.isExternal {
		return fmt.Errorf("автопуш отключён для твоей собственной папки %s: коммить и пушь сам или через Claude", g.worktreePath)
	}
	if err := checkGHCLI(); err != nil {
		return err
	}

	// Check if there are any changes to commit
	isDirty, err := g.IsDirty()
	if err != nil {
		return fmt.Errorf("failed to check for changes: %w", err)
	}

	if isDirty {
		// Stage all changes
		if _, err := g.runGitCommand(g.worktreePath, "add", "."); err != nil {
			log.ErrorLog.Print(err)
			return fmt.Errorf("failed to stage changes: %w", err)
		}

		// Create commit
		if _, err := g.runGitCommand(g.worktreePath, "commit", "-m", commitMessage, "--no-verify"); err != nil {
			log.ErrorLog.Print(err)
			return fmt.Errorf("failed to commit changes: %w", err)
		}
	}

	// First push the branch to remote to ensure it exists
	pushCmd := exec.Command("gh", "repo", "sync", "--source", "-b", g.branchName)
	pushCmd.Dir = g.worktreePath
	if err := pushCmd.Run(); err != nil {
		// If sync fails, try creating the branch on remote first
		gitPushCmd := exec.Command("git", "push", "-u", "origin", g.branchName)
		gitPushCmd.Dir = g.worktreePath
		if pushOutput, pushErr := gitPushCmd.CombinedOutput(); pushErr != nil {
			log.ErrorLog.Print(pushErr)
			return fmt.Errorf("failed to push branch: %s (%w)", pushOutput, pushErr)
		}
	}

	// Now sync with remote
	syncCmd := exec.Command("gh", "repo", "sync", "-b", g.branchName)
	syncCmd.Dir = g.worktreePath
	if output, err := syncCmd.CombinedOutput(); err != nil {
		log.ErrorLog.Print(err)
		return fmt.Errorf("failed to sync changes: %s (%w)", output, err)
	}

	// Open the branch in the browser
	if open {
		if err := g.OpenBranchURL(); err != nil {
			// Just log the error but don't fail the push operation
			log.ErrorLog.Printf("failed to open branch URL: %v", err)
		}
	}

	return nil
}

// CommitChanges commits changes locally without pushing to remote
func (g *GitWorktree) CommitChanges(commitMessage string) error {
	// Check if there are any changes to commit
	isDirty, err := g.IsDirty()
	if err != nil {
		return fmt.Errorf("failed to check for changes: %w", err)
	}

	if isDirty {
		// Stage all changes
		if _, err := g.runGitCommand(g.worktreePath, "add", "."); err != nil {
			log.ErrorLog.Print(err)
			return fmt.Errorf("failed to stage changes: %w", err)
		}

		// Create commit (local only)
		if _, err := g.runGitCommand(g.worktreePath, "commit", "-m", commitMessage, "--no-verify"); err != nil {
			log.ErrorLog.Print(err)
			return fmt.Errorf("failed to commit changes: %w", err)
		}
	}

	return nil
}

// IsDirty checks if the worktree has uncommitted changes
func (g *GitWorktree) IsDirty() (bool, error) {
	output, err := g.runGitCommand(g.worktreePath, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("failed to check worktree status: %w", err)
	}
	return len(output) > 0, nil
}

// IsValidWorktree reports whether the worktree path exists and contains a
// .git entry, i.e. git can still recognize it as a working tree.
// Returns (false, nil) if the worktree is orphaned (path or .git missing).
func (g *GitWorktree) IsValidWorktree() (bool, error) {
	if g.isExternal {
		// The user's own folder only needs to exist; it may not even be a git checkout.
		info, err := os.Stat(g.worktreePath)
		if err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, fmt.Errorf("failed to stat worktree path: %w", err)
		}
		return info.IsDir(), nil
	}
	if _, err := os.Stat(g.worktreePath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to stat worktree path: %w", err)
	}
	if _, err := os.Stat(filepath.Join(g.worktreePath, ".git")); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to stat worktree .git: %w", err)
	}
	return true, nil
}

// IsBranchCheckedOut checks if the instance branch is currently checked out
func (g *GitWorktree) IsBranchCheckedOut() (bool, error) {
	if g.isExternal {
		// The session lives in the user's own worktree; nothing to protect.
		return false, nil
	}
	output, err := g.runGitCommand(g.repoPath, "branch", "--show-current")
	if err != nil {
		return false, fmt.Errorf("failed to get current branch: %w", err)
	}
	return strings.TrimSpace(string(output)) == g.branchName, nil
}

// OpenBranchURL opens the branch URL in the default browser
func (g *GitWorktree) OpenBranchURL() error {
	// Check if GitHub CLI is available
	if err := checkGHCLI(); err != nil {
		return err
	}

	cmd := exec.Command("gh", "browse", "--branch", g.branchName)
	cmd.Dir = g.worktreePath
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to open branch URL: %w", err)
	}
	return nil
}
