package directive

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

// FuncSpan identifies a function by name and its source line range.
type FuncSpan struct {
	Name      string
	StartLine int
	EndLine   int
}

// Exclusion holds the result of scanning a Go file for covlens:ignore directives.
type Exclusion struct {
	WholeFile bool
	Functions []FuncSpan
}

// Parse reads filePath as a Go source file and returns an Exclusion
// describing any covlens:ignore directives found in comments.
func Parse(filePath string) (*Exclusion, error) {
	// Fast path: skip full AST parse if the marker isn't present at all.
	src, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	if !bytes.Contains(src, []byte("covlens:ignore")) {
		return &Exclusion{}, nil
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	excl := &Exclusion{}

	// File-level exclusion: check comments that appear before the package keyword.
	pkgLine := fset.Position(f.Package).Line
	for _, cg := range f.Comments {
		if fset.Position(cg.Pos()).Line >= pkgLine {
			continue
		}
		for _, c := range cg.List {
			if isIgnoreDirective(c.Text) {
				excl.WholeFile = true
				return excl, nil
			}
		}
	}

	// Function-level exclusion: check doc comments on function declarations.
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Doc == nil {
			continue
		}
		for _, c := range fn.Doc.List {
			if isIgnoreDirective(c.Text) {
				excl.Functions = append(excl.Functions, FuncSpan{
					Name:      fn.Name.Name,
					StartLine: fset.Position(fn.Pos()).Line,
					EndLine:   fset.Position(fn.End()).Line,
				})
				break
			}
		}
	}

	return excl, nil
}

// isIgnoreDirective reports whether commentText is a //covlens:ignore directive
// rather than prose that happens to mention the string. The directive form
// (consistent with //go:embed, //go:build, etc.) is the literal "covlens:ignore"
// at the start of the comment text, optionally separated from "//" or "/*" by
// whitespace, optionally followed by a free-text rationale.
func isIgnoreDirective(commentText string) bool {
	text := strings.TrimPrefix(commentText, "//")
	text = strings.TrimPrefix(text, "/*")
	text = strings.TrimSuffix(text, "*/")
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "covlens:ignore")
}
