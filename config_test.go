package covlens_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/erioch/covlens"
)

// TestLoadConfig_FlatYAMLBackwardCompat verifies that an existing covlens.yaml
// using the pre-refactor flat schema (auto_open / theme / show_excluded at top
// level) still parses correctly after those fields moved into the HTML
// sub-struct. The yaml:",inline" tag on Config.HTML is what makes this work.
func TestLoadConfig_FlatYAMLBackwardCompat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "covlens.yaml")
	yaml := `base_branch: develop
diff_threshold: 90
total_threshold: 80
output_dir: out
auto_open: false
theme: dark
show_excluded: true
ratchet_total: true
exclude_files:
  - "_gen\\.go$"
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

	// The three fields that moved into HTMLConfig must still be parsed from
	// their flat top-level keys.
	if cfg.HTML.AutoOpen {
		t.Error("HTML.AutoOpen = true, want false (yaml said auto_open: false)")
	}
	if cfg.HTML.Theme != "dark" {
		t.Errorf("HTML.Theme = %q, want %q", cfg.HTML.Theme, "dark")
	}
	if !cfg.HTML.ShowExcluded {
		t.Error("HTML.ShowExcluded = false, want true")
	}

	if len(cfg.ExcludeFiles) != 1 || cfg.ExcludeFiles[0] != `_gen\.go$` {
		t.Errorf("ExcludeFiles = %v, want [_gen\\.go$]", cfg.ExcludeFiles)
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
