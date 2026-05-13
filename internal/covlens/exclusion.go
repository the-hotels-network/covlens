package covlens

import (
	"path/filepath"
	"regexp"

	"github.com/erioch/covlens/internal/directive"
)

// exclusion captures the per-file decision shared by diff and full modes.
type exclusion struct {
	excluded     bool
	funcExcluded []directive.FuncSpan
}

// classifyExclusion applies the exclusion logic that diff mode runs in
// classifyFiles, so full mode can honor ExcludeFiles regexes and
// //covlens:ignore directives identically.
//
// relPath is matched against the user's exclude regexes; absPath is the
// filesystem path passed to directive.Parse.
func classifyExclusion(relPath, absPath string, excludeRes []*regexp.Regexp) exclusion {
	var e exclusion
	for _, re := range excludeRes {
		if re.MatchString(relPath) {
			e.excluded = true
			return e
		}
	}
	if excl, err := directive.Parse(absPath); err == nil {
		if excl.WholeFile {
			e.excluded = true
		} else {
			e.funcExcluded = excl.Functions
		}
	}
	// parse failure → no directive-based exclusions, continue
	return e
}

// regexExcluder returns a predicate suitable for aggregateFiltered that
// reports whether a profile's file (resolved via modPathMap and made
// relative to workDir) matches any of the runner's ExcludeFiles regexes.
//
// Limitation: only file-level regex exclusions are honored here, not
// //covlens:ignore directives. Honoring directives would require parsing
// every Go source file in the project, which is prohibitively expensive
// when the only output we need is an aggregate number. runFull already
// applies directives via its per-file iteration; this helper covers the
// diff-mode total and the baseline-at-merge-base comparison, where the
// file-level regex case is dominant.
func (r *runner) regexExcluder(modPathMap map[string]string, workDir string) func(string) bool {
	return func(profileFileName string) bool {
		absPath := resolveAbsPath(profileFileName, modPathMap)
		if absPath == "" {
			return true // unresolvable paths can't be checked; skip them from the total
		}
		relPath, _ := filepath.Rel(workDir, absPath)
		for _, re := range r.excludeRes {
			if re.MatchString(relPath) {
				return true
			}
		}
		return false
	}
}
