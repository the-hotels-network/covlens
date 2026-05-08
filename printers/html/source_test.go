package html

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/cover"
	"golang.org/x/tools/txtar"

	"github.com/erioch/covlens"
)

var update = flag.Bool("update", false, "regenerate golden output inside txtar fixtures")

// TestRenderSource_Goldens locks down the current chroma+annotator output for a
// handful of representative coverage shapes. It exists so the splitAndAnnotate
// refactor (#12) can proceed as a refactor rather than a rewrite.
//
// Each fixture is a txtar archive containing source.go, blocks.txt, an optional
// hunks.txt (presence switches diff mode on), and the generated output.golden.html.
// Run `go test ./printers/html -run RenderSource_Goldens -update` to regenerate.
func TestRenderSource_Goldens(t *testing.T) {
	matches, err := filepath.Glob("testdata/source/*.txtar")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("no fixtures matched testdata/source/*.txtar")
	}

	for _, path := range matches {
		name := strings.TrimSuffix(filepath.Base(path), ".txtar")
		t.Run(name, func(t *testing.T) {
			ar, err := txtar.ParseFile(path)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}

			src := mustReadArchive(t, ar, "source.go")
			blocks := parseBlocks(t, mustReadArchive(t, ar, "blocks.txt"))

			var hunks []covlens.Hunk
			if raw, ok := readArchive(ar, "hunks.txt"); ok {
				hunks = parseHunks(t, raw)
			}

			tmp := filepath.Join(t.TempDir(), "source.go")
			if err := os.WriteFile(tmp, src, 0o644); err != nil {
				t.Fatal(err)
			}

			got, err := RenderSource(tmp, blocks, hunks)
			if err != nil {
				t.Fatalf("RenderSource: %v", err)
			}

			if *update {
				writeArchive(ar, "output.golden.html", []byte(got))
				if err := os.WriteFile(path, txtar.Format(ar), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Logf("updated %s", path)
				return
			}

			// txtar.Format guarantees a trailing newline on every file's data,
			// so normalize before comparing.
			want := bytes.TrimRight(mustReadArchive(t, ar, "output.golden.html"), "\n")
			gotBytes := bytes.TrimRight([]byte(got), "\n")
			if !bytes.Equal(gotBytes, want) {
				t.Errorf("RenderSource output differs from golden — re-run with -update if intentional\n--- got ---\n%s\n--- want ---\n%s", gotBytes, want)
			}
		})
	}
}

func mustReadArchive(t *testing.T, ar *txtar.Archive, name string) []byte {
	t.Helper()
	data, ok := readArchive(ar, name)
	if !ok {
		t.Fatalf("file %q not present in archive", name)
	}
	return data
}

func readArchive(ar *txtar.Archive, name string) ([]byte, bool) {
	for _, f := range ar.Files {
		if f.Name == name {
			return f.Data, true
		}
	}
	return nil, false
}

func writeArchive(ar *txtar.Archive, name string, data []byte) {
	for i, f := range ar.Files {
		if f.Name == name {
			ar.Files[i].Data = data
			return
		}
	}
	ar.Files = append(ar.Files, txtar.File{Name: name, Data: data})
}

func parseBlocks(t *testing.T, raw []byte) []cover.ProfileBlock {
	t.Helper()
	var blocks []cover.ProfileBlock
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 6 {
			t.Fatalf("blocks.txt line %d: want 6 fields, got %d (%q)", i+1, len(fields), line)
		}
		nums := make([]int, 6)
		for j, f := range fields {
			n, err := strconv.Atoi(f)
			if err != nil {
				t.Fatalf("blocks.txt line %d field %d: %v", i+1, j+1, err)
			}
			nums[j] = n
		}
		blocks = append(blocks, cover.ProfileBlock{
			StartLine: nums[0],
			StartCol:  nums[1],
			EndLine:   nums[2],
			EndCol:    nums[3],
			NumStmt:   nums[4],
			Count:     nums[5],
		})
	}
	return blocks
}

func parseHunks(t *testing.T, raw []byte) []covlens.Hunk {
	t.Helper()
	var hunks []covlens.Hunk
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("hunks.txt line %d: want 2 fields, got %d", i+1, len(fields))
		}
		start, err := strconv.Atoi(fields[0])
		if err != nil {
			t.Fatalf("hunks.txt line %d start: %v", i+1, err)
		}
		end, err := strconv.Atoi(fields[1])
		if err != nil {
			t.Fatalf("hunks.txt line %d end: %v", i+1, err)
		}
		hunks = append(hunks, covlens.Hunk{Start: start, End: end})
	}
	return hunks
}
