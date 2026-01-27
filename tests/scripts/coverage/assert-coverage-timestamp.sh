#!/usr/bin/env bash
# File: `tests/scripts/coverage/assert-coverage-timestamp.sh`
set -euo pipefail

COV='coverage/integration/coverage.out'
if [ ! -f "$COV" ]; then
  echo "Missing coverage file: $COV" >&2
  exit 1
fi

UNIT='coverage/unit/coverage.out'
if [ ! -f "$UNIT" ]; then
  echo "Missing coverage file: $UNIT" >&2
  exit 1
fi

# Helper: return first numeric token from stat output (or 0)
get_mtime() {
  local file="$1"
  local raw res
  raw=$(stat -f %m "$file" 2>/dev/null || stat -c %Y "$file" 2>/dev/null || printf 0)
  res=$(printf '%s\n' "$raw" | awk '{for(i=1;i<=NF;i++) if($i ~ /^[0-9]+$/){print $i; exit}}')
  if [ -z "$res" ]; then
    printf '0'
  else
    printf '%s' "$res"
  fi
}

# Find newest mtime (seconds since epoch) among .go files (ignore vendor)
max=0
latest_go=''
while IFS= read -r -d '' f; do
  m=$(get_mtime "$f")
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

cov_m=$(get_mtime "$COV")
if [ -z "$cov_m" ] || [ "$cov_m" -eq 0 ]; then
  echo "Could not read mtime for $COV" >&2
  exit 1
fi

if [ "$cov_m" -gt "$max" ]; then
  echo "OK: $COV is newer than latest .go file ($latest_go)."
else
  echo "FAIL: $COV is NOT newer than latest .go file ($latest_go)." >&2
  echo "  $COV mtime: $(date -r "$cov_m" +'%F %T' 2>/dev/null || date -d @"$cov_m" +'%F %T' 2>/dev/null)"
  echo "  latest .go mtime: $(date -r "$max" +'%F %T' 2>/dev/null || date -d @"$max" +'%F %T' 2>/dev/null)"
  exit 2
fi

unit_m=$(get_mtime "$UNIT")
if [ -z "$unit_m" ] || [ "$unit_m" -eq 0 ]; then
  echo "Could not read mtime for $UNIT" >&2
  exit 1
fi

if [ "$unit_m" -gt "$max" ]; then
  echo "OK: $UNIT is newer than latest .go file ($latest_go)."
  exit 0
else
  echo "FAIL: $UNIT is NOT newer than latest .go file ($latest_go)." >&2
  echo "  $UNIT mtime: $(date -r "$unit_m" +'%F %T' 2>/dev/null || date -d @"$unit_m" +'%F %T' 2>/dev/null)"
  echo "  latest .go mtime: $(date -r "$max" +'%F %T' 2>/dev/null || date -d @"$max" +'%F %T' 2>/dev/null)"
  exit 2
fi
