#!/usr/bin/env bash
# CI gate: run `ekumen check --json` and `ekumen project --check --json` on a
# project root, and fail on invalid records, lane violations, conflicts, or
# committed decisions/<id>.yaml that don't match the events
# (D-projections-in-git). See docs/development.md.
#
#   scripts/ci-check.sh [root]       (default root: .)
#   EKUMEN_BIN=path/to/ekumen        (default: bin/ekumen)
#
# No .ekumen/events/ folder: skipped with a notice. A folder with no
# events: failure (the log was lost or never imported).
set -u
root=${1:-.}
bin=${EKUMEN_BIN:-bin/ekumen}
events="$root/.ekumen/events"

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
