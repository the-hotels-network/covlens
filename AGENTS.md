<!-- Generated: 2026-06-04 | Updated: 2026-06-04 -->

# covlens

## Purpose
Go coverage CLI that runs tests only on the packages you changed, measures
coverage for the **changed lines** (not whole files), validates two thresholds
(diff + total), and emits a self-contained HTML report plus a machine-readable
JSON sidecar. Exits 1 when a threshold fails — CI-friendly.

## Layout
| Path | Role |
|------|------|
| `cmd/covlens/main.go` | CLI entry point: flag parsing, flag→Config merge, calls `covlens.Run`, writes HTML+JSON, prints summary, sets exit code. The only `main`. |
| `internal/covlens/` | Orchestration core + the `Config`/`Report` types every other package depends on. See [Orchestration core](#orchestration-core-internalcovlens). |
| `internal/coverage/` | Shells out to `go test -coverprofile`, merges profiles, computes filtered coverage, classifies toolchain errors. See [Coverage runner](#coverage-runner-internalcoverage). |
| `internal/git/` | Thin `git` wrapper: merge-base, changed files, diff hunks, detached worktrees (for `--ratchet` baseline). One type: `git.Client{WorkDir}`. |
| `internal/packages/` | Resolves a file's import path + owning module root via cached `go list`; groups packages by module. Multi-module/monorepo aware. |
| `internal/directive/` | Parses `//covlens:ignore` directives (whole-file before `package`, or per-function doc comment) via `go/ast`. |
| `internal/printer/` | Three renderers of `covlens.Report`: `console` (ANSI summary), `html` (self-contained report, embedded CSS/template, chroma syntax highlighting), `json` (stable CI sidecar). See [Printers](#printers-internalprinter). |
| `docs/adr/` | Architecture Decision Records — read these before changing test flags, HTML-write behavior, or output formats. |
| `scripts/ci/` | Reference CI glue (e.g. CircleCI PR-comment script). Samples, NOT part of the binary's stability contract. |
| `example/` | Annotated reference `covlens.yaml`. |

## Architecture in one breath
`main` builds a `covlens.Config` → `covlens.Run(ctx, cfg)` returns a
`*covlens.Report` → printers turn that `Report` into console/HTML/JSON.
`Report` is the seam: the core never knows about output formats; printers never
re-measure. Two execution paths inside `Run`: **diff mode** (default, a 6-phase
pipeline) and **`--full`** (linear whole-project scan).

## Orchestration core (`internal/covlens/`)
This is the only package with non-obvious architecture; the rest are
self-explanatory from their package docs.

- **`Run` dispatches one of two paths.** Diff mode (`run()`) is a 6-phase
  pipeline: `detectChangedFiles → classifyFiles → resolvePackages →
  runCoverage → computeStats → buildReport`. Full mode (`runFull()`) is a short
  linear scan that shares only leaf helpers (`classifyExclusion`) with the diff
  path. Don't try to unify them — they were deliberately kept separate.
- **The `runner` struct is immutable request scope.** All fields are set once in
  `newRunner` and never mutated; phase outputs flow through return values
  (`coverageScope`, `coverageSubjects`, `coverageTargets`, `coverageProfiles`,
  `coverageStats`), not back onto the runner. Preserve this when adding a phase.
- **Total coverage is only computed under `--ratchet` (`-r`) or `--full`.** Plain
  diff mode skips the project-wide `go test ./...` entirely (it's the fast "did I
  cover what I touched?" check). The total gate passes vacuously when total
  wasn't measured.
- **`--ratchet` compares against a baseline** computed by checking out the
  merge-base into a temp detached worktree (`baseline.go`) and re-running total
  coverage there. Passes if total didn't drop by more than 0.1pp.
- **`DiffStatus`** (`report.go`) distinguishes a real measurement from the
  vacuous-pass cases: `measured`, `no-go-changes`, `only-deletions`,
  `all-excluded`. Printers branch on it; `main` short-circuits two of them.
- `runner_output.go` is pure terminal UX: the live N-line tailing window over
  `test_output.log`. Independent of measurement logic.

## Coverage runner (`internal/coverage/`)
- **Both compile failures and test failures are hard errors.** A coverage report
  alongside failing tests is a confusing dual signal. There is intentionally **no
  soft-warning channel** — see the design note in `runner.go`. If you ever need
  "collect coverage despite failing tests", the right shape is a typed
  `Report.TestsFailed bool` on the JSON sidecar, not a generic warnings path.
- **Toolchain errors are classified into actionable hints**: missing `covdata`
  tool, and the go#77820 driver/compiler version mismatch. These are propagated
  immediately instead of the raw `go test` error.
- `filter.go` is pure: intersect profile blocks with changed-line ranges (hunks)
  minus excluded ranges. No I/O, no shelling out.

## Printers (`internal/printer/`)
- **`covlens.Report` is the only input.** Printers never re-measure or re-read
  coverage profiles for the numbers — they format what the core already computed.
- **The binary emits transport-neutral outputs only** (ADR 0003): HTML for
  humans, JSON for machines. **Do NOT add a Markdown/Slack/Teams printer here** —
  platform renderings are composed downstream from the JSON sidecar. The JSON
  schema (`internal/printer/json`, `SchemaVersion`) is the stability contract;
  new fields are additive, renames/removals bump the schema.
- The HTML template + CSS are embedded via `embed.go` (`//go:embed`); the report
  is a single self-contained file.

## Build & test (Taskfile)
| Command | Does |
|---------|------|
| `task build` | `go build -o bin/covlens ./cmd/covlens` |
| `task test` | `go test ./...` (everything) |
| `task test:unit` | unit tests, excludes `/e2e` |
| `task test:e2e` | `go test ./internal/covlens/e2e/...` |
| `task check` | `vet` + unit tests — the local CI gate |
| `task fmt` / `task tidy` | `gofmt -w .` / `go mod tidy` |

e2e tests live in `internal/covlens/e2e/` and are gated by `testing.Short()` via
`TestMain`; covlens itself passes `-short`, so e2e is skipped under a covlens run.
To run them, invoke `go test ./...` directly.

## For AI Agents
- **Requires Go 1.25+.** A known toolchain-mismatch bug (golang/go#77820) surfaces
  as `compile: version ... does not match` when system Go < project Go — it's a Go
  bug, not ours; the fix is `GOTOOLCHAIN=<ver>` or upgrading system Go. covlens
  detects it and prints a hint (`internal/coverage/runner.go`).
- **`covlens.yaml` at the root configures covlens on itself**: `main.go`,
  `baseline.go`, `covlens.go` are excluded (orchestration, no unit seam); diff
  threshold 90, total 75.
- Commit style: Conventional Commits, single-line, no body, no Co-Author trailer.
- Before changing behavior covered by an ADR (`docs/adr/`), read it — the
  rejected alternatives are documented for a reason.
- Test fixtures: prefer txtar + table-driven testdata over a third inline-string
  fixture (or any fixture >30 lines).

<!-- MANUAL: notes below this line are preserved on regeneration -->
