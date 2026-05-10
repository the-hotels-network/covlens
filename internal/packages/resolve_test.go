package packages

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// goListAvailable skips the test if `go` isn't on PATH. Lookup shells out to
// `go list`, which is unavailable in some minimal environments.
func goListAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
}

// writeModuleWithPackage materializes a minimal Go module at root with one
// importable subpackage at pkg/<name>. Returns the absolute path to the
// subpackage directory.
func writeModuleWithPackage(t *testing.T, root, modulePath, name string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module "+modulePath+"\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "pkg", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".go"),
		[]byte("package "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// helper creates a file (and any parent dirs) inside base.
func touchFile(t *testing.T, base string, relPath string) {
	t.Helper()
	full := filepath.Join(base, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindModRoot_DirectParent(t *testing.T) {
	root := t.TempDir()
	touchFile(t, root, "svc/go.mod")
	dir := filepath.Join(root, "svc", "handler")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := FindModRoot(dir, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != filepath.Join(root, "svc") {
		t.Errorf("got %s, want %s", got, filepath.Join(root, "svc"))
	}
}

func TestFindModRoot_SeveralLevelsUp(t *testing.T) {
	root := t.TempDir()
	touchFile(t, root, "svc/go.mod")
	dir := filepath.Join(root, "svc", "internal", "handler", "v2")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := FindModRoot(dir, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != filepath.Join(root, "svc") {
		t.Errorf("got %s, want %s", got, filepath.Join(root, "svc"))
	}
}

func TestFindModRoot_AtGitRoot(t *testing.T) {
	root := t.TempDir()
	touchFile(t, root, "go.mod")
	dir := filepath.Join(root, "pkg", "util")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := FindModRoot(dir, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != root {
		t.Errorf("got %s, want %s", got, root)
	}
}

func TestFindModRoot_NoGoMod(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "pkg", "util")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := FindModRoot(dir, root)
	if err == nil {
		t.Fatal("expected error when no go.mod exists, got nil")
	}
}

func TestGroupByModule_MultipleModules(t *testing.T) {
	pkgs := []ModulePackage{
		{ImportPath: "example.com/svc/handler", ModuleRoot: "/repo/svc"},
		{ImportPath: "example.com/svc/model", ModuleRoot: "/repo/svc"},
		{ImportPath: "example.com/lib/util", ModuleRoot: "/repo/lib"},
	}

	got := GroupByModule(pkgs)

	if len(got) != 2 {
		t.Fatalf("expected 2 module groups, got %d", len(got))
	}
	svcPkgs := got["/repo/svc"]
	if len(svcPkgs) != 2 {
		t.Errorf("expected 2 packages for /repo/svc, got %d", len(svcPkgs))
	}
	libPkgs := got["/repo/lib"]
	if len(libPkgs) != 1 {
		t.Errorf("expected 1 package for /repo/lib, got %d", len(libPkgs))
	}
}

func TestGroupByModule_Empty(t *testing.T) {
	got := GroupByModule(nil)
	if len(got) != 0 {
		t.Errorf("expected empty map, got %d entries", len(got))
	}
}

func TestLookup_HappyPath_ResolvesImportPath(t *testing.T) {
	goListAvailable(t)

	root := t.TempDir()
	dir := writeModuleWithPackage(t, root, "example.com/test", "foo")

	cache := make(map[string]ModulePackage)
	got, err := Lookup(context.Background(), root, dir, cache)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.ImportPath != "example.com/test/pkg/foo" {
		t.Errorf("ImportPath = %q, want %q", got.ImportPath, "example.com/test/pkg/foo")
	}
	if got.ModuleRoot != root {
		t.Errorf("ModuleRoot = %q, want %q", got.ModuleRoot, root)
	}
	// First call populates the cache.
	if _, ok := cache[dir]; !ok {
		t.Errorf("expected cache to contain %q after Lookup", dir)
	}
}

// TestLookup_CacheHit pre-populates the cache to verify that subsequent
// lookups skip the `go list` shell-out entirely: the cached entry is returned
// even when the directory on disk would not resolve.
func TestLookup_CacheHit(t *testing.T) {
	// No goListAvailable check — this test exercises the cache path only;
	// no `go list` is invoked.
	dir := "/nonexistent/dir/that/will/never/exist"
	want := ModulePackage{
		ImportPath: "example.com/cached/pkg/foo",
		ModuleRoot: "/cached/root",
	}
	cache := map[string]ModulePackage{dir: want}

	got, err := Lookup(context.Background(), "/cached/root", dir, cache)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestLookup_NegativeCacheReturnsSentinel verifies that a pre-populated zero
// entry (recorded from a prior failed lookup) yields ErrLookupFailed without
// retrying the resolution.
func TestLookup_NegativeCacheReturnsSentinel(t *testing.T) {
	dir := "/nonexistent/dir"
	cache := map[string]ModulePackage{dir: {}} // negatively cached

	_, err := Lookup(context.Background(), "/some/root", dir, cache)
	if !errors.Is(err, ErrLookupFailed) {
		t.Errorf("got error %v, want ErrLookupFailed", err)
	}
}

// TestLookup_FailureCachesNegatively verifies that a real lookup failure is
// recorded in the cache so a second call returns the sentinel rather than
// shelling out again.
func TestLookup_FailureCachesNegatively(t *testing.T) {
	// No go.mod anywhere — Lookup will fail.
	root := t.TempDir()
	dir := filepath.Join(root, "no-module-here")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	cache := make(map[string]ModulePackage)
	_, err1 := Lookup(context.Background(), root, dir, cache)
	if err1 == nil {
		t.Fatal("expected first Lookup to fail (no go.mod), got nil")
	}
	// Cache should now hold a zero entry for dir.
	cached, ok := cache[dir]
	if !ok {
		t.Fatal("expected dir to be cached after failure")
	}
	if cached.ImportPath != "" {
		t.Errorf("expected zero-value cache entry, got %+v", cached)
	}

	// Second call: returns the sentinel, not the original error.
	_, err2 := Lookup(context.Background(), root, dir, cache)
	if !errors.Is(err2, ErrLookupFailed) {
		t.Errorf("second Lookup error = %v, want ErrLookupFailed", err2)
	}
}
