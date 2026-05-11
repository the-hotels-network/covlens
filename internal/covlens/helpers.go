package covlens

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"golang.org/x/tools/cover"

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

func fileStatusFor(cov, threshold float64) string {
	if cov < 0 {
		return "warn"
	}
	if cov >= threshold {
		return "ok"
	}
	return "fail"
}

// aggregateFiltered sums covered/total statements across profiles, skipping
// any profile for which excluded returns true. Returns the resulting coverage
// percentage (or 0 if no statements remained after filtering).
func aggregateFiltered(profiles []*cover.Profile, excluded func(profileFileName string) bool) float64 {
	var stmts, covered int64
	for _, p := range profiles {
		if excluded(p.FileName) {
			continue
		}
		for _, b := range p.Blocks {
			stmts += int64(b.NumStmt)
			if b.Count > 0 {
				covered += int64(b.NumStmt)
			}
		}
	}
	if stmts == 0 {
		return 0
	}
	return float64(covered) / float64(stmts) * 100
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

func logProgress(w io.Writer, msg string) {
	fmt.Fprintf(w, "\033[0;34m▶\033[0m %s\n", msg)
}

// openTestOutputLog returns the writer that `go test` subprocesses should
// write their stdout/stderr to, plus a cleanup func. Precedence:
//
//  1. VerboseTests=true → cfg.testOutput() (typically os.Stdout)
//  2. Caller-set TestOutput (e.g., io.Discard in tests) → respect it
//  3. Otherwise → create .coverage/test-output.log
//
// On any failure to create the log file, falls back to cfg.testOutput().
// Callers must invoke the returned cleanup func when finished.
func (r *runner) openTestOutputLog() (io.Writer, func()) {
	if r.cfg.VerboseTests {
		return r.cfg.testOutput(), func() {}
	}
	if r.cfg.TestOutput != nil {
		return r.cfg.TestOutput, func() {}
	}
	logPath := filepath.Join(r.outputDir, "test-output.log")
	f, err := os.Create(logPath)
	if err != nil {
		return r.cfg.testOutput(), func() {}
	}
	logProgress(r.cfg.stderr(), fmt.Sprintf("Test output → %s", logPath))
	return f, func() { _ = f.Close() }
}
