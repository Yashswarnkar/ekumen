#!/usr/bin/env bash
# CI gate: run `ekumen check --json` on a project root and fail on invalid
# records, lane violations or conflicts. See docs/development.md.
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
exit "$code"
