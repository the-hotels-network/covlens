package covlens

import "golang.org/x/tools/cover"

// Report holds the results of a coverage analysis run.
//
// Library consumers who only need numeric coverage should read DiffCoverage,
// TotalCoverage, and Files. The Sources slice is a separate channel of raw
// per-file rendering inputs intended for printers that produce source views
// (e.g. the HTML printer). Consumers that don't render source can ignore it.
type Report struct {
	DiffCoverage          float64
	TotalCoverage         float64
	BaselineTotalCoverage float64 // set when RatchetTotal is true, otherwise 0
	DiffPassed            bool
	TotalPassed           bool
	Files                 []FileCoverage
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
