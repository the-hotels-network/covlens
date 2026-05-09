package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// Client wraps git commands, executing them within WorkDir.
type Client struct {
	WorkDir string
}

// run executes a git command with the given arguments and returns trimmed stdout.
func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = c.WorkDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Root returns the repository root (git rev-parse --show-toplevel).
func (c *Client) Root(ctx context.Context) (string, error) {
	return c.run(ctx, "rev-parse", "--show-toplevel")
}

// VerifyBranch checks that a branch exists locally or as origin/<name>.
func (c *Client) VerifyBranch(ctx context.Context, name string) error {
	// Try local branch first.
	if _, err := c.run(ctx, "rev-parse", "--verify", name); err == nil {
		return nil
	}
	// Try origin/<name>.
	if _, err := c.run(ctx, "rev-parse", "--verify", "origin/"+name); err == nil {
		return nil
	}
	return fmt.Errorf("branch %q not found locally or as origin/%s", name, name)
}

// MergeBase returns the merge-base between HEAD and the given base ref.
// It tries the base name first, then falls back to origin/<base>.
func (c *Client) MergeBase(ctx context.Context, head, base string) (string, error) {
	if out, err := c.run(ctx, "merge-base", head, base); err == nil {
		return out, nil
	}
	out, err := c.run(ctx, "merge-base", head, "origin/"+base)
	if err != nil {
		return "", fmt.Errorf("cannot find merge-base for %s and %s (or origin/%s): %w", head, base, base, err)
	}
	return out, nil
}

// ChangedFiles returns deduplicated, sorted .go file paths (excluding _test.go)
// that have been changed relative to mergeBase or are untracked.
func (c *Client) ChangedFiles(ctx context.Context, mergeBase string) ([]string, error) {
	seen := make(map[string]bool)

	// Committed changes: mergeBase..HEAD.
	if out, err := c.run(ctx, "diff", "--name-only", mergeBase, "HEAD", "--", "*.go"); err == nil {
		for _, f := range strings.Split(out, "\n") {
			if f != "" {
				seen[f] = true
			}
		}
	}

	// Staged + unstaged changes relative to HEAD.
	if out, err := c.run(ctx, "diff", "--name-only", "HEAD", "--", "*.go"); err == nil {
		for _, f := range strings.Split(out, "\n") {
			if f != "" {
				seen[f] = true
			}
		}
	}

	// Untracked .go files.
	if out, err := c.run(ctx, "ls-files", "--others", "--exclude-standard"); err == nil {
		for _, f := range strings.Split(out, "\n") {
			if f != "" && strings.HasSuffix(f, ".go") {
				seen[f] = true
			}
		}
	}

	// Collect, filter out test files, sort.
	var files []string
	for f := range seen {
		if !strings.HasSuffix(f, "_test.go") {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	return files, nil
}

// IsTracked returns whether the given path is tracked by git.
//
// `git ls-files --error-unmatch` exits with status 1 when the path is genuinely
// untracked, and with status 128 (or fails to start) when something is actually
// broken — corrupt index, not a git repository, missing binary. Conflating the
// two would silently exclude files from coverage, so we only treat exit 1 as
// "untracked" and propagate every other failure.
func (c *Client) IsTracked(ctx context.Context, path string) (bool, error) {
	_, err := c.run(ctx, "ls-files", "--error-unmatch", path)
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

// AddWorktree creates a detached git worktree at dir checked out to the given commit.
func (c *Client) AddWorktree(ctx context.Context, dir, commit string) error {
	_, err := c.run(ctx, "worktree", "add", "--detach", dir, commit)
	return err
}

// RemoveWorktree removes a worktree previously created with AddWorktree.
// Does not take a context — it is always called as a cleanup defer.
func (c *Client) RemoveWorktree(dir string) error {
	_, err := c.run(context.Background(), "worktree", "remove", "--force", dir)
	return err
}

// DiffHunks returns the changed-line hunks for a single file.
// For untracked files it returns a single hunk {1, 999999} representing the entire file.
// For tracked files it parses the unified diff output from both committed and working-tree diffs.
func (c *Client) DiffHunks(ctx context.Context, mergeBase, path string) ([]Hunk, error) {
	tracked, err := c.IsTracked(ctx, path)
	if err != nil {
		return nil, err
	}
	if !tracked {
		return []Hunk{{Start: 1, End: 999999}}, nil
	}

	var allHunks []Hunk

	// Committed changes: mergeBase..HEAD.
	if out, err := c.run(ctx, "diff", "--unified=0", mergeBase, "HEAD", "--", path); err == nil {
		allHunks = append(allHunks, ParseHunks(out)...)
	}

	// Staged + unstaged changes relative to HEAD.
	if out, err := c.run(ctx, "diff", "--unified=0", "HEAD", "--", path); err == nil {
		allHunks = append(allHunks, ParseHunks(out)...)
	}

	return MergeHunks(allHunks), nil
}
