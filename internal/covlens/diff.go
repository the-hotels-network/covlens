package covlens

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/tools/cover"

	"github.com/the-hotels-network/covlens/internal/coverage"
	"github.com/the-hotels-network/covlens/internal/directive"
	"github.com/the-hotels-network/covlens/internal/packages"
)

// coverageScope is the input to a diff-mode run: the git context and the list
// of files that differ from the base branch.
type coverageScope struct {
	repoRoot     string
	mergeBase    string
	changedFiles []string
}

// coverageSubjects is the set of files under measurement, each annotated with
// its package, module, and exclusion metadata.
type coverageSubjects struct {
	files []fileState
}

// coverageTargets is the set of `go test` invocations needed to produce
// profiles for the subjects: packages grouped by their owning module root.
type coverageTargets struct {
	grouped     map[string][]string
	moduleRoots []string
}

// coverageProfiles is the raw output of `go test -coverprofile`: paths to the
// merged total / diff profiles, plus the parsed diff blocks for downstream use.
type coverageProfiles struct {
	totalProfilePath string
	diffProfilePath  string
	diffProfiles     []*cover.Profile
}

// coverageStats holds the computed numbers and per-file results derived from
// the profiles, with diff hunks and function-level exclusions applied.
type coverageStats struct {
	totalCov, diffCov, baselineCov float64
	diffStmts, diffCovered         int
	fileResults                    map[string]coverage.FileResult
	fileHunks                      map[string][]coverage.LineRange
}

// fileState tracks per-file metadata produced by classifyFiles.
type fileState struct {
	path         string
	excluded     bool
	deleted      bool                 // file was removed in this diff; no on-disk source to render
	profileKey   string               // "import/path/file.go"
	pkg          string               // import path of the package
	modRoot      string               // absolute path to the owning module's go.mod directory
	funcExcluded []directive.FuncSpan // function-level exclusions
}

func (r *runner) detectChangedFiles() (coverageScope, error) {
	var scope coverageScope

	root, err := r.git.Root(r.ctx)
	if err != nil {
		return scope, fmt.Errorf("finding git root: %w", err)
	}
	scope.repoRoot = root

	if err := r.git.VerifyBranch(r.ctx, r.cfg.BaseBranch); err != nil {
		return scope, err
	}

	mb, err := r.git.MergeBase(r.ctx, "HEAD", r.cfg.BaseBranch)
	if err != nil {
		return scope, err
	}
	scope.mergeBase = mb

	files, err := r.git.ChangedFiles(r.ctx, mb)
	if err != nil {
		return scope, fmt.Errorf("detecting changed files: %w", err)
	}
	scope.changedFiles = files
	return scope, nil
}

func (r *runner) classifyFiles(scope coverageScope) coverageSubjects {
	// packages.Lookup failures are intentionally swallowed: files outside any
	// resolvable module still appear in the report with empty profile keys so
	// downstream stages can mark them as "no data" rather than dropping them
	// silently. The signature has no error return because there is no failure
	// path the caller can usefully act on.
	pkgCache := make(map[string]packages.ModulePackage)
	var subjects coverageSubjects
	for _, f := range scope.changedFiles {
		fs := fileState{path: f}
		absPath := filepath.Join(scope.repoRoot, f)

		// Detect deletions early: the file appears in the git diff but no
		// longer exists in the working tree. Downstream stages skip these
		// — there are no profile blocks to filter, no source to render —
		// but classifyExclusion / packages.Lookup are also unsafe to run
		// on a missing path.
		if _, err := os.Stat(absPath); err != nil {
			fs.deleted = true
			subjects.files = append(subjects.files, fs)
			continue
		}

		excl := classifyExclusion(f, absPath, r.excludeRes)
		fs.excluded = excl.excluded
		fs.funcExcluded = excl.funcExcluded

		absDir := filepath.Join(scope.repoRoot, filepath.Dir(f))
		if info, err := packages.Lookup(r.ctx, scope.repoRoot, absDir, pkgCache); err == nil {
			fs.profileKey = info.ImportPath + "/" + filepath.Base(f)
			fs.pkg = info.ImportPath
			fs.modRoot = info.ModuleRoot
		}

		subjects.files = append(subjects.files, fs)
	}
	return subjects
}

