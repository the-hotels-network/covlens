// Package html generates self-contained HTML coverage reports for covlens.
package html

import (
	"fmt"
	"html/template"
	"os"
	"time"
)

// SourceFile holds rendered source HTML for one file.
type SourceFile struct {
	Path       string
	Package    string
	SourceHTML template.HTML
	Coverage   float64
	Status     string
}

// FileSummary holds per-file coverage data for the report table.
type FileSummary struct {
	Path       string
	Package    string
	Coverage   float64
	Statements int
	Covered    int
	Excluded   bool
	Status     string
}

// templateData is the struct passed to the HTML template.
type templateData struct {
	ReportInput
	GeneratedAt         string
	FileCount           int
	SourceFiles         []SourceFile
	CSS                 template.CSS
	TotalAboveThreshold bool // TotalCoverage >= TotalThreshold (independent of ratchet)
	HasDelta            bool // true when baseline coverage is available
	CoverageDelta       float64
	DeltaClass          string // "positive", "negative", or "neutral"
	InitialTheme        string // "light", "dark", or "" (auto — let CSS/JS decide)
}

// ReportInput contains all the data needed to generate an HTML report.
type ReportInput struct {
	DiffCoverage          float64
	TotalCoverage         float64
	BaselineTotalCoverage float64 // non-zero when RatchetTotal is true
	DiffPassed            bool
	TotalPassed           bool
	DiffThreshold         float64
	TotalThreshold        float64
	BaseBranch            string
	ShowExcluded          bool
	RatchetTotal          bool
	// Theme is the default report theme: "auto", "light", or "dark".
	Theme    string
	FullMode bool
	Files    []FileSummary
}

// Generate renders the HTML report as a single self-contained file.
func Generate(input ReportInput, sourceFiles []SourceFile, outputPath string) error {
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

	data := templateData{
		ReportInput:         input,
		GeneratedAt:         time.Now().Format("2006-01-02 15:04"),
		FileCount:           fileCount,
		SourceFiles:         sourceFiles,
		CSS:                 template.CSS(cssBytes),
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
