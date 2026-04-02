package coverage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProfile(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "test.out")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTotalCoverage_Basic(t *testing.T) {
	dir := t.TempDir()
	// 2 statements in block 1 (covered), 3 in block 2 (not covered) = 2/5 = 40%
	prof := writeProfile(t, dir, `mode: atomic
pkg/foo.go:1.1,5.1 2 1
pkg/foo.go:6.1,10.1 3 0
`)
	cov, err := TotalCoverage(prof)
	if err != nil {
		t.Fatal(err)
	}
	if cov != 40 {
		t.Errorf("got %.2f, want 40.00", cov)
	}
}

func TestTotalCoverage_AllCovered(t *testing.T) {
	dir := t.TempDir()
	prof := writeProfile(t, dir, `mode: atomic
pkg/foo.go:1.1,5.1 2 3
pkg/foo.go:6.1,10.1 3 1
`)
	cov, err := TotalCoverage(prof)
	if err != nil {
		t.Fatal(err)
	}
	if cov != 100 {
		t.Errorf("got %.2f, want 100.00", cov)
	}
}

func TestTotalCoverage_Empty(t *testing.T) {
	dir := t.TempDir()
	prof := writeProfile(t, dir, "mode: atomic\n")
	cov, err := TotalCoverage(prof)
	if err != nil {
		t.Fatal(err)
	}
	if cov != 0 {
		t.Errorf("got %.2f, want 0.00", cov)
	}
}

func TestPerFileCoverage(t *testing.T) {
	dir := t.TempDir()
	prof := writeProfile(t, dir, `mode: atomic
pkg/a.go:1.1,5.1 2 1
pkg/a.go:6.1,10.1 2 0
pkg/b.go:1.1,5.1 3 3
`)
	m, err := PerFileCoverage(prof)
	if err != nil {
		t.Fatal(err)
	}
	// a.go: 2/4 = 50%
	if got := m["pkg/a.go"]; got != 50 {
		t.Errorf("a.go got %.2f, want 50.00", got)
	}
	// b.go: 3/3 = 100%
	if got := m["pkg/b.go"]; got != 100 {
		t.Errorf("b.go got %.2f, want 100.00", got)
	}
}

func TestMergeProfiles(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.out")
	p2 := filepath.Join(dir, "b.out")
	os.WriteFile(p1, []byte("mode: atomic\npkg/a.go:1.1,5.1 2 1\n"), 0o644)
	os.WriteFile(p2, []byte("mode: atomic\npkg/b.go:1.1,5.1 3 1\n"), 0o644)

	dst := filepath.Join(dir, "merged.out")
	if err := MergeProfiles(dst, []string{p1, p2}); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(dst)
	content := string(data)
	// Should have exactly one mode: header
	if count := strings.Count(content, "mode:"); count != 1 {
		t.Errorf("expected 1 mode header, got %d", count)
	}
	// Should contain both files' data
	if !strings.Contains(content, "pkg/a.go") || !strings.Contains(content, "pkg/b.go") {
		t.Error("merged profile missing file data")
	}
}
