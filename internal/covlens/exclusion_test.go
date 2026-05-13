package covlens

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestFileCoverage_StatusFor(t *testing.T) {
	cases := []struct {
		name      string
		fc        FileCoverage
		threshold float64
		want      string
	}{
		{"excluded", FileCoverage{Excluded: true, Coverage: -1}, 80, "excluded"},
		{"no data", FileCoverage{Coverage: -1}, 80, "warn"},
		{"above threshold", FileCoverage{Coverage: 90}, 80, "ok"},
		{"exactly at threshold", FileCoverage{Coverage: 80}, 80, "ok"},
		{"below threshold", FileCoverage{Coverage: 79.9}, 80, "fail"},
		{"zero coverage zero threshold", FileCoverage{Coverage: 0}, 0, "ok"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fc.StatusFor(tc.threshold); got != tc.want {
				t.Errorf("StatusFor(%v) = %q, want %q", tc.threshold, got, tc.want)
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
