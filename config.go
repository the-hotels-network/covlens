package covlens

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Config controls covlens behavior. Load from covlens.yaml, override with CLI flags.
type Config struct {
	BaseBranch     string   `yaml:"base_branch"`
	DiffThreshold  float64  `yaml:"diff_threshold"`
	TotalThreshold float64  `yaml:"total_threshold"`
	OutputDir      string   `yaml:"output_dir"`
	AutoOpen       bool     `yaml:"auto_open"`
	ExcludeFiles   []string `yaml:"exclude_files"`
	ShowExcluded   bool     `yaml:"show_excluded"`
	// RatchetTotal fails only if total coverage drops below the base branch value,
	// rather than comparing against the fixed TotalThreshold.
	RatchetTotal bool `yaml:"ratchet_total"`
	// Theme sets the default theme for the HTML report: "auto", "light", or "dark".
	// "auto" follows the OS preference. Users can always override via the in-page toggle.
	Theme    string `yaml:"theme"`
	FullMode bool   `yaml:"-"` // set by --full CLI flag, not persisted to config
	WorkDir  string `yaml:"-"`
}

// DefaultConfig returns a Config with sensible defaults matching the original bash script.
func DefaultConfig() Config {
	return Config{
		BaseBranch:     "main",
		DiffThreshold:  80,
		TotalThreshold: 70,
		OutputDir:      ".coverage",
		AutoOpen:       true,
		ShowExcluded:   true,
		Theme:          "auto",
	}
}

// LoadConfig reads a YAML config file and merges it on top of defaults.
// A missing file is not an error — pure defaults are returned.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config: %w", err)
	}
	return cfg, nil
}

// Validate checks that thresholds are in range and exclude patterns compile.
func (c Config) Validate() error {
	if c.DiffThreshold < 0 || c.DiffThreshold > 100 {
		return fmt.Errorf("diff_threshold must be between 0 and 100, got %.1f", c.DiffThreshold)
	}
	if c.TotalThreshold < 0 || c.TotalThreshold > 100 {
		return fmt.Errorf("total_threshold must be between 0 and 100, got %.1f", c.TotalThreshold)
	}
	if c.Theme != "" && c.Theme != "auto" && c.Theme != "light" && c.Theme != "dark" {
		return fmt.Errorf("theme must be \"auto\", \"light\", or \"dark\", got %q", c.Theme)
	}
	for _, pattern := range c.ExcludeFiles {
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("invalid exclude_files pattern %q: %w", pattern, err)
		}
	}
	return nil
}
