package packages

import (
	"os"
	"path/filepath"
	"testing"
)

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
