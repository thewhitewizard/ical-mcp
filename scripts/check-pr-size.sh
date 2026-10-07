#!/usr/bin/env bash
# Counts Go lines changed (added + deleted, tests included) since the merge-base
# with the base branch, and fails when the total exceeds the PR limit.
#
# Usage: check-pr-size.sh [base-ref]
#   base-ref   defaults to $PR_BASE_REF, then origin/main, then main.
#   PR_MAX_GO_LINES overrides the limit (default 500).
#
# Committed, staged, unstaged and untracked *.go files are all counted, so the
# check gives the same answer before a commit as it will in CI.
#
# Exit status: 0 within the limit, 1 over the limit, 2 when the check can't run.
set -euo pipefail

limit="${PR_MAX_GO_LINES:-500}"
base="${1:-${PR_BASE_REF:-}}"
if [[ -z "$base" ]]; then
  if git rev-parse -q --verify origin/main >/dev/null; then base=origin/main; else base=main; fi
fi

# Pathspecs are relative to the current directory; count the whole repository.
cd "$(git rev-parse --show-toplevel)"

if ! merge_base="$(git merge-base HEAD "$base" 2>&1)"; then
  echo "Cannot find the merge-base with '$base': $merge_base" >&2
  exit 2
fi

# Renames (-M) count only their edits, as on GitHub. Binary files report "-"
# in --numstat; they are not Go source lines.
tracked="$(git diff --numstat -M "$merge_base" -- '*.go' |
  awk '$1 != "-" { n += $1 + $2 } END { print n + 0 }')"

# awk's NR also counts a last line that has no trailing newline (wc -l does not).
untracked=0
while IFS= read -r -d '' file; do
  untracked=$((untracked + $(awk 'END { print NR }' "$file")))
done < <(git ls-files -z --others --exclude-standard -- '*.go')

total=$((tracked + untracked))
echo "Go lines changed since $base: $total / $limit"

if ((total > limit)); then
  echo "PR size limit exceeded by $((total - limit)) Go lines." >&2
  echo "Ship what is coherent now and open a follow-up ticket (see \"Règles de PR\" in CLAUDE.md)." >&2
  exit 1
fi
