#!/usr/bin/env bash
# File: `assert-coverage-timestamp.sh`
set -euo pipefail

COV='coverage/integration/coverage.out'
if [ ! -f "$COV" ]; then
  echo "Missing coverage file: $COV" >&2
  exit 1
fi

# Find newest mtime (seconds since epoch) among .go files (ignore vendor)
max=0
latest_go=''
while IFS= read -r -d '' f; do
  m=$(stat -f %m "$f" 2>/dev/null || stat -c %Y "$f" 2>/dev/null || echo 0)
  if [ -z "$m" ] || [ "$m" -eq 0 ]; then continue; fi
  if [ "$m" -gt "$max" ]; then
    max=$m
    latest_go="$f"
  fi
done < <(find . -type f -name '*.go' -not -path './vendor/*' -print0)

if [ "$max" -eq 0 ]; then
  echo "No .go files found to compare against." >&2
  exit 1
fi

cov_m=$(stat -f %m "$COV" 2>/dev/null || stat -c %Y "$COV" 2>/dev/null || echo 0)
if [ -z "$cov_m" ] || [ "$cov_m" -eq 0 ]; then
  echo "Could not read mtime for $COV" >&2
  exit 1
fi

if [ "$cov_m" -gt "$max" ]; then
  echo "OK: $COV is newer than latest .go file ($latest_go)."
  exit 0
else
  echo "FAIL: $COV is NOT newer than latest .go file ($latest_go)." >&2
  echo "  $COV mtime: $(date -r "$cov_m" +'%F %T' 2>/dev/null || date -d @"$cov_m" +'%F %T' 2>/dev/null)"
  echo "  latest .go mtime: $(date -r "$max" +'%F %T' 2>/dev/null || date -d @"$max" +'%F %T' 2>/dev/null)"
  exit 2
fi