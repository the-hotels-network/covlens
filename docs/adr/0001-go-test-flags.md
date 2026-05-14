# go test flags covlens passes

covlens shells out `go test -short -count=1 -coverprofile=… -covermode=atomic [-coverpkg=…]` from `internal/coverage/runner.go`. We pass `-count=1` as Go's documented idiom for disabling the test cache, even though `-covermode=atomic` already happens to bypass it today — explicit intent is robust to future changes in Go's cacheable-flag allowlist. We deliberately do NOT hardcode `-race`: it requires `CGO_ENABLED=1` (breaking covlens on `golang:alpine`/distroless CI images), multiplies test wall time by 2–5×, and conflates race detection with coverage gating. Users who want race detection set `GOFLAGS="-race" covlens` — the subprocess inherits the env.

## Considered options for `-race`

- **Hardcode default-on** (with or without `--no-race`): rejected — silent CGO portability regression for users on minimal images; cost paid by users with no concurrent code.
- **Opt-in `--race` flag (plus `test.race` in covlens.yaml)**: rejected as scope creep — `GOFLAGS` already provides a zero-code-change escape hatch, and adding a flag commits us to maintaining its interaction with `GOFLAGS` indefinitely.
- **`GOFLAGS="-race" covlens`** (chosen): zero code change, env-scoped, composes with any other `go test` flags a user may want to add.
