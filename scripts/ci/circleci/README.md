# CircleCI reference glue for covlens

Sample scripts that wire a covlens run into CircleCI + GitHub. **Not part of the covlens binary's stability contract** — see [ADR 0003](../../../docs/adr/0003-transport-neutral-outputs.md). Copy what you need into your own repo; the JSON sidecar (`coverage_report.json`, schema `1`) is the stable interface.

## `pr-comment.sh`

Posts a sticky PR comment summarising the covlens run (pass/fail, diff %, total %, link to the HTML report stored as a CircleCI artifact). Updates the existing comment on subsequent pushes — one comment per PR.

### Required env

| var | source | purpose |
|---|---|---|
| `GH_TOKEN` (or `GITHUB_TOKEN`) | project context | `gh` auth; needs `repo` scope (PR write) |
| `CIRCLE_TOKEN` | project context | CircleCI API; reads artifact URL |
| `CIRCLE_PULL_REQUEST` | built-in | PR URL; unset on non-PR builds (script no-ops) |
| `CIRCLE_PROJECT_USERNAME` | built-in | org/user |
| `CIRCLE_PROJECT_REPONAME` | built-in | repo |
| `CIRCLE_BUILD_NUM` | built-in | job number for artifact lookup |

### Required tools in the CI image

`gh`, `jq`, `curl`. All present in `cimg/go` images. On Alpine-based images install them first.

### Usage

```yaml
- run:
    name: covlens PR comment
    when: always           # comment on threshold failure too
    command: .circleci/scripts/pr-comment.sh .coverage/coverage_report.json
```

Place this step **after** `covlens` and `store_artifacts`, **before** any threshold-enforcement step that exits 1 — `when: always` covers it either way, but ordering keeps logs readable.

### Schema pinning

The script asserts `schema == "1"`. If covlens bumps the schema, update the script and pin a compatible covlens version (`go install …/covlens@vX.Y.Z`). See [`internal/printer/json`](../../../internal/printer/json) for the schema.
