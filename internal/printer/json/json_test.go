package json

import (
	"bytes"
	encjson "encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/the-hotels-network/covlens/internal/covlens"
)

func TestEncode_RoundTrip(t *testing.T) {
	in := Report{
		Schema:     SchemaVersion,
		Mode:       "diff",
		BaseBranch: "main",
		Diff: &DiffSection{
			Status:    string(covlens.DiffStatusMeasured),
			Coverage:  82.5,
			Threshold: 80,
			Passed:    true,
		},
		TotalCoverage:         71.0,
		BaselineTotalCoverage: 70.5,
		TotalThreshold:        70,
		TotalPassed:           true,
		RatchetTotal:          true,
		HTMLReportPath:        "/tmp/coverage_report.html",
		Files: []File{
			{Path: "foo.go", Package: "example/foo", Coverage: 100, Statements: 5, Covered: 5, Status: "ok"},
			{Path: "bar.go", Package: "example/bar", Coverage: 50, Statements: 4, Covered: 2, Status: "fail"},
			{Path: "mocks_x.go", Package: "example/mocks", Coverage: -1, Excluded: true, Status: "excluded"},
		},
	}

	var buf bytes.Buffer
	if err := Encode(&buf, in); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var got Report
	if err := encjson.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput was:\n%s", err, buf.String())
	}

	if got.Schema != SchemaVersion {
		t.Errorf("Schema = %q, want %q", got.Schema, SchemaVersion)
	}
	if got.Mode != "diff" {
		t.Errorf("Mode = %q, want %q", got.Mode, "diff")
	}
	if got.Diff == nil {
		t.Fatal("Diff = nil, want a populated DiffSection")
	}
	if got.Diff.Coverage != 82.5 {
		t.Errorf("Diff.Coverage = %v, want 82.5", got.Diff.Coverage)
	}
	if got.Diff.Status != "measured" {
		t.Errorf("Diff.Status = %q, want %q", got.Diff.Status, "measured")
	}
	if !got.RatchetTotal {
		t.Error("RatchetTotal = false, want true")
	}
	if len(got.Files) != 3 {
		t.Fatalf("Files: got %d, want 3", len(got.Files))
	}
	if !got.Files[2].Excluded {
		t.Error("Files[2].Excluded = false, want true (mocks_x.go)")
	}
	if got.Files[2].Status != "excluded" {
		t.Errorf("Files[2].Status = %q, want %q", got.Files[2].Status, "excluded")
	}
	if got.HTMLReportPath != "/tmp/coverage_report.html" {
		t.Errorf("HTMLReportPath = %q, want %q", got.HTMLReportPath, "/tmp/coverage_report.html")
	}
}

func TestEncode_OmitsEmptyOptionalFields(t *testing.T) {
	// Minimal Report — exercises omitempty on RatchetTotal, baseline, htmlReportPath, etc.
	in := Report{
		Schema: SchemaVersion,
		Mode:   "diff",
		Diff: &DiffSection{
			Status:    string(covlens.DiffStatusMeasured),
			Threshold: 80,
			Passed:    true,
		},
		TotalThreshold: 70,
		TotalPassed:    true,
		Files:          []File{},
	}

	var buf bytes.Buffer
	if err := Encode(&buf, in); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out := buf.String()

	// Required fields must always appear.
	for _, want := range []string{`"schema"`, `"mode"`, `"diff"`, `"totalPassed"`, `"files"`} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing required field %s\n%s", want, out)
		}
	}
	// Optional fields must be omitted when zero/empty.
	for _, banned := range []string{`"baselineTotalCoverage"`, `"ratchetTotal"`, `"htmlReportPath"`, `"baseBranch"`} {
		if strings.Contains(out, banned) {
			t.Errorf("output unexpectedly contains optional field %s\n%s", banned, out)
		}
	}
}

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")

	report := &covlens.Report{
		TotalCoverage: 85.0,
		TotalPassed:   true,
		Diff: &covlens.DiffSection{
			Status:    covlens.DiffStatusMeasured,
			Coverage:  90.0,
			Threshold: 80,
			Passed:    true,
		},
		Files: []covlens.FileCoverage{
			{Path: "foo.go", Package: "pkg", Coverage: 90.0, Statements: 10, Covered: 9},
			{Path: "bar.go", Excluded: true, Coverage: -1},
		},
	}
	cfg := covlens.Config{BaseBranch: "main", DiffThreshold: 80, TotalThreshold: 70}

	if err := Write(report, cfg, "/tmp/report.html", path); err != nil {
		t.Fatalf("Write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var got Report
	if err := encjson.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput:\n%s", err, data)
	}

	if got.Mode != "diff" {
		t.Errorf("Mode = %q, want diff", got.Mode)
	}
	if got.TotalCoverage != 85.0 {
		t.Errorf("TotalCoverage = %v, want 85.0", got.TotalCoverage)
	}
	if got.Diff == nil || got.Diff.Coverage != 90.0 {
		t.Errorf("Diff.Coverage missing or wrong; got %+v", got.Diff)
	}
	if got.HTMLReportPath != "/tmp/report.html" {
		t.Errorf("HTMLReportPath = %q, want /tmp/report.html", got.HTMLReportPath)
	}
	if len(got.Files) != 2 {
		t.Fatalf("Files: got %d, want 2", len(got.Files))
	}
	if got.Files[1].Status != "excluded" {
		t.Errorf("Files[1].Status = %q, want excluded", got.Files[1].Status)
	}
}

func TestWrite_FullMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")

	report := &covlens.Report{TotalCoverage: 75.0, TotalPassed: true}
	cfg := covlens.Config{TotalThreshold: 70, DiffThreshold: 80, FullMode: true}

	if err := Write(report, cfg, "", path); err != nil {
		t.Fatalf("Write: %v", err)
	}

	data, _ := os.ReadFile(path)
	var got Report
	encjson.Unmarshal(data, &got)

	if got.Mode != "full" {
		t.Errorf("Mode = %q, want full", got.Mode)
	}
	if got.Diff != nil {
		t.Errorf("Diff = %+v, want nil in full mode", got.Diff)
	}
}
