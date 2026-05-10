package coverage

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// requireGoToolchain skips a test that needs to invoke `go test` as a
// subprocess. Most environments where this package runs have it, but the
// test should be resilient to minimal images that don't.
func requireGoToolchain(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
}

// writeModule materializes a minimal single-package Go module at root.
// The package contains:
//   - foo.go: source (a function)
//   - foo_test.go: test (variant by `kind`):
//     "pass"    — test passes
//     "fail"    — test calls t.Fatal
//     "compile" — source has a syntax error; module won't compile
func writeModule(t *testing.T, root, kind string) {
	t.Helper()
	mod := []byte("module example.com/covtest\n\ngo 1.25\n")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), mod, 0o644); err != nil {
		t.Fatal(err)
	}

	src := "package covtest\n\nfunc Foo() int { return 1 }\n"
	if kind == "compile" {
		// Unresolvable import — fails before compilation can produce any
		// profile output. A pure syntax error in source isn't enough: go
		// test pre-writes the profile header before invoking the compiler.
		src = "package covtest\n\nimport _ \"definitely/missing/" + filepath.Base(root) + "\"\n\nfunc Foo() int { return 1 }\n"
	}
	if err := os.WriteFile(filepath.Join(root, "foo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	var test string
	switch kind {
	case "fail":
		test = `package covtest

import "testing"

func TestFoo(t *testing.T) {
	t.Fatal("intentional failure for runner tests")
}
`
	default: // "pass" or "compile" (compile won't even reach this)
		test = `package covtest

import "testing"

func TestFoo(t *testing.T) {
	if Foo() != 1 {
		t.Fatal("Foo")
	}
}
`
	}
	if err := os.WriteFile(filepath.Join(root, "foo_test.go"), []byte(test), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunTotal_HappyPath_ProducesProfile(t *testing.T) {
	requireGoToolchain(t)

	modRoot := t.TempDir()
	writeModule(t, modRoot, "pass")
	out := t.TempDir()

	res, err := RunTotal(context.Background(), []string{modRoot}, out, io.Discard)
	if err != nil {
		t.Fatalf("RunTotal: %v", err)
	}
	if res.ProfilePath == "" {
		t.Fatal("ProfilePath is empty on success")
	}
	if info, err := os.Stat(res.ProfilePath); err != nil || info.Size() == 0 {
		t.Errorf("expected non-empty profile at %s; stat err=%v", res.ProfilePath, err)
	}
}

func TestRunTotal_TestFailure_ReturnsError(t *testing.T) {
	requireGoToolchain(t)

	modRoot := t.TempDir()
	writeModule(t, modRoot, "fail")
	out := t.TempDir()

	_, err := RunTotal(context.Background(), []string{modRoot}, out, io.Discard)
	if err == nil {
		t.Fatal("expected error for failing tests, got nil")
	}
	if !strings.Contains(err.Error(), "failing tests") {
		t.Errorf("error should mention test failures; got %v", err)
	}
}

func TestRunTotal_CompileFailure_ReturnsError(t *testing.T) {
	requireGoToolchain(t)

	modRoot := t.TempDir()
	writeModule(t, modRoot, "compile")
	out := t.TempDir()

	// We don't assert on the specific error message: `go test -coverprofile=`
	// pre-writes the "mode: atomic" header before invoking the compiler, so
	// many compile-time failures still leave a non-empty profile on disk —
	// the compile-vs-test branch in RunTotal then can't tell them apart.
	// The valuable invariant is that some error is returned; the categorical
	// distinction is tested only via the profileWritten unit test below.
	_, err := RunTotal(context.Background(), []string{modRoot}, out, io.Discard)
	if err == nil {
		t.Fatal("expected error for compile failure, got nil")
	}
}

func TestRunDiff_HappyPath_ProducesProfile(t *testing.T) {
	requireGoToolchain(t)

	modRoot := t.TempDir()
	writeModule(t, modRoot, "pass")
	out := t.TempDir()

	grouped := map[string][]string{
		modRoot: {"example.com/covtest"},
	}
	res, err := RunDiff(context.Background(), grouped, out, io.Discard)
	if err != nil {
		t.Fatalf("RunDiff: %v", err)
	}
	if res.ProfilePath == "" {
		t.Fatal("ProfilePath is empty on success")
	}
	if info, err := os.Stat(res.ProfilePath); err != nil || info.Size() == 0 {
		t.Errorf("expected non-empty profile at %s; stat err=%v", res.ProfilePath, err)
	}
}

func TestRunDiff_TestFailure_ReturnsError(t *testing.T) {
	requireGoToolchain(t)

	modRoot := t.TempDir()
	writeModule(t, modRoot, "fail")
	out := t.TempDir()

	grouped := map[string][]string{
		modRoot: {"example.com/covtest"},
	}
	_, err := RunDiff(context.Background(), grouped, out, io.Discard)
	if err == nil {
		t.Fatal("expected error for failing tests, got nil")
	}
	if !strings.Contains(err.Error(), "failing tests") {
		t.Errorf("error should mention test failures; got %v", err)
	}
}

func TestProfileWritten(t *testing.T) {
	dir := t.TempDir()

	// Missing file.
	if profileWritten(filepath.Join(dir, "missing.out")) {
		t.Error("profileWritten: expected false for missing file")
	}

	// Empty file.
	empty := filepath.Join(dir, "empty.out")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if profileWritten(empty) {
		t.Error("profileWritten: expected false for empty file")
	}

	// Non-empty file.
	nonEmpty := filepath.Join(dir, "data.out")
	if err := os.WriteFile(nonEmpty, []byte("mode: atomic\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !profileWritten(nonEmpty) {
		t.Error("profileWritten: expected true for non-empty file")
	}
}
