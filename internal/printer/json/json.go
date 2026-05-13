// Package json renders covlens reports as a stable, machine-readable JSON
// document for CI consumers. The schema is deliberately decoupled from the
// in-memory covlens.Report so internal renames don't break consumers.
package json

import (
	encjson "encoding/json"
	"io"
	"os"

	"github.com/erioch/covlens/internal/covlens"
)

// SchemaVersion identifies the wire format. Bump on breaking changes
// (renamed/removed/retyped fields). Adding fields is non-breaking.
const SchemaVersion = "1"

// Report is the JSON-serialized form of a covlens run.
type Report struct {
	Schema string `json:"schema"`
	Mode   string `json:"mode"` // "diff" or "full"

	BaseBranch string `json:"baseBranch,omitempty"`

	DiffCoverage          float64 `json:"diffCoverage"`
	TotalCoverage         float64 `json:"totalCoverage"`
	BaselineTotalCoverage float64 `json:"baselineTotalCoverage,omitempty"`

	DiffThreshold  float64 `json:"diffThreshold"`
	TotalThreshold float64 `json:"totalThreshold"`
	RatchetTotal   bool    `json:"ratchetTotal,omitempty"`

	DiffPassed  bool `json:"diffPassed"`
	TotalPassed bool `json:"totalPassed"`

	HTMLReportPath string `json:"htmlReportPath,omitempty"`

	Files []File `json:"files"`
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
	files := make([]File, 0, len(r.Files))
	for _, fc := range r.Files {
		files = append(files, File{
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

	out := Report{
		Schema:                SchemaVersion,
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
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return Encode(f, out)
}
