#!/usr/bin/env bash
# release-notes-stats.sh — regenerate the provenance block for a release
# notes file from git state.
#
# Why this exists: release notes that hardcode commit counts and diff
# stats go stale every time a new commit lands on the release branch.
# The v1.0.6 self-audit flagged this cycle — we'd update the note to
# "61 commits" and four follow-up commits later it was already wrong.
# Rather than fight the drift, this script regenerates the block at
# PR-finalization time so it matches the actual merged state.
#
# Usage:
#   scripts/release-notes-stats.sh <base-tag>
#
# Example:
#   scripts/release-notes-stats.sh v1.0.5
#
# Output: multi-line block suitable for pasting into the Provenance
# section of docs/releases/v<x.y.z>-release-notes.md.
#
# Run from the repo root (any directory actually; the script cds to
# its own parent's parent first).

set -euo pipefail

BASE_TAG="${1:-}"
if [ -z "$BASE_TAG" ]; then
    echo "usage: $0 <base-tag>" >&2
    echo "example: $0 v1.0.5" >&2
    exit 2
fi

# Move to repo root so git commands work regardless of CWD.
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR/.."

# Validate that the base tag exists.
if ! git rev-parse --verify "$BASE_TAG" >/dev/null 2>&1; then
    echo "error: base tag '$BASE_TAG' not found in this repo" >&2
    exit 1
fi

COMMITS=$(git rev-list --count "${BASE_TAG}..HEAD")
BRANCH=$(git rev-parse --abbrev-ref HEAD)

# git diff --shortstat: " 172 files changed, 23267 insertions(+), 805 deletions(-)"
# parse into three numbers.
SHORTSTAT=$(git diff --shortstat "${BASE_TAG}..HEAD" 2>/dev/null || echo "")
FILES=$(echo "$SHORTSTAT"   | awk '{print $1}')
INS=$(echo "$SHORTSTAT"     | grep -oE '[0-9]+ insertion' | awk '{print $1}')
DEL=$(echo "$SHORTSTAT"     | grep -oE '[0-9]+ deletion'  | awk '{print $1}')
INS="${INS:-0}"
DEL="${DEL:-0}"

cat <<BLOCK
- Branch: $BRANCH
- Commits since $BASE_TAG: $COMMITS
- Files changed: $FILES
- Insertions / deletions: +$INS / −$DEL
- Regenerated $(date -u +%Y-%m-%dT%H:%M:%SZ)
BLOCK
