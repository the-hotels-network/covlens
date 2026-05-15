package main

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"

	"github.com/the-hotels-network/covlens/internal/covlens"
	"github.com/the-hotels-network/covlens/internal/printer/console"
	"github.com/the-hotels-network/covlens/internal/printer/html"
	"github.com/the-hotels-network/covlens/internal/printer/json"
)

func main() {
	baseBranch := flag.String("base-branch", "", "Base branch for comparison")
	diffThreshold := flag.Float64("diff-threshold", 0, "Minimum coverage % for changed code")
	totalThreshold := flag.Float64("total-threshold", 0, "Minimum coverage % for entire project")
	outputDir := flag.String("output-dir", "", "Directory for generated artifacts")
	open := flag.Bool("open", false, "Force opening the report in the browser (overrides auto_open: false in config)")
	noOpen := flag.Bool("no-open", false, "Don't open report in browser (overrides auto_open: true in config)")
	ratchet := flag.Bool("ratchet", false, "Fail only if total coverage drops vs base branch")
	flag.BoolVar(ratchet, "r", false, "shorthand for --ratchet")
	full := flag.Bool("full", false, "Full project scan: show coverage for all files, no diff required")
	flag.BoolVar(full, "f", false, "shorthand for --full")
	configPath := flag.String("config", "covlens.yaml", "Path to config file")
	verbose := flag.Bool("verbose", false, "Stream `go test` output to stdout (default: capture to .coverage/test_output.log)")
	flag.BoolVar(verbose, "v", false, "shorthand for --verbose")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cfg, err := covlens.LoadConfig(*configPath)
	if err != nil {
		console.Error(os.Stderr, "Error loading config: %v", err)
		os.Exit(1)
	}

	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "base-branch":
			cfg.BaseBranch = *baseBranch
		case "diff-threshold":
			cfg.DiffThreshold = *diffThreshold
		case "total-threshold":
			cfg.TotalThreshold = *totalThreshold
		case "output-dir":
			cfg.OutputDir = *outputDir
		case "no-open":
			cfg.HTML.AutoOpen = !*noOpen
		case "open":
			cfg.HTML.AutoOpen = *open
		case "ratchet", "r":
			cfg.RatchetTotal = *ratchet
		case "full", "f":
			cfg.FullMode = *full
		case "verbose", "v":
			cfg.VerboseTests = *verbose
		}
	})

	report, err := covlens.Run(ctx, cfg)
	if err != nil {
		console.Error(os.Stderr, "Error: %v", err)
		os.Exit(1)
	}

	// HTML first — its path is referenced from the JSON sidecar. Always
	// written so downstream CI tooling can rely on the file existing.
	htmlPath, err := writeHTMLReport(report, cfg)
	if err != nil {
		console.Error(os.Stderr, "failed to generate HTML report: %v", err)
		htmlPath = ""
	}

	// JSON sidecar for CI consumers — always written so a parser can rely
	// on the file existing whenever covlens completed successfully.
	jsonPath, err := writeJSONReport(report, cfg, htmlPath)
	if err != nil {
		console.Error(os.Stderr, "failed to generate JSON report: %v", err)
		jsonPath = ""
	}

	// Short-circuit cases A and B-del: no Files to render, no useful
	// per-file summary. B-exc keeps the full flow (it has excluded
	// entries in Files and a meaningful total to report).
	if report.Diff != nil && (report.Diff.Status == covlens.DiffStatusNoGoChanges || report.Diff.Status == covlens.DiffStatusOnlyDeletions) {
		var msg string
		switch report.Diff.Status {
		case covlens.DiffStatusOnlyDeletions:
			msg = "All changed Go files were deleted; nothing to measure."
		default:
			msg = "No changed Go files detected relative to '" + cfg.BaseBranch + "'."
		}
		console.Info(os.Stdout, msg)
		if htmlPath != "" {
			console.Info(os.Stdout, "HTML report: "+htmlPath)
		}
		if jsonPath != "" {
			console.Info(os.Stdout, "JSON report: "+jsonPath)
		}
		return
	}

	console.PrintSummary(os.Stdout, report, cfg)

	if htmlPath != "" {
		console.Info(os.Stdout, "HTML report: "+htmlPath)
		if cfg.HTML.AutoOpen {
			openBrowser(htmlPath)
		}
	}
	if jsonPath != "" {
		console.Info(os.Stdout, "JSON report: "+jsonPath)
	}

	diffPassed := report.Diff == nil || report.Diff.Passed
	if !diffPassed || !report.TotalPassed {
		console.Error(os.Stderr, "Coverage insufficient. Review the report for uncovered lines.")
		os.Exit(1)
	}

	console.Info(os.Stdout, "All thresholds passed!")
}

// writeHTMLReport renders the HTML coverage report and returns its path.
func writeHTMLReport(r *covlens.Report, cfg covlens.Config) (string, error) {
	path := filepath.Join(r.OutputDir, "coverage_report.html")
	if err := html.Generate(r, cfg, path); err != nil {
		return "", err
	}
	return path, nil
}

// writeJSONReport renders the machine-readable JSON sidecar and returns its path.
func writeJSONReport(r *covlens.Report, cfg covlens.Config, htmlPath string) (string, error) {
	path := filepath.Join(r.OutputDir, "coverage_report.json")
	if err := json.Write(r, cfg, htmlPath, path); err != nil {
		return "", err
	}
	return path, nil
}

func openBrowser(path string) {
	url := "file://" + path
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	_ = cmd.Start() // fire-and-forget: browser launch failure is non-fatal
}
