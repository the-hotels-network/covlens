package covlens

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestResolveAbsPath(t *testing.T) {
	cases := []struct {
		name       string
		profileKey string
		modPathMap map[string]string
		want       string
	}{
		{
			name:       "single module",
			profileKey: "example.com/lib/foo/bar.go",
			modPathMap: map[string]string{
				"example.com/lib": "/repo",
			},
			want: filepath.Join("/repo", "foo", "bar.go"),
		},
		{
			// Nested modules where the directory layout DOES NOT mirror the
			// module path: outer module lives in /repo/A, inner in /repo/B.
			// The longer module path must win, otherwise the result depends
			// on map iteration order.
			name:       "nested modules — longer path wins",
			profileKey: "example.com/lib/internal/util.go",
			modPathMap: map[string]string{
				"example.com/lib":          "/repo/A",
				"example.com/lib/internal": "/repo/B",
			},
			want: filepath.Join("/repo/B", "util.go"),
		},
		{
			// Same nested modules, but the profile key only matches the outer
			// module — must resolve under the outer.
			name:       "nested modules — outer-only file",
			profileKey: "example.com/lib/cmd/main.go",
			modPathMap: map[string]string{
				"example.com/lib":          "/repo/A",
				"example.com/lib/internal": "/repo/B",
			},
			want: filepath.Join("/repo/A", "cmd", "main.go"),
		},
		{
			name:       "no matching module",
			profileKey: "other.com/x/y.go",
			modPathMap: map[string]string{
				"example.com/lib": "/repo",
			},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Run several times to flush out any map-iteration nondeterminism
			// the fix is meant to eliminate.
			for i := 0; i < 100; i++ {
				if got := resolveAbsPath(tc.profileKey, tc.modPathMap); got != tc.want {
					t.Fatalf("resolveAbsPath(%q) = %q, want %q (iteration %d)",
						tc.profileKey, got, tc.want, i)
				}
			}
		})
	}
}

func TestFindAllModuleRoots(t *testing.T) {
	writeFile := func(t *testing.T, path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("single root", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "go.mod"), "module x\n")
		got, err := findAllModuleRoots(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, []string{dir}) {
			t.Errorf("got %v, want %v", got, []string{dir})
		}
	})

	t.Run("nested submodule", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "go.mod"), "module x\n")
		writeFile(t, filepath.Join(dir, "sub", "go.mod"), "module x/sub\n")
		got, err := findAllModuleRoots(dir)
		if err != nil {
			t.Fatal(err)
		}
		sort.Strings(got)
		want := []string{dir, filepath.Join(dir, "sub")}
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("skips vendor, .git, node_modules", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "go.mod"), "module x\n")
		for _, skip := range []string{"vendor", ".git", "node_modules"} {
			writeFile(t, filepath.Join(dir, skip, "go.mod"), "module bogus\n")
		}
		got, err := findAllModuleRoots(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, []string{dir}) {
			t.Errorf("got %v, want [%q]", got, dir)
		}
	})
}

func TestBuildModulePathMap(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}

	t.Run("single module", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"),
			[]byte("module example.com/test\n\ngo 1.21\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		m, err := buildModulePathMap(context.Background(), []string{dir})
		if err != nil {
			t.Fatal(err)
		}
		if got := m["example.com/test"]; got != dir {
			t.Errorf(`m["example.com/test"] = %q, want %q`, got, dir)
		}
		if len(m) != 1 {
			t.Errorf("got %d entries, want 1: %+v", len(m), m)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		m, err := buildModulePathMap(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(m) != 0 {
			t.Errorf("got %d entries, want 0: %+v", len(m), m)
		}
	})
}
