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
	"runtime"
	"strings"

	"golang.org/x/tools/cover"

	"github.com/erioch/covlens/coverage"
	"github.com/erioch/covlens/directive"
	"github.com/erioch/covlens/git"
	"github.com/erioch/covlens/packages"
	"github.com/erioch/covlens/report"
)

// Run executes the full coverage analysis pipeline.
func Run(ctx context.Context, cfg Config) (*Report, error) {
	// 1. Validate and prepare
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

	// 2. Git operations
	gc := &git.Client{WorkDir: cfg.WorkDir}
	gitRoot, err := gc.Root(ctx)
	if err != nil {
		return nil, fmt.Errorf("finding git root: %w", err)
	}
	if err := gc.VerifyBranch(ctx, cfg.BaseBranch); err != nil {
		return nil, err
	}
	mergeBase, err := gc.MergeBase(ctx, "HEAD", cfg.BaseBranch)
	if err != nil {
		return nil, err
	}

	// 3. Changed files (relative to git root)
	changedFiles, err := gc.ChangedFiles(ctx, mergeBase)
	if err != nil {
		return nil, fmt.Errorf("detecting changed files: %w", err)
	}
	if len(changedFiles) == 0 {
		return &Report{DiffPassed: true, TotalPassed: true}, nil
	}

	// 4. Compile exclude regexps
	var excludeRes []*regexp.Regexp
	for _, pat := range cfg.ExcludeFiles {
		re, _ := regexp.Compile(pat) // already validated
		excludeRes = append(excludeRes, re)
	}

	// 5. Categorize files: excluded vs included, parse directives
	type fileState struct {
		path         string
		excluded     bool
		profileKey   string               // "import/path/file.go"
		pkg          string               // import path of the package
		funcExcluded []directive.FuncSpan // function-level exclusions
	}

	profileKeyCache := make(map[string]string) // absDir → importPath
	var allFiles []fileState

	for _, f := range changedFiles {
		fs := fileState{path: f}

		// Check regex exclusion
		for _, re := range excludeRes {
			if re.MatchString(f) {
				fs.excluded = true
				break
			}
		}

		// Resolve profile key (import_path/basename)
		fs.profileKey, fs.pkg = resolveProfileKey(ctx, gitRoot, f, profileKeyCache)

		// Parse directives for non-excluded files
		if !fs.excluded {
			absPath := filepath.Join(gitRoot, f)
			if excl, err := directive.Parse(absPath); err == nil {
				if excl.WholeFile {
					fs.excluded = true
				} else {
					fs.funcExcluded = excl.Functions
				}
			}
			// parse failure → skip directives, continue
		}

		allFiles = append(allFiles, fs)
	}

	// 6. Resolve packages from non-excluded files
	var includedFiles []string
	for _, fs := range allFiles {
		if !fs.excluded {
			includedFiles = append(includedFiles, fs.path)
		}
	}

	pkgs, err := packages.Resolve(ctx, gitRoot, includedFiles)
	if err != nil {
		return nil, fmt.Errorf("resolving packages: %w", err)
	}
	grouped := packages.GroupByModule(pkgs)

	moduleRoots := make([]string, 0, len(grouped))
	for root := range grouped {
		moduleRoots = append(moduleRoots, root)
	}

	// 7. Run tests
	log("Running total coverage...")
	totalProfilePath, err := coverage.RunTotal(ctx, moduleRoots, outputDir, os.Stdout)
	if err != nil {
		return nil, fmt.Errorf("running total coverage: %w", err)
	}

	log("Running diff coverage...")
	diffProfilePath, err := coverage.RunDiff(ctx, grouped, outputDir, os.Stdout)
	if err != nil {
		return nil, fmt.Errorf("running diff coverage: %w", err)
	}

	// 8. Compute total coverage
	totalCov, _ := coverage.TotalCoverage(totalProfilePath) // error means 0% coverage, safe to proceed

	// Ratchet: compute baseline total coverage from the merge-base worktree
	var baselineCov float64
	if cfg.RatchetTotal {
		log("Computing baseline total coverage at merge-base...")
		baselineCov, err = baselineTotalCoverage(ctx, gc, mergeBase, gitRoot, moduleRoots, outputDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not compute baseline coverage, falling back to threshold: %v\n", err)
		}
	}

	// 9. Build hunk maps and exclusion maps for diff coverage
	diffProfiles, _ := cover.ParseProfiles(diffProfilePath) // empty profiles produce zero coverage, safe to proceed

	fileHunks := make(map[string][]git.Hunk)
	excludedRanges := make(map[string][]git.Hunk)

	for _, fs := range allFiles {
		if fs.excluded || fs.profileKey == "" {
			continue
		}

		hunks, err := gc.DiffHunks(ctx, mergeBase, fs.path)
		if err != nil {
			continue
		}
		fileHunks[fs.profileKey] = hunks

		// Convert function-level exclusions to Hunk ranges
		for _, fn := range fs.funcExcluded {
			excludedRanges[fs.profileKey] = append(
				excludedRanges[fs.profileKey],
				git.Hunk{Start: fn.StartLine, End: fn.EndLine},
			)
		}
	}

	// 10. Compute filtered diff coverage (aggregate + per-file counts)
	diffStmts, diffCovered, fileResults := coverage.FilteredCoverage(diffProfiles, fileHunks, excludedRanges)
	var diffCov float64
	if diffStmts > 0 {
		diffCov = float64(diffCovered) / float64(diffStmts) * 100
	}

	// 11. Build FileCoverage entries using diff-only coverage per file
	var fileCoverages []FileCoverage
	for _, fs := range allFiles {
		fc := FileCoverage{
			Path:    fs.path,
			Package: fs.pkg,
		}

		if fs.excluded {
			fc.Excluded = true
			fc.Status = "excluded"
			fc.Coverage = -1
		} else if r, ok := fileResults[fs.profileKey]; ok {
			fc.Statements = r.Stmts
			fc.Covered = r.Covered
			fc.Coverage = float64(r.Covered) / float64(r.Stmts) * 100
			fc.Status = fileStatusFor(fc.Coverage, cfg.DiffThreshold)
		} else {
			fc.Coverage = -1
			fc.Status = "warn"
		}

		fileCoverages = append(fileCoverages, fc)
	}

	totalPassed := totalCov >= cfg.TotalThreshold
	if cfg.RatchetTotal {
		if baselineCov > 0 {
			// Pass if total coverage hasn't dropped by more than 0.1pp.
			totalPassed = totalCov >= baselineCov-0.1
		}
		// If baseline could not be computed, fall through to threshold check above.
	}

	r := &Report{
		DiffCoverage:          diffCov,
		TotalCoverage:         totalCov,
		BaselineTotalCoverage: baselineCov,
		DiffPassed:            diffCov >= cfg.DiffThreshold,
		TotalPassed:           totalPassed,
		Files:                 fileCoverages,
	}

	// 13. Render source files for HTML report (diff view: only changed lines ± context)
	var sourceFiles []report.SourceFile
	profileMap := make(map[string]*cover.Profile)
	for _, p := range diffProfiles {
		profileMap[p.FileName] = p
	}

	for _, fs := range allFiles {
		if fs.excluded || fs.profileKey == "" {
			continue
		}
		absPath := filepath.Join(gitRoot, fs.path)
		var blocks []cover.ProfileBlock
		if p, ok := profileMap[fs.profileKey]; ok {
			blocks = p.Blocks
		}

		// Convert git.Hunk → report.Hunk for the source renderer.
		var rHunks []report.Hunk
		for _, h := range fileHunks[fs.profileKey] {
			rHunks = append(rHunks, report.Hunk{Start: h.Start, End: h.End})
		}

		// Diff coverage for the source viewer header.
		var diffFileCov float64 = -1
		if r, ok := fileResults[fs.profileKey]; ok && r.Stmts > 0 {
			diffFileCov = float64(r.Covered) / float64(r.Stmts) * 100
		}

		html, err := report.RenderSource(absPath, blocks, rHunks)
		if err != nil {
			continue
		}
		sourceFiles = append(sourceFiles, report.SourceFile{
			Path:       fs.path,
			Package:    fs.pkg,
			SourceHTML: html,
			Coverage:   diffFileCov,
			Status:     fileStatusFor(diffFileCov, cfg.DiffThreshold),
		})
	}

	// 14. Generate HTML report
	reportPath := filepath.Join(outputDir, "coverage_report.html")

	// Convert FileCoverage to report.FileSummary
	var fileSummaries []report.FileSummary
	for _, fc := range fileCoverages {
		fileSummaries = append(fileSummaries, report.FileSummary{
			Path:       fc.Path,
			Package:    fc.Package,
			Coverage:   fc.Coverage,
			Statements: fc.Statements,
			Covered:    fc.Covered,
			Excluded:   fc.Excluded,
			Status:     fc.Status,
		})
	}

	reportInput := report.ReportInput{
		DiffCoverage:          r.DiffCoverage,
		TotalCoverage:         r.TotalCoverage,
		BaselineTotalCoverage: r.BaselineTotalCoverage,
		DiffPassed:            r.DiffPassed,
		TotalPassed:           r.TotalPassed,
		DiffThreshold:         cfg.DiffThreshold,
		TotalThreshold:        cfg.TotalThreshold,
		BaseBranch:            cfg.BaseBranch,
		ShowExcluded:          cfg.ShowExcluded,
		RatchetTotal:          cfg.RatchetTotal,
		Theme:                 cfg.Theme,
		Files:                 fileSummaries,
	}

	if err := report.Generate(reportInput, sourceFiles, reportPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to generate HTML report: %v\n", err)
	} else {
		r.ReportPath = reportPath
	}

	// 15. Auto-open browser
	if cfg.AutoOpen && r.ReportPath != "" {
		openBrowser(r.ReportPath)
	}

	return r, nil
}

