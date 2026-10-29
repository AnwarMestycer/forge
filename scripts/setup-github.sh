#!/usr/bin/env bash
# Creates one GitHub milestone per roadmap section and one issue per unchecked
# "- [ ]" item in ROADMAP.md. Safe to re-run: existing milestones/issues are skipped.
#
# Usage:  ./scripts/setup-github.sh            # create on GitHub (needs: gh auth login)
#         DRY_RUN=1 ./scripts/setup-github.sh  # just print what would be created
set -euo pipefail

REPO="${REPO:-AnwarMestycer/forge}"
ROADMAP="${ROADMAP:-$(dirname "$0")/../ROADMAP.md}"
DRY_RUN="${DRY_RUN:-0}"

declare -A MS_SEEN=()

ensure_milestone() {
  local title="$1"
  [[ -n "${MS_SEEN[$title]:-}" ]] && return
  MS_SEEN[$title]=1
  if [[ "$DRY_RUN" == 1 ]]; then echo "MILESTONE  $title"; return; fi
  if gh api "repos/$REPO/milestones?state=all&per_page=100" --jq '.[].title' | grep -Fxq "$title"; then
    echo "exists     $title"
  else
    gh api "repos/$REPO/milestones" -f title="$title" >/dev/null
    echo "created    $title"
  fi
}

ensure_issue() {
  local milestone="$1" title="$2"
  if [[ "$DRY_RUN" == 1 ]]; then echo "  issue    $title"; return; fi
  if gh issue list -R "$REPO" --state all --search "\"$title\" in:title" --json title --jq '.[].title' | grep -Fxq "$title"; then
    echo "  exists   $title"
  else
    gh issue create -R "$REPO" --title "$title" --milestone "$milestone" \
      --body "From ROADMAP.md → **$milestone**." >/dev/null
    echo "  created  $title"
  fi
}

current=""
while IFS= read -r line; do
  # Milestone headers: "## M0 — Foundations", "## Spike — ...", "## Testbed"
  if [[ "$line" =~ ^##\ (M[0-9]+|Spike|Testbed)(.*)$ ]]; then
    current="$(echo "${line#\#\# }" | sed 's/[[:space:]]*$//')"
    ensure_milestone "$current"
    continue
  fi
  # Any other "## " header ends milestone parsing
  if [[ "$line" =~ ^##\  ]]; then current=""; continue; fi
  # Unchecked items become issues
  if [[ -n "$current" && "$line" =~ ^-\ \[\ \]\ (.+)$ ]]; then
    item="${BASH_REMATCH[1]}"
    ensure_issue "$current" "${current%% —*}: $item"
  fi
done < "$ROADMAP"
