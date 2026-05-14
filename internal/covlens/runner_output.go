package covlens

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

func logProgress(w io.Writer, msg string) {
	fmt.Fprintf(w, "\033[0;34m▶\033[0m %s\n", msg)
}

var testLogTailInterval = 1 * time.Second

const testLogTailLines = 5

// openTestOutputLog returns the writer that `go test` subprocesses should
// write their stdout/stderr to, plus a cleanup func. Precedence:
//
//  1. VerboseTests=true → cfg.testOutput() (typically os.Stdout)
//  2. Caller-set TestOutput (e.g., io.Discard in tests) → respect it
//  3. TTY stderr → capture to test_output.log + N-line in-place rewriting window
//  4. Non-TTY → capture silently (CI always uses -verbose; non-TTY is not a user terminal)
//
// On any failure to create the log file, falls back to cfg.testOutput().
// Callers must invoke the returned cleanup func when finished.
func (r *runner) openTestOutputLog() (io.Writer, func()) {
	if r.cfg.VerboseTests {
		return r.cfg.testOutput(), func() {}
	}
	if r.cfg.TestOutput != nil {
		return r.cfg.TestOutput, func() {}
	}
	logPath := filepath.Join(r.outputDir, "test_output.log")
	f, err := os.Create(logPath)
	if err != nil {
		return r.cfg.testOutput(), func() {}
	}
	logProgress(r.cfg.stderr(), fmt.Sprintf("Test output → %s", logPath))

	if !isTTY(r.cfg.stderr()) {
		return f, func() { _ = f.Close() }
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go tailTestWindow(logPath, r.cfg.stderr(), stop, done)

	return f, func() {
		close(stop)
		<-done
		_ = f.Close()
	}
}

// isTTY reports whether w is a character device (i.e. a real terminal).
func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// maxProgressLineWidth is the maximum printed width of a progress line.
// Lines longer than this are truncated to prevent terminal wrapping, which
// would cause cursor-up to undershoot and expand the window indefinitely.
const maxProgressLineWidth = 140

// tailTestWindow polls path every testLogTailInterval, printing the last
// testLogTailLines lines in-place using ANSI cursor-up overwrite.
// On stop, it erases the progress window so the caller can print cleanly.
func tailTestWindow(path string, w io.Writer, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(testLogTailInterval)
	defer ticker.Stop()
	var prev []string
	written := 0
	for {
		select {
		case <-stop:
			if written > 0 {
				// Move cursor to start of window, erase each line, leave cursor there.
				fmt.Fprintf(w, "\033[%dA\r", written)
				for i := 0; i < written; i++ {
					fmt.Fprintf(w, "\033[2K\n")
				}
				fmt.Fprintf(w, "\033[%dA\r", written)
			}
			return
		case <-ticker.C:
			lines := lastNLogLines(path, testLogTailLines)
			if len(lines) == 0 || slices.Equal(lines, prev) {
				continue
			}
			if written > 0 {
				fmt.Fprintf(w, "\033[%dA\r", written)
			}
			for _, line := range lines {
				if len(line) > maxProgressLineWidth {
					line = line[:maxProgressLineWidth-1] + "…"
				}
				fmt.Fprintf(w, "\033[K\033[0;34m▶\033[0m %s\n", line)
			}
			written = len(lines)
			prev = lines
		}
	}
}

// lastNLogLines returns up to n non-empty lines from the tail of path by
// reading the final 4 KiB — enough to find recent output without scanning
// the whole file.
func lastNLogLines(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	size, err := f.Seek(0, io.SeekEnd)
	if err != nil || size == 0 {
		return nil
	}
	const tailBytes = 4096
	start := size - tailBytes
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil
	}
	scanner := bufio.NewScanner(f)
	var lines []string
	for scanner.Scan() {
		if t := strings.TrimSpace(scanner.Text()); t != "" {
			lines = append(lines, t)
		}
	}
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}
