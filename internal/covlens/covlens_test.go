package covlens_test

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/txtar"

	"github.com/erioch/covlens/internal/covlens"
)

// TestRun_DiffCoverage builds a real git repo from a txtar fixture (main →
// feature with known coverage characteristics) and asserts that covlens.Run
// computes the expected per-file and aggregate numbers end-to-end.
//
// Diff coverage on foo.go is ~50% (1 of 2 added stmts covered).
func TestRun_DiffCoverage(t *testing.T) {
	requireExecutables(t, "git", "go")

	repo := setupTxtarRepo(t, "testdata/repos/diff_coverage.txtar")

	cfg := covlens.DefaultConfig()
	cfg.WorkDir = repo
	cfg.HTML.AutoOpen = false
	cfg.Stderr = io.Discard     // silence ▶ progress lines in test output
	cfg.TestOutput = io.Discard // silence go test subprocess chatter
	cfg.DiffThreshold = 40      // permissive — we expect ~50%
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

	if report.OutputDir == "" {
		t.Error("OutputDir empty — expected the resolved output directory to be set")
	}
	if len(report.Sources) == 0 {
		t.Error("Sources empty — expected per-file rendering inputs for printers")
	}
	if report.SourceRoot == "" {
		t.Error("SourceRoot empty — printers need it to resolve Source paths")
	}
}

// TestRun_NoChangedFiles exercises the early-exit branch in detectChangedFiles:
// when no .go files differ between HEAD and the base branch, Run must return a
// passing report without invoking go test.
func TestRun_NoChangedFiles(t *testing.T) {
	requireExecutables(t, "git", "go")

	repo := setupTxtarRepo(t, "testdata/repos/no_changed_files.txtar")

	cfg := covlens.DefaultConfig()
	cfg.WorkDir = repo
	cfg.HTML.AutoOpen = false
	cfg.Stderr = io.Discard
	cfg.TestOutput = io.Discard

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

// TestRun_FullMode_HonorsExclusions verifies that --full mode applies the
// same exclusion rules as diff mode: ExcludeFiles regexes mark files as
// excluded, //covlens:ignore on a whole file does the same, and
// //covlens:ignore on a single function subtracts that function's blocks
// from the file's coverage numbers.
//
// Pre-fix, runFull ignored all three signals and reported every file with
// raw coverage — making the same config produce different answers in diff
// vs full mode.
func TestRun_FullMode_HonorsExclusions(t *testing.T) {
	requireExecutables(t, "git", "go")

	repo := setupTxtarRepo(t, "testdata/repos/full_exclusions.txtar")

	cfg := covlens.DefaultConfig()
	cfg.WorkDir = repo
	cfg.HTML.AutoOpen = false
	cfg.Stderr = io.Discard
	cfg.TestOutput = io.Discard
	cfg.FullMode = true
	cfg.ExcludeFiles = []string{`^mocks_.*\.go$`, `_gen\.go$`}
	cfg.TotalThreshold = 0 // we're testing exclusion plumbing, not thresholds

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	report, err := covlens.Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report == nil {
		t.Fatal("Run returned nil report")
	}

	byPath := make(map[string]covlens.FileCoverage)
	for _, fc := range report.Files {
		byPath[fc.Path] = fc
	}

	for _, name := range []string{"mocks_foo.go", "foo_gen.go", "skipme.go"} {
		fc, ok := byPath[name]
		if !ok {
			t.Errorf("%s missing from report.Files; got %v", name, keysOf(byPath))
			continue
		}
		if !fc.Excluded {
			t.Errorf("%s: Excluded = false, want true", name)
		}
		if fc.Status != "excluded" {
			t.Errorf("%s: Status = %q, want %q", name, fc.Status, "excluded")
		}
	}

	// partial.go has two functions; only Counted() should count.
	partial, ok := byPath["partial.go"]
	if !ok {
		t.Fatalf("partial.go missing from report.Files; got %v", keysOf(byPath))
	}
	if partial.Excluded {
		t.Error("partial.go: Excluded = true, want false (only one function is ignored)")
	}
	if partial.Statements != 1 {
		t.Errorf("partial.go: Statements = %d, want 1 (IgnoredFunc should be subtracted)", partial.Statements)
	}
	if partial.Covered != 1 {
		t.Errorf("partial.go: Covered = %d, want 1", partial.Covered)
	}
	if partial.Coverage < 99 {
		t.Errorf("partial.go: Coverage = %.1f%%, want 100%%", partial.Coverage)
	}

	// Sanity: foo.go is a regular tested file and should land at 100%.
	foo, ok := byPath["foo.go"]
	if !ok {
		t.Fatalf("foo.go missing from report.Files; got %v", keysOf(byPath))
	}
	if foo.Coverage < 99 {
		t.Errorf("foo.go: Coverage = %.1f%%, want 100%%", foo.Coverage)
	}

	// Total coverage must reflect exclusions: only foo.go (100%) and
	// partial.go's Counted() function (100%) should contribute. mocks_foo.go,
	// foo_gen.go, and skipme.go are excluded entirely; partial.go's
	// IgnoredFunc is subtracted. So aggregate total should be 100%.
	//
	// Pre-fix, TotalCoverage was computed via coverage.TotalCoverage on the
	// raw profile, ignoring all exclusions — so users who excluded mocks
	// would still see their threshold dragged down by the mocks' 0%.
	if report.TotalCoverage < 99 {
		t.Errorf("TotalCoverage = %.1f%%, want 100%% (excluded files must not count toward total)", report.TotalCoverage)
	}
}

func keysOf(m map[string]covlens.FileCoverage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func requireExecutables(t *testing.T, names ...string) {
	t.Helper()
	for _, n := range names {
		if _, err := exec.LookPath(n); err != nil {
			t.Skipf("%s not on PATH", n)
		}
	}
}

// setupTxtarRepo materializes a git repo from a txtar fixture. Files prefixed
// `base/` are committed on `main`; files prefixed `head/` are committed on a
// `feature` branch checked out from main. Other prefixes are rejected so a
// typo can't silently produce an empty commit.
//
// Returns the absolute repo path, checked out at `feature`.
func setupTxtarRepo(t *testing.T, fixturePath string) string {
	t.Helper()

	ar, err := txtar.ParseFile(fixturePath)
	if err != nil {
		t.Fatalf("parse %s: %v", fixturePath, err)
	}

	var baseFiles, headFiles []txtar.File
	for _, f := range ar.Files {
		switch {
		case strings.HasPrefix(f.Name, "base/"):
			f.Name = strings.TrimPrefix(f.Name, "base/")
			baseFiles = append(baseFiles, f)
		case strings.HasPrefix(f.Name, "head/"):
			f.Name = strings.TrimPrefix(f.Name, "head/")
			headFiles = append(headFiles, f)
		default:
			t.Fatalf("%s: file %q lacks required base/ or head/ prefix", fixturePath, f.Name)
		}
	}

	if len(baseFiles) == 0 {
		t.Fatalf("%s: no base/ files — at least one is required for the initial commit", fixturePath)
	}

	dir := t.TempDir()
	gitEnv := isolatedGitEnv()
	runGit := gitRunner(t, dir, gitEnv)
	write := fileWriter(t, dir)

	runGit("init", "-b", "main")
	for _, f := range baseFiles {
		write(f.Name, string(f.Data))
	}
	runGit("add", ".")
	runGit("commit", "-m", "base")

	runGit("checkout", "-b", "feature")
	if len(headFiles) > 0 {
		for _, f := range headFiles {
			write(f.Name, string(f.Data))
		}
		runGit("add", ".")
		runGit("commit", "-m", "head")
	}

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
