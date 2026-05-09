package coverage

import "golang.org/x/tools/cover"

// LineRange is a contiguous, inclusive line range used as input to coverage
// filtering. It is the package's own primitive so callers can translate from
// their own range types (git diff hunks, directive function spans, etc.)
// at the call site, keeping coverage decoupled from any particular source
// of line ranges.
type LineRange struct {
	Start int
	End   int
}

// InRange returns true if any range in ranges overlaps [start, end].
func InRange(ranges []LineRange, start, end int) bool {
	for _, r := range ranges {
		if start <= r.End && end >= r.Start {
			return true
		}
	}
	return false
}

// FileResult holds filtered statement counts for a single file.
type FileResult struct {
	Stmts   int
	Covered int
}

// FilteredCoverage computes statement and covered counts from profile blocks
// that overlap changed line ranges (hunks) and don't fall within excluded ranges.
//
// fileHunks keys are profile file names (e.g. "pkg/file.go").
// excludedRanges uses the same key format and specifies function ranges to ignore
// (from //covlens:ignore directives).
//
// Returns aggregate totals and a per-file breakdown keyed by profile file name.
func FilteredCoverage(profiles []*cover.Profile, fileHunks map[string][]LineRange, excludedRanges map[string][]LineRange) (stmts, covered int, perFile map[string]FileResult) {
	perFile = make(map[string]FileResult)
	for _, p := range profiles {
		hunks, ok := fileHunks[p.FileName]
		if !ok {
			continue
		}
		excluded := excludedRanges[p.FileName]
		var fileStmts, fileCovered int
		for _, b := range p.Blocks {
			if !InRange(hunks, b.StartLine, b.EndLine) {
				continue
			}
			if len(excluded) > 0 && InRange(excluded, b.StartLine, b.EndLine) {
				continue
			}
			fileStmts += b.NumStmt
			if b.Count > 0 {
				fileCovered += b.NumStmt
			}
		}
		if fileStmts > 0 {
			perFile[p.FileName] = FileResult{Stmts: fileStmts, Covered: fileCovered}
		}
		stmts += fileStmts
		covered += fileCovered
	}
	return
}
