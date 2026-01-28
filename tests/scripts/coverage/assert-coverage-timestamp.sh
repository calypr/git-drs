#!/usr/bin/env bash
# File: `tests/scripts/coverage/assert-coverage-timestamp.sh`
set -euo pipefail

COV='coverage/combined.out'
if [ ! -f "$COV" ]; then
  echo "Missing coverage file: $COV" >&2
  exit 1
fi

# Helper: return first numeric token from stat output (or 0)
get_mtime() {
  local file="$1"
  local line ts epoch raw res nowyear

  # 1) GNU ls --full-time => date in "YYYY-MM-DD HH:MM:SS" form
  if line=$(ls -l --full-time "$file" 2>/dev/null); then
    ts=$(printf '%s\n' "$line" | awk '{for(i=1;i<NF;i++) if($i ~ /^[0-9]{4}-[0-9]{2}-[0-9]{2}$/){print $(i) " " $(i+1); exit}}')
    if [ -n "$ts" ]; then
      epoch=$(date -d "$ts" +%s 2>/dev/null || date -j -f "%Y-%m-%d %H:%M:%S" "$ts" +%s 2>/dev/null || printf 0)
      printf '%s' "${epoch:-0}"; return
    fi
  fi

  # 2) BSD/GNU ls -lT => "Mon DD HH:MM:SS YYYY"
  if line=$(ls -lT "$file" 2>/dev/null); then
    ts=$(printf '%s\n' "$line" | awk '{for(i=1;i<NF-3;i++) if($i ~ /^[A-Z][a-z]{2}$/ && $(i+2) ~ /:/ && $(i+3) ~ /^[0-9]{4}$/){print $(i) " " $(i+1) " " $(i+2) " " $(i+3); exit}}')
    if [ -n "$ts" ]; then
      epoch=$(date -d "$ts" +%s 2>/dev/null || date -j -f "%b %d %T %Y" "$ts" +%s 2>/dev/null || printf 0)
      printf '%s' "${epoch:-0}"; return
    fi
  fi

  # 3) Plain ls -l => "Mon DD HH:MM" (recent) or "Mon DD YYYY" (older)
  if line=$(ls -l "$file" 2>/dev/null); then
    ts=$(printf '%s\n' "$line" | awk '{for(i=1;i<NF-2;i++) if($i ~ /^[A-Z][a-z]{2}$/ && $(i+1) ~ /^[0-9]{1,2}$/){print $(i)" "$(i+1)" "$(i+2); exit}}')
    if [ -n "$ts" ]; then
      if printf '%s\n' "$ts" | awk '{print $3}' | grep -q ':'; then
        nowyear=$(date +%Y 2>/dev/null || date -j +%Y 2>/dev/null || printf '%s' "$(date +%Y)")
        ts="$ts $nowyear"
        epoch=$(date -d "$ts" +%s 2>/dev/null || date -j -f "%b %d %H:%M %Y" "$ts" +%s 2>/dev/null || printf 0)
      else
        epoch=$(date -d "$ts" +%s 2>/dev/null || date -j -f "%b %d %Y" "$ts" +%s 2>/dev/null || printf 0)
      fi
      printf '%s' "${epoch:-0}"; return
    fi
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
