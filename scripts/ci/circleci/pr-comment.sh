#!/usr/bin/env bash
# Posts (or updates) a sticky PR comment summarising a covlens run.
# Sample script — copy into your repo. Not part of the covlens binary contract.
# See ../../../docs/adr/0003-transport-neutral-outputs.md
set -euo pipefail
export LC_ALL=C  # printf "%.1f" is locale-sensitive (comma vs dot)

# --- load & validate report ---
json="${1:-.coverage/coverage_report.json}"
[ -f "$json" ] || { echo "covlens report not found: $json" >&2; exit 1; }
schema=$(jq -r .schema "$json")
[ "$schema" = "1" ] || { echo "covlens JSON schema mismatch: got $schema, expected 1" >&2; exit 1; }

# --- non-PR build: no-op ---
pr="${CIRCLE_PULL_REQUEST:-}"; pr="${pr##*/}"
[ -n "$pr" ] || { echo "not a PR build — skipping comment"; exit 0; }

# --- nothing-to-report: skip ---
diff_status=$(jq -r '.diff.status // ""' "$json")
case "$diff_status" in
  no-go-changes|only-deletions|all-excluded)
    echo "diff has nothing to measure (status=$diff_status) — skipping comment"; exit 0 ;;
esac

# --- resolve artifact URL ---
artifact_url="https://app.circleci.com/private/output/job/${CIRCLE_WORKFLOW_JOB_ID}/artifacts/${CIRCLE_NODE_INDEX:-0}/coverage/coverage_report.html"

# --- compose markdown body ---
total_cov=$(printf "%.1f" "$(jq -r '.totalCoverage' "$json")")
total_passed=$(jq -r '.totalPassed' "$json")
total_threshold=$(jq -r '.totalThreshold' "$json")
has_diff=$(jq -r 'has("diff")' "$json")
has_baseline=$(jq -r 'has("baselineTotalCoverage")' "$json")

status_icon() { [ "$1" = "true" ] && echo "✅" || echo "❌"; }

ratchet_line=""
if [ "$has_baseline" = "true" ]; then
  ratchet=$(jq -r '.totalCoverage - .baselineTotalCoverage' "$json")
  ratchet_passed=$(jq -r '.totalCoverage >= .baselineTotalCoverage' "$json")
  ratchet=$(printf "%+.2f" "$ratchet")
  ratchet_line="
Ratchet vs baseline: ${ratchet}pp $(status_icon "${ratchet_passed}")"
fi

diff_row=""
if [ "$has_diff" = "true" ]; then
  diff_cov=$(printf "%.1f" "$(jq -r '.diff.coverage' "$json")")
  diff_passed=$(jq -r '.diff.passed' "$json")
  diff_threshold=$(jq -r '.diff.threshold' "$json")
  diff_row="| Changed lines | ${diff_cov}% | ${diff_threshold}% | $(status_icon "${diff_passed}") |
"
fi

body="<!-- covlens -->
## Coverage report

| Metric | Coverage | Threshold | |
|---|---|---|---|
${diff_row}| Total project | ${total_cov}% | ${total_threshold}% | $(status_icon "${total_passed}") |
${ratchet_line}

[View full report](${artifact_url})"

# --- sticky upsert ---
repo="${CIRCLE_PROJECT_USERNAME}/${CIRCLE_PROJECT_REPONAME}"
existing=$(gh api "repos/$repo/issues/$pr/comments" --paginate \
  --jq '.[] | select(.body | startswith("<!-- covlens -->")) | .id' | head -1)

if [ -n "$existing" ]; then
  jq -n --arg b "$body" '{body: $b}' \
    | gh api -X PATCH "repos/$repo/issues/comments/$existing" --input - >/dev/null
  echo "Updated sticky comment $existing on PR #$pr"
else
  printf '%s' "$body" | gh pr comment "$pr" --body-file -
  echo "Created sticky comment on PR #$pr"
fi
