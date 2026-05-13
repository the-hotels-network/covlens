package covlens

import (
	"testing"

	"golang.org/x/tools/cover"
)

func TestAggregateFiltered(t *testing.T) {
	prof := func(name string, blocks ...cover.ProfileBlock) *cover.Profile {
		return &cover.Profile{FileName: name, Blocks: blocks}
	}
	blk := func(n, c int) cover.ProfileBlock {
		return cover.ProfileBlock{NumStmt: n, Count: c}
	}
	never := func(string) bool { return false }
	skipOne := func(s string) bool { return s == "skip.go" }

	cases := []struct {
		name     string
		profiles []*cover.Profile
		excluded func(string) bool
		want     float64
	}{
		{"empty profiles", nil, never, 0},
		{"all covered", []*cover.Profile{prof("a.go", blk(2, 1), blk(3, 1))}, never, 100},
		{"half covered", []*cover.Profile{prof("a.go", blk(2, 1), blk(2, 0))}, never, 50},
		{"excluded file does not count", []*cover.Profile{prof("a.go", blk(1, 1)), prof("skip.go", blk(10, 0))}, skipOne, 100},
		{"zero statements yields zero, not NaN", []*cover.Profile{prof("a.go", blk(0, 0))}, never, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := aggregateFiltered(tc.profiles, tc.excluded); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
