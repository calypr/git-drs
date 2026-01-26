#!/usr/bin/env bash
# bash
set -euo pipefail

FILE='fixtures/TARGET-ALL-P2/sub-directory-1/file-0003.dat'
LINE='TARGET-ALL-P2/sub-directory-1/file-0003.dat'
TARGET_BYTES=$((20 * 1024 * 1024))  # 20 MiB

mkdir -p "$(dirname "$FILE")"

# create an empty file if it doesn't exist
: > "$FILE"

# make a temporary chunk file (10k lines) to reduce syscalls
TMP_CHUNK="$(mktemp)"
trap 'rm -f "$TMP_CHUNK"' EXIT

for ((i=0; i<10000; i++)); do
  printf '%s\n' "$LINE" >> "$TMP_CHUNK"
done

# Append chunks until file size exceeds target
while true; do
  current_size=$(stat -f%z "$FILE" 2>/dev/null || echo 0)
  if [ "$current_size" -gt "$TARGET_BYTES" ]; then
    printf 'done: %s is %d bytes\n' "$FILE" "$current_size" >&2
    break
  fi

  cat "$TMP_CHUNK" >> "$FILE"

  # optional progress output every iteration
  current_size=$(stat -f%z "$FILE")
  printf 'progress: %s is %d bytes\n' "$FILE" "$current_size" >&2
done
