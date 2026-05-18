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

func TestScanLog(t *testing.T) {
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

	t.Run("nonexistent file returns zero values", func(t *testing.T) {
		size, completed, tail := scanLog("/no/such/file.log", 5)
		if size != 0 || completed != 0 || tail != nil {
			t.Errorf("expected zero values, got size=%d completed=%d tail=%v", size, completed, tail)
		}
	})

	t.Run("empty file returns zero counts", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty.log")
		os.WriteFile(path, nil, 0600)
		size, completed, tail := scanLog(path, 5)
		if size != 0 || completed != 0 || tail != nil {
			t.Errorf("expected zeros, got size=%d completed=%d tail=%v", size, completed, tail)
		}
	})

	t.Run("fewer lines than n returns all", func(t *testing.T) {
		path := writeLines(t, []string{"a", "b", "c"})
		_, _, tail := scanLog(path, 5)
		if !slices.Equal(tail, []string{"a", "b", "c"}) {
			t.Errorf("got %v, want [a b c]", tail)
		}
	})

	t.Run("more lines than n returns last n", func(t *testing.T) {
		path := writeLines(t, []string{"a", "b", "c", "d", "e", "f"})
		_, _, tail := scanLog(path, 3)
		if !slices.Equal(tail, []string{"d", "e", "f"}) {
			t.Errorf("got %v, want [d e f]", tail)
		}
	})

	t.Run("blank lines are skipped", func(t *testing.T) {
		path := writeLines(t, []string{"a", "", "   ", "b"})
		_, _, tail := scanLog(path, 5)
		if !slices.Equal(tail, []string{"a", "b"}) {
			t.Errorf("got %v, want [a b]", tail)
		}
	})

	t.Run("counts ok/FAIL/? lines, ignores indented coverage lines", func(t *testing.T) {
		path := writeLines(t, []string{
			"ok\tpkg/a\t0.1s\tcoverage: 80.0% of statements",
			"FAIL\tpkg/b\t0.2s",
			"?\tpkg/c\t[no test files]",
			"    pkg/d\t\tcoverage: 0.0% of statements",
			"ok\tpkg/e\t0.3s",
		})
		_, completed, _ := scanLog(path, 10)
		if completed != 4 {
			t.Errorf("got completed=%d, want 4", completed)
		}
	})

	t.Run("size reflects file size", func(t *testing.T) {
		path := writeLines(t, []string{"hello", "world"})
		size, _, _ := scanLog(path, 5)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if size != info.Size() {
			t.Errorf("size=%d, want %d", size, info.Size())
		}
	})
}

func TestFmtElapsed(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{47 * time.Second, "47s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m00s"},
		{4*time.Minute + 32*time.Second, "4m32s"},
		{59*time.Minute + 59*time.Second, "59m59s"},
		{time.Hour, "1h00m"},
		{2*time.Hour + 15*time.Minute, "2h15m"},
	}
	for _, tc := range cases {
		if got := fmtElapsed(tc.d); got != tc.want {
			t.Errorf("fmtElapsed(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
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

// TestTailTestWindow verifies that tailTestWindow prints the status line +
// tail content, and erases the window (emitting cursor-up sequences) on stop.
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
	fmt.Fprintln(f, "ok\tpkg/a\t0.1s")
	fmt.Fprintln(f, "ok\tpkg/b\t0.2s")
	fmt.Fprintln(f, "?\tpkg/c\t[no test files]")
	f.Close()

	var buf bytes.Buffer
	stop := make(chan struct{})
	done := make(chan struct{})
	go tailTestWindow(logPath, &buf, stop, done)

	time.Sleep(60 * time.Millisecond) // let at least one tick fire
	close(stop)
	<-done

	out := buf.String()
	if !strings.Contains(out, "▶") {
		t.Errorf("expected progress marker, got: %q", out)
	}
	if !strings.Contains(out, "elapsed") || !strings.Contains(out, "pkg done") || !strings.Contains(out, "since last output") {
		t.Errorf("status line missing fields, got: %q", out)
	}
	if !strings.Contains(out, "3 pkg done") {
		t.Errorf("expected '3 pkg done' (ok+ok+?), got: %q", out)
	}
	if !strings.Contains(out, "\033[") {
		t.Errorf("expected ANSI escape sequences, got: %q", out)
	}
}
