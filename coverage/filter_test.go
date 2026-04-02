package coverage

import (
	"testing"

	"github.com/erioch/covlens/git"
	"golang.org/x/tools/cover"
)

func block(startLine, endLine, numStmt, count int) cover.ProfileBlock {
	return cover.ProfileBlock{
		StartLine: startLine, StartCol: 1,
		EndLine: endLine, EndCol: 1,
		NumStmt: numStmt, Count: count,
	}
}

func TestFilteredCoverage_BlockInsideHunk(t *testing.T) {
	profiles := []*cover.Profile{{
		FileName: "pkg/foo.go",
		Blocks:   []cover.ProfileBlock{block(10, 15, 3, 1)},
	}}
	hunks := map[string][]git.Hunk{"pkg/foo.go": {{Start: 8, End: 20}}}
	stmts, covered, _ := FilteredCoverage(profiles, hunks, nil)
	if stmts != 3 || covered != 3 {
		t.Errorf("got stmts=%d covered=%d, want 3, 3", stmts, covered)
	}
}

func TestFilteredCoverage_BlockOutsideAllHunks(t *testing.T) {
	profiles := []*cover.Profile{{
		FileName: "pkg/foo.go",
		Blocks:   []cover.ProfileBlock{block(50, 60, 5, 1)},
	}}
	hunks := map[string][]git.Hunk{"pkg/foo.go": {{Start: 1, End: 10}}}
	stmts, covered, _ := FilteredCoverage(profiles, hunks, nil)
	if stmts != 0 || covered != 0 {
		t.Errorf("got stmts=%d covered=%d, want 0, 0", stmts, covered)
	}
}

func TestFilteredCoverage_BlockPartiallyOverlapping(t *testing.T) {
	profiles := []*cover.Profile{{
		FileName: "pkg/foo.go",
		Blocks:   []cover.ProfileBlock{block(8, 12, 2, 1)},
	}}
	hunks := map[string][]git.Hunk{"pkg/foo.go": {{Start: 10, End: 20}}}
	stmts, covered, _ := FilteredCoverage(profiles, hunks, nil)
	if stmts != 2 || covered != 2 {
		t.Errorf("got stmts=%d covered=%d, want 2, 2", stmts, covered)
	}
}

func TestFilteredCoverage_BlockInsideExcludedRange(t *testing.T) {
	profiles := []*cover.Profile{{
		FileName: "pkg/foo.go",
		Blocks:   []cover.ProfileBlock{block(10, 15, 3, 1)},
	}}
	hunks := map[string][]git.Hunk{"pkg/foo.go": {{Start: 1, End: 50}}}
	excluded := map[string][]git.Hunk{"pkg/foo.go": {{Start: 8, End: 20}}}
	stmts, covered, _ := FilteredCoverage(profiles, hunks, excluded)
	if stmts != 0 || covered != 0 {
		t.Errorf("got stmts=%d covered=%d, want 0, 0", stmts, covered)
	}
}

func TestFilteredCoverage_UncoveredBlockInHunk(t *testing.T) {
	profiles := []*cover.Profile{{
		FileName: "pkg/foo.go",
		Blocks:   []cover.ProfileBlock{block(10, 15, 4, 0)},
	}}
	hunks := map[string][]git.Hunk{"pkg/foo.go": {{Start: 1, End: 50}}}
	stmts, covered, _ := FilteredCoverage(profiles, hunks, nil)
	if stmts != 4 || covered != 0 {
		t.Errorf("got stmts=%d covered=%d, want 4, 0", stmts, covered)
	}
}

func TestFilteredCoverage_EmptyProfiles(t *testing.T) {
	stmts, covered, _ := FilteredCoverage(nil, nil, nil)
	if stmts != 0 || covered != 0 {
		t.Errorf("got stmts=%d covered=%d, want 0, 0", stmts, covered)
	}
}

func TestFilteredCoverage_NoHunksForFile(t *testing.T) {
	profiles := []*cover.Profile{{
		FileName: "pkg/bar.go",
		Blocks:   []cover.ProfileBlock{block(1, 10, 5, 5)},
	}}
	hunks := map[string][]git.Hunk{"pkg/other.go": {{Start: 1, End: 100}}}
	stmts, covered, _ := FilteredCoverage(profiles, hunks, nil)
	if stmts != 0 || covered != 0 {
		t.Errorf("got stmts=%d covered=%d, want 0, 0", stmts, covered)
	}
}

func TestFilteredCoverage_PerFileResult(t *testing.T) {
	profiles := []*cover.Profile{
		{FileName: "pkg/a.go", Blocks: []cover.ProfileBlock{block(1, 5, 3, 1)}},
		{FileName: "pkg/b.go", Blocks: []cover.ProfileBlock{block(1, 5, 2, 0)}},
	}
	hunks := map[string][]git.Hunk{
		"pkg/a.go": {{Start: 1, End: 10}},
		"pkg/b.go": {{Start: 1, End: 10}},
	}
	_, _, perFile := FilteredCoverage(profiles, hunks, nil)
	if r := perFile["pkg/a.go"]; r.Stmts != 3 || r.Covered != 3 {
		t.Errorf("a.go: got %+v, want {3 3}", r)
	}
	if r := perFile["pkg/b.go"]; r.Stmts != 2 || r.Covered != 0 {
		t.Errorf("b.go: got %+v, want {2 0}", r)
	}
}

func TestFilteredCoverage_MultipleBlocksMixed(t *testing.T) {
	profiles := []*cover.Profile{{
		FileName: "pkg/foo.go",
		Blocks: []cover.ProfileBlock{
			block(5, 8, 2, 1),   // inside hunk, covered
			block(15, 20, 3, 0), // inside hunk, NOT covered
			block(50, 60, 4, 1), // outside hunk
		},
	}}
	hunks := map[string][]git.Hunk{"pkg/foo.go": {{Start: 1, End: 25}}}
	stmts, covered, _ := FilteredCoverage(profiles, hunks, nil)
	if stmts != 5 || covered != 2 {
		t.Errorf("got stmts=%d covered=%d, want 5, 2", stmts, covered)
	}
}
