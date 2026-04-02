package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/erioch/covlens"
)

func main() {
	baseBranch := flag.String("base-branch", "", "Base branch for comparison")
	diffThreshold := flag.Float64("diff-threshold", 0, "Minimum coverage % for changed code")
	totalThreshold := flag.Float64("total-threshold", 0, "Minimum coverage % for entire project")
	outputDir := flag.String("output-dir", "", "Directory for generated artifacts")
	noOpen := flag.Bool("no-open", false, "Don't open report in browser")
	ratchet := flag.Bool("ratchet", false, "Fail only if total coverage drops vs base branch")
	full := flag.Bool("full", false, "Full project scan: show coverage for all files, no diff required")
	configPath := flag.String("config", "covlens.yaml", "Path to config file")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cfg, err := covlens.LoadConfig(*configPath)
	if err != nil {
		fail("Error loading config: %v", err)
		os.Exit(1)
	}

	// CLI flags override config only when explicitly set
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
			cfg.AutoOpen = !*noOpen
		case "ratchet":
			cfg.RatchetTotal = *ratchet
		case "full":
			cfg.FullMode = *full
		}
	})

	report, err := covlens.Run(ctx, cfg)
	if err != nil {
		fail("Error: %v", err)
		os.Exit(1)
	}

	if len(report.Files) == 0 {
		warn(fmt.Sprintf("No changed Go files detected relative to '%s'.", cfg.BaseBranch))
		return
	}

	printSummary(report, cfg)

	if !report.DiffPassed || !report.TotalPassed {
		fail("Coverage insufficient. Review the report for uncovered lines.")
		os.Exit(1)
	}

	ok("All thresholds passed!")
}

// Terminal colors
const (
	cRed    = "\033[0;31m"
	cGreen  = "\033[0;32m"
	cYellow = "\033[1;33m"
	cBold   = "\033[1m"
	cReset  = "\033[0m"
)

func ok(msg string) { fmt.Printf("%s✔%s %s\n", cGreen, cReset, msg) }
func fail(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s✘%s %s\n", cRed, cReset, fmt.Sprintf(f, a...))
}
func warn(msg string) { fmt.Printf("%s⚠%s %s\n", cYellow, cReset, msg) }

func printSummary(r *covlens.Report, cfg covlens.Config) {
	fmt.Printf("\n%sSummary%s\n\n", cBold, cReset)

	// Diff coverage (skipped in full mode)
	if !cfg.FullMode {
		fmt.Printf("  %-30s %.2f%% (threshold: %.0f%%)\n", "Diff coverage:", r.DiffCoverage, cfg.DiffThreshold)
		if r.DiffPassed {
			fmt.Printf("  %s✔%s  Diff threshold passed\n", cGreen, cReset)
		} else {
			fmt.Printf("  %s✘%s  Diff threshold not met\n", cRed, cReset)
		}
	}

	// Total coverage
	if cfg.RatchetTotal && r.BaselineTotalCoverage > 0 {
		fmt.Printf("  %-30s %.2f%% (baseline: %.2f%%)\n", "Total coverage:", r.TotalCoverage, r.BaselineTotalCoverage)
		if r.TotalPassed {
			fmt.Printf("  %s✔%s  Total coverage did not drop\n", cGreen, cReset)
		} else {
			fmt.Printf("  %s✘%s  Total coverage dropped vs base branch\n", cRed, cReset)
		}
	} else {
		fmt.Printf("  %-30s %.2f%% (threshold: %.0f%%)\n", "Total coverage:", r.TotalCoverage, cfg.TotalThreshold)
		if r.TotalPassed {
			fmt.Printf("  %s✔%s  Total threshold passed\n", cGreen, cReset)
		} else {
			fmt.Printf("  %s✘%s  Total threshold not met\n", cRed, cReset)
		}
	}
	fmt.Println()

	// File breakdown
	if len(r.Files) > 0 {
		fmt.Printf("  %sFiles:%s\n", cBold, cReset)
		for _, f := range r.Files {
			switch f.Status {
			case "ok":
				fmt.Printf("    %s✔%s %-50s %s%.1f%%%s\n", cGreen, cReset, f.Path, cGreen, f.Coverage, cReset)
			case "fail":
				fmt.Printf("    %s✘%s %-50s %s%.1f%%%s\n", cRed, cReset, f.Path, cRed, f.Coverage, cReset)
			case "excluded":
				fmt.Printf("    %s–%s %-50s %s(excluded)%s\n", cYellow, cReset, f.Path, cYellow, cReset)
			default:
				fmt.Printf("    %s⚠%s %-50s %s(no data)%s\n", cYellow, cReset, f.Path, cYellow, cReset)
			}
		}
		fmt.Println()
	}

	// Report path
	if r.ReportPath != "" {
		ok(fmt.Sprintf("Report: %s", r.ReportPath))
	}
}
