package json

import (
	"bytes"
	encjson "encoding/json"
	"strings"
	"testing"
)

func TestEncode_RoundTrip(t *testing.T) {
	in := Report{
		Schema:                SchemaVersion,
		Mode:                  "diff",
		BaseBranch:            "main",
		DiffCoverage:          82.5,
		TotalCoverage:         71.0,
		BaselineTotalCoverage: 70.5,
		DiffThreshold:         80,
		TotalThreshold:        70,
		RatchetTotal:          true,
		DiffPassed:            true,
		TotalPassed:           true,
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
	if got.DiffCoverage != 82.5 {
		t.Errorf("DiffCoverage = %v, want 82.5", got.DiffCoverage)
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
		Schema:         SchemaVersion,
		Mode:           "diff",
		DiffCoverage:   0,
		TotalCoverage:  0,
		DiffThreshold:  80,
		TotalThreshold: 70,
		DiffPassed:     true,
		TotalPassed:    true,
		Files:          []File{},
	}

	var buf bytes.Buffer
	if err := Encode(&buf, in); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out := buf.String()

	// Required fields must always appear.
	for _, want := range []string{`"schema"`, `"mode"`, `"diffCoverage"`, `"diffPassed"`, `"files"`} {
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
