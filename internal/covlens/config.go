package covlens

import (
	"errors"
	"fmt"
	"io"
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
	ExcludeFiles   []string `yaml:"exclude_files"`
	// RatchetTotal fails only if total coverage drops below the base branch value,
	// rather than comparing against the fixed TotalThreshold.
	RatchetTotal bool `yaml:"ratchet_total"`

	// ShowExcluded controls whether excluded files appear in the console
	// summary and HTML report. The JSON sidecar always includes them so
	// machine consumers see the complete set regardless of this flag.
	// Default: true.
	ShowExcluded bool `yaml:"show_excluded"`

	// VerboseTests streams the raw `go test` stdout/stderr to TestOutput
	// (defaults to os.Stdout). When false (default), test output is captured
	// to .coverage/test_output.log and only the covlens progress summary
	// appears on the terminal. Useful for large projects where the streamed
	// per-package "ok pkg ..." lines drown out everything else.
	VerboseTests bool `yaml:"verbose_tests"`

	FullMode bool   `yaml:"-"` // set by --full CLI flag, not persisted to config
	WorkDir  string `yaml:"-"`

	// HTML groups settings that only matter when consuming the HTML report.
	HTML HTMLConfig `yaml:"html"`

	// Stderr receives covlens progress lines (e.g. "▶ Running total coverage...").
	// Defaults to os.Stderr. Set to io.Discard to silence.
	Stderr io.Writer `yaml:"-"`
	// TestOutput receives the streamed stdout/stderr of the `go test` subprocess.
	// Defaults to os.Stdout. Set to io.Discard to silence.
	TestOutput io.Writer `yaml:"-"`
}

// HTMLConfig holds presentation-only settings used by the HTML printer.
// Library users who never render HTML can leave the zero value.
type HTMLConfig struct {
	AutoOpen bool `yaml:"auto_open"`
	// Theme sets the default theme for the HTML report: "auto", "light", or "dark".
	// "auto" follows the OS preference. Users can always override via the in-page toggle.
	Theme string `yaml:"theme"`
}

func (c Config) stderr() io.Writer {
	if c.Stderr == nil {
		return os.Stderr
	}
	return c.Stderr
}

func (c Config) testOutput() io.Writer {
	if c.TestOutput == nil {
		return os.Stdout
	}
	return c.TestOutput
}

// DefaultConfig returns a Config with sensible defaults matching the original bash script.
func DefaultConfig() Config {
	return Config{
		BaseBranch:     "main",
		DiffThreshold:  80,
		TotalThreshold: 70,
		OutputDir:      ".coverage",
		ShowExcluded:   true,
		HTML: HTMLConfig{
			AutoOpen: true,
			Theme:    "auto",
		},
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
//
// All field violations are reported together via errors.Join so the user
// can fix every problem in a single edit pass instead of one-per-rerun.
// Each error is prefixed with the YAML key for grep-ability.
func (c Config) Validate() error {
	var errs []error

	if c.DiffThreshold < 0 || c.DiffThreshold > 100 {
		errs = append(errs, fmt.Errorf("diff_threshold: must be between 0 and 100, got %.1f", c.DiffThreshold))
	}
	if c.TotalThreshold < 0 || c.TotalThreshold > 100 {
		errs = append(errs, fmt.Errorf("total_threshold: must be between 0 and 100, got %.1f", c.TotalThreshold))
	}
	if c.HTML.Theme != "" && c.HTML.Theme != "auto" && c.HTML.Theme != "light" && c.HTML.Theme != "dark" {
		errs = append(errs, fmt.Errorf("theme: must be \"auto\", \"light\", or \"dark\", got %q", c.HTML.Theme))
	}
	if _, err := c.compileExcludes(); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// compileExcludes compiles the ExcludeFiles patterns into regexps.
// Returns nil when no patterns are configured. The first invalid pattern
// produces an error and aborts compilation.
func (c Config) compileExcludes() ([]*regexp.Regexp, error) {
	if len(c.ExcludeFiles) == 0 {
		return nil, nil
	}
	out := make([]*regexp.Regexp, 0, len(c.ExcludeFiles))
	for _, pattern := range c.ExcludeFiles {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("exclude_files: invalid pattern %q: %w", pattern, err)
		}
		out = append(out, re)
	}
	return out, nil
}