func (r *runner) resolvePackages(subjects coverageSubjects) coverageTargets {
	// Reuses the lookups already performed in classifyFiles — no second
	// `go list` round, just grouping.
	var modPkgs []packages.ModulePackage
	seen := make(map[string]bool)
	for _, fs := range subjects.files {
		if fs.excluded || fs.modRoot == "" || fs.pkg == "" {
			continue
		}
		key := fs.modRoot + "::" + fs.pkg
		if seen[key] {
			continue
		}
		seen[key] = true
		modPkgs = append(modPkgs, packages.ModulePackage{
			ImportPath: fs.pkg,
			ModuleRoot: fs.modRoot,
		})
	}
	grouped := packages.GroupByModule(modPkgs)

	moduleRoots := make([]string, 0, len(grouped))
	for root := range grouped {
		moduleRoots = append(moduleRoots, root)
	}
	return coverageTargets{grouped: grouped, moduleRoots: moduleRoots}
}

func (r *runner) runCoverage(targets coverageTargets) (coverageProfiles, error) {
	var profiles coverageProfiles

	testOut, closeLog := r.openTestOutputLog()
	defer closeLog()

	logProgress(r.cfg.stderr(), "Running total coverage...")
	totalRes, err := coverage.RunTotal(r.ctx, targets.moduleRoots, r.outputDir, testOut)
	if err != nil {
		return profiles, fmt.Errorf("running total coverage: %w", err)
	}
	profiles.totalProfilePath = totalRes.ProfilePath

	logProgress(r.cfg.stderr(), "Running diff coverage...")
	diffRes, err := coverage.RunDiff(r.ctx, targets.grouped, r.outputDir, testOut)
	if err != nil {
		return profiles, fmt.Errorf("running diff coverage: %w", err)
	}
	profiles.diffProfilePath = diffRes.ProfilePath

	parsed, err := cover.ParseProfiles(diffRes.ProfilePath)
	if err != nil {
		return profiles, fmt.Errorf("parsing diff coverage profile: %w", err)
	}
	profiles.diffProfiles = parsed
	return profiles, nil
}

func (r *runner) computeStats(scope coverageScope, subjects coverageSubjects, targets coverageTargets, profiles coverageProfiles) (coverageStats, error) {
	var stats coverageStats

	totalProfiles, err := cover.ParseProfiles(profiles.totalProfilePath)
	if err != nil {
		return stats, fmt.Errorf("parsing total coverage profile: %w", err)
	}
	modPathMap, err := buildModulePathMap(r.ctx, targets.moduleRoots)
	if err != nil {
		return stats, fmt.Errorf("building module path map: %w", err)
	}
	stats.totalCov = aggregateFiltered(totalProfiles, r.regexExcluder(modPathMap, r.cfg.WorkDir))

	if r.cfg.RatchetTotal {
		logProgress(r.cfg.stderr(), "Computing baseline total coverage at merge-base...")
		bc, err := r.baselineTotalCoverage(scope, targets)
		if err != nil {
			return stats, fmt.Errorf("computing baseline coverage (required by --ratchet): %w", err)
		}
		stats.baselineCov = bc
	}

	stats.fileHunks = make(map[string][]coverage.LineRange)
	excludedRanges := make(map[string][]coverage.LineRange)

	for _, fs := range subjects.files {
		if fs.excluded || fs.deleted || fs.profileKey == "" {
			continue
		}
		hunks, err := r.git.DiffHunks(r.ctx, scope.mergeBase, fs.path)
		if err != nil {
			return stats, fmt.Errorf("computing diff hunks for %s: %w", fs.path, err)
		}
		ranges := make([]coverage.LineRange, len(hunks))
		for i, h := range hunks {
			ranges[i] = coverage.LineRange{Start: h.Start, End: h.End}
		}
		stats.fileHunks[fs.profileKey] = ranges

		for _, fn := range fs.funcExcluded {
			excludedRanges[fs.profileKey] = append(
				excludedRanges[fs.profileKey],
				coverage.LineRange{Start: fn.StartLine, End: fn.EndLine},
			)
		}
	}

	stats.diffStmts, stats.diffCovered, stats.fileResults = coverage.FilteredCoverage(profiles.diffProfiles, stats.fileHunks, excludedRanges)
	if stats.diffStmts > 0 {
		stats.diffCov = float64(stats.diffCovered) / float64(stats.diffStmts) * 100
	}
	return stats, nil
}

