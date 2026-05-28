package e2e

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

	"github.com/the-hotels-network/covlens/internal/covlens"
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
	if report.Diff == nil {
		t.Fatal("Diff = nil; want a populated DiffSection in diff mode")
	}
	if report.Diff.Status != covlens.DiffStatusMeasured {
		t.Errorf("Diff.Status = %q, want %q", report.Diff.Status, covlens.DiffStatusMeasured)
	}
	if report.Diff.Coverage < 30 || report.Diff.Coverage > 70 {
		t.Errorf("Diff.Coverage = %.1f%%, want ~50%%", report.Diff.Coverage)
	}
	if report.TotalCoverage <= 0 {
		t.Errorf("TotalCoverage = %.1f%%, expected > 0", report.TotalCoverage)
	}

	if !report.Diff.Passed {
		t.Errorf("Diff.Passed = false (threshold %.0f, coverage %.1f)",
			cfg.DiffThreshold, report.Diff.Coverage)
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
	if report.Diff == nil {
		t.Fatal("Diff = nil; want a DiffSection even for an empty diff")
	}
	if report.Diff.Status != covlens.DiffStatusNoGoChanges {
		t.Errorf("Diff.Status = %q, want %q", report.Diff.Status, covlens.DiffStatusNoGoChanges)
	}
	if !report.Diff.Passed || !report.TotalPassed {
		t.Errorf("expected both passes for empty diff; got Diff.Passed=%v TotalPassed=%v",
			report.Diff.Passed, report.TotalPassed)
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
		if !fc.Excluded {
			t.Errorf("%s: Excluded = false, want true", name)
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

// TestRun_DiffMode_DeletedFileExcluded guards the regression discovered when
// running covlens on a branch that deletes a Go file: the deleted path was
// being added to Report.Sources, which then caused HTML rendering to fail
// when the printer tried to read the (now-missing) source from disk.
//
// After the fix, deleted files appear in neither Files nor Sources — covlens
// doesn't try to render coverage for code that no longer exists.
func TestRun_DiffMode_DeletedFileExcluded(t *testing.T) {
	requireExecutables(t, "git", "go")

	dir := t.TempDir()
	env := isolatedGitEnv()
	runGit := gitRunner(t, dir, env)
	write := fileWriter(t, dir)

	// base: foo.go (with Foo) + foo_test.go (testing Foo).
	runGit("init", "-b", "main")
	write("go.mod", "module example.com/del\n\ngo 1.21\n")
	write("foo.go", "package foo\n\nfunc Foo() int { return 1 }\n")
	write("foo_test.go", "package foo\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) {\n\tif Foo() != 1 { t.Fail() }\n}\n")
	runGit("add", ".")
	runGit("commit", "-m", "base")

	// feature: delete foo.go, rewrite foo_test.go so the module still
	// compiles, and add bar.go so there's a non-deletion changed file too.
	runGit("checkout", "-b", "feature")
	if err := os.Remove(filepath.Join(dir, "foo.go")); err != nil {
		t.Fatal(err)
	}
	write("foo_test.go", "package foo\n\nimport \"testing\"\n\nfunc TestBar(t *testing.T) { _ = Bar() }\n")
	write("bar.go", "package foo\n\nfunc Bar() int { return 2 }\n")
	runGit("add", "-A")
	runGit("commit", "-m", "delete foo, add bar")

	cfg := covlens.DefaultConfig()
	cfg.WorkDir = dir
	cfg.HTML.AutoOpen = false
	cfg.Stderr = io.Discard
	cfg.TestOutput = io.Discard
	cfg.DiffThreshold = 0
	cfg.TotalThreshold = 0

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	report, err := covlens.Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, fc := range report.Files {
		if fc.Path == "foo.go" {
			t.Errorf("deleted foo.go unexpectedly appears in report.Files: %+v", fc)
		}
	}
	for _, s := range report.Sources {
		if s.Path == "foo.go" {
			t.Errorf("deleted foo.go unexpectedly appears in report.Sources: %+v", s)
		}
	}

	// Sanity: bar.go should still show up (it's a normal added file).
	var foundBar bool
	for _, fc := range report.Files {
		if fc.Path == "bar.go" {
			foundBar = true
			break
		}
	}
	if !foundBar {
		t.Errorf("expected bar.go in report.Files; got %d entries", len(report.Files))
	}
}

// TestRun_DiffMode_OnlyDeletions guards the empty-state behavior when the
// PR's diff consists entirely of deletions: covlens must still succeed
// (vacuous diff pass), the report must surface OnlyDeletions=true so callers
// can render a meaningful message, and Files must be empty.
func TestRun_DiffMode_OnlyDeletions(t *testing.T) {
	requireExecutables(t, "git", "go")

	dir := t.TempDir()
	env := isolatedGitEnv()
	runGit := gitRunner(t, dir, env)
	write := fileWriter(t, dir)

	// base: two packages so feature can delete one and leave the module valid.
	runGit("init", "-b", "main")
	write("go.mod", "module example.com/onlydel\n\ngo 1.21\n")
	write("pkg_keep/keep.go", "package keep\n\nfunc Keep() int { return 1 }\n")
	write("pkg_keep/keep_test.go", "package keep\n\nimport \"testing\"\n\nfunc TestKeep(t *testing.T) {\n\tif Keep() != 1 { t.Fail() }\n}\n")
	write("pkg_drop/drop.go", "package drop\n\nfunc Drop() int { return 2 }\n")
	write("pkg_drop/drop_test.go", "package drop\n\nimport \"testing\"\n\nfunc TestDrop(t *testing.T) {\n\tif Drop() != 2 { t.Fail() }\n}\n")
	runGit("add", ".")
	runGit("commit", "-m", "base")

	// feature: delete the whole pkg_drop package. No additions, no edits.
	runGit("checkout", "-b", "feature")
	if err := os.RemoveAll(filepath.Join(dir, "pkg_drop")); err != nil {
		t.Fatal(err)
	}
	runGit("add", "-A")
	runGit("commit", "-m", "drop pkg_drop")

	cfg := covlens.DefaultConfig()
	cfg.WorkDir = dir
	cfg.HTML.AutoOpen = false
	cfg.Stderr = io.Discard
	cfg.TestOutput = io.Discard
	cfg.DiffThreshold = 80 // would fail if 0.0% were evaluated
	cfg.TotalThreshold = 0

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	report, err := covlens.Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if report.Diff == nil {
		t.Fatal("Diff = nil; want a DiffSection")
	}
	if !report.Diff.Passed {
		t.Errorf("Diff.Passed = false — should be vacuously true for deletion-only diff (Coverage=%.1f%%)", report.Diff.Coverage)
	}
	if !report.TotalPassed {
		t.Errorf("TotalPassed = false (TotalCoverage=%.1f%%)", report.TotalCoverage)
	}
	if len(report.Files) != 0 {
		t.Errorf("expected 0 files for deletion-only diff, got %d: %+v", len(report.Files), report.Files)
	}
	if report.Diff.Status != covlens.DiffStatusOnlyDeletions {
		t.Errorf("Diff.Status = %q, want %q", report.Diff.Status, covlens.DiffStatusOnlyDeletions)
	}
}

// TestRun_DiffMode_AllChangedFilesExcluded guards the fix for the edge case
// where every changed file is matched by ExcludeFiles. Before the fix,
// diffCov defaulted to 0.0 and DiffPassed = (0.0 >= threshold) = false,
// causing a spurious failure even though there was nothing measurable to check.
// After the fix, measurable == 0 → DiffPassed is vacuously true.
func TestRun_DiffMode_AllChangedFilesExcluded(t *testing.T) {
	requireExecutables(t, "git", "go")

	dir := t.TempDir()
	env := isolatedGitEnv()
	runGit := gitRunner(t, dir, env)
	write := fileWriter(t, dir)

	// base: foo.go (fully tested) — gives total coverage something to measure.
	runGit("init", "-b", "main")
	write("go.mod", "module example.com/excl\n\ngo 1.21\n")
	write("foo.go", "package foo\n\nfunc Foo() int { return 1 }\n")
	write("foo_test.go", "package foo\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) {\n\tif Foo() != 1 { t.Fail() }\n}\n")
	runGit("add", ".")
	runGit("commit", "-m", "base")

	// feature: only change a generated file that will be excluded.
	runGit("checkout", "-b", "feature")
	write("foo_gen.go", "package foo\n\n// generated — do not edit\nfunc Gen() int { return 99 }\n")
	runGit("add", ".")
	runGit("commit", "-m", "add generated file")

	cfg := covlens.DefaultConfig()
	cfg.WorkDir = dir
	cfg.HTML.AutoOpen = false
	cfg.Stderr = io.Discard
	cfg.TestOutput = io.Discard
	cfg.ExcludeFiles = []string{`_gen\.go$`}
	cfg.DiffThreshold = 80 // would fail if 0.0% were evaluated
	cfg.TotalThreshold = 50 // project-wide total: foo.go is fully tested at HEAD

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	report, err := covlens.Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if report.Diff == nil {
		t.Fatal("Diff = nil; want a DiffSection")
	}
	if !report.Diff.Passed {
		t.Errorf("Diff.Passed = false — should be vacuously true when all changed files are excluded (Coverage=%.1f%%)", report.Diff.Coverage)
	}
	if report.Diff.Status != covlens.DiffStatusAllExcluded {
		t.Errorf("Diff.Status = %q, want %q", report.Diff.Status, covlens.DiffStatusAllExcluded)
	}
	if !report.TotalPassed {
		t.Errorf("TotalPassed = false (TotalCoverage=%.1f%%)", report.TotalCoverage)
	}
	// Project-wide total: even though every changed file is excluded,
	// RunTotal scans the whole repo and finds foo.go fully covered.
	if report.TotalCoverage < 99 {
		t.Errorf("TotalCoverage = %.1f%%, want ~100%% (project-wide total ignores diff exclusions)", report.TotalCoverage)
	}

	// The excluded file must appear in the report as excluded, not measurable.
	var gen *covlens.FileCoverage
	for i := range report.Files {
		if strings.HasSuffix(report.Files[i].Path, "foo_gen.go") {
			gen = &report.Files[i]
			break
		}
	}
	if gen == nil {
		t.Fatalf("foo_gen.go missing from report.Files; got %+v", report.Files)
	}
	if !gen.Excluded {
		t.Errorf("foo_gen.go: Excluded = false, want true")
	}
}

// TestRun_FullMode_GoWorkspace guards the fix for the silent-empty-report
// bug that triggered when the target repo used a Go workspace (go.work).
//
// Pre-fix, `go list -m` under a workspace prints every workspace member
// on its own line. buildModulePathMap stored that multi-line blob as a
// map key, so the prefix match in resolveAbsPath never matched any
// profile entry — every file was dropped and the report came out with
// Files=[] and TotalCoverage=0, despite a populated coverage profile.
//
// Post-fix, buildModulePathMap runs `go list -m` with GOWORK=off so each
// module reports only its own path, and files from both modules show up
// in the report with the right coverage.
func TestRun_FullMode_GoWorkspace(t *testing.T) {
	requireExecutables(t, "git", "go")

	repo := setupTxtarRepo(t, "testdata/repos/full_workspace.txtar")

	cfg := covlens.DefaultConfig()
	cfg.WorkDir = repo
	cfg.HTML.AutoOpen = false
	cfg.Stderr = io.Discard
	cfg.TestOutput = io.Discard
	cfg.FullMode = true
	cfg.TotalThreshold = 0

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

	// Both modules must be represented. Pre-fix, both were dropped.
	foo, ok := byPath["foo.go"]
	if !ok {
		t.Fatalf("foo.go (root module) missing from report.Files; got %v", keysOf(byPath))
	}
	if foo.Coverage < 99 {
		t.Errorf("foo.go: Coverage = %.1f%%, want 100%%", foo.Coverage)
	}

	bar, ok := byPath[filepath.Join("toolbox", "bar.go")]
	if !ok {
		t.Fatalf("toolbox/bar.go (nested module) missing from report.Files; got %v", keysOf(byPath))
	}
	if bar.Coverage < 99 {
		t.Errorf("toolbox/bar.go: Coverage = %.1f%%, want 100%%", bar.Coverage)
	}

	if report.TotalCoverage < 99 {
		t.Errorf("TotalCoverage = %.1f%%, want 100%% (both modules fully tested)", report.TotalCoverage)
	}
}

// TestRun_RatchetTotal_FailsOnRegression covers the --ratchet path
// (baselineTotalCoverage): when total coverage drops vs the merge-base,
// the gate must fail even if the current value clears the absolute
// TotalThreshold. The runner checks out merge-base into a temp worktree,
// re-runs `go test -coverprofile` there, and compares.
//
// Without this test, internal/covlens/baseline.go is unexercised by E2E.
func TestRun_RatchetTotal_FailsOnRegression(t *testing.T) {
	requireExecutables(t, "git", "go")

	repo := setupTxtarRepo(t, "testdata/repos/ratchet_drop.txtar")

	cfg := covlens.DefaultConfig()
	cfg.WorkDir = repo
	cfg.HTML.AutoOpen = false
	cfg.Stderr = io.Discard
	cfg.TestOutput = io.Discard
	cfg.DiffThreshold = 0   // diff is uncovered (new Bar); not what we're testing
	cfg.TotalThreshold = 40 // current 50% clears this — only ratchet should trip
	cfg.RatchetTotal = true

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	report, err := covlens.Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report == nil {
		t.Fatal("Run returned nil report")
	}

	if report.BaselineTotalCoverage < 99 {
		t.Errorf("BaselineTotalCoverage = %.1f%%, want ~100%% (merge-base had Foo fully tested)",
			report.BaselineTotalCoverage)
	}
	if report.TotalCoverage < 40 || report.TotalCoverage > 60 {
		t.Errorf("TotalCoverage = %.1f%%, want ~50%% (Foo covered, Bar not)",
			report.TotalCoverage)
	}
	if report.TotalCoverage >= report.BaselineTotalCoverage {
		t.Errorf("expected TotalCoverage (%.1f) < BaselineTotalCoverage (%.1f) — fixture should produce a regression",
			report.TotalCoverage, report.BaselineTotalCoverage)
	}
	if report.TotalPassed {
		t.Errorf("TotalPassed = true, want false — ratchet should fail when total drops from %.1f%% to %.1f%%",
			report.BaselineTotalCoverage, report.TotalCoverage)
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
