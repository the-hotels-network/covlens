#!/usr/bin/env bash
# Posts (or updates) a sticky PR comment summarising a covlens run.
# Sample script — copy into your repo. Not part of the covlens binary contract.
# See ../../../docs/adr/0003-transport-neutral-outputs.md
set -euo pipefail

json="${1:-.coverage/coverage_report.json}"
[ -f "$json" ] || { echo "covlens report not found: $json" >&2; exit 1; }

# --- schema pin ---
schema=$(jq -r .schema "$json")
[ "$schema" = "1" ] || { echo "covlens JSON schema mismatch: got $schema, expected 1" >&2; exit 1; }

# --- non-PR build: no-op ---
pr="${CIRCLE_PULL_REQUEST##*/}"
[ -n "${pr:-}" ] || { echo "not a PR build — skipping comment"; exit 0; }

# --- resolve artifact URL via CircleCI API ---
slug="gh/${CIRCLE_PROJECT_USERNAME}/${CIRCLE_PROJECT_REPONAME}"
artifact_url=$(curl -sSL -H "Circle-Token: ${CIRCLE_TOKEN:?CIRCLE_TOKEN required}" \
  "https://circleci.com/api/v2/project/${slug}/${CIRCLE_BUILD_NUM}/artifacts" \
  | jq -r '.items[]? | select(.path | endswith("coverage_report.html")) | .url' \
  | head -1)

# --- compose markdown body ---
body=$(jq -r --arg url "$artifact_url" '
  "<!-- covlens -->\n" +
  "**Coverage** " + (if (.diff.passed // true) and .totalPassed then "✅ pass" else "❌ fail" end) + "\n\n" +
  "| scope | coverage | threshold |\n|---|---|---|\n" +
  (if .diff then "| diff  | \(.diff.coverage)% | \(.diff.threshold)% |\n" else "" end) +
  "| total | \(.totalCoverage)% | \(.totalThreshold)% |\n" +
  (if $url != "" then "\n[HTML report](\($url))" else "" end)
' "$json")

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
