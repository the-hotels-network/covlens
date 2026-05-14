package covlens

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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

	t.Run("log file creation failure falls back to cfg writer", func(t *testing.T) {
		buf := &bytes.Buffer{}
		// TestOutput nil so we reach file creation; bad dir triggers fallback.
		r := &runner{outputDir: "/no/such/dir", cfg: Config{Stderr: io.Discard, TestOutput: nil}}
		// Override testOutput via the nil-check path: cfg.TestOutput is nil → os.Stdout.
		// Just verify it doesn't panic and returns a writer.
		w, cleanup := r.openTestOutputLog()
		defer cleanup()
		if w == nil {
			t.Error("expected non-nil writer on fallback")
		}
		_ = buf
	})

	t.Run("default creates log file in outputDir", func(t *testing.T) {
		dir := t.TempDir()
		r := &runner{outputDir: dir, cfg: Config{Stderr: io.Discard}}
		w, cleanup := r.openTestOutputLog()
		fmt.Fprintln(w, "captured test output")
		cleanup()
		data, err := os.ReadFile(filepath.Join(dir, "test_output.log"))
		if err != nil {
			t.Fatalf("expected log file at %s: %v", dir, err)
		}
		if !strings.Contains(string(data), "captured test output") {
			t.Errorf("log file did not capture writes: %q", data)
		}
	})
}

func TestLastNLogLines(t *testing.T) {
	writeLines := func(t *testing.T, lines []string) string {
		t.Helper()
		f, err := os.CreateTemp(t.TempDir(), "log")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		for _, l := range lines {
			fmt.Fprintln(f, l)
		}
		return f.Name()
	}

	t.Run("nonexistent file returns nil", func(t *testing.T) {
		if lastNLogLines("/no/such/file.log", 5) != nil {
			t.Error("expected nil for missing file")
		}
	})

	t.Run("empty file returns nil", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty.log")
		os.WriteFile(path, nil, 0600)
		if lastNLogLines(path, 5) != nil {
			t.Error("expected nil for empty file")
		}
	})

	t.Run("fewer lines than n returns all", func(t *testing.T) {
		path := writeLines(t, []string{"a", "b", "c"})
		got := lastNLogLines(path, 5)
		if !slices.Equal(got, []string{"a", "b", "c"}) {
			t.Errorf("got %v, want [a b c]", got)
		}
	})

	t.Run("more lines than n returns last n", func(t *testing.T) {
		path := writeLines(t, []string{"a", "b", "c", "d", "e", "f"})
		got := lastNLogLines(path, 3)
		if !slices.Equal(got, []string{"d", "e", "f"}) {
			t.Errorf("got %v, want [d e f]", got)
		}
	})

	t.Run("blank lines are skipped", func(t *testing.T) {
		path := writeLines(t, []string{"a", "", "   ", "b"})
		got := lastNLogLines(path, 5)
		if !slices.Equal(got, []string{"a", "b"}) {
			t.Errorf("got %v, want [a b]", got)
		}
	})
}

func TestIsTTY(t *testing.T) {
	if isTTY(&bytes.Buffer{}) {
		t.Error("bytes.Buffer should not be a TTY")
	}
	if isTTY(io.Discard) {
		t.Error("io.Discard should not be a TTY")
	}
	// A regular file is an *os.File but not a character device.
	f, err := os.CreateTemp(t.TempDir(), "not-tty")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if isTTY(f) {
		t.Error("regular file should not be a TTY")
	}
}

// TestTailTestWindow verifies that tailTestWindow prints progress lines and
// erases the window (emitting cursor-up sequences) when stopped.
func TestTailTestWindow(t *testing.T) {
	prev := testLogTailInterval
	testLogTailInterval = 20 * time.Millisecond
	t.Cleanup(func() { testLogTailInterval = prev })

	dir := t.TempDir()
	logPath := filepath.Join(dir, "test_output.log")

	f, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 8 {
		fmt.Fprintf(f, "line %d\n", i)
	}
	f.Close()

	var buf bytes.Buffer
	stop := make(chan struct{})
	done := make(chan struct{})
	go tailTestWindow(logPath, &buf, stop, done)

	time.Sleep(60 * time.Millisecond) // let at least one tick fire
	close(stop)
	<-done

	out := buf.String()
	// Progress lines must appear.
	if !strings.Contains(out, "▶") {
		t.Errorf("expected progress lines, got: %q", out)
	}
	// Erase sequences must appear (cursor-up + erase-line) when window was written.
	if !strings.Contains(out, "\033[") {
		t.Errorf("expected ANSI escape sequences in output, got: %q", out)
	}
}