// buildReport shapes the compute results into the final *Report. It collapses
// the former buildFileEntries / assembleReport / finalizeReport stages into
// one pure formatting step.
func (r *runner) buildReport(scope coverageScope, subjects coverageSubjects, profiles coverageProfiles, stats coverageStats) *Report {
	files := make([]FileCoverage, 0, len(subjects.files))
	for _, fs := range subjects.files {
		// Deleted files don't appear in the report: there's no source to
		// render and no current coverage to measure. They show up in the
		// diff for accounting reasons; covlens isn't the right tool to
		// surface "you deleted this." (git already does.)
		if fs.deleted {
			continue
		}
		fc := FileCoverage{
			Path:    fs.path,
			Package: fs.pkg,
		}
		if fs.excluded {
			fc.Excluded = true
			fc.Coverage = -1
		} else if res, ok := stats.fileResults[fs.profileKey]; ok {
			fc.Statements = res.Stmts
			fc.Covered = res.Covered
			fc.Coverage = float64(res.Covered) / float64(res.Stmts) * 100
		} else {
			fc.Coverage = -1
		}
		files = append(files, fc)
	}

	measurable := 0
	for _, fs := range subjects.files {
		if !fs.excluded && !fs.deleted {
			measurable++
		}
	}

	onlyDeletions := false
	if len(subjects.files) > 0 {
		onlyDeletions = true
		for _, fs := range subjects.files {
			if !fs.deleted {
				onlyDeletions = false
				break
			}
		}
	}

	totalPassed := stats.totalCov >= r.cfg.TotalThreshold
	if r.cfg.RatchetTotal && stats.baselineCov > 0 {
		// Pass if total coverage hasn't dropped by more than 0.1pp.
		totalPassed = stats.totalCov >= stats.baselineCov-0.1
	}
	// If RatchetTotal is set but baseline could not be computed, fall through to threshold check.

	profileMap := make(map[string]*cover.Profile, len(profiles.diffProfiles))
	for _, p := range profiles.diffProfiles {
		profileMap[p.FileName] = p
	}

	var sources []SourceData
	for _, fs := range subjects.files {
		if fs.excluded || fs.deleted || fs.profileKey == "" {
			continue
		}
		var blocks []cover.ProfileBlock
		if p, ok := profileMap[fs.profileKey]; ok {
			blocks = p.Blocks
		}

		var hunks []Hunk
		for _, h := range stats.fileHunks[fs.profileKey] {
			hunks = append(hunks, Hunk{Start: h.Start, End: h.End})
		}

		sources = append(sources, SourceData{
			Path:    fs.path,
			Package: fs.pkg,
			Blocks:  blocks,
			Hunks:   hunks,
		})
	}

	return &Report{
		DiffCoverage:          stats.diffCov,
		TotalCoverage:         stats.totalCov,
		BaselineTotalCoverage: stats.baselineCov,
		DiffPassed:            measurable == 0 || stats.diffCov >= r.cfg.DiffThreshold,
		TotalPassed:           totalPassed,
		Files:                 files,
		OutputDir:             r.outputDir,
		SourceRoot:            scope.repoRoot,
		Sources:               sources,
		OnlyDeletions:         onlyDeletions,
	}
}
