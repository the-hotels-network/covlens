package console_test

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/erioch/covlens/internal/covlens"
	"github.com/erioch/covlens/internal/printer/console"
)

// ansiRE matches ANSI SGR escape sequences. Tests strip them so assertions
// stay readable and aren't broken by future color tweaks.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// assertContainsAll fails the test if any of the wanted substrings is missing
// from the ANSI-stripped output. Reports the full output on failure so the
// reader can see what was actually rendered.
func assertContainsAll(t *testing.T, got string, wants ...string) {
	t.Helper()
	clean := stripANSI(got)
	for _, w := range wants {
		if !strings.Contains(clean, w) {
			t.Errorf("output missing %q\n--- got ---\n%s", w, clean)
		}
	}
}

func assertNotContains(t *testing.T, got string, banned ...string) {
	t.Helper()
	clean := stripANSI(got)
	for _, b := range banned {
		if strings.Contains(clean, b) {
			t.Errorf("output unexpectedly contains %q\n--- got ---\n%s", b, clean)
		}
	}
}

func TestPrintSummary_DiffMode_AllPass(t *testing.T) {
	cfg := covlens.Config{DiffThreshold: 80, TotalThreshold: 70}
	report := &covlens.Report{
		DiffCoverage:  92.5,
		TotalCoverage: 78.0,
		DiffPassed:    true,
		TotalPassed:   true,
		Files: []covlens.FileCoverage{
			{Path: "foo.go", Coverage: 92.5, Status: "ok"},
		},
	}

	var buf bytes.Buffer
	console.PrintSummary(&buf, report, cfg)

	assertContainsAll(t, buf.String(),
		"Summary",
		"Diff coverage:", "92.50%", "threshold: 80%",
		"Diff threshold passed",
		"Total coverage:", "78.00%", "threshold: 70%",
		"Total threshold passed",
		"Files:", "foo.go", "92.5%",
	)
}

func TestPrintSummary_DiffMode_DiffFailed(t *testing.T) {
	cfg := covlens.Config{DiffThreshold: 80, TotalThreshold: 70}
	report := &covlens.Report{
		DiffCoverage: 50.0, TotalCoverage: 78.0,
		DiffPassed: false, TotalPassed: true,
	}

	var buf bytes.Buffer
	console.PrintSummary(&buf, report, cfg)

	assertContainsAll(t, buf.String(),
		"Diff threshold not met",
		"Total threshold passed",
	)
}

func TestPrintSummary_DiffMode_TotalFailed(t *testing.T) {
	cfg := covlens.Config{DiffThreshold: 80, TotalThreshold: 70}
	report := &covlens.Report{
		DiffCoverage: 92.0, TotalCoverage: 50.0,
		DiffPassed: true, TotalPassed: false,
	}

	var buf bytes.Buffer
	console.PrintSummary(&buf, report, cfg)

	assertContainsAll(t, buf.String(),
		"Diff threshold passed",
		"Total threshold not met",
	)
}

func TestPrintSummary_FullMode_SkipsDiffSection(t *testing.T) {
	cfg := covlens.Config{TotalThreshold: 70, FullMode: true}
	report := &covlens.Report{
		TotalCoverage: 78.0, TotalPassed: true,
		DiffPassed: true, // ignored in full mode
	}

	var buf bytes.Buffer
	console.PrintSummary(&buf, report, cfg)

	assertContainsAll(t, buf.String(),
		"Total coverage:", "Total threshold passed",
	)
	assertNotContains(t, buf.String(),
		"Diff coverage:", "Diff threshold",
	)
}

func TestPrintSummary_Ratchet_WithBaseline_DidNotDrop(t *testing.T) {
	cfg := covlens.Config{DiffThreshold: 80, RatchetTotal: true}
	report := &covlens.Report{
		DiffCoverage: 95.0, TotalCoverage: 78.5, BaselineTotalCoverage: 78.0,
		DiffPassed: true, TotalPassed: true,
	}

	var buf bytes.Buffer
	console.PrintSummary(&buf, report, cfg)

	assertContainsAll(t, buf.String(),
		"Total coverage:", "78.50%", "baseline: 78.00%",
		"Total coverage did not drop",
	)
	// "threshold:" still appears for the diff section; only the total section
	// switches to baseline in ratchet mode. Don't ban it globally.
	assertNotContains(t, buf.String(), "Total threshold")
}

