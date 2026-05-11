package covlens

import (
	"context"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/cover"

	"github.com/erioch/covlens/internal/coverage"
)

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

	testOut, closeLog := r.openTestOutputLog()
	defer closeLog()

	logProgress(r.cfg.stderr(), "Running full coverage...")
	totalRes, err := coverage.RunTotal(r.ctx, moduleRoots, r.outputDir, testOut)
	if err != nil {
		return nil, fmt.Errorf("running coverage: %w", err)
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
	var totalStmts, totalCovered int

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

		var excludedRanges []coverage.LineRange
		for _, fn := range excl.funcExcluded {
			excludedRanges = append(excludedRanges, coverage.LineRange{Start: fn.StartLine, End: fn.EndLine})
		}

		var stmts, covered int
		for _, b := range p.Blocks {
			if len(excludedRanges) > 0 && coverage.InRange(excludedRanges, b.StartLine, b.EndLine) {
				continue
			}
			stmts += b.NumStmt
			if b.Count > 0 {
				covered += b.NumStmt
			}
		}
		totalStmts += stmts
		totalCovered += covered

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

	var totalCov float64
	if totalStmts > 0 {
		totalCov = float64(totalCovered) / float64(totalStmts) * 100
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
