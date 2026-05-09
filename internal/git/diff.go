package git

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Hunk represents a range of changed lines [Start, End] (1-indexed, inclusive).
type Hunk struct{ Start, End int }

// hunkHeaderRe matches unified diff hunk headers: @@ ... +start[,count] @@
var hunkHeaderRe = regexp.MustCompile(`^@@ .* \+(\d+)(?:,(\d+))? @@`)

// ParseHunks extracts changed-line ranges from unified diff output.
// It scans for hunk headers of the form @@ ... +start[,count] @@.
// If count is absent it defaults to 1. If count is 0 (pure deletion) the hunk is skipped.
func ParseHunks(diffOutput string) []Hunk {
	var hunks []Hunk
	for _, line := range strings.Split(diffOutput, "\n") {
		m := hunkHeaderRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		start, _ := strconv.Atoi(m[1]) // m[1] matched \d+ by regex; Atoi cannot fail
		count := 1
		if m[2] != "" {
			count, _ = strconv.Atoi(m[2]) // m[2] matched \d+ by regex; Atoi cannot fail
		}
		if count == 0 {
			continue // pure deletion
		}
		hunks = append(hunks, Hunk{Start: start, End: start + count - 1})
	}
	return hunks
}

// MergeHunks sorts hunks by Start and merges overlapping or adjacent ranges.
func MergeHunks(hunks []Hunk) []Hunk {
	if len(hunks) == 0 {
		return nil
	}
	sort.Slice(hunks, func(i, j int) bool {
		return hunks[i].Start < hunks[j].Start
	})
	merged := []Hunk{hunks[0]}
	for _, h := range hunks[1:] {
		last := &merged[len(merged)-1]
		if h.Start <= last.End+1 {
			// Overlapping or adjacent — extend.
			if h.End > last.End {
				last.End = h.End
			}
		} else {
			merged = append(merged, h)
		}
	}
	return merged
}