func TestPrintSummary_Ratchet_WithBaseline_Dropped(t *testing.T) {
	cfg := covlens.Config{DiffThreshold: 80, RatchetTotal: true}
	report := &covlens.Report{
		DiffCoverage: 95.0, TotalCoverage: 70.0, BaselineTotalCoverage: 78.0,
		DiffPassed: true, TotalPassed: false,
	}

	var buf bytes.Buffer
	console.PrintSummary(&buf, report, cfg)

	assertContainsAll(t, buf.String(),
		"baseline: 78.00%",
		"Total coverage dropped vs base branch",
	)
}

// TestPrintSummary_Ratchet_BaselineZero exercises the fallback path when
// --ratchet was set but the baseline could not be computed (BaselineTotalCoverage = 0).
// In that case PrintSummary falls through to the threshold display.
func TestPrintSummary_Ratchet_BaselineZero(t *testing.T) {
	cfg := covlens.Config{DiffThreshold: 80, TotalThreshold: 70, RatchetTotal: true}
	report := &covlens.Report{
		DiffCoverage: 95.0, TotalCoverage: 78.0, BaselineTotalCoverage: 0,
		DiffPassed: true, TotalPassed: true,
	}

	var buf bytes.Buffer
	console.PrintSummary(&buf, report, cfg)

	assertContainsAll(t, buf.String(),
		"threshold: 70%",
		"Total threshold passed",
	)
	assertNotContains(t, buf.String(), "baseline:", "did not drop")
}

func TestPrintSummary_NoFiles_SkipsFilesSection(t *testing.T) {
	cfg := covlens.Config{DiffThreshold: 80, TotalThreshold: 70}
	report := &covlens.Report{
		DiffCoverage: 92.0, TotalCoverage: 78.0,
		DiffPassed: true, TotalPassed: true,
		Files: nil,
	}

	var buf bytes.Buffer
	console.PrintSummary(&buf, report, cfg)

	assertNotContains(t, buf.String(), "Files:")
}

func TestPrintSummary_FileStatuses(t *testing.T) {
	cfg := covlens.Config{DiffThreshold: 80, TotalThreshold: 70}
	report := &covlens.Report{
		DiffPassed: true, TotalPassed: true,
		Files: []covlens.FileCoverage{
			{Path: "ok.go", Coverage: 95.0, Status: "ok"},
			{Path: "fail.go", Coverage: 40.0, Status: "fail"},
			{Path: "mocks.go", Coverage: -1, Excluded: true, Status: "excluded"},
			{Path: "unknown.go", Coverage: -1, Status: "warn"},
		},
	}

	var buf bytes.Buffer
	console.PrintSummary(&buf, report, cfg)

	assertContainsAll(t, buf.String(),
		"ok.go", "95.0%",
		"fail.go", "40.0%",
		"mocks.go", "(excluded)",
		"unknown.go", "(no data)",
	)
}

func TestInfo(t *testing.T) {
	var buf bytes.Buffer
	console.Info(&buf, "All thresholds passed!")

	got := buf.String()
	assertContainsAll(t, got, "All thresholds passed!")
	if !strings.HasSuffix(got, "\n") {
		t.Error("Info: expected trailing newline")
	}
	if !strings.Contains(got, "\x1b[0;32m") {
		t.Error("Info: expected green ANSI escape (\\x1b[0;32m)")
	}
}

func TestError(t *testing.T) {
	var buf bytes.Buffer
	console.Error(&buf, "failed to load %s: %d", "config.yaml", 42)

	got := buf.String()
	assertContainsAll(t, got, "failed to load config.yaml: 42")
	if !strings.HasSuffix(got, "\n") {
		t.Error("Error: expected trailing newline")
	}
	if !strings.Contains(got, "\x1b[0;31m") {
		t.Error("Error: expected red ANSI escape (\\x1b[0;31m)")
	}
}
