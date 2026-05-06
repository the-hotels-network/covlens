package covlens_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/erioch/covlens"
)

// TestRun_DiffCoverage builds a real git repo in t.TempDir() with main → feature
// branches and known coverage characteristics, then asserts that covlens.Run
// computes the expected per-file and aggregate numbers end-to-end.
//
// Layout:
//
//	main:    foo.go has Add (fully covered by foo_test.go).
//	feature: foo.go adds Sub (covered) and Mul (uncovered).
//
// Diff coverage on foo.go must therefore land at ~50% (1 of 2 added stmts covered).
func TestRun_DiffCoverage(t *testing.T) {
	requireExecutables(t, "git", "go")

	repo := setupTestRepo(t)

	cfg := covlens.DefaultConfig()
	cfg.WorkDir = repo
	cfg.AutoOpen = false
	cfg.DiffThreshold = 40 // permissive — we expect ~50%
	cfg.TotalThreshold = 40

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	report, err := covlens.Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report == nil {
		t.Fatal("Run returned nil report")
	}

	if len(report.Files) == 0 {
		t.Fatal("expected at least one changed file in report, got 0")
	}

	var foo *covlens.FileCoverage
	for i := range report.Files {
		if strings.HasSuffix(report.Files[i].Path, "foo.go") {
			foo = &report.Files[i]
			break
		}
	}
	if foo == nil {
		t.Fatalf("foo.go not in report.Files; got %+v", report.Files)
	}

	if foo.Coverage < 30 || foo.Coverage > 70 {
		t.Errorf("foo.go diff coverage = %.1f%%, want ~50%%", foo.Coverage)
	}
	if report.DiffCoverage < 30 || report.DiffCoverage > 70 {
		t.Errorf("DiffCoverage = %.1f%%, want ~50%%", report.DiffCoverage)
	}
	if report.TotalCoverage <= 0 {
		t.Errorf("TotalCoverage = %.1f%%, expected > 0", report.TotalCoverage)
	}

	if !report.DiffPassed {
		t.Errorf("DiffPassed = false (threshold %.0f, coverage %.1f)",
			cfg.DiffThreshold, report.DiffCoverage)
	}
	if !report.TotalPassed {
		t.Errorf("TotalPassed = false (threshold %.0f, coverage %.1f)",
			cfg.TotalThreshold, report.TotalCoverage)
	}

	if report.ReportPath == "" {
		t.Error("ReportPath empty — expected an HTML report to be generated")
	} else if _, err := os.Stat(report.ReportPath); err != nil {
		t.Errorf("HTML report missing at %q: %v", report.ReportPath, err)
	}
}

// TestRun_NoChangedFiles exercises the early-exit branch in detectChangedFiles:
// when no .go files differ between HEAD and the base branch, Run must return a
// passing report without invoking go test.
func TestRun_NoChangedFiles(t *testing.T) {
	requireExecutables(t, "git", "go")

	repo := setupRepoWithDocsOnlyChange(t)

	cfg := covlens.DefaultConfig()
	cfg.WorkDir = repo
	cfg.AutoOpen = false

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	report, err := covlens.Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !report.DiffPassed || !report.TotalPassed {
		t.Errorf("expected both passes for empty diff; got DiffPassed=%v TotalPassed=%v",
			report.DiffPassed, report.TotalPassed)
	}
	if len(report.Files) != 0 {
		t.Errorf("expected 0 files for docs-only change, got %d: %+v", len(report.Files), report.Files)
	}
}

func requireExecutables(t *testing.T, names ...string) {
	t.Helper()
	for _, n := range names {
		if _, err := exec.LookPath(n); err != nil {
			t.Skipf("%s not on PATH", n)
		}
	}
}

// setupTestRepo creates a self-contained git repo: main with Add+test, feature
// with Add+Sub (covered) + Mul (uncovered). Returns the absolute repo path,
// checked out at feature.
func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitEnv := isolatedGitEnv()

	runGit := gitRunner(t, dir, gitEnv)
	write := fileWriter(t, dir)

	runGit("init", "-b", "main")

	write("go.mod", "module example.com/testrepo\n\ngo 1.21\n")
	write("foo.go", `package foo

func Add(a, b int) int { return a + b }
`)
	write("foo_test.go", `package foo

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fail()
	}
}
`)
	runGit("add", ".")
	runGit("commit", "-m", "initial")

	runGit("checkout", "-b", "feature")

	write("foo.go", `package foo

func Add(a, b int) int { return a + b }

func Sub(a, b int) int { return a - b }

func Mul(a, b int) int { return a * b }
`)
	write("foo_test.go", `package foo

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fail()
	}
}

func TestSub(t *testing.T) {
	if Sub(5, 3) != 2 {
		t.Fail()
	}
}
`)
	runGit("add", ".")
	runGit("commit", "-m", "add Sub and Mul")

	return dir
}

// setupRepoWithDocsOnlyChange creates a repo whose feature branch differs from
// main only in a non-.go file — covlens.Run should report no changed files.
func setupRepoWithDocsOnlyChange(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitEnv := isolatedGitEnv()

	runGit := gitRunner(t, dir, gitEnv)
	write := fileWriter(t, dir)

	runGit("init", "-b", "main")

	write("go.mod", "module example.com/testrepo\n\ngo 1.21\n")
	write("foo.go", `package foo

func Add(a, b int) int { return a + b }
`)
	write("foo_test.go", `package foo

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Fail()
	}
}
`)
	runGit("add", ".")
	runGit("commit", "-m", "initial")

	runGit("checkout", "-b", "feature")
	write("README.md", "# docs only change\n")
	runGit("add", ".")
	runGit("commit", "-m", "docs")

	return dir
}

func isolatedGitEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.com",
		// Prevent the host's global/system git config from leaking into
		// the test (hooks, signing, default branch override, ...).
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
}

func gitRunner(t *testing.T, dir string, env []string) func(args ...string) {
	t.Helper()
	return func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

func fileWriter(t *testing.T, dir string) func(rel, content string) {
	t.Helper()
	return func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
