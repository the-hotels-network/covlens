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
	"strings"

	"golang.org/x/tools/cover"

	"github.com/erioch/covlens/coverage"
	"github.com/erioch/covlens/directive"
	"github.com/erioch/covlens/git"
	"github.com/erioch/covlens/packages"
	"github.com/erioch/covlens/printers/html"
)

// Run executes the full coverage analysis pipeline.
func Run(ctx context.Context, cfg Config) (*Report, error) {
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

	if cfg.FullMode {
		return runFull(ctx, cfg, outputDir)
	}

	excludeRes, err := cfg.compileExcludes()
	if err != nil {
		return nil, fmt.Errorf("compiling exclude patterns: %w", err)
	}

	s := &runState{
		ctx:        ctx,
		cfg:        cfg,
		outputDir:  outputDir,
		excludeRes: excludeRes,
		pkgCache:   make(map[string]packages.ModulePackage),
	}
	for _, st := range diffPipeline {
		if err := st(s); err != nil {
			return nil, err
		}
		if s.done {
			return s.report, nil
		}
	}
	return s.report, nil
}

// stage is one step in the diff-coverage pipeline. Stages share state via
// runState and may set s.done to short-circuit the remaining stages.
type stage func(*runState) error

// diffPipeline is the ordered list of stages run by Run for diff-mode coverage.
// Pattern: Pipes-and-filters.
var diffPipeline = []stage{
	detectChangedFiles,
	classifyFiles,
	resolvePackages,
	runCoverage,
	computeCoverage,
	buildFileEntries,
	assembleReport,
	finalizeReport,
}

// fileState tracks per-file metadata accumulated through the pipeline.
type fileState struct {
	path         string
	excluded     bool
	profileKey   string               // "import/path/file.go"
	pkg          string               // import path of the package
	modRoot      string               // absolute path to the owning module's go.mod directory
	funcExcluded []directive.FuncSpan // function-level exclusions
}

// runState carries the data flowing through the diff-coverage pipeline.
// Each stage reads from and writes to this struct; setting done=true
// short-circuits the remaining stages.
type runState struct {
	ctx       context.Context
	cfg       Config
	outputDir string
	done      bool

	// derived once before the pipeline starts
	excludeRes []*regexp.Regexp
	pkgCache   map[string]packages.ModulePackage // absDir → import path / module root, shared across stages

	// git
	gc        *git.Client
	gitRoot   string
	mergeBase string

	// files
	changedFiles []string
	allFiles     []fileState

	// packages
	grouped     map[string][]string
	moduleRoots []string

	// coverage
	totalProfilePath string
	diffProfilePath  string
	diffProfiles     []*cover.Profile
	fileHunks        map[string][]git.Hunk
	excludedRanges   map[string][]git.Hunk
	fileResults      map[string]coverage.FileResult

	// numbers
	totalCov    float64
	diffCov     float64
	baselineCov float64
	diffStmts   int
	diffCovered int

	// output
	fileCoverages []FileCoverage
	warnings      []string
	report        *Report
}

func detectChangedFiles(s *runState) error {
	s.gc = &git.Client{WorkDir: s.cfg.WorkDir}
	root, err := s.gc.Root(s.ctx)
	if err != nil {
		return fmt.Errorf("finding git root: %w", err)
	}
	s.gitRoot = root

	if err := s.gc.VerifyBranch(s.ctx, s.cfg.BaseBranch); err != nil {
		return err
	}

	mb, err := s.gc.MergeBase(s.ctx, "HEAD", s.cfg.BaseBranch)
	if err != nil {
		return err
	}
	s.mergeBase = mb

	files, err := s.gc.ChangedFiles(s.ctx, mb)
	if err != nil {
		return fmt.Errorf("detecting changed files: %w", err)
	}
	s.changedFiles = files

	if len(files) == 0 {
		s.report = &Report{DiffPassed: true, TotalPassed: true, OutputDir: s.outputDir}
		s.done = true
	}
	return nil
}

