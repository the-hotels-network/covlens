package covlens

// Report holds the results of a coverage analysis run.
type Report struct {
	DiffCoverage          float64
	TotalCoverage         float64
	BaselineTotalCoverage float64 // set when RatchetTotal is true, otherwise 0
	DiffPassed            bool
	TotalPassed           bool
	Files                 []FileCoverage
	ReportPath            string
	// Warnings collects non-fatal issues observed during the run that
	// callers may want to surface (e.g. a corrupt coverage profile, a
	// per-file diff hunk failure). Fatal failures are returned as errors.
	Warnings []string
}

// FileCoverage holds coverage data for a single source file.
type FileCoverage struct {
	Path       string
	Package    string
	Coverage   float64 // -1 if no statements
	Statements int
	Covered    int
	Excluded   bool
	Status     string // "ok", "fail", "warn", "excluded"
}
