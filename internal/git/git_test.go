package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsTracked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	repo := setupGitRepo(t)
	c := &Client{WorkDir: repo}
	ctx := context.Background()

	t.Run("tracked file returns true", func(t *testing.T) {
		got, err := c.IsTracked(ctx, "tracked.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got {
			t.Error("IsTracked(tracked.go) = false, want true")
		}
	})

	t.Run("untracked file returns false with no error", func(t *testing.T) {
		// Create the file but do not git add it.
		if err := os.WriteFile(filepath.Join(repo, "untracked.go"), []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := c.IsTracked(ctx, "untracked.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got {
			t.Error("IsTracked(untracked.go) = true, want false")
		}
	})

	t.Run("nonexistent path returns false with no error", func(t *testing.T) {
		got, err := c.IsTracked(ctx, "does-not-exist.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got {
			t.Error("IsTracked(does-not-exist.go) = true, want false")
		}
	})

	t.Run("non-repo propagates error instead of swallowing it", func(t *testing.T) {
		// Run IsTracked against a directory that isn't a git repo at all.
		// Pre-fix this returned (false, nil) and silently excluded the file
		// from coverage. Post-fix it must surface the failure.
		nonRepo := t.TempDir()
		broken := &Client{WorkDir: nonRepo}
		_, err := broken.IsTracked(ctx, "anything.go")
		if err == nil {
			t.Fatal("IsTracked outside a git repo returned nil error — should propagate")
		}
		if !strings.Contains(err.Error(), "not a git repository") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

// setupGitRepo creates a minimal hermetic git repo containing one tracked file.
func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir, run := newGitDir(t)
	run("init", "-b", "main")
	writeFile(t, dir, "tracked.go", "package x\n")
	run("add", "tracked.go")
	run("commit", "-m", "initial")
	return dir
}

// newGitDir returns (path, run) where run executes git with hermetic env.
func newGitDir(t *testing.T) (string, func(args ...string)) {
	t.Helper()
	dir := t.TempDir()
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return dir, run
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupBranchedRepo creates a repo with main + feature branch, a committed
// change on feature, plus staged and untracked changes layered on top.
//
// Layout after setup, on the `feature` branch:
//   - committed.go: present on main; modified on feature
//   - staged.go:    new file, staged but not committed
//   - untracked.go: new file, untracked
//   - main_test.go: test file (filtered out by ChangedFiles)
func setupBranchedRepo(t *testing.T) string {
	t.Helper()
	dir, run := newGitDir(t)
	run("init", "-b", "main")

	writeFile(t, dir, "committed.go", "package x\n\nfunc Hello() {}\n")
	run("add", "committed.go")
	run("commit", "-m", "initial")

	run("checkout", "-b", "feature")
	writeFile(t, dir, "committed.go", "package x\n\nfunc Hello() {}\n\nfunc World() {}\n")
	run("add", "committed.go")
	run("commit", "-m", "feature commit")

	writeFile(t, dir, "staged.go", "package x\n")
	run("add", "staged.go")

	writeFile(t, dir, "untracked.go", "package x\n")
	writeFile(t, dir, "main_test.go", "package x\n")
	return dir
}

func skipNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

func TestRoot(t *testing.T) {
	skipNoGit(t)
	repo := setupGitRepo(t)
	got, err := (&Client{WorkDir: repo}).Root(context.Background())
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	// Root() returns the canonical path, which may differ from t.TempDir()
	// on macOS (where /var is a symlink to /private/var). Compare on the
	// final path component instead.
	if filepath.Base(got) != filepath.Base(repo) {
		t.Errorf("Root = %q, want path ending in %q", got, filepath.Base(repo))
	}
}

func TestVerifyBranch_LocalExists(t *testing.T) {
	skipNoGit(t)
	repo := setupBranchedRepo(t)
	c := &Client{WorkDir: repo}
	if err := c.VerifyBranch(context.Background(), "main"); err != nil {
		t.Errorf("VerifyBranch(main) = %v, want nil", err)
	}
}

func TestVerifyBranch_NotFound(t *testing.T) {
	skipNoGit(t)
	repo := setupBranchedRepo(t)
	c := &Client{WorkDir: repo}
	err := c.VerifyBranch(context.Background(), "no-such-branch")
	if err == nil {
		t.Fatal("VerifyBranch on missing branch returned nil, want error")
	}
	if !strings.Contains(err.Error(), "no-such-branch") {
		t.Errorf("error should name the missing branch; got %v", err)
	}
}

func TestMergeBase_LocalBranch(t *testing.T) {
	skipNoGit(t)
	repo := setupBranchedRepo(t)
	c := &Client{WorkDir: repo}
	got, err := c.MergeBase(context.Background(), "HEAD", "main")
	if err != nil {
		t.Fatalf("MergeBase: %v", err)
	}
	if len(got) != 40 {
		t.Errorf("MergeBase = %q, want a 40-char SHA", got)
	}
}

func TestMergeBase_NotFound(t *testing.T) {
	skipNoGit(t)
	repo := setupBranchedRepo(t)
	c := &Client{WorkDir: repo}
	_, err := c.MergeBase(context.Background(), "HEAD", "no-such-branch")
	if err == nil {
		t.Fatal("MergeBase on missing branch returned nil, want error")
	}
}

func TestChangedFiles_CombinesAllSources(t *testing.T) {
	skipNoGit(t)
	repo := setupBranchedRepo(t)
	c := &Client{WorkDir: repo}

	// Diff against main.
	mb, err := c.MergeBase(context.Background(), "HEAD", "main")
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.ChangedFiles(context.Background(), mb)
	if err != nil {
		t.Fatalf("ChangedFiles: %v", err)
	}

	// Expected: committed.go (modified), staged.go (staged), untracked.go (untracked).
	// main_test.go is untracked AND ends in _test.go — filtered out.
	want := map[string]bool{
		"committed.go": true,
		"staged.go":    true,
		"untracked.go": true,
	}
	if len(got) != len(want) {
		t.Errorf("got %d files, want %d: got=%v", len(got), len(want), got)
	}
	for _, f := range got {
		if !want[f] {
			t.Errorf("unexpected file in ChangedFiles: %q", f)
		}
	}
	// Result must be sorted.
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Errorf("ChangedFiles output not sorted: %v", got)
			break
		}
	}
	// No test files.
	for _, f := range got {
		if strings.HasSuffix(f, "_test.go") {
			t.Errorf("test file %q should be excluded from ChangedFiles", f)
		}
	}
}

func TestAddAndRemoveWorktree(t *testing.T) {
	skipNoGit(t)
	repo := setupGitRepo(t)
	c := &Client{WorkDir: repo}
	ctx := context.Background()

	head, err := c.run(ctx, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	wt := filepath.Join(t.TempDir(), "wt")
	if err := c.AddWorktree(ctx, wt, head); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	// Worktree dir must exist with the file checked out.
	if _, err := os.Stat(filepath.Join(wt, "tracked.go")); err != nil {
		t.Errorf("worktree contents missing: %v", err)
	}
	if err := c.RemoveWorktree(wt); err != nil {
		t.Errorf("RemoveWorktree: %v", err)
	}
}

func TestDiffHunks_UntrackedReturnsWholeFile(t *testing.T) {
	skipNoGit(t)
	repo := setupBranchedRepo(t)
	c := &Client{WorkDir: repo}

	mb, err := c.MergeBase(context.Background(), "HEAD", "main")
	if err != nil {
		t.Fatal(err)
	}
	hunks, err := c.DiffHunks(context.Background(), mb, "untracked.go")
	if err != nil {
		t.Fatalf("DiffHunks: %v", err)
	}
	// Untracked files are reported as the whole-file sentinel hunk.
	if len(hunks) != 1 || hunks[0].Start != 1 || hunks[0].End != 999999 {
		t.Errorf("DiffHunks(untracked) = %v, want [{1 999999}]", hunks)
	}
}

func TestDiffHunks_TrackedFileReportsCommittedHunks(t *testing.T) {
	skipNoGit(t)
	repo := setupBranchedRepo(t)
	c := &Client{WorkDir: repo}

	mb, err := c.MergeBase(context.Background(), "HEAD", "main")
	if err != nil {
		t.Fatal(err)
	}
	hunks, err := c.DiffHunks(context.Background(), mb, "committed.go")
	if err != nil {
		t.Fatalf("DiffHunks: %v", err)
	}
	if len(hunks) == 0 {
		t.Error("expected at least one hunk for committed.go (added a function on feature)")
	}
	// The added function starts after the original two lines of committed.go,
	// so any reported hunk should sit above line 2.
	for _, h := range hunks {
		if h.End < 3 {
			t.Errorf("unexpected early hunk %v — change was past line 2", h)
		}
	}
}