func classifyFiles(s *runState) error {
	for _, f := range s.changedFiles {
		fs := fileState{path: f}

		for _, re := range s.excludeRes {
			if re.MatchString(f) {
				fs.excluded = true
				break
			}
		}

		absDir := filepath.Join(s.gitRoot, filepath.Dir(f))
		if info, err := packages.Lookup(s.ctx, s.gitRoot, absDir, s.pkgCache); err == nil {
			fs.profileKey = info.ImportPath + "/" + filepath.Base(f)
			fs.pkg = info.ImportPath
			fs.modRoot = info.ModuleRoot
		}

		if !fs.excluded {
			absPath := filepath.Join(s.gitRoot, f)
			if excl, err := directive.Parse(absPath); err == nil {
				if excl.WholeFile {
					fs.excluded = true
				} else {
					fs.funcExcluded = excl.Functions
				}
			}
			// parse failure → skip directives, continue
		}

		s.allFiles = append(s.allFiles, fs)
	}
	return nil
}

func resolvePackages(s *runState) error {
	// Reuse the lookups already performed in classifyFiles via s.pkgCache.
	// No second `go list` round is needed — we just group.
	var modPkgs []packages.ModulePackage
	seen := make(map[string]bool)
	for _, fs := range s.allFiles {
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
	s.grouped = packages.GroupByModule(modPkgs)

	s.moduleRoots = make([]string, 0, len(s.grouped))
	for root := range s.grouped {
		s.moduleRoots = append(s.moduleRoots, root)
	}
	return nil
}

func runCoverage(s *runState) error {
	logProgress(s.cfg.stderr(), "Running total coverage...")
	totalRes, err := coverage.RunTotal(s.ctx, s.moduleRoots, s.outputDir, s.cfg.testOutput())
	if err != nil {
		return fmt.Errorf("running total coverage: %w", err)
	}
	s.totalProfilePath = totalRes.ProfilePath
	s.warnings = append(s.warnings, totalRes.Warnings...)

	logProgress(s.cfg.stderr(), "Running diff coverage...")
	diffRes, err := coverage.RunDiff(s.ctx, s.grouped, s.outputDir, s.cfg.testOutput())
	if err != nil {
		return fmt.Errorf("running diff coverage: %w", err)
	}
	s.diffProfilePath = diffRes.ProfilePath
	s.warnings = append(s.warnings, diffRes.Warnings...)
	return nil
}

func computeCoverage(s *runState) error {
	totalCov, err := coverage.TotalCoverage(s.totalProfilePath)
	if err != nil {
		s.warnings = append(s.warnings, fmt.Sprintf("could not parse total coverage profile: %v", err))
	}
	s.totalCov = totalCov

	if s.cfg.RatchetTotal {
		logProgress(s.cfg.stderr(), "Computing baseline total coverage at merge-base...")
		bc, err := baselineTotalCoverage(s.ctx, s.gc, s.mergeBase, s.gitRoot, s.moduleRoots, s.outputDir)
		if err != nil {
			return fmt.Errorf("computing baseline coverage (required by --ratchet): %w", err)
		}
		s.baselineCov = bc
	}

	diffProfiles, err := cover.ParseProfiles(s.diffProfilePath)
	if err != nil {
		s.warnings = append(s.warnings, fmt.Sprintf("could not parse diff coverage profile: %v", err))
	}
	s.diffProfiles = diffProfiles

	s.fileHunks = make(map[string][]git.Hunk)
	s.excludedRanges = make(map[string][]git.Hunk)

	for _, fs := range s.allFiles {
		if fs.excluded || fs.profileKey == "" {
			continue
		}
		hunks, err := s.gc.DiffHunks(s.ctx, s.mergeBase, fs.path)
		if err != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("could not compute diff hunks for %s: %v", fs.path, err))
			continue
		}
		s.fileHunks[fs.profileKey] = hunks

		for _, fn := range fs.funcExcluded {
			s.excludedRanges[fs.profileKey] = append(
				s.excludedRanges[fs.profileKey],
				git.Hunk{Start: fn.StartLine, End: fn.EndLine},
			)
		}
	}

	s.diffStmts, s.diffCovered, s.fileResults = coverage.FilteredCoverage(s.diffProfiles, s.fileHunks, s.excludedRanges)
	if s.diffStmts > 0 {
		s.diffCov = float64(s.diffCovered) / float64(s.diffStmts) * 100
	}
	return nil
}

