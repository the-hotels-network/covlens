package covlens

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogProgress(t *testing.T) {
	var buf bytes.Buffer
	logProgress(&buf, "running tests")
	out := buf.String()
	if !strings.Contains(out, "running tests") {
		t.Errorf("output missing message: %q", out)
	}
	if !strings.Contains(out, "▶") {
		t.Errorf("output missing ▶ marker: %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("output not newline-terminated: %q", out)
	}
}

// TestOpenTestOutputLog exercises the three precedence branches: verbose
// passes through cfg.testOutput(), an explicit TestOutput is respected
// unchanged, and the default creates a log file under outputDir.
func TestOpenTestOutputLog(t *testing.T) {
	t.Run("verbose mode returns cfg writer", func(t *testing.T) {
		buf := &bytes.Buffer{}
		r := &runner{cfg: Config{VerboseTests: true, TestOutput: buf, Stderr: io.Discard}}
		w, cleanup := r.openTestOutputLog()
		defer cleanup()
		if w != buf {
			t.Errorf("expected cfg writer, got %T", w)
		}
	})

	t.Run("explicit TestOutput is respected when not verbose", func(t *testing.T) {
		buf := &bytes.Buffer{}
		r := &runner{cfg: Config{TestOutput: buf, Stderr: io.Discard}}
		w, cleanup := r.openTestOutputLog()
		defer cleanup()
		if w != buf {
			t.Errorf("expected cfg writer, got %T", w)
		}
	})

	t.Run("default creates log file in outputDir", func(t *testing.T) {
		dir := t.TempDir()
		r := &runner{outputDir: dir, cfg: Config{Stderr: io.Discard}}
		w, cleanup := r.openTestOutputLog()
		fmt.Fprintln(w, "captured test output")
		cleanup()
		data, err := os.ReadFile(filepath.Join(dir, "test-output.log"))
		if err != nil {
			t.Fatalf("expected log file at %s: %v", dir, err)
		}
		if !strings.Contains(string(data), "captured test output") {
			t.Errorf("log file did not capture writes: %q", data)
		}
	})
}