// runFull runs a full-project coverage scan without requiring any git diff.
// Every instrumented file is listed in the report with complete source view.
func runFull(ctx context.Context, cfg Config, outputDir string) (*Report, error) {
	moduleRoots, err := findAllModuleRoots(cfg.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("finding module roots: %w", err)
	}

	log("Running full coverage...")
	profilePath, err := coverage.RunTotal(ctx, moduleRoots, outputDir, os.Stdout)
	if err != nil {
		return nil, fmt.Errorf("running coverage: %w", err)
	}

	totalCov, _ := coverage.TotalCoverage(profilePath)   // error means 0% coverage, safe to proceed
	totalProfiles, _ := cover.ParseProfiles(profilePath) // empty profiles produce no file entries

	modPathMap, err := buildModulePathMap(ctx, moduleRoots)
	if err != nil {
		return nil, fmt.Errorf("building module path map: %w", err)
	}

	var fileCoverages []FileCoverage
	var sourceFiles []report.SourceFile
	var fileSummaries []report.FileSummary

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
		fileSummaries = append(fileSummaries, report.FileSummary{
			Path:       relPath,
			Package:    pkg,
			Coverage:   cov,
			Statements: stmts,
			Covered:    covered,
			Status:     status,
		})

		// nil hunks → RenderSource shows the full file
		html, err := report.RenderSource(absPath, p.Blocks, nil)
		if err != nil {
			continue
		}
		sourceFiles = append(sourceFiles, report.SourceFile{
			Path:       relPath,
			Package:    pkg,
			SourceHTML: html,
			Coverage:   cov,
			Status:     status,
		})
	}

	r := &Report{
		TotalCoverage: totalCov,
		TotalPassed:   totalCov >= cfg.TotalThreshold,
		DiffPassed:    true,
		Files:         fileCoverages,
	}

	reportPath := filepath.Join(outputDir, "coverage_report.html")
	reportInput := report.ReportInput{
		TotalCoverage:  r.TotalCoverage,
		TotalPassed:    r.TotalPassed,
		DiffPassed:     true,
		TotalThreshold: cfg.TotalThreshold,
		BaseBranch:     cfg.BaseBranch,
		ShowExcluded:   cfg.ShowExcluded,
		Theme:          cfg.Theme,
		FullMode:       true,
		Files:          fileSummaries,
	}
	if err := report.Generate(reportInput, sourceFiles, reportPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to generate HTML report: %v\n", err)
	} else {
		r.ReportPath = reportPath
	}

	if cfg.AutoOpen && r.ReportPath != "" {
		openBrowser(r.ReportPath)
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

	profilePath, err := coverage.RunTotal(ctx, baseRoots, baseOutputDir, io.Discard)
	if err != nil {
		return 0, err
	}
	return coverage.TotalCoverage(profilePath)
}

// resolveProfileKey returns the coverage profile key and import path for a
// git-relative file path. Profile keys have the form "import/path/file.go".
func resolveProfileKey(ctx context.Context, gitRoot, filePath string, cache map[string]string) (profileKey, importPath string) {
	absDir := filepath.Join(gitRoot, filepath.Dir(filePath))

	imp, ok := cache[absDir]
	if !ok {
		var err error
		imp, err = packages.ImportPath(ctx, absDir, gitRoot)
		if err != nil {
			cache[absDir] = ""
			return "", ""
		}
		cache[absDir] = imp
	}

	if imp == "" {
		return "", ""
	}
	return imp + "/" + filepath.Base(filePath), imp
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

func openBrowser(path string) {
	url := "file://" + path
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	_ = cmd.Start() // fire-and-forget: browser launch failure is non-fatal
}

func log(msg string) {
	fmt.Fprintf(os.Stderr, "\033[0;34m▶\033[0m %s\n", msg)
}
