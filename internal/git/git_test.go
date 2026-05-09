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

	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "tracked.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "tracked.go")
	run("commit", "-m", "initial")

	return dir
}
