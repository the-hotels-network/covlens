package directive

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFileLevelIgnore(t *testing.T) {
	src := `//covlens:ignore
package foo

func Hello() {}
`
	path := writeTemp(t, "file.go", src)
	excl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if !excl.WholeFile {
		t.Fatal("expected WholeFile to be true")
	}
}

func TestFunctionLevelIgnore(t *testing.T) {
	src := `package foo

//covlens:ignore
func Secret() {
	_ = 1
}
`
	path := writeTemp(t, "func.go", src)
	excl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if excl.WholeFile {
		t.Fatal("expected WholeFile to be false")
	}
	if len(excl.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(excl.Functions))
	}
	fn := excl.Functions[0]
	if fn.Name != "Secret" {
		t.Fatalf("expected function name Secret, got %s", fn.Name)
	}
	if fn.StartLine != 4 {
		t.Fatalf("expected StartLine 4, got %d", fn.StartLine)
	}
	if fn.EndLine != 6 {
		t.Fatalf("expected EndLine 6, got %d", fn.EndLine)
	}
}

func TestNoDirectives(t *testing.T) {
	src := `package foo

func Hello() {}

func World() {}
`
	path := writeTemp(t, "clean.go", src)
	excl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if excl.WholeFile {
		t.Fatal("expected WholeFile to be false")
	}
	if len(excl.Functions) != 0 {
		t.Fatalf("expected 0 functions, got %d", len(excl.Functions))
	}
}

func TestMultiLineDocComment(t *testing.T) {
	src := `package foo

// Important function.
// covlens:ignore
// Does secret things.
func Secret() {
	_ = 1
}
`
	path := writeTemp(t, "multiline.go", src)
	excl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if excl.WholeFile {
		t.Fatal("expected WholeFile to be false")
	}
	if len(excl.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(excl.Functions))
	}
	if excl.Functions[0].Name != "Secret" {
		t.Fatalf("expected function name Secret, got %s", excl.Functions[0].Name)
	}
}

func TestMultipleFunctionsOnlyOneIgnored(t *testing.T) {
	src := `package foo

func Public() {
	_ = 1
}

//covlens:ignore
func Private() {
	_ = 2
}

func Another() {
	_ = 3
}
`
	path := writeTemp(t, "multi.go", src)
	excl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if excl.WholeFile {
		t.Fatal("expected WholeFile to be false")
	}
	if len(excl.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(excl.Functions))
	}
	fn := excl.Functions[0]
	if fn.Name != "Private" {
		t.Fatalf("expected function name Private, got %s", fn.Name)
	}
}

func TestBlockCommentFileLevelIgnore(t *testing.T) {
	src := `/* covlens:ignore */
package foo

func Hello() {}
`
	path := writeTemp(t, "block.go", src)
	excl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if !excl.WholeFile {
		t.Fatal("expected WholeFile to be true")
	}
}

// TestProseMentionDoesNotIgnore guards the regression discovered by running
// covlens on its own directive package: a doc comment that *talks about* the
// covlens:ignore marker (without applying it as a directive) was being
// mis-detected because of substring matching.
func TestProseMentionDoesNotIgnore(t *testing.T) {
	src := `package foo

// Parse scans a Go file and returns any covlens:ignore directives found
// in comments. The string "covlens:ignore" appearing inside this prose
// is descriptive, not a directive.
func Parse() {
	_ = 1
}
`
	path := writeTemp(t, "prose.go", src)
	excl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if excl.WholeFile {
		t.Error("expected WholeFile=false; prose mention is not a directive")
	}
	if len(excl.Functions) != 0 {
		t.Errorf("expected 0 ignored functions; got %d (prose mention is not a directive)", len(excl.Functions))
	}
}

// TestProseMentionInFileHeaderDoesNotIgnore guards the same regression at the
// file-level path: a comment above the package keyword that mentions the
// directive in prose should NOT trigger whole-file exclusion.
func TestProseMentionInFileHeaderDoesNotIgnore(t *testing.T) {
	src := `// Package foo demonstrates the covlens:ignore marker in prose.
// Real directives appear at the start of a comment, like //covlens:ignore.
package foo

func Hello() {}
`
	path := writeTemp(t, "header.go", src)
	excl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if excl.WholeFile {
		t.Error("expected WholeFile=false; prose mention above package is not a directive")
	}
}

// TestDirectiveWithTrailingMessage ensures the canonical form with a trailing
// rationale ("//covlens:ignore — reason") is still detected.
func TestDirectiveWithTrailingMessage(t *testing.T) {
	src := `package foo

//covlens:ignore — generated, do not edit
func Generated() {
	_ = 1
}
`
	path := writeTemp(t, "trailing.go", src)
	excl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(excl.Functions) != 1 || excl.Functions[0].Name != "Generated" {
		t.Errorf("expected Generated to be ignored; got %+v", excl.Functions)
	}
}
