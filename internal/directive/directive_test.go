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
