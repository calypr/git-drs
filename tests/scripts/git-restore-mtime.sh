#!/usr/bin/env bash
# File: git-restore-mtime.sh
# 
# Restores file modification times (mtimes) based on the timestamp of the last
# Git commit that modified each file. This is useful when files are checked out
# with uniform mtimes (e.g., by actions/checkout@v4) but you need to preserve
# the actual commit history for timestamp-based assertions.
#
# The script works on both macOS and Linux by detecting the appropriate touch
# command format.

set -euo pipefail

# Detect OS for proper touch command
OS_TYPE="$(uname -s)"

# Temporary files for counters
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

echo 0 > "$TMPDIR/processed"
echo 0 > "$TMPDIR/errors"

echo "Restoring file modification times from Git history..."

# Iterate through all tracked files
while IFS= read -r file; do
  # Skip if file doesn't exist (e.g., deleted but still in git ls-files)
  if [ ! -f "$file" ]; then
    continue
  fi
  
  # Get the timestamp of the last commit that modified this file
  # %ct gives the committer timestamp in seconds since Unix epoch
  timestamp=$(git log -1 --format=%ct -- "$file" 2>/dev/null || echo "")
  
  if [ -z "$timestamp" ]; then
    # File might be new or untracked, skip it
    continue
  fi
  
  # Set the file's mtime based on OS
  success=1
  if [ "$OS_TYPE" = "Darwin" ]; then
    # macOS: convert timestamp to YYYYMMDDhhmm.SS format
    datetime=$(date -r "$timestamp" +%Y%m%d%H%M.%S 2>/dev/null || echo "")
    if [ -n "$datetime" ]; then
      if ! touch -t "$datetime" "$file" 2>/dev/null; then
        success=0
      fi
    else
      success=0
    fi
  else
    # Linux uses -d flag with @timestamp
    if ! touch -d "@$timestamp" "$file" 2>/dev/null; then
      success=0
    fi
  fi
  
  # Update counters
  processed=$(<"$TMPDIR/processed")
  errors=$(<"$TMPDIR/errors")
  echo $((processed + 1)) > "$TMPDIR/processed"
  if [ "$success" -eq 0 ]; then
    echo $((errors + 1)) > "$TMPDIR/errors"
  fi
done < <(git ls-files)

processed=$(<"$TMPDIR/processed")
errors=$(<"$TMPDIR/errors")

echo "Restored mtimes for $processed files (errors: $errors)"

if [ "$errors" -gt 0 ]; then
  echo "Warning: Failed to restore mtime for $errors files" >&2
fi

exit 0
