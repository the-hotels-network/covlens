package covlens

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/the-hotels-network/covlens/internal/git"
)

// Run executes a coverage analysis and returns the resulting report.
func Run(ctx context.Context, cfg Config) (*Report, error) {
	r, err := newRunner(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if r.cfg.FullMode {
		return r.runFull()
	}
	return r.run()
}

// runner holds the immutable request scope shared by every phase of a run.
// All fields are set once in newRunner and never mutated afterwards; phase
// outputs flow through return values, not back onto the runner.
type runner struct {
	ctx        context.Context
	cfg        Config
	outputDir  string
	excludeRes []*regexp.Regexp
	git        *git.Client
}

func newRunner(ctx context.Context, cfg Config) (*runner, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	for _, tool := range []string{"git", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			return nil, fmt.Errorf("%s not found on PATH", tool)
		}
	}
	if cfg.WorkDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("getting working directory: %w", err)
		}
		cfg.WorkDir = wd
	}

	outputDir := cfg.OutputDir
	if !filepath.IsAbs(outputDir) {
		outputDir = filepath.Join(cfg.WorkDir, outputDir)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating output directory: %w", err)
	}

	excludeRes, err := cfg.compileExcludes()
	if err != nil {
		return nil, fmt.Errorf("compiling exclude patterns: %w", err)
	}

	return &runner{
		ctx:        ctx,
		cfg:        cfg,
		outputDir:  outputDir,
		excludeRes: excludeRes,
		git:        &git.Client{WorkDir: cfg.WorkDir},
	}, nil
}

// run executes diff-mode coverage analysis: detect changed files vs the base
// branch, classify them, run targeted coverage, and assemble the report.
func (r *runner) run() (*Report, error) {
	scope, err := r.detectChangedFiles()
	if err != nil {
		return nil, err
	}
	if len(scope.changedFiles) == 0 {
		return r.emptyReport(), nil
	}

	subjects := r.classifyFiles(scope)

	targets := r.resolvePackages(subjects)

	profiles, err := r.runCoverage(targets)
	if err != nil {
		return nil, err
	}

	stats, err := r.computeStats(scope, subjects, targets, profiles)
	if err != nil {
		return nil, err
	}

	return r.buildReport(scope, subjects, profiles, stats), nil
}

func (r *runner) emptyReport() *Report {
	return &Report{DiffPassed: true, TotalPassed: true, OutputDir: r.outputDir}
}
