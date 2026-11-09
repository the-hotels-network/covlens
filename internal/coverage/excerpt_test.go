package coverage

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"
)

func TestFailureExcerpt(t *testing.T) {
	matches, err := filepath.Glob("testdata/excerpt/*.txtar")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("no fixtures matched testdata/excerpt/*.txtar")
	}
	for _, path := range matches {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".txtar"), func(t *testing.T) {
			ar, err := txtar.ParseFile(path)
			if err != nil {
				t.Fatal(err)
			}
			files := map[string][]byte{}
			for _, f := range ar.Files {
				files[f.Name] = f.Data
			}
			got := FailureExcerpt(files["output"])
			want := strings.TrimRight(string(files["want"]), "\n")
			if got != want {
				t.Errorf("excerpt mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

func TestFailureExcerpt_TruncatesLongOutput(t *testing.T) {
	var b strings.Builder
	for i := range maxExcerptLines + 5 {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	lines := strings.Split(FailureExcerpt([]byte(b.String())), "\n")
	if len(lines) != maxExcerptLines+1 {
		t.Fatalf("got %d lines, want %d", len(lines), maxExcerptLines+1)
	}
	if lines[0] != "line 0" {
		t.Errorf("first line = %q, want head kept", lines[0])
	}
	if last := lines[len(lines)-1]; last != "... (5 more lines)" {
		t.Errorf("last line = %q", last)
	}
}
