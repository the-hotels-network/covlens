package covlens_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erioch/covlens/internal/covlens"
)

// TestLoadConfig_ParsesHTMLBlock verifies that auto_open and theme are parsed
// from the html: sub-key in covlens.yaml.
func TestLoadConfig_ParsesHTMLBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "covlens.yaml")
	yaml := `base_branch: develop
diff_threshold: 90
total_threshold: 80
output_dir: out
show_excluded: true
ratchet_total: true
exclude_files:
  - "_gen\\.go$"
html:
  auto_open: false
  theme: dark
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := covlens.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.BaseBranch != "develop" {
		t.Errorf("BaseBranch = %q, want %q", cfg.BaseBranch, "develop")
	}
	if cfg.DiffThreshold != 90 {
		t.Errorf("DiffThreshold = %v, want 90", cfg.DiffThreshold)
	}
	if !cfg.RatchetTotal {
		t.Error("RatchetTotal = false, want true")
	}
	if cfg.HTML.AutoOpen {
		t.Error("HTML.AutoOpen = true, want false")
	}
	if cfg.HTML.Theme != "dark" {
		t.Errorf("HTML.Theme = %q, want %q", cfg.HTML.Theme, "dark")
	}
	if !cfg.ShowExcluded {
		t.Error("ShowExcluded = false, want true")
	}
	if len(cfg.ExcludeFiles) != 1 || cfg.ExcludeFiles[0] != `_gen\.go$` {
		t.Errorf("ExcludeFiles = %v, want [_gen\\.go$]", cfg.ExcludeFiles)
	}
}

// TestValidate covers each field-level rule individually plus a multi-error
// case proving Validate accumulates problems via errors.Join instead of
// returning the first one.
func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*covlens.Config)
		wantErr []string // substrings every returned error must contain (all-of)
	}{
		{
			name:    "default config is valid",
			mutate:  func(c *covlens.Config) {},
			wantErr: nil,
		},
		{
			name:    "diff_threshold above range",
			mutate:  func(c *covlens.Config) { c.DiffThreshold = 150 },
			wantErr: []string{"diff_threshold:", "150"},
		},
		{
			name:    "diff_threshold below range",
			mutate:  func(c *covlens.Config) { c.DiffThreshold = -1 },
			wantErr: []string{"diff_threshold:"},
		},
		{
			name:    "total_threshold out of range",
			mutate:  func(c *covlens.Config) { c.TotalThreshold = 200 },
			wantErr: []string{"total_threshold:", "200"},
		},
		{
			name:    "invalid theme",
			mutate:  func(c *covlens.Config) { c.HTML.Theme = "neon" },
			wantErr: []string{"theme:", "neon"},
		},
		{
			name:    "empty theme is valid (means use default)",
			mutate:  func(c *covlens.Config) { c.HTML.Theme = "" },
			wantErr: nil,
		},
		{
			name:    "invalid exclude regex",
			mutate:  func(c *covlens.Config) { c.ExcludeFiles = []string{"[unclosed"} },
			wantErr: []string{"exclude_files:", "[unclosed"},
		},
		{
			name: "multiple errors are reported together",
			mutate: func(c *covlens.Config) {
				c.DiffThreshold = 150
				c.TotalThreshold = -1
				c.HTML.Theme = "neon"
				c.ExcludeFiles = []string{"[unclosed"}
			},
			wantErr: []string{
				"diff_threshold:",
				"total_threshold:",
				"theme:",
				"exclude_files:",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := covlens.DefaultConfig()
			tc.mutate(&cfg)
			err := cfg.Validate()

			if len(tc.wantErr) == 0 {
				if err != nil {
					t.Fatalf("Validate: unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate: got nil, want error containing %v", tc.wantErr)
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error missing %q\nfull error:\n%s", want, err.Error())
				}
			}
		})
	}
}

// TestLoadConfig_MalformedYAML asserts that a syntactically broken config file
// surfaces a wrapped error tagged "parsing config:" instead of silently
// returning defaults. Defaults-on-error would mask user typos.
func TestLoadConfig_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "covlens.yaml")
	// Mapping value where a scalar is expected → YAML parse error.
	if err := os.WriteFile(path, []byte("diff_threshold: {oops\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := covlens.LoadConfig(path)
	if err == nil {
		t.Fatal("LoadConfig: got nil, want parse error")
	}
	if !strings.Contains(err.Error(), "parsing config") {
		t.Errorf("error message missing 'parsing config': %v", err)
	}
}

// TestLoadConfig_MissingFileReturnsDefaults makes sure removing the file
// doesn't error and returns the expected default Config (including the
// default HTML sub-struct values).
func TestLoadConfig_MissingFileReturnsDefaults(t *testing.T) {
	cfg, err := covlens.LoadConfig(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig of missing file: %v", err)
	}
	want := covlens.DefaultConfig()
	if cfg.HTML != want.HTML {
		t.Errorf("HTML = %+v, want %+v", cfg.HTML, want.HTML)
	}
	if cfg.BaseBranch != want.BaseBranch {
		t.Errorf("BaseBranch = %q, want %q", cfg.BaseBranch, want.BaseBranch)
	}
}
