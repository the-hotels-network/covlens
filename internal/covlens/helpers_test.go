package covlens

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"golang.org/x/tools/cover"
)

func TestFileStatusFor(t *testing.T) {
	cases := []struct {
		name      string
		cov       float64
		threshold float64
		want      string
	}{
		{"no data", -1, 80, "warn"},
		{"above threshold", 90, 80, "ok"},
		{"exactly at threshold", 80, 80, "ok"},
		{"below threshold", 79.9, 80, "fail"},
		{"zero coverage zero threshold", 0, 0, "ok"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fileStatusFor(tc.cov, tc.threshold); got != tc.want {
				t.Errorf("fileStatusFor(%v, %v) = %q, want %q",
					tc.cov, tc.threshold, got, tc.want)
			}
		})
	}
}

func TestClassifyExclusion(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		full := filepath.Join(dir, name)
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return full
	}

	wholeFile := write("whole.go", "//covlens:ignore\n\npackage foo\n\nfunc Foo() {}\n")
	funcLevel := write("func.go", "package foo\n\n//covlens:ignore\nfunc Ignored() {}\n\nfunc Counted() {}\n")
	plain := write("plain.go", "package foo\n\nfunc Foo() {}\n")
	missing := filepath.Join(dir, "does_not_exist.go")

	rxMock := regexp.MustCompile(`^mocks_.*\.go$`)

	cases := []struct {
		name         string
		relPath      string
		absPath      string
		excludeRes   []*regexp.Regexp
		wantExcluded bool
		wantFuncs    int
	}{
		{"regex match short-circuits before file read", "mocks_foo.go", missing, []*regexp.Regexp{rxMock}, true, 0},
		{"no regex, plain file", "plain.go", plain, nil, false, 0},
		{"whole-file directive", "whole.go", wholeFile, nil, true, 0},
		{"function-level directive", "func.go", funcLevel, nil, false, 1},
		{"missing file → parse error swallowed", "missing.go", missing, nil, false, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyExclusion(tc.relPath, tc.absPath, tc.excludeRes)
			if got.excluded != tc.wantExcluded {
				t.Errorf("excluded = %v, want %v", got.excluded, tc.wantExcluded)
			}
			if len(got.funcExcluded) != tc.wantFuncs {
				t.Errorf("funcExcluded count = %d, want %d (%+v)",
					len(got.funcExcluded), tc.wantFuncs, got.funcExcluded)
			}
		})
	}
}

func TestAggregateFiltered(t *testing.T) {
	prof := func(name string, blocks ...cover.ProfileBlock) *cover.Profile {
		return &cover.Profile{FileName: name, Blocks: blocks}
	}
	blk := func(n, c int) cover.ProfileBlock {
		return cover.ProfileBlock{NumStmt: n, Count: c}
	}
	never := func(string) bool { return false }
	skipOne := func(s string) bool { return s == "skip.go" }

	cases := []struct {
		name     string
		profiles []*cover.Profile
		excluded func(string) bool
		want     float64
	}{
		{"empty profiles", nil, never, 0},
		{"all covered", []*cover.Profile{prof("a.go", blk(2, 1), blk(3, 1))}, never, 100},
		{"half covered", []*cover.Profile{prof("a.go", blk(2, 1), blk(2, 0))}, never, 50},
		{"excluded file does not count", []*cover.Profile{prof("a.go", blk(1, 1)), prof("skip.go", blk(10, 0))}, skipOne, 100},
		{"zero statements yields zero, not NaN", []*cover.Profile{prof("a.go", blk(0, 0))}, never, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := aggregateFiltered(tc.profiles, tc.excluded); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRegexExcluder(t *testing.T) {
	r := &runner{
		excludeRes: []*regexp.Regexp{regexp.MustCompile(`mocks_.*\.go$`)},
	}
	modPathMap := map[string]string{"example.com/mod": "/work"}
	pred := r.regexExcluder(modPathMap, "/work")

	cases := []struct {
		name     string
		fileName string
		want     bool
	}{
		{"unresolvable path is treated as excluded", "other.com/x/y.go", true},
		{"resolves and matches exclude regex", "example.com/mod/mocks_foo.go", true},
		{"resolves and does not match", "example.com/mod/foo.go", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pred(tc.fileName); got != tc.want {
				t.Errorf("predicate(%q) = %v, want %v", tc.fileName, got, tc.want)
			}
		})
	}
}
