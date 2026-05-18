package covlens

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
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

// pkgCompletionRe matches `go test` lines emitted when a package finishes:
//
//	ok      pkg/path    0.123s    coverage: 12.3% of statements
//	FAIL    pkg/path    0.456s
//	?       pkg/path    [no test files]
//
// Indented coverage-only lines (emitted by -coverpkg for packages with no
// own tests) start with whitespace and do not match.
var pkgCompletionRe = regexp.MustCompile(`^(ok|FAIL|\?)\s`)

// tailTestWindow polls path every testLogTailInterval. It renders a status
// line (elapsed · packages done · time since last output) followed by the
// last testLogTailLines lines, in-place using ANSI cursor-up overwrite.
// The "time since last output" field grows whenever the log file stops
// growing — a direct liveness signal for stuck/hung test runs.
// On stop, it erases the window so the caller can print cleanly.
func tailTestWindow(path string, w io.Writer, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(testLogTailInterval)
	defer ticker.Stop()

	start := time.Now()
	lastActivity := start
	var lastSize int64
	var prevTail []string
	var prevStatus string
	written := 0

	for {
		select {
		case <-stop:
			if written > 0 {
				fmt.Fprintf(w, "\033[%dA\r", written)
				for i := 0; i < written; i++ {
					fmt.Fprintf(w, "\033[2K\n")
				}
				fmt.Fprintf(w, "\033[%dA\r", written)
			}
			return
		case <-ticker.C:
			size, completed, tail := scanLog(path, testLogTailLines)
			if size > lastSize {
				lastActivity = time.Now()
				lastSize = size
			}
			status := fmt.Sprintf("\033[0;34m▶\033[0m %s elapsed · %d pkg done · %s since last output",
				fmtElapsed(time.Since(start)), completed, fmtElapsed(time.Since(lastActivity)))
			if status == prevStatus && slices.Equal(tail, prevTail) {
				continue
			}
			if written > 0 {
				fmt.Fprintf(w, "\033[%dA\r", written)
			}
			fmt.Fprintf(w, "\033[K%s\n", status)
			for _, line := range tail {
				if len(line) > maxProgressLineWidth {
					line = line[:maxProgressLineWidth-1] + "…"
				}
				fmt.Fprintf(w, "\033[K\033[0;34m▶\033[0m %s\n", line)
			}
			written = 1 + len(tail)
			prevStatus = status
			prevTail = tail
		}
	}
}

// scanLog walks the entire log file, counting package-completion lines and
// capturing the last n non-empty lines. Returns the file size so callers
// can detect "no new bytes since last tick" → stuck.
func scanLog(path string, n int) (size int64, completed int, tail []string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return
	}
	size = info.Size()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var lines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if pkgCompletionRe.MatchString(line) {
			completed++
		}
		lines = append(lines, line)
	}
	if len(lines) > n {
		tail = lines[len(lines)-n:]
	} else {
		tail = lines
	}
	return
}

func fmtElapsed(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d/time.Minute), int((d%time.Minute)/time.Second))
	}
	return fmt.Sprintf("%dh%02dm", int(d/time.Hour), int((d%time.Hour)/time.Minute))
}
