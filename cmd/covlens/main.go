package main

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"

	"github.com/erioch/covlens"
	"github.com/erioch/covlens/printers/console"
	"github.com/erioch/covlens/printers/html"
	"github.com/erioch/covlens/printers/json"
)

func main() {
	baseBranch := flag.String("base-branch", "", "Base branch for comparison")
	diffThreshold := flag.Float64("diff-threshold", 0, "Minimum coverage % for changed code")
	totalThreshold := flag.Float64("total-threshold", 0, "Minimum coverage % for entire project")
	outputDir := flag.String("output-dir", "", "Directory for generated artifacts")
	noOpen := flag.Bool("no-open", false, "Don't open report in browser")
	noHTML := flag.Bool("no-html", false, "Skip HTML report generation entirely (implies --no-open)")
	ratchet := flag.Bool("ratchet", false, "Fail only if total coverage drops vs base branch")
	full := flag.Bool("full", false, "Full project scan: show coverage for all files, no diff required")
	configPath := flag.String("config", "covlens.yaml", "Path to config file")
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
		case "ratchet":
			cfg.RatchetTotal = *ratchet
		case "full":
			cfg.FullMode = *full
		}
	})

	report, err := covlens.Run(ctx, cfg)
	if err != nil {
		console.Error(os.Stderr, "Error: %v", err)
		os.Exit(1)
	}

	console.PrintWarnings(os.Stderr, report)

	// HTML first — its path is referenced from the JSON sidecar.
	htmlPath := ""
	if !*noHTML && len(report.Files) > 0 {
		p, err := writeHTMLReport(report, cfg)
		if err != nil {
			console.Error(os.Stderr, "failed to generate HTML report: %v", err)
		} else {
			htmlPath = p
		}
	}

	// JSON sidecar for CI consumers — always written so a parser can rely
	// on the file existing whenever covlens completed successfully.
	if _, err := writeJSONReport(report, cfg, htmlPath); err != nil {
		console.Error(os.Stderr, "failed to generate JSON report: %v", err)
	}

	if len(report.Files) == 0 {
		console.Info(os.Stdout, "No changed Go files detected relative to '"+cfg.BaseBranch+"'.")
		return
	}

	console.PrintSummary(os.Stdout, report, cfg)

	if htmlPath != "" {
		console.Info(os.Stdout, "Report: "+htmlPath)
		if cfg.HTML.AutoOpen {
			openBrowser(htmlPath)
		}
	}

	if !report.DiffPassed || !report.TotalPassed {
		console.Error(os.Stderr, "Coverage insufficient. Review the report for uncovered lines.")
		os.Exit(1)
	}

	console.Info(os.Stdout, "All thresholds passed!")
}

// writeHTMLReport renders the HTML coverage report and returns its path.
func writeHTMLReport(r *covlens.Report, cfg covlens.Config) (string, error) {
	path := filepath.Join(r.OutputDir, "coverage_report.html")

	files := make([]html.FileSummary, 0, len(r.Files))
	for _, fc := range r.Files {
		files = append(files, html.FileSummary{
			Path:       fc.Path,
			Package:    fc.Package,
			Coverage:   fc.Coverage,
			Statements: fc.Statements,
			Covered:    fc.Covered,
			Excluded:   fc.Excluded,
			Status:     fc.Status,
		})
	}

	input := html.ReportInput{
		DiffCoverage:          r.DiffCoverage,
		TotalCoverage:         r.TotalCoverage,
		BaselineTotalCoverage: r.BaselineTotalCoverage,
		DiffPassed:            r.DiffPassed,
		TotalPassed:           r.TotalPassed,
		DiffThreshold:         cfg.DiffThreshold,
		TotalThreshold:        cfg.TotalThreshold,
		BaseBranch:            cfg.BaseBranch,
		ShowExcluded:          cfg.HTML.ShowExcluded,
		RatchetTotal:          cfg.RatchetTotal,
		Theme:                 cfg.HTML.Theme,
		FullMode:              cfg.FullMode,
		Files:                 files,
	}

	if err := html.Generate(input, r.SourceFiles, path); err != nil {
		return "", err
	}
	return path, nil
}

// writeJSONReport renders the machine-readable JSON sidecar and returns its path.
func writeJSONReport(r *covlens.Report, cfg covlens.Config, htmlPath string) (string, error) {
	path := filepath.Join(r.OutputDir, "coverage_report.json")

	files := make([]json.File, 0, len(r.Files))
	for _, fc := range r.Files {
		files = append(files, json.File{
			Path:       fc.Path,
			Package:    fc.Package,
			Coverage:   fc.Coverage,
			Statements: fc.Statements,
			Covered:    fc.Covered,
			Excluded:   fc.Excluded,
			Status:     fc.Status,
		})
	}

	mode := "diff"
	if cfg.FullMode {
		mode = "full"
	}

	out := json.Report{
		Schema:                json.SchemaVersion,
		Mode:                  mode,
		BaseBranch:            cfg.BaseBranch,
		DiffCoverage:          r.DiffCoverage,
		TotalCoverage:         r.TotalCoverage,
		BaselineTotalCoverage: r.BaselineTotalCoverage,
		DiffThreshold:         cfg.DiffThreshold,
		TotalThreshold:        cfg.TotalThreshold,
		RatchetTotal:          cfg.RatchetTotal,
		DiffPassed:            r.DiffPassed,
		TotalPassed:           r.TotalPassed,
		HTMLReportPath:        htmlPath,
		Files:                 files,
		Warnings:              r.Warnings,
	}

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if err := json.Encode(f, out); err != nil {
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