func buildFileEntries(s *runState) error {
	for _, fs := range s.allFiles {
		fc := FileCoverage{
			Path:    fs.path,
			Package: fs.pkg,
		}

		if fs.excluded {
			fc.Excluded = true
			fc.Status = "excluded"
			fc.Coverage = -1
		} else if r, ok := s.fileResults[fs.profileKey]; ok {
			fc.Statements = r.Stmts
			fc.Covered = r.Covered
			fc.Coverage = float64(r.Covered) / float64(r.Stmts) * 100
			fc.Status = fileStatusFor(fc.Coverage, s.cfg.DiffThreshold)
		} else {
			fc.Coverage = -1
			fc.Status = "warn"
		}

		s.fileCoverages = append(s.fileCoverages, fc)
	}
	return nil
}

func assembleReport(s *runState) error {
	totalPassed := s.totalCov >= s.cfg.TotalThreshold
	if s.cfg.RatchetTotal && s.baselineCov > 0 {
		// Pass if total coverage hasn't dropped by more than 0.1pp.
		totalPassed = s.totalCov >= s.baselineCov-0.1
	}
	// If RatchetTotal is set but baseline could not be computed, fall through to threshold check.

	s.report = &Report{
		DiffCoverage:          s.diffCov,
		TotalCoverage:         s.totalCov,
		BaselineTotalCoverage: s.baselineCov,
		DiffPassed:            s.diffCov >= s.cfg.DiffThreshold,
		TotalPassed:           totalPassed,
		Files:                 s.fileCoverages,
		OutputDir:             s.outputDir,
	}
	return nil
}

func finalizeReport(s *runState) error {
	// Build per-file rendered source for the HTML printer (diff view: only
	// changed lines ± context). Library users who don't want HTML can ignore
	// Report.SourceFiles.
	profileMap := make(map[string]*cover.Profile)
	for _, p := range s.diffProfiles {
		profileMap[p.FileName] = p
	}

	for _, fs := range s.allFiles {
		if fs.excluded || fs.profileKey == "" {
			continue
		}
		absPath := filepath.Join(s.gitRoot, fs.path)
		var blocks []cover.ProfileBlock
		if p, ok := profileMap[fs.profileKey]; ok {
			blocks = p.Blocks
		}

		var rHunks []html.Hunk
		for _, h := range s.fileHunks[fs.profileKey] {
			rHunks = append(rHunks, html.Hunk{Start: h.Start, End: h.End})
		}

		var diffFileCov float64 = -1
		if r, ok := s.fileResults[fs.profileKey]; ok && r.Stmts > 0 {
			diffFileCov = float64(r.Covered) / float64(r.Stmts) * 100
		}

		rendered, err := html.RenderSource(absPath, blocks, rHunks)
		if err != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("could not render source for %s: %v", fs.path, err))
			continue
		}
		s.report.SourceFiles = append(s.report.SourceFiles, html.SourceFile{
			Path:       fs.path,
			Package:    fs.pkg,
			SourceHTML: rendered,
			Coverage:   diffFileCov,
			Status:     fileStatusFor(diffFileCov, s.cfg.DiffThreshold),
		})
	}

	s.report.Warnings = s.warnings
	return nil
}

