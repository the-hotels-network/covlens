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

// RunTotal runs `go test -short -coverprofile -covermode=atomic ./...` in each
// module root and returns the path to the merged coverage profile.
func RunTotal(ctx context.Context, moduleRoots []string, outputDir string, stdout io.Writer) (string, error) {
	var partials []string
	for i, root := range moduleRoots {
		prof := filepath.Join(outputDir, fmt.Sprintf("total_%d.out", i))
		cmd := exec.CommandContext(ctx, "go", "test", "-short",
			"-coverprofile="+prof, "-covermode=atomic", "./...")
		cmd.Dir = root
		cmd.Stdout = stdout
		cmd.Stderr = stdout
		_ = cmd.Run() // non-zero exit is OK — partial results are valid
		partials = append(partials, prof)
	}
	merged := filepath.Join(outputDir, "coverage.out")
	if err := MergeProfiles(merged, partials); err != nil {
		return "", err
	}
	return merged, nil
}

// RunDiff runs coverage only for the specified packages, grouped by module root.
// The coverpkg flag ensures cross-package coverage is captured.
func RunDiff(ctx context.Context, grouped map[string][]string, outputDir string, stdout io.Writer) (string, error) {
	var partials []string
	i := 0
	for root, pkgs := range grouped {
		prof := filepath.Join(outputDir, fmt.Sprintf("diff_%d.out", i))
		covPkg := strings.Join(pkgs, ",")
		args := []string{"test", "-short",
			"-coverprofile=" + prof, "-covermode=atomic",
			"-coverpkg=" + covPkg}
		args = append(args, pkgs...)
		cmd := exec.CommandContext(ctx, "go", args...)
		cmd.Dir = root
		cmd.Stdout = stdout
		cmd.Stderr = stdout
		_ = cmd.Run() // non-zero exit is OK — partial results are valid
		partials = append(partials, prof)
		i++
	}
	merged := filepath.Join(outputDir, "coverage_diff.out")
	if err := MergeProfiles(merged, partials); err != nil {
		return "", err
	}
	return merged, nil
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
