// Package json renders covlens reports as a stable, machine-readable JSON
// document for CI consumers. The schema is deliberately decoupled from the
// in-memory covlens.Report so internal renames don't break consumers.
package json

import (
	encjson "encoding/json"
	"io"
	"os"

	"github.com/the-hotels-network/covlens/internal/covlens"
)

// SchemaVersion identifies the wire format. Bump on breaking changes
// (renamed/removed/retyped fields). Adding fields is non-breaking.
const SchemaVersion = "1"

// Report is the JSON-serialized form of a covlens run.
type Report struct {
	Schema string `json:"schema"`
	Mode   string `json:"mode"` // "diff" or "full"

	BaseBranch string `json:"baseBranch,omitempty"`

	Diff *DiffSection `json:"diff,omitempty"` // omitted in --full mode

	TotalCoverage         float64 `json:"totalCoverage"`
	BaselineTotalCoverage float64 `json:"baselineTotalCoverage,omitempty"`
	TotalThreshold        float64 `json:"totalThreshold"`
	TotalPassed           bool    `json:"totalPassed"`
	RatchetTotal          bool    `json:"ratchetTotal,omitempty"`

	HTMLReportPath string `json:"htmlReportPath,omitempty"`

	Files []File `json:"files"`
}

// DiffSection is the JSON-serialized form of the diff portion of a run.
type DiffSection struct {
	Status    string  `json:"status"` // "measured" | "no-go-changes" | "only-deletions" | "all-excluded"
	Coverage  float64 `json:"coverage"`
	Threshold float64 `json:"threshold"`
	Passed    bool    `json:"passed"`
}

// File is a per-file entry in Report.Files.
//
// Coverage is -1 for files with no coverage data (excluded files, files
// outside any test scope). Consumers should check Excluded first.
type File struct {
	Path       string  `json:"path"`
	Package    string  `json:"package,omitempty"`
	Coverage   float64 `json:"coverage"`
	Statements int     `json:"statements,omitempty"`
	Covered    int     `json:"covered,omitempty"`
	Excluded   bool    `json:"excluded,omitempty"`
	Status     string  `json:"status"`
}

// Encode writes r as indented JSON to w.
func Encode(w io.Writer, r Report) error {
	enc := encjson.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// Write maps r and cfg to the JSON wire format and writes it to path.
func Write(r *covlens.Report, cfg covlens.Config, htmlPath, path string) error {
	threshold := cfg.DiffThreshold
	if cfg.FullMode {
		threshold = cfg.TotalThreshold
	}

	files := make([]File, 0, len(r.Files))
	for _, fc := range r.Files {
		files = append(files, File{
			Path:       fc.Path,
			Package:    fc.Package,
			Coverage:   fc.Coverage,
			Statements: fc.Statements,
			Covered:    fc.Covered,
			Excluded:   fc.Excluded,
			Status:     fc.StatusFor(threshold),
		})
	}

	mode := "diff"
	if cfg.FullMode {
		mode = "full"
	}

	out := Report{
		Schema:                SchemaVersion,
		Mode:                  mode,
		BaseBranch:            cfg.BaseBranch,
		TotalCoverage:         r.TotalCoverage,
		BaselineTotalCoverage: r.BaselineTotalCoverage,
		TotalThreshold:        cfg.TotalThreshold,
		TotalPassed:           r.TotalPassed,
		RatchetTotal:          cfg.RatchetTotal,
		HTMLReportPath:        htmlPath,
		Files:                 files,
	}
	if r.Diff != nil {
		out.Diff = &DiffSection{
			Status:    string(r.Diff.Status),
			Coverage:  r.Diff.Coverage,
			Threshold: r.Diff.Threshold,
			Passed:    r.Diff.Passed,
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return Encode(f, out)
}
