# Binary emits transport-neutral outputs only

covlens writes two report formats: HTML (humans) and JSON (machines). Platform-specific renderings — Markdown for GitHub PR comments, Slack Blocks, Teams cards, GitLab MR notes — are deliberately NOT printers inside the binary. Consumers compose those formats from the JSON sidecar (`coverage_report.json`, schema documented at `internal/printer/json`). Reference glue scripts for specific CI/chat platforms live under `scripts/ci/<platform>/` in this repo (e.g. `scripts/ci/circleci/pr-comment.sh`) — copy/vendor them; they are samples, not part of the binary's stability contract. The JSON schema *is* the stability contract.

## Considered options

- **Markdown printer in the binary** (`covlens` writes `coverage_report.md`): rejected — Markdown is a GitHub-PR-comment-shaped rendering, not a transport-neutral format. Shipping it invites N more platform-specific printers (Slack, Teams, GitLab) inside a tool whose job is coverage measurement. The JSON sidecar already exposes every field a Markdown comment needs; rendering in ~15 lines of `jq` keeps the binary's scope clean.
- **`covlens pr-comment` subcommand that shells to `gh`**: rejected — gives turnkey UX but drags GitHub-specific concerns (sticky-comment dance, `gh` auth surface, `CIRCLE_PULL_REQUEST` env coupling) into the binary. Future GitLab/Bitbucket users would either fork or live behind a `--platform` flag. Cleaner to keep the binary unaware of where its JSON ends up.
- **Reference scripts in-repo, transport-neutral binary** (chosen): the binary stays focused; the scripts make integration concrete for the dominant case (THN's CircleCI + GitHub) without making it the only case. New platforms land as sibling folders under `scripts/ci/` without binary changes.

## Implications

- JSON schema bumps (`schema: "1"` → `"2"`) are breaking for consumers. Reference scripts MUST assert the schema version before parsing.
- Reference scripts are documented as samples — drift between a vendored copy and the upstream script is expected and acceptable.
- The HTML report URL embedded in PR comments comes from the CircleCI artifacts API, not from a covlens-emitted field. covlens reports the local filesystem path (`htmlReportPath`); resolving it to a publicly-reachable URL is the CI glue's job.
