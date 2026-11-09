package coverage

import (
	"fmt"
	"strings"
)

// The head is kept because the first failure is usually the root cause.
const maxExcerptLines = 80

func FailureExcerpt(out []byte) string {
	var kept []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if isSuccessNoise(line) {
			continue
		}
		kept = append(kept, line)
	}
	if len(kept) == 0 {
		return ""
	}
	if len(kept) > maxExcerptLines {
		omitted := len(kept) - maxExcerptLines
		kept = append(kept[:maxExcerptLines], fmt.Sprintf("... (%d more lines)", omitted))
	}
	return strings.Join(kept, "\n")
}

func isSuccessNoise(line string) bool {
	switch {
	case strings.TrimSpace(line) == "":
		return true
	case strings.HasPrefix(line, "ok  \t"):
		return true
	case strings.HasPrefix(line, "?   \t"):
		return true
	case strings.HasPrefix(line, "go: downloading "):
		return true
	case strings.HasPrefix(line, "\t") && strings.Contains(line, "\tcoverage: "):
		// "\t<pkg>\t\tcoverage: 0.0% of statements": package without test files.
		return true
	}
	return false
}
