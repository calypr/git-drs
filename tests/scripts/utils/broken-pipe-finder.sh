#!/usr/bin/env bash

# strict
set -euo pipefail

# echo commands as they are executed
# set -x

# check parameter is a file
if [ ! -f "$1" ]; then
  echo "Usage: $0 \`<file>\`" >&2
  echo "error: \`$1\` does not exist or is not a regular file" >&2
  exit 2
fi

# example
# echo '[foo] bar' | sed 's/[][]//g'
# prints: foo bar
# capture START pids into array (portable; works on macOS bash)
STARTED_PIDS=()
while IFS= read -r pid; do
  STARTED_PIDS+=("$pid")
done < <(grep "START" "$1"  | awk '{print $1}' | sed 's/[][]//g' | sort -n || true)

# if no started pids were found, print error to stderr and exit non-zero
if [ "${#STARTED_PIDS[@]}" -eq 0 ]; then
  echo "Error: no START entries found in $1" >&2
  exit 3
fi

echo "Found ${#STARTED_PIDS[@]} started pids"

# capture pids with "stdin closed" into array (portable)
PROPERLY_CLOSED_PIDS=()
while IFS= read -r pid; do
  PROPERLY_CLOSED_PIDS+=("$pid")
done < <(grep "COMPLETED" "$1"  | awk '{print $1}' | sed 's/[][]//g' | sort -n | uniq || true)

echo "Found ${#PROPERLY_CLOSED_PIDS[@]} properly closed pids"

# compute started-but-not-properly-closed pids (portable)
NOT_CLOSED=()
while IFS= read -r pid; do
  NOT_CLOSED+=("$pid")
done < <(comm -23 \
  <(printf "%s\n" "${STARTED_PIDS[@]}" | sort -n | uniq) \
  <(printf "%s\n" "${PROPERLY_CLOSED_PIDS[@]}" | sort -n | uniq) || true)

for pid in "${NOT_CLOSED[@]}"; do
  printf '%s\n' "----- PID: $pid Last activity -----"
  grep -F "$pid" "$1" | tail -n 10 || true
done

#echo "Searching for pids with 'missing authorizations' in $1"
#grep "missing authorizations" $1  | awk '{print $5}' | sed 's/[][]//g' | sort  | uniq -c

