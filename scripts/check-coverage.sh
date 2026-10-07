#!/usr/bin/env bash
# Measures test coverage of the production Go lines (not *_test.go) added or
# modified since the merge-base with the base branch, and fails when too few of
# them are covered. Lines no cover block spans (comments, declarations, blank
# lines) are not executable and are left out.
#
# Usage: check-coverage.sh [profile] [base-ref]
#   profile    a go test -coverprofile file; without one, the script runs
#              go test -coverpkg=./... ./... itself.
#   base-ref   defaults to $PR_BASE_REF, then origin/main, then main.
#   COVERAGE_MIN overrides the minimum percentage (default 80).
#
# Committed, staged, unstaged and untracked *.go files are all considered, so
# the check gives the same answer before a commit as it will in CI.
#
# Exit status: 0 at or above the minimum, 1 below it, 2 when the check can't run.
set -euo pipefail

min="${COVERAGE_MIN:-80}"
profile="${1:-}"
base="${2:-${PR_BASE_REF:-}}"
if [[ -z "$base" ]]; then
  if git rev-parse -q --verify origin/main >/dev/null; then base=origin/main; else base=main; fi
fi

if [[ -n "$profile" ]]; then
  if [[ ! -r "$profile" ]]; then
    echo "Cannot read cover profile '$profile'" >&2
    exit 2
  fi
  profile="$(cd "$(dirname "$profile")" && pwd)/$(basename "$profile")"
fi

# Pathspecs are relative to the current directory; check the whole repository.
cd "$(git rev-parse --show-toplevel)"

if ! merge_base="$(git merge-base HEAD "$base" 2>&1)"; then
  echo "Cannot find the merge-base with '$base': $merge_base" >&2
  exit 2
fi

# changed prints one "file:line" per production Go line added or modified.
changed() {
  git diff -U0 --no-color --no-ext-diff -M --src-prefix=a/ --dst-prefix=b/ \
    "$merge_base" -- '*.go' ':(exclude)*_test.go' |
    awk '/^\+\+\+ / { file = substr($0, 7); next }
      /^@@ / {
        n = split(substr($3, 2), r, ",")
        count = n > 1 ? r[2] : 1
        for (i = 0; i < count; i++) print file ":" r[1] + i
      }'
  git ls-files -z --others --exclude-standard -- '*.go' ':(exclude)*_test.go' |
    xargs -0 -r awk '{ print FILENAME ":" FNR }'
}

changed_lines="$(changed)"
if [[ -z "$changed_lines" ]]; then
  echo "No production Go line changed since $base."
  exit 0
fi

if [[ -z "$profile" ]]; then
  profile="$(mktemp)"
  trap 'rm -f "$profile"' EXIT
  go test -coverpkg=./... -coverprofile="$profile" ./... >&2 || exit 2
fi

module="$(awk '$1 == "module" { print $2; exit }' go.mod)"

# A line counts when a block with statements spans it, and is covered when any
# such block ran: with -coverpkg, each test binary reports every package. A
# block ending at column 1 stops before that line's code (an if body's "}").
printf '%s\n' "$changed_lines" | awk -v prefix="$module/" -v min="$min" -v base="$base" '
  FNR == NR { if (!($0 in changed)) { changed[$0] = 1; order[++n] = $0 }; next }
  FNR == 1 && /^mode:/ { next }
  {
    sub(/\r$/, "")
    colon = index($1, ":")
    file = substr($1, 1, colon - 1)
    if (index(file, prefix) == 1) file = substr(file, length(prefix) + 1)
    split(substr($1, colon + 1), pos, /[.,]/)
    if ($2 == 0) next
    last = pos[4] == 1 ? pos[3] - 1 : pos[3]
    for (l = pos[1]; l <= last; l++) {
      key = file ":" l
      if (!(key in changed)) continue
      executable[key] = 1
      if ($3 > 0) covered[key] = 1
    }
  }
  END {
    for (i = 1; i <= n; i++) {
      if (!(order[i] in executable)) continue
      total++
      if (order[i] in covered) hit++; else missed[++m] = order[i]
    }
    if (total == 0) { print "No executable production Go line changed since " base "."; exit 0 }
    if (m > 0) { print "Uncovered changed lines:"; for (i = 1; i <= m; i++) print "  " missed[i] }
    printf "Changed-line coverage since %s: %d/%d (%.1f%%), minimum %s%%\n", base, hit, total, 100 * hit / total, min
    if (hit * 100 < min * total) {
      fflush()
      print "Add tests for the lines above (see \"Règles de PR\" in CLAUDE.md)." > "/dev/stderr"
      exit 1
    }
  }' - "$profile"
