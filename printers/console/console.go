// Package console renders covlens reports as ANSI-colored terminal output.
package console

import (
	"fmt"
	"io"

	"github.com/erioch/covlens"
)

const (
	cRed    = "\033[0;31m"
	cGreen  = "\033[0;32m"
	cYellow = "\033[1;33m"
	cBold   = "\033[1m"
	cReset  = "\033[0m"
)

// PrintSummary writes the human-readable coverage summary (thresholds,
// pass/fail status, per-file breakdown) to out.
func PrintSummary(out io.Writer, r *covlens.Report, cfg covlens.Config) {
	fmt.Fprintf(out, "\n%sSummary%s\n\n", cBold, cReset)

	// Diff coverage (skipped in full mode).
	if !cfg.FullMode {
		fmt.Fprintf(out, "  %-30s %.2f%% (threshold: %.0f%%)\n", "Diff coverage:", r.DiffCoverage, cfg.DiffThreshold)
		if r.DiffPassed {
			fmt.Fprintf(out, "  %s✔%s  Diff threshold passed\n", cGreen, cReset)
		} else {
			fmt.Fprintf(out, "  %s✘%s  Diff threshold not met\n", cRed, cReset)
		}
	}

	// Total coverage.
	if cfg.RatchetTotal && r.BaselineTotalCoverage > 0 {
		fmt.Fprintf(out, "  %-30s %.2f%% (baseline: %.2f%%)\n", "Total coverage:", r.TotalCoverage, r.BaselineTotalCoverage)
		if r.TotalPassed {
			fmt.Fprintf(out, "  %s✔%s  Total coverage did not drop\n", cGreen, cReset)
		} else {
			fmt.Fprintf(out, "  %s✘%s  Total coverage dropped vs base branch\n", cRed, cReset)
		}
	} else {
		fmt.Fprintf(out, "  %-30s %.2f%% (threshold: %.0f%%)\n", "Total coverage:", r.TotalCoverage, cfg.TotalThreshold)
		if r.TotalPassed {
			fmt.Fprintf(out, "  %s✔%s  Total threshold passed\n", cGreen, cReset)
		} else {
			fmt.Fprintf(out, "  %s✘%s  Total threshold not met\n", cRed, cReset)
		}
	}
	fmt.Fprintln(out)

	// Per-file breakdown.
	if len(r.Files) > 0 {
		fmt.Fprintf(out, "  %sFiles:%s\n", cBold, cReset)
		for _, f := range r.Files {
			switch f.Status {
			case "ok":
				fmt.Fprintf(out, "    %s✔%s %-50s %s%.1f%%%s\n", cGreen, cReset, f.Path, cGreen, f.Coverage, cReset)
			case "fail":
				fmt.Fprintf(out, "    %s✘%s %-50s %s%.1f%%%s\n", cRed, cReset, f.Path, cRed, f.Coverage, cReset)
			case "excluded":
				fmt.Fprintf(out, "    %s–%s %-50s %s(excluded)%s\n", cYellow, cReset, f.Path, cYellow, cReset)
			default:
				fmt.Fprintf(out, "    %s⚠%s %-50s %s(no data)%s\n", cYellow, cReset, f.Path, cYellow, cReset)
			}
		}
		fmt.Fprintln(out)
	}
}

// PrintWarnings writes each non-fatal warning recorded on the report,
// one per line, prefixed with the standard ⚠ glyph.
func PrintWarnings(out io.Writer, r *covlens.Report) {
	for _, w := range r.Warnings {
		fmt.Fprintf(out, "%s⚠%s %s\n", cYellow, cReset, w)
	}
}

// Info writes a green-checkmark success line to out.
func Info(out io.Writer, msg string) {
	fmt.Fprintf(out, "%s✔%s %s\n", cGreen, cReset, msg)
}

// Error writes a red-cross failure line to out.
func Error(out io.Writer, format string, args ...any) {
	fmt.Fprintf(out, "%s✘%s %s\n", cRed, cReset, fmt.Sprintf(format, args...))
}
