package covlens

import (
	"io"
	"os"

	"golang.org/x/tools/cover"

	"github.com/the-hotels-network/covlens/internal/coverage"
)

// baselineTotalCoverage checks out scope.mergeBase into a temporary worktree,
// runs project-wide total coverage there, and returns the percentage.
//
// Modules are re-discovered inside the worktree rather than reused from HEAD
// so that modules added or removed between merge-base and HEAD don't skew the
// comparison: the baseline reflects what the project actually looked like.
func (r *runner) baselineTotalCoverage(scope coverageScope) (float64, error) {
	tmpDir, err := os.MkdirTemp("", "covlens-baseline-*")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(tmpDir)

	if err := r.git.AddWorktree(r.ctx, tmpDir, scope.mergeBase); err != nil {
		return 0, err
	}
	defer r.git.RemoveWorktree(tmpDir)

	baseRoots, err := findAllModuleRoots(tmpDir)
	if err != nil {
		return 0, err
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
