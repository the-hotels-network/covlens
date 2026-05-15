package html

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/cover"

	"github.com/the-hotels-network/covlens/internal/covlens"
)

// realSourceRoot writes the given filename + content to a temp dir and
// returns the dir. Tests that exercise Sources need real on-disk files
// because Generate invokes RenderSource.
func realSourceRoot(t *testing.T, name, src string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// readGenerated runs Generate and returns the output file's contents.
// Fails the test on any error.
func readGenerated(t *testing.T, r *covlens.Report, cfg covlens.Config) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "report.html")
	if err := Generate(r, cfg, out); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestGenerate_HappyPath_DiffMode(t *testing.T) {
	const src = "package foo\n\nfunc Foo() int { return 1 }\n"
	root := realSourceRoot(t, "foo.go", src)
	r := &covlens.Report{
		Diff: &covlens.DiffSection{
			Status:    covlens.DiffStatusMeasured,
			Coverage:  85.5,
			Threshold: 80,
			Passed:    true,
		},
		TotalCoverage: 78.0,
		TotalPassed:   true,
		SourceRoot:    root,
		Files: []covlens.FileCoverage{
			{Path: "foo.go", Package: "example/foo", Coverage: 85.5, Statements: 5, Covered: 4},
		},
		Sources: []covlens.SourceData{{
			Path:    "foo.go",
			Package: "example/foo",
			Blocks:  []cover.ProfileBlock{{StartLine: 3, StartCol: 1, EndLine: 3, EndCol: 30, NumStmt: 1, Count: 1}},
			Hunks:   []covlens.Hunk{{Start: 3, End: 3}},
		}},
	}
	cfg := covlens.Config{DiffThreshold: 80, TotalThreshold: 70, BaseBranch: "main"}

	got := readGenerated(t, r, cfg)

	for _, want := range []string{
		"foo.go",      // file path in summary
		"85.5",        // diff coverage
		"main",        // base branch
		"example/foo", // package label
		"Foo",         // source code rendered into the page
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestGenerate_FullMode_OmitsBaseBranch(t *testing.T) {
	r := &covlens.Report{
		// Diff is nil in full mode.
		TotalCoverage: 78.0,
		TotalPassed:   true,
		Files:         []covlens.FileCoverage{{Path: "foo.go", Coverage: 78.0}},
	}
	cfg := covlens.Config{TotalThreshold: 70, FullMode: true}

	got := readGenerated(t, r, cfg)

	if !strings.Contains(got, "78.0") {
		t.Error("expected total coverage 78.0 in output")
	}
	if !strings.Contains(got, "foo.go") {
		t.Error("expected foo.go in output")
	}
}

func TestGenerate_RatchetWithBaseline_Positive(t *testing.T) {
	r := &covlens.Report{
		Diff: &covlens.DiffSection{
			Status: covlens.DiffStatusMeasured, Coverage: 95.0, Threshold: 80, Passed: true,
		},
		TotalCoverage: 80.0, BaselineTotalCoverage: 75.0, TotalPassed: true,
		Files: []covlens.FileCoverage{{Path: "foo.go", Coverage: 95}},
	}
	cfg := covlens.Config{DiffThreshold: 80, RatchetTotal: true}

	got := readGenerated(t, r, cfg)

	// Both the current and baseline numbers should appear; positive delta
	// implies the template's positive-class is selected.
	for _, want := range []string{"80", "75", "positive"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestGenerate_RatchetWithBaseline_Negative(t *testing.T) {
	r := &covlens.Report{
		Diff: &covlens.DiffSection{
			Status: covlens.DiffStatusMeasured, Coverage: 95.0, Threshold: 80, Passed: true,
		},
		TotalCoverage: 70.0, BaselineTotalCoverage: 75.0, TotalPassed: false,
		Files: []covlens.FileCoverage{{Path: "foo.go", Coverage: 95}},
	}
	cfg := covlens.Config{DiffThreshold: 80, RatchetTotal: true}

	got := readGenerated(t, r, cfg)

	if !strings.Contains(got, "negative") {
		t.Error("expected negative delta class in output")
	}
}

func TestGenerate_NoRatchetBaseline_NeutralDelta(t *testing.T) {
	// RatchetTotal is true but BaselineTotalCoverage is 0 — merge-base has
	// no measurable coverage (empty repo, all-excluded baseline, or genuinely
	// 0% covered). Compute errors abort the run; this is not that path.
	// Generate falls through to non-ratchet behavior; HasDelta is false and
	// the delta class stays neutral.
	r := &covlens.Report{
		Diff: &covlens.DiffSection{
			Status: covlens.DiffStatusMeasured, Coverage: 95.0, Threshold: 80, Passed: true,
		},
		TotalCoverage: 70.0, BaselineTotalCoverage: 0, TotalPassed: true,
		Files: []covlens.FileCoverage{{Path: "foo.go", Coverage: 95}},
	}
	cfg := covlens.Config{DiffThreshold: 80, TotalThreshold: 60, RatchetTotal: true}

	got := readGenerated(t, r, cfg)

	// HasDelta is false when baseline is 0, so the badge element (gated on
	// {{if .HasDelta}} in the template) should not render. Match the actual
	// HTML attribute form ('class="badge badge-delta-...') so we don't trip
	// over the same selector names embedded in CSS rules.
	if strings.Contains(got, `class="badge badge-delta-`) {
		t.Error("expected no rendered delta badge element when baseline is 0")
	}
}

func TestGenerate_ThemeForcedDark(t *testing.T) {
	r := &covlens.Report{
		Diff: &covlens.DiffSection{
			Status: covlens.DiffStatusMeasured, Threshold: 80, Passed: true,
		},
		TotalPassed: true,
		Files:       []covlens.FileCoverage{{Path: "foo.go", Coverage: 90}},
	}
	cfg := covlens.Config{DiffThreshold: 80, TotalThreshold: 70}
	cfg.HTML.Theme = "dark"

	got := readGenerated(t, r, cfg)

	if !strings.Contains(got, "dark") {
		t.Error("expected dark theme to surface in output")
	}
}

func TestGenerate_ThemeForcedLight(t *testing.T) {
	r := &covlens.Report{
		Diff: &covlens.DiffSection{
			Status: covlens.DiffStatusMeasured, Threshold: 80, Passed: true,
		},
		TotalPassed: true,
		Files:       []covlens.FileCoverage{{Path: "foo.go", Coverage: 90}},
	}
	cfg := covlens.Config{DiffThreshold: 80, TotalThreshold: 70}
	cfg.HTML.Theme = "light"

	got := readGenerated(t, r, cfg)

	if !strings.Contains(got, "light") {
		t.Error("expected light theme to surface in output")
	}
}

func TestGenerate_ShowExcludedAffectsFileCount(t *testing.T) {
	// Two files in Files: one regular, one excluded. With ShowExcluded=false
	// fileCount should be 1; with ShowExcluded=true it should be 2.
	files := []covlens.FileCoverage{
		{Path: "foo.go", Coverage: 90},
		{Path: "mocks.go", Excluded: true, Coverage: -1},
	}

	cfgHidden := covlens.Config{DiffThreshold: 80, TotalThreshold: 70}
	cfgHidden.ShowExcluded = false

	cfgShown := cfgHidden
	cfgShown.ShowExcluded = true

	r := &covlens.Report{
		Diff: &covlens.DiffSection{
			Status: covlens.DiffStatusMeasured, Threshold: 80, Passed: true,
		},
		TotalPassed: true, Files: files,
	}

	hidden := readGenerated(t, r, cfgHidden)
	shown := readGenerated(t, r, cfgShown)

	// The "shown" output renders mocks.go as a row; the "hidden" output
	// excludes it from the per-file table. Distinguishing cleanly without
	// HTML parsing: the excluded path appears in the file table only when
	// shown. (Both outputs may mention "mocks.go" elsewhere — e.g. data
	// embedded for client-side toggling — but the counted-file row is the
	// behavior under test, asserted by length difference.)
	if len(shown) <= len(hidden) {
		t.Errorf("expected ShowExcluded=true output to be longer than hidden (got %d vs %d)",
			len(shown), len(hidden))
	}
}

func TestGenerate_RenderSourceFailure_PropagatesError(t *testing.T) {
	// SourceRoot points at a real dir, but the Source's Path doesn't exist.
	// RenderSource should fail; Generate should propagate the error.
	r := &covlens.Report{
		Diff: &covlens.DiffSection{
			Status: covlens.DiffStatusMeasured, Threshold: 80, Passed: true,
		},
		TotalPassed: true,
		SourceRoot:  t.TempDir(),
		Files: []covlens.FileCoverage{
			{Path: "missing.go", Coverage: 50},
		},
		Sources: []covlens.SourceData{{Path: "missing.go"}},
	}
	cfg := covlens.Config{DiffThreshold: 80, TotalThreshold: 70}

	out := filepath.Join(t.TempDir(), "report.html")
	err := Generate(r, cfg, out)
	if err == nil {
		t.Fatal("expected error for missing source file, got nil")
	}
	if !strings.Contains(err.Error(), "missing.go") {
		t.Errorf("error should name the failing source file; got %v", err)
	}
}

func TestGenerate_OutputCreationFailure_PropagatesError(t *testing.T) {
	r := &covlens.Report{
		Diff: &covlens.DiffSection{
			Status: covlens.DiffStatusMeasured, Threshold: 80, Passed: true,
		},
		TotalPassed: true,
	}
	cfg := covlens.Config{DiffThreshold: 80, TotalThreshold: 70}

	// Use a path inside a non-existent parent directory; os.Create fails
	// because the parent doesn't exist.
	out := filepath.Join(t.TempDir(), "no-such-dir", "report.html")
	err := Generate(r, cfg, out)
	if err == nil {
		t.Fatal("expected error for unwritable output path, got nil")
	}
	if !strings.Contains(err.Error(), "creating output file") {
		t.Errorf("error should mention output file creation; got %v", err)
	}
}
