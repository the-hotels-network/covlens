package covlens

import "golang.org/x/tools/cover"

// aggregateFiltered sums covered/total statements across profiles, skipping
// any profile for which excluded returns true. Returns the resulting coverage
// percentage (or 0 if no statements remained after filtering).
func aggregateFiltered(profiles []*cover.Profile, excluded func(profileFileName string) bool) float64 {
	var stmts, covered int64
	for _, p := range profiles {
		if excluded(p.FileName) {
			continue
		}
		for _, b := range p.Blocks {
			stmts += int64(b.NumStmt)
			if b.Count > 0 {
				covered += int64(b.NumStmt)
			}
		}
	}
	if stmts == 0 {
		return 0
	}
	return float64(covered) / float64(stmts) * 100
}
