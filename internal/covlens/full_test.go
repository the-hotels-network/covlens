package covlens

import (
	"path/filepath"
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
