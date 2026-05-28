// Package html generates self-contained HTML coverage reports for covlens.
package html

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"time"

	"github.com/the-hotels-network/covlens/internal/covlens"
)

// fileSummary is the per-file row in the report table (template-only).
type fileSummary struct {
	Path       string
	Package    string
	Coverage   float64
	Statements int
	Covered    int
	Excluded   bool
	Status     string
}

// sourceFile holds the rendered HTML for one file's source view (template-only).
type sourceFile struct {
	Path       string
	Package    string
	SourceHTML template.HTML
	Coverage   float64
	Status     string
}

// reportInput is the flat per-template payload the existing template expects.
// Internal to this package — built from covlens.Report + covlens.Config.
type reportInput struct {
	DiffStatus            covlens.DiffStatus
	DiffCoverage          float64
	DiffThreshold         float64
	DiffPassed            bool
	TotalCoverage         float64
	BaselineTotalCoverage float64
	TotalPassed           bool
	TotalThreshold        float64
	BaseBranch            string
	ShowExcluded          bool
	RatchetTotal          bool
	Theme                 string
	FullMode              bool
	Files                 []fileSummary
}

// templateData is the struct passed to the HTML template.
type templateData struct {
	reportInput
	GeneratedAt         string
	FileCount           int
	SourceFiles         []sourceFile
	CSS                 template.CSS
	TotalMeasured       bool
	TotalAboveThreshold bool
	HasDelta            bool
	CoverageDelta       float64
	DeltaClass          string
	InitialTheme        string
}

// Generate renders the HTML report from a covlens.Report into a single
// self-contained file at outputPath.
//
// A failure to render any single file's source view is a hard error: the
// likely causes (file deleted, permission denied, disk error) are systemic,
// and a partial report with silently-missing source views is worse UX than
// a clear failure message.
func Generate(r *covlens.Report, cfg covlens.Config, outputPath string) error {
	cssBytes, err := content.ReadFile("static/style.css")
	if err != nil {
		return fmt.Errorf("reading embedded CSS: %w", err)
	}

	tmplBytes, err := content.ReadFile("templates/report.html.tmpl")
	if err != nil {
		return fmt.Errorf("reading embedded template: %w", err)
	}

	tmpl, err := template.New("report").Parse(string(tmplBytes))
	if err != nil {
		return fmt.Errorf("parsing template: %w", err)
	}

	threshold := cfg.DiffThreshold
	if cfg.FullMode {
		threshold = cfg.TotalThreshold
	}

	files := make([]fileSummary, 0, len(r.Files))
	covByPath := make(map[string]covlens.FileCoverage, len(r.Files))
	for _, fc := range r.Files {
		files = append(files, fileSummary{
			Path:       fc.Path,
			Package:    fc.Package,
			Coverage:   fc.Coverage,
			Statements: fc.Statements,
			Covered:    fc.Covered,
			Excluded:   fc.Excluded,
			Status:     fc.StatusFor(threshold),
		})
		covByPath[fc.Path] = fc
	}

	sourceFiles := make([]sourceFile, 0, len(r.Sources))
	for _, src := range r.Sources {
		absPath := filepath.Join(r.SourceRoot, src.Path)
		rendered, err := RenderSource(absPath, src.Blocks, src.Hunks)
		if err != nil {
			return fmt.Errorf("rendering source for %s: %w", src.Path, err)
		}
		fc := covByPath[src.Path]
		sourceFiles = append(sourceFiles, sourceFile{
			Path:       src.Path,
			Package:    src.Package,
			SourceHTML: rendered,
			Coverage:   fc.Coverage,
			Status:     fc.StatusFor(threshold),
		})
	}

	input := reportInput{
		TotalCoverage:         r.TotalCoverage,
		BaselineTotalCoverage: r.BaselineTotalCoverage,
		TotalPassed:           r.TotalPassed,
		TotalThreshold:        cfg.TotalThreshold,
		BaseBranch:            cfg.BaseBranch,
		ShowExcluded:          cfg.ShowExcluded,
		RatchetTotal:          cfg.RatchetTotal,
		Theme:                 cfg.HTML.Theme,
		FullMode:              cfg.FullMode,
		Files:                 files,
	}
	if r.Diff != nil {
		input.DiffStatus = r.Diff.Status
		input.DiffCoverage = r.Diff.Coverage
		input.DiffThreshold = r.Diff.Threshold
		input.DiffPassed = r.Diff.Passed
	}

	fileCount := 0
	for _, f := range input.Files {
		if !f.Excluded || input.ShowExcluded {
			fileCount++
		}
	}

	hasDelta := input.RatchetTotal && input.BaselineTotalCoverage > 0
	delta := input.TotalCoverage - input.BaselineTotalCoverage
	deltaClass := "neutral"
	if hasDelta {
		if delta > 0.01 {
			deltaClass = "positive"
		} else if delta < -0.01 {
			deltaClass = "negative"
		}
	}

	initialTheme := ""
	if input.Theme == "light" || input.Theme == "dark" {
		initialTheme = input.Theme
	}

	// Total is only measured when RunTotal actually ran: full mode always
	// measures it; diff mode only under --ratchet. Otherwise the total
	// badge is hidden — the number isn't meaningful and showing 0% would
	// be misleading.
	totalMeasured := cfg.FullMode || cfg.RatchetTotal

	data := templateData{
		reportInput:         input,
		GeneratedAt:         time.Now().Format("2006-01-02 15:04"),
		FileCount:           fileCount,
		SourceFiles:         sourceFiles,
		CSS:                 template.CSS(cssBytes),
		TotalMeasured:       totalMeasured,
		TotalAboveThreshold: input.TotalCoverage >= input.TotalThreshold,
		HasDelta:            hasDelta,
		CoverageDelta:       delta,
		DeltaClass:          deltaClass,
		InitialTheme:        initialTheme,
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("executing template: %w", err)
	}

	return nil
}
