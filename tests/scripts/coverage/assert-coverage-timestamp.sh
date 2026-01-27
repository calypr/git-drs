#!/usr/bin/env bash
# File: `tests/scripts/coverage/assert-coverage-timestamp.sh`
set -euo pipefail

latest_go_time=$(git log  --format=%ct -- ./**/*.go  | sort | tail -1)
combinded_time=$(git log -1 --format=%ct -- coverage/combined.out)

echo for debugging:
git rev-list HEAD | while read commit; do echo $(git log -1 $commit --pretty=format:'%ct' --name-only  ); done | sort | uniq

if [ "$combinded_time" -gt "$latest_go_time" ]; then
  echo "OK: $combinded_time  coverage/combined.out is newer than latest .go file ($latest_go_time)."
  exit 0
else
  echo "FAIL: $combinded_time  coverage/combined.out is NOT newer than latest .go file ($latest_go_time)." >&2
  echo "  coverage/combined.out mtime: $(date -r coverage/combined.out +'%F %T' 2>/dev/null || date -d @"$unit_m" +'%F %T' 2>/dev/null)"
  echo "  latest .go mtime: $(git rev-list HEAD | while read commit; do echo $(git log -1 $commit --pretty=format:'%ct' --name-only -- ./**/*.go ); done | sort | tail -1)"
  exit 2
fi
