package coverage

import (
	"golang.org/x/tools/cover"
)

// TotalCoverage parses a coverage profile and returns the overall coverage percentage.
func TotalCoverage(profilePath string) (float64, error) {
	profiles, err := cover.ParseProfiles(profilePath)
	if err != nil {
		return 0, err
	}
	var total, covered int64
	for _, p := range profiles {
		for _, b := range p.Blocks {
			total += int64(b.NumStmt)
			if b.Count > 0 {
				covered += int64(b.NumStmt)
			}
		}
	}
	if total == 0 {
		return 0, nil
	}
	return float64(covered) / float64(total) * 100, nil
}

// PerFileCoverage parses a coverage profile and returns coverage percentage per file.
// Files with no statements get coverage -1.
func PerFileCoverage(profilePath string) (map[string]float64, error) {
	profiles, err := cover.ParseProfiles(profilePath)
	if err != nil {
		return nil, err
	}
	return PerFileCoverageFromProfiles(profiles), nil
}

// PerFileCoverageFromProfiles computes per-file coverage from already-parsed profiles.
func PerFileCoverageFromProfiles(profiles []*cover.Profile) map[string]float64 {
	result := make(map[string]float64)
	for _, p := range profiles {
		var total, covered int64
		for _, b := range p.Blocks {
			total += int64(b.NumStmt)
			if b.Count > 0 {
				covered += int64(b.NumStmt)
			}
		}
		if total == 0 {
			result[p.FileName] = -1
		} else {
			result[p.FileName] = float64(covered) / float64(total) * 100
		}
	}
	return result
}
