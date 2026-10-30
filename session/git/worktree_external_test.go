package git

import (
	"claude-squad/log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// setupRepoWithExternalWorktree creates repo/ on main and repo-feature/ (a user-made
// worktree) on branch "feature", mirroring a layout like modules + modules-emoji-search.
func setupRepoWithExternalWorktree(t *testing.T) (repo, external string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	repo = filepath.Join(root, "repo")
	external = filepath.Join(root, "repo-feature")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "init")
	runGit(t, repo, "worktree", "add", "-q", "-b", "feature", external)
	return repo, external
}

func TestAttachToExistingWorktree(t *testing.T) {
	repo, external := setupRepoWithExternalWorktree(t)

	wt, err := NewGitWorktreeFromBranch(repo, "feature", "my-session")
	if err != nil {
		t.Fatal(err)
	}
	if !wt.IsExternal() {
		t.Fatal("expected session to attach to the existing worktree")
	}
	realExternal, _ := filepath.EvalSymlinks(external)
	realGot, _ := filepath.EvalSymlinks(wt.GetWorktreePath())
	if realGot != realExternal {
		t.Fatalf("worktree path = %s, want %s", realGot, realExternal)
	}
	if err := wt.Setup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if wt.GetBaseCommitSHA() == "" {
		t.Fatal("base commit not recorded")
	}

	// Uncommitted work in the user's folder must survive Remove and Cleanup.
	dirtyFile := filepath.Join(external, "work-in-progress.txt")
	if err := os.WriteFile(dirtyFile, []byte("wip"), 0644); err != nil {
		t.Fatal(err)
	}
	if checked, _ := wt.IsBranchCheckedOut(); checked {
		t.Fatal("external worktree must not block kill")
	}
	if err := wt.Remove(); err != nil {
		t.Fatal(err)
	}
	if err := wt.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dirtyFile); err != nil {
		t.Fatalf("uncommitted file was lost: %v", err)
	}
	if out, err := exec.Command("git", "-C", repo, "show-ref", "--verify", "refs/heads/feature").CombinedOutput(); err != nil {
		t.Fatalf("branch was deleted: %s", out)
	}
	worktrees, err := ListWorktrees(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := worktrees["feature"]; !ok {
		t.Fatal("external worktree was unregistered from git")
	}
}

func TestAttachToMainCheckout(t *testing.T) {
	repo, _ := setupRepoWithExternalWorktree(t)
	wt, err := NewGitWorktreeFromBranch(repo, "main", "on-main")
	if err != nil {
		t.Fatal(err)
	}
	if !wt.IsExternal() {
		t.Fatal("branch checked out in the main folder should attach to it")
	}
}

func TestBranchWithoutWorktreeStillGetsNewOne(t *testing.T) {
	repo, _ := setupRepoWithExternalWorktree(t)
	runGit(t, repo, "branch", "other")
	wt, err := NewGitWorktreeFromBranch(repo, "other", "other-session")
	if err != nil {
		t.Fatal(err)
	}
	if wt.IsExternal() {
		t.Fatal("branch without a worktree must get a managed worktree")
	}
	if err := wt.Setup(); err != nil {
		t.Fatal(err)
	}
	if err := wt.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt.GetWorktreePath()); !os.IsNotExist(err) {
		t.Fatal("managed worktree should be removed on cleanup")
	}
}

func TestSearchBranchesPutsWorktreeBranchesFirst(t *testing.T) {
	repo, _ := setupRepoWithExternalWorktree(t)
	runGit(t, repo, "branch", "zzz-newer")
	branches, err := SearchBranches(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) < 3 || branches[0] != "main" && branches[0] != "feature" || branches[len(branches)-1] != "zzz-newer" {
		t.Fatalf("unexpected order: %v", branches)
	}
}

func TestExternalWorktreeDiffDoesNotTouchIndex(t *testing.T) {
	repo, external := setupRepoWithExternalWorktree(t)
	if err := os.WriteFile(filepath.Join(external, "secret-local.yaml"), []byte("token: x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "a.txt"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	wt, err := NewGitWorktreeFromBranch(repo, "feature", "s")
	if err != nil {
		t.Fatal(err)
	}
	if err := wt.Setup(); err != nil {
		t.Fatal(err)
	}
	stats := wt.Diff()
	if stats.Error != nil || stats.Added == 0 {
		t.Fatalf("expected diff of tracked change, got %+v", stats)
	}
	out, _ := exec.Command("git", "-C", external, "status", "--porcelain").CombinedOutput()
	for _, line := range splitNonEmpty(string(out)) {
		if line == "A  secret-local.yaml" || line == " A secret-local.yaml" {
			t.Fatalf("untracked file was added to the index: %q", out)
		}
	}
	if err := wt.PushChanges("x", false); err == nil {
		t.Fatal("push must be refused for external worktrees")
	}
}

func TestExternalNonGitFolder(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	folder := filepath.Join(root, "plain-folder")
	if err := os.MkdirAll(folder, 0755); err != nil {
		t.Fatal(err)
	}
	wt := NewGitWorktreeFromStorage(root, folder, "s", "plain", "", true, true)
	if valid, err := wt.IsValidWorktree(); err != nil || !valid {
		t.Fatalf("plain folder should be valid: %v %v", valid, err)
	}
	if err := wt.Setup(); err != nil {
		t.Fatalf("setup on plain folder: %v", err)
	}
	if stats := wt.Diff(); stats.Error != nil {
		t.Fatalf("diff on plain folder should be silent: %v", stats.Error)
	}
	if err := wt.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(folder); err != nil {
		t.Fatal("plain folder removed")
	}
}

func splitNonEmpty(s string) []string {
	var r []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			r = append(r, l)
		}
	}
	return r
}

func TestMain(m *testing.M) {
	log.Initialize(false)
	defer log.Close()
	os.Exit(m.Run())
}
