#!/usr/bin/env bash
# File: `tests/scripts/coverage/combine-coverage.sh`
set -euo pipefail

COV_INT='coverage/integration/coverage.out'
COV_UNIT='coverage/unit/coverage.out'

# ensure coverage files exist
if [ ! -f "$COV_INT" ]; then
  echo "Missing coverage file: $COV_INT" >&2
  echo "Run integration tests to produce $COV_INT" >&2
  exit 1
fi

if [ ! -f "$COV_UNIT" ]; then
  echo "Missing coverage file: $COV_UNIT" >&2
  echo "Run unit tests to produce $COV_UNIT" >&2
  exit 1
fi

# defaults (can be overridden in env)
COMBINED_PROFILE="${COMBINED_PROFILE:-coverage/combined.out}"
COMBINED_HTML="${COMBINED_HTML:-coverage/combined.html}"

# ensure gocovmerge is available; attempt install and add bin dir to PATH if needed
if ! command -v gocovmerge >/dev/null 2>&1; then
  echo "gocovmerge not found — attempting to install..."
  if ! go install go.shabbyrobe.org/gocovmerge/cmd/gocovmerge@latest; then
    echo "go install failed" >&2
    exit 1
  fi

  # determine install dir: prefer GOBIN, fallback to GOPATH/bin, fallback to $HOME/go/bin
  BIN_DIR="$(go env GOBIN 2>/dev/null || true)"
  if [ -z "$BIN_DIR" ]; then
    GOPATH="$(go env GOPATH 2>/dev/null || true)"
    BIN_DIR="${GOPATH:-$HOME/go}/bin"
  fi

  if [ -d "$BIN_DIR" ]; then
    PATH="$BIN_DIR:$PATH"
  fi

  if ! command -v gocovmerge >/dev/null 2>&1; then
    echo "gocovmerge still not found after install. Binary likely at: ${BIN_DIR}" >&2
    echo "Add ${BIN_DIR} to your PATH and re-run the script." >&2
    exit 1
  fi
fi

gocovmerge "$COV_INT" "$COV_UNIT" > "${COMBINED_PROFILE}"

echo "Combined coverage profile saved to ${COMBINED_PROFILE}"

go tool cover -html="${COMBINED_PROFILE}" -o "${COMBINED_HTML}"
echo "Combined coverage html saved to ${COMBINED_HTML}"

coverage=$(go tool cover -func="${COMBINED_PROFILE}" | grep total | awk '{print substr($3, 1, length($3)-1)}')
echo "Total combined coverage: ${coverage}%"
