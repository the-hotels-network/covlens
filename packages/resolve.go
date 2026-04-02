package packages

import (
	"bytes"
	"context"
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
	// Check gitRoot itself.
	if _, err := os.Stat(filepath.Join(gitRoot, "go.mod")); err == nil {
		return gitRoot, nil
	}
	return "", fmt.Errorf("no go.mod found between %s and %s", dir, gitRoot)
}

// Resolve maps a list of files (paths relative to gitRoot) to their Go import
// paths. Files that cannot be resolved (no go.mod, go list failure) are
// silently skipped.
func Resolve(ctx context.Context, gitRoot string, files []string) ([]ModulePackage, error) {
	gitRoot = filepath.Clean(gitRoot)
	seen := make(map[string]struct{})
	var result []ModulePackage

	for _, file := range files {
		if file == "" {
			continue
		}
		absDir := filepath.Join(gitRoot, filepath.Dir(file))

		modRoot, err := FindModRoot(absDir, gitRoot)
		if err != nil {
			continue // skip — no go.mod
		}

		relDir, err := filepath.Rel(modRoot, absDir)
		if err != nil {
			continue
		}

		target := "./" + filepath.ToSlash(relDir)

		// Dedup by (modRoot, target) before shelling out.
		key := modRoot + "::" + target
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		importPath, err := goList(ctx, modRoot, target)
		if err != nil || importPath == "" {
			continue
		}

		result = append(result, ModulePackage{
			ImportPath: importPath,
			ModuleRoot: modRoot,
		})
	}
	return result, nil
}

// ImportPath resolves the Go import path for a directory.
// absDir must be an absolute path to the directory; gitRoot is the repo root.
func ImportPath(ctx context.Context, absDir, gitRoot string) (string, error) {
	modRoot, err := FindModRoot(absDir, gitRoot)
	if err != nil {
		return "", err
	}
	relDir, err := filepath.Rel(modRoot, absDir)
	if err != nil {
		return "", err
	}
	target := "./" + filepath.ToSlash(relDir)
	return goList(ctx, modRoot, target)
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
