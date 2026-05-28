// Package console renders covlens reports as ANSI-colored terminal output.
package console

import (
	"fmt"
	"io"

	"github.com/the-hotels-network/covlens/internal/covlens"
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

	// Diff coverage (skipped in full mode). For non-measured statuses
	// (only AllExcluded reaches here; NoGoChanges/OnlyDeletions are
	// short-circuited in main before PrintSummary runs) replace the
	// numeric line with a chip explaining the vacuous pass.
	if r.Diff != nil {
		switch r.Diff.Status {
		case covlens.DiffStatusMeasured:
			fmt.Fprintf(out, "  %-30s %.2f%% (threshold: %.0f%%)\n", "Diff coverage:", r.Diff.Coverage, r.Diff.Threshold)
			if r.Diff.Passed {
				fmt.Fprintf(out, "  %s✔%s  Diff threshold passed\n", cGreen, cReset)
			} else {
				fmt.Fprintf(out, "  %s✘%s  Diff threshold not met\n", cRed, cReset)
			}
		case covlens.DiffStatusAllExcluded:
			fmt.Fprintf(out, "  %s–%s  Diff coverage:                 %sall changed Go files excluded by exclude_files%s\n", cYellow, cReset, cYellow, cReset)
		}
	}

	// Total coverage. Rendered only when actually measured: full mode
	// (no Diff section) always measures total; diff mode measures it
	// only under --ratchet.
	totalMeasured := r.Diff == nil || cfg.RatchetTotal
	if totalMeasured {
		if cfg.RatchetTotal && r.BaselineTotalCoverage > 0 {
			delta := r.TotalCoverage - r.BaselineTotalCoverage
			// Color the delta to give an at-a-glance signal: green when
			// coverage rose, red when it fell, neutral within the ±0.01pp band
			// that's effectively a wash.
			deltaColor := cReset
			if delta > 0.01 {
				deltaColor = cGreen
			} else if delta < -0.01 {
				deltaColor = cRed
			}
			if cfg.TotalThreshold > 0 {
				fmt.Fprintf(out, "  %-30s %.2f%% (threshold: %.0f%%, baseline: %.2f%%, %sΔ %+.2fpp%s)\n",
					"Total coverage:", r.TotalCoverage, cfg.TotalThreshold, r.BaselineTotalCoverage,
					deltaColor, delta, cReset)
			} else {
				fmt.Fprintf(out, "  %-30s %.2f%% (baseline: %.2f%%, %sΔ %+.2fpp%s)\n",
					"Total coverage:", r.TotalCoverage, r.BaselineTotalCoverage,
					deltaColor, delta, cReset)
			}
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
	}
	fmt.Fprintln(out)

	// Per-file breakdown. When entries exist but all of them get filtered
	// (e.g. all changed files excluded with ShowExcluded=false), surface a
	// hint instead of a bare "Files:" header followed by silence.
	if len(r.Files) > 0 {
		visible := 0
		for _, f := range r.Files {
			if !f.Excluded || cfg.ShowExcluded {
				visible++
			}
		}
		fmt.Fprintf(out, "  %sFiles:%s\n", cBold, cReset)
		if visible == 0 {
			fmt.Fprintf(out, "    %s(no measurable changed files)%s\n", cYellow, cReset)
		} else {
			threshold := cfg.DiffThreshold
			if cfg.FullMode {
				threshold = cfg.TotalThreshold
			}
			for _, f := range r.Files {
				if f.Excluded && !cfg.ShowExcluded {
					continue
				}
				switch f.StatusFor(threshold) {
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
		}
		fmt.Fprintln(out)
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
