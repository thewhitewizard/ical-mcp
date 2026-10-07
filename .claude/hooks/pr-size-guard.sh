#!/usr/bin/env bash
# Claude Code PreToolUse hook (Bash): runs scripts/check-pr-size.sh before
# `git commit`, `git push` and `gh pr create`, and blocks them (exit 2) when the
# PR exceeds the Go line limit. Any other command passes through untouched.
set -uo pipefail

check="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/scripts/check-pr-size.sh"

# Pull tool_input.command out of the JSON payload without depending on jq:
# blank out escapes (\\, \", \n, \t, \r), then take the first "command" string.
tool_cmd="$(sed -e 's/\\\\/ /g' -e 's/\\[ntr"]/ /g' |
  grep -oE '"command"[[:space:]]*:[[:space:]]*"[^"]*"' | head -n 1)"

# `git [options] commit|push` (e.g. `git -C dir commit`) or `gh pr create`.
git_opts='([[:space:]]+-[^[:space:]]+([[:space:]]+[^-[:space:]][^[:space:]]*)?)*'
guarded="(^|[^[:alnum:]_.-])(git${git_opts}[[:space:]]+(commit|push)|gh[[:space:]]+pr[[:space:]]+create)([^[:alnum:]_-]|\$)"
if ! grep -qE "$guarded" <<<"$tool_cmd"; then
  exit 0
fi

report="$(cd "${CLAUDE_PROJECT_DIR:-.}" && bash "$check" 2>&1)"
case $? in
  0) exit 0 ;;
  1)
    {
      echo "Blocked by the PR size guard."
      echo "$report"
      echo "Stop here: deliver the coherent part and propose a follow-up ticket instead of exceeding the limit."
    } >&2
    ;;
  *)
    {
      echo "Blocked: the PR size check could not run."
      echo "$report"
    } >&2
    ;;
esac
exit 2
