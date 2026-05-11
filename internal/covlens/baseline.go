package covlens

import (
	"io"
	"os"
	"path/filepath"

	"golang.org/x/tools/cover"

	"github.com/erioch/covlens/internal/coverage"
)

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
	profiles, err := cover.ParseProfiles(res.ProfilePath)
	if err != nil {
		return 0, err
	}
	baseModPathMap, err := buildModulePathMap(r.ctx, baseRoots)
	if err != nil {
		return 0, err
	}
	// Filter against tmpDir-rooted paths so the user's exclude patterns
	// (which target git-relative paths) match correctly: at merge-base, the
	// equivalent file lives at tmpDir/<git-rel-path>. Filtering keeps the
	// baseline comparable to the current run, which also filters.
	return aggregateFiltered(profiles, r.regexExcluder(baseModPathMap, tmpDir)), nil
}
