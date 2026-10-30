package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A branch that merged fresh master shows only its own lines.
func TestDiffCountsOnlyTheBranch(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	work := filepath.Join(root, "work")
	gitT(t, root, "init", "-q", "--bare", "-b", "master", origin)
	gitT(t, root, "clone", "-q", origin, work)
	write(t, filepath.Join(work, "a.txt"), "1\n")
	gitT(t, work, "add", ".")
	gitT(t, work, "commit", "-qm", "init")
	gitT(t, work, "push", "-q", "origin", "HEAD:master")
	start := gitT(t, work, "rev-parse", "HEAD")
	gitT(t, work, "checkout", "-qb", "feature")
	write(t, filepath.Join(work, "f.txt"), "mine\n")
	gitT(t, work, "add", ".")
	gitT(t, work, "commit", "-qm", "feature")
	// master moves on by 100 lines and is merged into the branch
	other := filepath.Join(root, "other")
	gitT(t, root, "clone", "-q", origin, other)
	write(t, filepath.Join(other, "m.txt"), strings.Repeat("x\n", 100))
	gitT(t, other, "add", ".")
	gitT(t, other, "commit", "-qm", "master work")
	gitT(t, other, "push", "-q", "origin", "HEAD:master")
	gitT(t, work, "fetch", "-q")
	gitT(t, work, "merge", "-q", "--no-edit", "origin/master")

	g := NewGitWorktreeFromStorage(work, work, "s", "feature", start, true, true)
	st := g.DiffNumstat()
	if st.Error != nil {
		t.Fatal(st.Error)
	}
	if st.Added != 1 || st.Removed != 0 {
		t.Fatalf("want +1 -0 (only the branch), got +%d -%d", st.Added, st.Removed)
	}
}
