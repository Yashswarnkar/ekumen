#!/usr/bin/env bash
# CI gate: run `ekumen check --json` and `ekumen project --check --json` on a
# project root, and fail on invalid records, lane violations, conflicts, or
# committed decisions/<id>.yaml that don't match the events
# (D-projections-in-git). See docs/development.md.
#
#   scripts/ci-check.sh [root]       (default root: .)
#   EKUMEN_BIN=path/to/ekumen        (default: bin/ekumen)
#
# The event log is append-only: against a base ref, an events file that
# was deleted, modified or renamed fails, and so does a missing
# .ekumen/events/ that exists on the base. The base is $EKUMEN_CI_BASE;
# else, in GitHub Actions, the pull request's target branch or the push's
# previous commit (fetched when the checkout is shallow); else the
# merge-base with origin/main. With no base, the append-only check is
# skipped and so is a missing .ekumen/events/, with a notice. A folder
# with no events always fails.
set -u
root=${1:-.}
bin=${EKUMEN_BIN:-bin/ekumen}
events="$root/.ekumen/events"

g() { git -C "$root" "$@"; }

resolve() {
  if g rev-parse -q --verify "$1^{commit}" >/dev/null 2>&1; then
    g rev-parse "$1^{commit}"
  elif g fetch -q --no-tags --depth=1 origin "$2" >/dev/null 2>&1; then
    g rev-parse FETCH_HEAD
  fi
}

base=""
if [ -n "${EKUMEN_CI_BASE:-}" ]; then
  base=$(resolve "$EKUMEN_CI_BASE" "$EKUMEN_CI_BASE")
  if [ -z "$base" ]; then
    echo "::error::ekumen check: EKUMEN_CI_BASE=$EKUMEN_CI_BASE is not a commit"
    exit 1
  fi
elif [ -n "${GITHUB_BASE_REF:-}" ]; then
  base=$(resolve "refs/remotes/origin/$GITHUB_BASE_REF" "refs/heads/$GITHUB_BASE_REF")
elif [ "${GITHUB_EVENT_NAME:-}" = push ] && [ -n "${GITHUB_EVENT_PATH:-}" ] && command -v jq >/dev/null; then
  before=$(jq -r '.before // empty' "$GITHUB_EVENT_PATH")
  if [ -n "${before//0/}" ]; then
    base=$(resolve "$before" "$before")
  fi
fi
if [ -z "$base" ] && g rev-parse -q --verify origin/main >/dev/null 2>&1; then
  base=$(g merge-base HEAD origin/main 2>/dev/null)
fi

if [ -n "$base" ]; then
  echo "ekumen check: append-only check against $base"
  if [ -n "$(g ls-tree -d --name-only "$base" -- .ekumen/events)" ] && [ ! -d "$events" ]; then
    echo "::error::ekumen check: $events exists on the base ($base) but is missing"
    exit 1
  fi
  changed=$(g diff -M --diff-filter=DMR --name-only "$base" -- .ekumen/events)
  if [ -n "$changed" ]; then
    echo "::error::ekumen check: the event log is append-only, but these files were deleted, modified or renamed since $base:"
    echo "$changed"
    exit 1
  fi
fi
if [ ! -d "$events" ]; then
  echo "::notice::ekumen check skipped: $events does not exist yet"
  exit 0
fi
if [ -z "$(find "$events" -type f -name '*.json' -print -quit)" ]; then
  echo "::error::ekumen check: $events exists but holds no events"
  exit 1
fi

# Observed PR state is not needed to validate records.
out=$(EKUMEN_OBSERVE=off "$bin" --root "$root" check --json)
code=$?
echo "$out"
if [ "$code" -ne 0 ]; then
  echo "::error::ekumen check failed (exit $code): see the JSON above"
fi

# registry.yaml and state.json are local (git-ignored); per-item files are
# committed and must match the events.
pout=$(EKUMEN_OBSERVE=off "$bin" --root "$root" project --check --json)
pcode=$?
echo "$pout"
if [ "$pcode" -ne 0 ]; then
  echo "::error::ekumen project --check failed (exit $pcode): run \`ekumen project\` and commit .ekumen/decisions/"
fi

if [ "$code" -ne 0 ]; then
  exit "$code"
fi
exit "$pcode"
