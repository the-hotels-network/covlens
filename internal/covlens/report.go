package covlens

import "golang.org/x/tools/cover"

// DiffStatus classifies the diff portion of a run. Used by callers to choose
// between rendering diff coverage metrics and rendering an explanatory chip
// when the diff is not meaningfully measurable.
type DiffStatus string

const (
	// DiffStatusMeasured: the diff contained measurable Go code; Coverage
	// and Passed reflect a real measurement against Threshold.
	DiffStatusMeasured DiffStatus = "measured"
	// DiffStatusNoGoChanges: no Go files appeared in the diff at all
	// (e.g., a docs-only PR). Passed is vacuously true.
	DiffStatusNoGoChanges DiffStatus = "no-go-changes"
	// DiffStatusOnlyDeletions: every changed Go file was a pure deletion.
	// Passed is vacuously true.
	DiffStatusOnlyDeletions DiffStatus = "only-deletions"
	// DiffStatusAllExcluded: Go files were changed but every one matched
	// exclude_files. Passed is vacuously true.
	DiffStatusAllExcluded DiffStatus = "all-excluded"
)

// DiffSection holds the diff portion of a run. Self-contained: a consumer
// holding only a *DiffSection can interpret pass/fail without consulting
// Config. Threshold captures the value the run was evaluated against.
type DiffSection struct {
	Status    DiffStatus
	Coverage  float64
	Threshold float64
	Passed    bool
}

// Report holds the results of a coverage analysis run.
//
// Diff is nil in --full mode (no diff is computed). In diff mode it is always
// non-nil; consumers should check Diff.Status to distinguish measured runs
// from vacuous-pass cases (no Go changes, only deletions, all excluded).
type Report struct {
	// Diff is the diff portion of the run. Nil in --full mode.
	Diff *DiffSection

	TotalCoverage         float64
	BaselineTotalCoverage float64 // set when RatchetTotal is true, otherwise 0
	TotalPassed           bool

	Files []FileCoverage
	// OutputDir is the absolute path to the directory where coverage
	// profiles were written. Printers (e.g. the HTML renderer) typically
	// write their output here.
	OutputDir string
	// SourceRoot is the absolute path that SourceData.Path entries are
	// relative to. Printers that need to read a file from disk combine it
	// with the entry's Path. In diff mode this is the git working-tree
	// root; in full mode it's the configured WorkDir.
	SourceRoot string
	// Sources carries raw per-file rendering inputs (profile blocks, diff
	// hunks) for printers that render source views. Excluded files and
	// files with no profile data are NOT in this slice — only files with
	// renderable content appear here.
	Sources []SourceData
}

// FileCoverage holds coverage data for a single source file.
type FileCoverage struct {
	Path       string
	Package    string
	Coverage   float64 // -1 if no statements
	Statements int
	Covered    int
	Excluded   bool
}

// StatusFor returns the verdict string for this file given threshold.
// Callers should pass cfg.DiffThreshold in diff mode, cfg.TotalThreshold in full mode.
func (f FileCoverage) StatusFor(threshold float64) string {
	if f.Excluded {
		return "excluded"
	}
	if f.Coverage < 0 {
		return "warn"
	}
	if f.Coverage >= threshold {
		return "ok"
	}
	return "fail"
}

// SourceData carries raw inputs a printer needs to render the source view
// of a single file with coverage overlay.
//
// Path is git-relative; combine with Report.GitRoot to read the file.
// Hunks may be nil — that signals "render the full file" (used in full mode
// and for files outside the diff).
type SourceData struct {
	Path    string
	Package string
	Blocks  []cover.ProfileBlock
	Hunks   []Hunk
}

// Hunk is a contiguous changed-line range, inclusive on both ends (1-based).
type Hunk struct {
	Start int
	End   int
}
