package coverage

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RunResult holds the outcome of a multi-module coverage run.
type RunResult struct {
	// ProfilePath is the path to the merged coverage profile.
	ProfilePath string
	// Warnings collects per-module non-fatal issues — most commonly a
	// `go test` non-zero exit with a coverage profile still written
	// (test failures during a run that produced valid coverage data).
	Warnings []string
}

// RunTotal runs `go test -short -coverprofile -covermode=atomic ./...` in each
// module root and returns the merged coverage profile.
//
// A module that exits non-zero and produces no coverage profile (typically a
// compile failure) is treated as a hard error and aborts the run after all
// modules have been attempted. A module that exits non-zero but writes a
// profile is recorded as a warning — coverage data is still valid.
func RunTotal(ctx context.Context, moduleRoots []string, outputDir string, output io.Writer) (RunResult, error) {
	var partials []string
	var res RunResult
	var compileFailures []string

	for i, root := range moduleRoots {
		prof := filepath.Join(outputDir, fmt.Sprintf("total_%d.out", i))
		cmd := exec.CommandContext(ctx, "go", "test", "-short",
			"-coverprofile="+prof, "-covermode=atomic", "./...")
		cmd.Dir = root
		cmd.Stdout = output
		cmd.Stderr = output
		runErr := cmd.Run()

		if runErr != nil && !profileWritten(prof) {
			compileFailures = append(compileFailures, fmt.Sprintf("%s: %v", root, runErr))
			continue
		}
		if runErr != nil {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("module %s: tests failed, coverage still collected: %v", root, runErr))
		}
		partials = append(partials, prof)
	}

	if len(compileFailures) > 0 {
		return res, fmt.Errorf("module(s) failed to produce coverage profile (likely compile failure): %s",
			strings.Join(compileFailures, "; "))
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
// match RunTotal: compile failures are hard errors, test failures with valid
// profile data are warnings.
func RunDiff(ctx context.Context, grouped map[string][]string, outputDir string, output io.Writer) (RunResult, error) {
	var partials []string
	var res RunResult
	var compileFailures []string

	i := 0
	for root, pkgs := range grouped {
		prof := filepath.Join(outputDir, fmt.Sprintf("diff_%d.out", i))
		i++
		covPkg := strings.Join(pkgs, ",")
		args := []string{"test", "-short",
			"-coverprofile=" + prof, "-covermode=atomic",
			"-coverpkg=" + covPkg}
		args = append(args, pkgs...)
		cmd := exec.CommandContext(ctx, "go", args...)
		cmd.Dir = root
		cmd.Stdout = output
		cmd.Stderr = output
		runErr := cmd.Run()

		if runErr != nil && !profileWritten(prof) {
			compileFailures = append(compileFailures, fmt.Sprintf("%s: %v", root, runErr))
			continue
		}
		if runErr != nil {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("module %s: tests failed, coverage still collected: %v", root, runErr))
		}
		partials = append(partials, prof)
	}

	if len(compileFailures) > 0 {
		return res, fmt.Errorf("module(s) failed to produce coverage profile (likely compile failure): %s",
			strings.Join(compileFailures, "; "))
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
