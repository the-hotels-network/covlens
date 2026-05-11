// Package e2e contains end-to-end tests that build real git repos, invoke
// covlens.Run, and assert on rendered Reports. They are slow (~8s total) and
// skip under `go test -short`, which is the flag covlens itself passes when
// shelling out to `go test` — so dogfood runs of `covlens --full` measure
// unit-only coverage. To run them, use `go test ./...` directly.
package e2e

import (
	"flag"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
	}
	os.Exit(m.Run())
}