// runFull runs a full-project coverage scan without requiring any git diff.
// Every instrumented file is listed in the report with complete source view.
func runFull(ctx context.Context, cfg Config, outputDir string) (*Report, error) {
	moduleRoots, err := findAllModuleRoots(cfg.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("finding module roots: %w", err)
	}

	logProgress(cfg.stderr(), "Running full coverage...")
	totalRes, err := coverage.RunTotal(ctx, moduleRoots, outputDir, cfg.testOutput())
	if err != nil {
		return nil, fmt.Errorf("running coverage: %w", err)
	}
	warnings := append([]string{}, totalRes.Warnings...)

	totalCov, err := coverage.TotalCoverage(totalRes.ProfilePath)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("could not parse coverage profile: %v", err))
	}
	totalProfiles, err := cover.ParseProfiles(totalRes.ProfilePath)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("could not parse coverage profile blocks: %v", err))
	}

	modPathMap, err := buildModulePathMap(ctx, moduleRoots)
	if err != nil {
		return nil, fmt.Errorf("building module path map: %w", err)
	}

	var fileCoverages []FileCoverage
	var sourceFiles []html.SourceFile

	for _, p := range totalProfiles {
		absPath := resolveAbsPath(p.FileName, modPathMap)
		if absPath == "" {
			continue
		}
		relPath, _ := filepath.Rel(cfg.WorkDir, absPath) // both paths are absolute; error only on Windows volume mismatch
		pkg := filepath.ToSlash(filepath.Dir(p.FileName))

		var stmts, covered int
		for _, b := range p.Blocks {
			stmts += b.NumStmt
			if b.Count > 0 {
				covered += b.NumStmt
			}
		}
		var cov float64 = -1
		if stmts > 0 {
			cov = float64(covered) / float64(stmts) * 100
		}
		status := fileStatusFor(cov, cfg.TotalThreshold)

		fileCoverages = append(fileCoverages, FileCoverage{
			Path:       relPath,
			Package:    pkg,
			Coverage:   cov,
			Statements: stmts,
			Covered:    covered,
			Status:     status,
		})

		// nil hunks → RenderSource shows the full file
		rendered, err := html.RenderSource(absPath, p.Blocks, nil)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("could not render source for %s: %v", relPath, err))
			continue
		}
		sourceFiles = append(sourceFiles, html.SourceFile{
			Path:       relPath,
			Package:    pkg,
			SourceHTML: rendered,
			Coverage:   cov,
			Status:     status,
		})
	}

	r := &Report{
		TotalCoverage: totalCov,
		TotalPassed:   totalCov >= cfg.TotalThreshold,
		DiffPassed:    true,
		Files:         fileCoverages,
		SourceFiles:   sourceFiles,
		OutputDir:     outputDir,
		Warnings:      warnings,
	}
	return r, nil
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
func resolveAbsPath(profileKey string, modPathMap map[string]string) string {
	for modPath, modRoot := range modPathMap {
		if strings.HasPrefix(profileKey, modPath+"/") {
			rel := profileKey[len(modPath)+1:]
			return filepath.Join(modRoot, filepath.FromSlash(rel))
		}
	}
	return ""
}

// baselineTotalCoverage checks out mergeBase into a temporary worktree,
// runs total coverage there, and returns the percentage.
func baselineTotalCoverage(ctx context.Context, gc *git.Client, mergeBase, gitRoot string, moduleRoots []string, outputDir string) (float64, error) {
	tmpDir, err := os.MkdirTemp("", "covlens-baseline-*")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(tmpDir)

	if err := gc.AddWorktree(ctx, tmpDir, mergeBase); err != nil {
		return 0, err
	}
	defer gc.RemoveWorktree(tmpDir)

	// Translate each module root to its equivalent path inside the worktree.
	baseRoots := make([]string, len(moduleRoots))
	for i, root := range moduleRoots {
		rel, err := filepath.Rel(gitRoot, root)
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

	res, err := coverage.RunTotal(ctx, baseRoots, baseOutputDir, io.Discard)
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
