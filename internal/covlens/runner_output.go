package covlens

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func logProgress(w io.Writer, msg string) {
	fmt.Fprintf(w, "\033[0;34m▶\033[0m %s\n", msg)
}

// openTestOutputLog returns the writer that `go test` subprocesses should
// write their stdout/stderr to, plus a cleanup func. Precedence:
//
//  1. VerboseTests=true → cfg.testOutput() (typically os.Stdout)
//  2. Caller-set TestOutput (e.g., io.Discard in tests) → respect it
//  3. Otherwise → create .coverage/test-output.log
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
	logPath := filepath.Join(r.outputDir, "test-output.log")
	f, err := os.Create(logPath)
	if err != nil {
		return r.cfg.testOutput(), func() {}
	}
	logProgress(r.cfg.stderr(), fmt.Sprintf("Test output → %s", logPath))
	return f, func() { _ = f.Close() }
}
