package coverage

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// missingToolRe matches `go: no such tool "<name>"` lines that Go emits when
// `go test` tries to invoke a toolchain tool (typically covdata, in our case)
// that is absent from the active toolchain's tool dir. Auto-downloaded
// toolchains often ship a minimal toolset missing these, causing this exact
// error pattern.
var missingToolRe = regexp.MustCompile(`go: no such tool "([^"]+)"`)

// classifyMissingTool returns the missing tool name (e.g., "covdata") if the
// captured `go test` output contains the marker pattern, or "" otherwise.
func classifyMissingTool(combined []byte) string {
	m := missingToolRe.FindSubmatch(combined)
	if m == nil {
		return ""
	}
	return string(m[1])
}

// missingToolHint returns a user-actionable error message when a Go toolchain
// tool is missing during coverage instrumentation. Centralized so both
// RunTotal and RunDiff produce the same guidance.
func missingToolHint(tool, root string) error {
	return fmt.Errorf("Go toolchain is missing %q (required for coverage instrumentation in %s). "+
		"This typically means Go's auto-toolchain downloaded a minimal toolset that excludes "+
		"standalone tools. Upgrade your system Go to match the project's go.mod toolchain "+
		"directive, or set GOTOOLCHAIN=local to use the system Go directly", tool, root)
}

// RunResult holds the outcome of a multi-module coverage run.
type RunResult struct {
	// ProfilePath is the path to the merged coverage profile.
	ProfilePath string
}

// runModule executes `go test` for a single module root and reports the outcome.
// tool is non-empty when the run failed because a required toolchain tool is absent;
// callers should propagate that as an immediate error via missingToolHint.
func runModule(ctx context.Context, root string, goTestArgs []string, profPath string, output io.Writer) (written bool, tool string, err error) {
	cmd := exec.CommandContext(ctx, "go", goTestArgs...)
	cmd.Dir = root
	var capture bytes.Buffer
	cmd.Stdout = io.MultiWriter(output, &capture)
	cmd.Stderr = io.MultiWriter(output, &capture)
	err = cmd.Run()
	if err != nil {
		tool = classifyMissingTool(capture.Bytes())
	}
	written = profileWritten(profPath)
	return
}

// RunTotal runs `go test -short -coverprofile -covermode=atomic ./...` in each
// module root and returns the merged coverage profile.
//
// Any module that exits non-zero is a hard error: compile failures (no profile
// written) and test failures (profile written, but tests failed) both abort
// the run after all modules have been attempted.
//
// Design note on test-failure-with-valid-profile being a hard error:
// a coverage report alongside failing tests is a confusing dual signal, and
// the failures already streamed live to the caller's writer. If a user ever
// needs the inverse — collect coverage despite failing tests — the right
// shape is a typed Report.TestsFailed bool surfaced via the JSON sidecar,
// NOT a generic warnings channel. Re-deciding from scratch is cheaper than
// maintaining a soft-warning path that has no current callers.
func RunTotal(ctx context.Context, moduleRoots []string, outputDir string, output io.Writer) (RunResult, error) {
	var partials []string
	var res RunResult
	var compileFailures, testFailures []string

	for i, root := range moduleRoots {
		prof := filepath.Join(outputDir, fmt.Sprintf("total_%d.out", i))
		args := []string{"test", "-short", "-count=1", "-coverprofile=" + prof, "-covermode=atomic", "./..."}
		written, tool, runErr := runModule(ctx, root, args, prof, output)
		if tool != "" {
			return res, missingToolHint(tool, root)
		}
		if runErr != nil && !written {
			compileFailures = append(compileFailures, fmt.Sprintf("%s: %v", root, runErr))
			continue
		}
		if runErr != nil {
			testFailures = append(testFailures, fmt.Sprintf("%s: %v", root, runErr))
			continue
		}
		partials = append(partials, prof)
	}

	if len(compileFailures) > 0 {
		return res, fmt.Errorf("module(s) failed to produce coverage profile (likely compile failure): %s",
			strings.Join(compileFailures, "; "))
	}
	if len(testFailures) > 0 {
		return res, fmt.Errorf("module(s) had failing tests: %s", strings.Join(testFailures, "; "))
	}

	merged := filepath.Join(outputDir, "coverage.out")
	if err := MergeProfiles(merged, partials); err != nil {
		return res, err
	}
	res.ProfilePath = merged
	return res, nil
}

// RunDiff runs coverage only for the specified packages, grouped by module root.
// The coverpkg flag ensures cross-package coverage is captured. Failure semantics
// match RunTotal: any non-zero `go test` exit (compile failure or test failure)
// is a hard error.
func RunDiff(ctx context.Context, grouped map[string][]string, outputDir string, output io.Writer) (RunResult, error) {
	var partials []string
	var res RunResult
	var compileFailures, testFailures []string

	i := 0
	for root, pkgs := range grouped {
		prof := filepath.Join(outputDir, fmt.Sprintf("diff_%d.out", i))
		i++
		covPkg := strings.Join(pkgs, ",")
		args := []string{"test", "-short", "-count=1", "-coverprofile=" + prof, "-covermode=atomic", "-coverpkg=" + covPkg}
		args = append(args, pkgs...)
		written, tool, runErr := runModule(ctx, root, args, prof, output)
		if tool != "" {
			return res, missingToolHint(tool, root)
		}
		if runErr != nil && !written {
			compileFailures = append(compileFailures, fmt.Sprintf("%s: %v", root, runErr))
			continue
		}
		if runErr != nil {
			testFailures = append(testFailures, fmt.Sprintf("%s: %v", root, runErr))
			continue
		}
		partials = append(partials, prof)
	}

	if len(compileFailures) > 0 {
		return res, fmt.Errorf("module(s) failed to produce coverage profile (likely compile failure): %s",
			strings.Join(compileFailures, "; "))
	}
	if len(testFailures) > 0 {
		return res, fmt.Errorf("module(s) had failing tests: %s", strings.Join(testFailures, "; "))
	}

	merged := filepath.Join(outputDir, "coverage_diff.out")
	if err := MergeProfiles(merged, partials); err != nil {
		return res, err
	}
	res.ProfilePath = merged
	return res, nil
}

// profileWritten returns whether path exists with non-empty contents.
// `go test` only writes a coverage profile after instrumentation succeeds,
// so an empty/missing file after a non-zero exit indicates the test binary
// never ran (typically a compile failure).
func profileWritten(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

// MergeProfiles concatenates coverage profiles, deduplicating the "mode:" header.
func MergeProfiles(dst string, sources []string) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	wroteHeader := false
	for _, src := range sources {
		f, err := os.Open(src)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "mode:") {
				if !wroteHeader {
					fmt.Fprintln(out, line)
					wroteHeader = true
				}
				continue
			}
			if line != "" {
				fmt.Fprintln(out, line)
			}
		}
		f.Close()
	}
	if !wroteHeader {
		fmt.Fprintln(out, "mode: atomic")
	}
	return nil
}
