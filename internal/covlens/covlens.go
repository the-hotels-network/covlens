package covlens

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/tools/cover"

	"github.com/erioch/covlens/internal/coverage"
	"github.com/erioch/covlens/internal/directive"
	"github.com/erioch/covlens/internal/git"
	"github.com/erioch/covlens/internal/packages"
)

// Run executes a coverage analysis and returns the resulting report.
func Run(ctx context.Context, cfg Config) (*Report, error) {
	r, err := newRunner(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if r.cfg.FullMode {
		return r.runFull()
	}
	return r.run()
}

// runner holds the immutable request scope shared by every phase of a run.
// All fields are set once in newRunner and never mutated afterwards; phase
// outputs flow through return values, not back onto the runner.
type runner struct {
	ctx        context.Context
	cfg        Config
	outputDir  string
	excludeRes []*regexp.Regexp
	git        *git.Client
}

func newRunner(ctx context.Context, cfg Config) (*runner, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	for _, tool := range []string{"git", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			return nil, fmt.Errorf("%s not found on PATH", tool)
		}
	}
	if cfg.WorkDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("getting working directory: %w", err)
		}
		cfg.WorkDir = wd
	}

	outputDir := cfg.OutputDir
	if !filepath.IsAbs(outputDir) {
		outputDir = filepath.Join(cfg.WorkDir, outputDir)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating output directory: %w", err)
	}

	excludeRes, err := cfg.compileExcludes()
	if err != nil {
		return nil, fmt.Errorf("compiling exclude patterns: %w", err)
	}

	return &runner{
		ctx:        ctx,
		cfg:        cfg,
		outputDir:  outputDir,
		excludeRes: excludeRes,
		git:        &git.Client{WorkDir: cfg.WorkDir},
	}, nil
}

// run executes diff-mode coverage analysis: detect changed files vs the base
// branch, classify them, run targeted coverage, and assemble the report.
func (r *runner) run() (*Report, error) {
	scope, err := r.detectChangedFiles()
	if err != nil {
		return nil, err
	}
	if len(scope.changedFiles) == 0 {
		return r.emptyReport(), nil
	}

	subjects, err := r.classifyFiles(scope)
	if err != nil {
		return nil, err
	}

	targets := r.resolvePackages(subjects)

	profiles, err := r.runCoverage(targets)
	if err != nil {
		return nil, err
	}

	stats, err := r.computeStats(scope, subjects, targets, profiles)
	if err != nil {
		return nil, err
	}

	return r.buildReport(scope, subjects, profiles, stats), nil
}

func (r *runner) emptyReport() *Report {
	return &Report{DiffPassed: true, TotalPassed: true, OutputDir: r.outputDir}
}

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
	fileHunks                      map[string][]git.Hunk
}

