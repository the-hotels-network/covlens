package covlens

import (
	"testing"

	"golang.org/x/tools/cover"

	"github.com/the-hotels-network/covlens/internal/coverage"
)

func TestResolvePackages(t *testing.T) {
	r := &runner{}
	subjects := coverageSubjects{files: []fileState{
		{path: "a.go", pkg: "ex.com/x/a", modRoot: "/repo"},
		{path: "b.go", pkg: "ex.com/x/a", modRoot: "/repo"}, // dup pkg, same root
		{path: "c.go", pkg: "ex.com/x/b", modRoot: "/repo"},
		{path: "d.go", pkg: "ex.com/y", modRoot: "/repo/y"}, // separate module
		{path: "e.go", excluded: true, pkg: "ex.com/excl", modRoot: "/repo"},
		{path: "f.go", pkg: "", modRoot: "/repo"},      // no pkg → skip
		{path: "g.go", pkg: "ex.com/x/a", modRoot: ""}, // no modRoot → skip
	}}

	targets := r.resolvePackages(subjects)

	if len(targets.grouped) != 2 {
		t.Fatalf("grouped: got %d roots, want 2: %v", len(targets.grouped), targets.grouped)
	}
	repoPkgs := targets.grouped["/repo"]
	var aCount int
	for _, p := range repoPkgs {
		if p == "ex.com/x/a" {
			aCount++
		}
	}
	if aCount != 1 {
		t.Errorf(`grouped["/repo"]: ex.com/x/a appears %d times, want 1 (dedup)`, aCount)
	}
	if len(repoPkgs) != 2 {
		t.Errorf(`grouped["/repo"]: got %v, want 2 unique packages`, repoPkgs)
	}
	if yPkgs := targets.grouped["/repo/y"]; len(yPkgs) != 1 || yPkgs[0] != "ex.com/y" {
		t.Errorf(`grouped["/repo/y"] = %v, want [ex.com/y]`, yPkgs)
	}
	if len(targets.moduleRoots) != 2 {
		t.Errorf("moduleRoots: got %d, want 2: %v", len(targets.moduleRoots), targets.moduleRoots)
	}
}

func TestBuildReport_ExcludedFile(t *testing.T) {
	r := &runner{cfg: Config{DiffThreshold: 80, TotalThreshold: 70}}
	subjects := coverageSubjects{files: []fileState{
		{path: "mocks.go", excluded: true, pkg: "ex.com/m"},
	}}
	rep := r.buildReport(coverageScope{}, subjects, coverageProfiles{}, coverageStats{})
	if len(rep.Files) != 1 {
		t.Fatalf("Files: got %d, want 1", len(rep.Files))
	}
	f := rep.Files[0]
	if !f.Excluded || f.Coverage != -1 {
		t.Errorf("got %+v, want Excluded=true Coverage=-1", f)
	}
	if len(rep.Sources) != 0 {
		t.Errorf("Sources: got %d, want 0 (excluded files don't render)", len(rep.Sources))
	}
}

func TestBuildReport_DeletedFileOmitted(t *testing.T) {
	r := &runner{cfg: Config{}}
	subjects := coverageSubjects{files: []fileState{
		{path: "removed.go", deleted: true, pkg: "ex.com/r"},
		{path: "kept.go", pkg: "ex.com/k", profileKey: "ex.com/k/kept.go"},
	}}
	stats := coverageStats{
		fileResults: map[string]coverage.FileResult{"ex.com/k/kept.go": {Stmts: 4, Covered: 4}},
	}
	rep := r.buildReport(coverageScope{}, subjects, coverageProfiles{}, stats)
	for _, f := range rep.Files {
		if f.Path == "removed.go" {
			t.Error("deleted file appears in Files; should be omitted")
		}
	}
	for _, s := range rep.Sources {
		if s.Path == "removed.go" {
			t.Error("deleted file appears in Sources; should be omitted")
		}
	}
}

func TestBuildReport_NoProfileDataGetsWarn(t *testing.T) {
	r := &runner{cfg: Config{DiffThreshold: 80}}
	subjects := coverageSubjects{files: []fileState{
		{path: "no_data.go", pkg: "ex.com/n", profileKey: "ex.com/n/no_data.go"},
	}}
	rep := r.buildReport(coverageScope{}, subjects, coverageProfiles{}, coverageStats{
		fileResults: map[string]coverage.FileResult{},
	})
	f0 := rep.Files[0]
	if len(rep.Files) != 1 || f0.StatusFor(80) != "warn" || f0.Coverage != -1 {
		t.Errorf("got %+v, want one entry status=warn Coverage=-1", rep.Files)
	}
}

func TestBuildReport_RatchetPolicy(t *testing.T) {
	cases := []struct {
		name           string
		ratchet        bool
		totalCov       float64
		baselineCov    float64
		totalThreshold float64
		want           bool
	}{
		{"ratchet pass: drop within 0.1pp", true, 79.95, 80, 0, true},
		{"ratchet fail: drop exceeds 0.1pp", true, 75, 80, 0, false},
		{"ratchet with zero baseline falls back to threshold", true, 80, 0, 70, true},
		{"no ratchet: above threshold", false, 80, 0, 70, true},
		{"no ratchet: below threshold", false, 60, 0, 70, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &runner{cfg: Config{RatchetTotal: tc.ratchet, TotalThreshold: tc.totalThreshold}}
			stats := coverageStats{totalCov: tc.totalCov, baselineCov: tc.baselineCov}
			rep := r.buildReport(coverageScope{}, coverageSubjects{}, coverageProfiles{}, stats)
			if rep.TotalPassed != tc.want {
				t.Errorf("TotalPassed = %v, want %v (totalCov=%v baseline=%v threshold=%v ratchet=%v)",
					rep.TotalPassed, tc.want, tc.totalCov, tc.baselineCov, tc.totalThreshold, tc.ratchet)
			}
		})
	}
}

func TestBuildReport_SourcesCarryBlocksAndHunks(t *testing.T) {
	r := &runner{cfg: Config{}}
	block := cover.ProfileBlock{StartLine: 10, EndLine: 20, NumStmt: 3, Count: 1}
	diffProf := &cover.Profile{FileName: "ex.com/p/file.go", Blocks: []cover.ProfileBlock{block}}
	subjects := coverageSubjects{files: []fileState{
		{path: "file.go", pkg: "ex.com/p", profileKey: "ex.com/p/file.go"},
	}}
	profiles := coverageProfiles{diffProfiles: []*cover.Profile{diffProf}}
	stats := coverageStats{
		fileResults: map[string]coverage.FileResult{"ex.com/p/file.go": {Stmts: 3, Covered: 3}},
		fileHunks: map[string][]coverage.LineRange{
			"ex.com/p/file.go": {{Start: 15, End: 18}},
		},
	}
	rep := r.buildReport(coverageScope{}, subjects, profiles, stats)
	if len(rep.Sources) != 1 {
		t.Fatalf("Sources: got %d, want 1", len(rep.Sources))
	}
	src := rep.Sources[0]
	if src.Path != "file.go" || src.Package != "ex.com/p" {
		t.Errorf("Source path/pkg: got %q/%q, want file.go/ex.com/p", src.Path, src.Package)
	}
	if len(src.Blocks) != 1 || src.Blocks[0] != block {
		t.Errorf("Source.Blocks = %+v, want [%+v]", src.Blocks, block)
	}
	if len(src.Hunks) != 1 || src.Hunks[0].Start != 15 || src.Hunks[0].End != 18 {
		t.Errorf("Source.Hunks = %+v, want [{15 18}]", src.Hunks)
	}
}
