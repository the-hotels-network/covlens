package covlens

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestStderrFallback(t *testing.T) {
	if got := (Config{}).stderr(); got != os.Stderr {
		t.Errorf("Config{}.stderr() did not fall back to os.Stderr: got %v", got)
	}
	buf := &bytes.Buffer{}
	if got := (Config{Stderr: buf}).stderr(); got != buf {
		t.Errorf("Config{Stderr: buf}.stderr() did not return the configured writer")
	}
	if got := (Config{Stderr: io.Discard}).stderr(); got != io.Discard {
		t.Errorf("Config{Stderr: io.Discard}.stderr() did not return io.Discard")
	}
}

func TestTestOutputFallback(t *testing.T) {
	if got := (Config{}).testOutput(); got != os.Stdout {
		t.Errorf("Config{}.testOutput() did not fall back to os.Stdout: got %v", got)
	}
	buf := &bytes.Buffer{}
	if got := (Config{TestOutput: buf}).testOutput(); got != buf {
		t.Errorf("Config{TestOutput: buf}.testOutput() did not return the configured writer")
	}
}