// fileState tracks per-file metadata produced by classifyFiles.
type fileState struct {
	path         string
	excluded     bool
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

func (r *runner) classifyFiles(scope coverageScope) (coverageSubjects, error) {
	pkgCache := make(map[string]packages.ModulePackage)
	var subjects coverageSubjects
	for _, f := range scope.changedFiles {
		fs := fileState{path: f}
		absPath := filepath.Join(scope.repoRoot, f)

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
	return subjects, nil
}

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

	logProgress(r.cfg.stderr(), "Running total coverage...")
	totalRes, err := coverage.RunTotal(r.ctx, targets.moduleRoots, r.outputDir, r.cfg.testOutput())
	if err != nil {
		return profiles, fmt.Errorf("running total coverage: %w", err)
	}
	profiles.totalProfilePath = totalRes.ProfilePath

	logProgress(r.cfg.stderr(), "Running diff coverage...")
	diffRes, err := coverage.RunDiff(r.ctx, targets.grouped, r.outputDir, r.cfg.testOutput())
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

	totalCov, err := coverage.TotalCoverage(profiles.totalProfilePath)
	if err != nil {
		return stats, fmt.Errorf("parsing total coverage profile: %w", err)
	}
	stats.totalCov = totalCov

	if r.cfg.RatchetTotal {
		logProgress(r.cfg.stderr(), "Computing baseline total coverage at merge-base...")
		bc, err := r.baselineTotalCoverage(scope, targets)
		if err != nil {
			return stats, fmt.Errorf("computing baseline coverage (required by --ratchet): %w", err)
		}
		stats.baselineCov = bc
	}

	stats.fileHunks = make(map[string][]git.Hunk)
	excludedRanges := make(map[string][]git.Hunk)

	for _, fs := range subjects.files {
		if fs.excluded || fs.profileKey == "" {
			continue
		}
		hunks, err := r.git.DiffHunks(r.ctx, scope.mergeBase, fs.path)
		if err != nil {
			return stats, fmt.Errorf("computing diff hunks for %s: %w", fs.path, err)
		}
		stats.fileHunks[fs.profileKey] = hunks

		for _, fn := range fs.funcExcluded {
			excludedRanges[fs.profileKey] = append(
				excludedRanges[fs.profileKey],
				git.Hunk{Start: fn.StartLine, End: fn.EndLine},
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
		fc := FileCoverage{
			Path:    fs.path,
			Package: fs.pkg,
		}
		if fs.excluded {
			fc.Excluded = true
			fc.Status = "excluded"
			fc.Coverage = -1
		} else if res, ok := stats.fileResults[fs.profileKey]; ok {
			fc.Statements = res.Stmts
			fc.Covered = res.Covered
			fc.Coverage = float64(res.Covered) / float64(res.Stmts) * 100
			fc.Status = fileStatusFor(fc.Coverage, r.cfg.DiffThreshold)
		} else {
			fc.Coverage = -1
			fc.Status = "warn"
		}
		files = append(files, fc)
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
		if fs.excluded || fs.profileKey == "" {
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
		DiffPassed:            stats.diffCov >= r.cfg.DiffThreshold,
		TotalPassed:           totalPassed,
		Files:                 files,
		OutputDir:             r.outputDir,
		SourceRoot:            scope.repoRoot,
		Sources:               sources,
	}
}

// runFull runs a full-project coverage scan without requiring any git diff.
// Every instrumented file is listed in the report with complete source view.
// It does not use the diff-mode phase decomposition; the flow is short and
// linear, and it shares only leaf helpers (classifyExclusion, fileStatusFor)
// with the diff path.
func (r *runner) runFull() (*Report, error) {
	moduleRoots, err := findAllModuleRoots(r.cfg.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("finding module roots: %w", err)
	}

	logProgress(r.cfg.stderr(), "Running full coverage...")
	totalRes, err := coverage.RunTotal(r.ctx, moduleRoots, r.outputDir, r.cfg.testOutput())
	if err != nil {
		return nil, fmt.Errorf("running coverage: %w", err)
	}

	totalCov, err := coverage.TotalCoverage(totalRes.ProfilePath)
	if err != nil {
		return nil, fmt.Errorf("parsing coverage profile: %w", err)
	}
	totalProfiles, err := cover.ParseProfiles(totalRes.ProfilePath)
	if err != nil {
		return nil, fmt.Errorf("parsing coverage profile blocks: %w", err)
	}

	modPathMap, err := buildModulePathMap(r.ctx, moduleRoots)
	if err != nil {
		return nil, fmt.Errorf("building module path map: %w", err)
	}

	var fileCoverages []FileCoverage
	var sources []SourceData

	for _, p := range totalProfiles {
		absPath := resolveAbsPath(p.FileName, modPathMap)
		if absPath == "" {
			continue
		}
		relPath, _ := filepath.Rel(r.cfg.WorkDir, absPath) // both paths are absolute; error only on Windows volume mismatch
		pkg := filepath.ToSlash(filepath.Dir(p.FileName))

		excl := classifyExclusion(relPath, absPath, r.excludeRes)
		if excl.excluded {
			fileCoverages = append(fileCoverages, FileCoverage{
				Path:     relPath,
				Package:  pkg,
				Coverage: -1,
				Excluded: true,
				Status:   "excluded",
			})
			continue
		}

		var excludedRanges []git.Hunk
		for _, fn := range excl.funcExcluded {
			excludedRanges = append(excludedRanges, git.Hunk{Start: fn.StartLine, End: fn.EndLine})
		}

		var stmts, covered int
		for _, b := range p.Blocks {
			if len(excludedRanges) > 0 && git.InRange(excludedRanges, b.StartLine, b.EndLine) {
				continue
			}
			stmts += b.NumStmt
			if b.Count > 0 {
				covered += b.NumStmt
			}
		}
		var cov float64 = -1
		if stmts > 0 {
			cov = float64(covered) / float64(stmts) * 100
		}
		status := fileStatusFor(cov, r.cfg.TotalThreshold)

		fileCoverages = append(fileCoverages, FileCoverage{
			Path:       relPath,
			Package:    pkg,
			Coverage:   cov,
			Statements: stmts,
			Covered:    covered,
			Status:     status,
		})

		// nil Hunks signals "render the full file" to printers.
		sources = append(sources, SourceData{
			Path:    relPath,
			Package: pkg,
			Blocks:  p.Blocks,
			Hunks:   nil,
		})
	}

	return &Report{
		TotalCoverage: totalCov,
		TotalPassed:   totalCov >= r.cfg.TotalThreshold,
		DiffPassed:    true,
		Files:         fileCoverages,
		Sources:       sources,
		OutputDir:     r.outputDir,
		SourceRoot:    r.cfg.WorkDir,
	}, nil
}

// findAllModuleRoots walks dir and returns every directory containing a go.mod,
// skipping .git, vendor, and node_modules.
func findAllModuleRoots(dir string) ([]string, error) {
	var roots []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "go.mod" {
			roots = append(roots, filepath.Dir(path))
		}
		return nil
	})
	return roots, err
}

// buildModulePathMap runs `go list -m` in each module root and returns
// a map from module import path to its root directory.
func buildModulePathMap(ctx context.Context, roots []string) (map[string]string, error) {
	m := make(map[string]string)
	for _, root := range roots {
		cmd := exec.CommandContext(ctx, "go", "list", "-m")
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		m[strings.TrimSpace(string(out))] = root
	}
	return m, nil
}

// resolveAbsPath converts a coverage profile key (e.g. "github.com/x/y/pkg/file.go")
// to an absolute filesystem path using the module path map.
//
// In monorepos with nested modules (e.g. example.com/lib and
// example.com/lib/internal), the longer module path must win — otherwise
// resolution depends on map iteration order and is non-deterministic.
func resolveAbsPath(profileKey string, modPathMap map[string]string) string {
	paths := make([]string, 0, len(modPathMap))
	for p := range modPathMap {
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })

	for _, modPath := range paths {
		if strings.HasPrefix(profileKey, modPath+"/") {
			rel := profileKey[len(modPath)+1:]
			return filepath.Join(modPathMap[modPath], filepath.FromSlash(rel))
		}
	}
	return ""
}

// baselineTotalCoverage checks out scope.mergeBase into a temporary worktree,
// runs total coverage there, and returns the percentage.
func (r *runner) baselineTotalCoverage(scope coverageScope, targets coverageTargets) (float64, error) {
	tmpDir, err := os.MkdirTemp("", "covlens-baseline-*")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(tmpDir)

	if err := r.git.AddWorktree(r.ctx, tmpDir, scope.mergeBase); err != nil {
		return 0, err
	}
	defer r.git.RemoveWorktree(tmpDir)

	// Translate each module root to its equivalent path inside the worktree.
	baseRoots := make([]string, len(targets.moduleRoots))
	for i, root := range targets.moduleRoots {
		rel, err := filepath.Rel(scope.repoRoot, root)
		if err != nil {
			return 0, err
		}
		baseRoots[i] = filepath.Join(tmpDir, rel)
	}

	baseOutputDir, err := os.MkdirTemp("", "covlens-baseline-out-*")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(baseOutputDir)

	res, err := coverage.RunTotal(r.ctx, baseRoots, baseOutputDir, io.Discard)
	if err != nil {
		return 0, err
	}
	return coverage.TotalCoverage(res.ProfilePath)
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

func logProgress(w io.Writer, msg string) {
	fmt.Fprintf(w, "\033[0;34m▶\033[0m %s\n", msg)
}
