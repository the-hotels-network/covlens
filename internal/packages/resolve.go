// Package packages resolves Go import paths and module roots for files
// inside a repository. Lookups are cached so callers can dedupe `go list`
// invocations across the lifetime of a run.
package packages

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ModulePackage ties an import path to the module root that owns it.
type ModulePackage struct {
	ImportPath string
	ModuleRoot string // absolute path to go.mod directory
}

// ErrLookupFailed is returned by Lookup on cache hits where the original
// resolution failed. Callers that ignore the error can branch on
// ModulePackage.ImportPath == "" instead.
var ErrLookupFailed = errors.New("packages: lookup previously failed")

// Lookup returns the import path and module root for absDir. The cache map
// is shared across calls — a directory whose import path has already been
// resolved (or has previously failed to resolve) skips the `go list` shell-out.
//
// On first lookup, failures are negatively cached as a zero ModulePackage so
// repeated calls do not re-invoke `go list`. Subsequent lookups for a
// previously-failed directory return ErrLookupFailed.
func Lookup(ctx context.Context, gitRoot, absDir string, cache map[string]ModulePackage) (ModulePackage, error) {
	if cached, ok := cache[absDir]; ok {
		if cached.ImportPath == "" {
			return ModulePackage{}, ErrLookupFailed
		}
		return cached, nil
	}

	info, err := lookupUncached(ctx, gitRoot, absDir)
	if err != nil {
		cache[absDir] = ModulePackage{}
		return ModulePackage{}, err
	}
	cache[absDir] = info
	return info, nil
}

func lookupUncached(ctx context.Context, gitRoot, absDir string) (ModulePackage, error) {
	modRoot, err := FindModRoot(absDir, gitRoot)
	if err != nil {
		return ModulePackage{}, err
	}
	relDir, err := filepath.Rel(modRoot, absDir)
	if err != nil {
		return ModulePackage{}, err
	}
	target := "./" + filepath.ToSlash(relDir)
	importPath, err := goList(ctx, modRoot, target)
	if err != nil {
		return ModulePackage{}, err
	}
	if importPath == "" {
		return ModulePackage{}, fmt.Errorf("go list returned empty import path for %s", absDir)
	}
	return ModulePackage{ImportPath: importPath, ModuleRoot: modRoot}, nil
}

// FindModRoot walks up from dir toward gitRoot looking for go.mod.
// Both dir and gitRoot must be absolute paths.
// Returns the directory containing go.mod, or an error if none is found.
func FindModRoot(dir, gitRoot string) (string, error) {
	dir = filepath.Clean(dir)
	gitRoot = filepath.Clean(gitRoot)

	cur := dir
	for cur != gitRoot && cur != "/" && cur != "." {
		if _, err := os.Stat(filepath.Join(cur, "go.mod")); err == nil {
			return cur, nil
		}
		cur = filepath.Dir(cur)
	}
	if _, err := os.Stat(filepath.Join(gitRoot, "go.mod")); err == nil {
		return gitRoot, nil
	}
	return "", fmt.Errorf("no go.mod found between %s and %s", dir, gitRoot)
}

// goList runs `go list <target>` in the given working directory and returns
// the first line of stdout (the import path).
func goList(ctx context.Context, workDir, target string) (string, error) {
	cmd := exec.CommandContext(ctx, "go", "list", target)
	cmd.Dir = workDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go list %s: %w: %s", target, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// GroupByModule groups import paths by their ModuleRoot. The returned map key
// is the absolute module root path; the value is the list of import paths
// belonging to that module.
func GroupByModule(pkgs []ModulePackage) map[string][]string {
	m := make(map[string][]string)
	for _, p := range pkgs {
		m[p.ModuleRoot] = append(m[p.ModuleRoot], p.ImportPath)
	}
	return m
}
