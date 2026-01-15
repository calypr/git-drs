#!/usr/bin/env bash
set -u -o pipefail

usage() {
  cat <<'USAGE'
Usage: ./mc-ping-alias.sh <alias> [--insecure]

Example:
  ./mc-ping-alias.sh myminio --insecure
USAGE
}

if (( $# < 1 )); then
  usage
  exit 1
fi

ALIAS="$1"
INSEC_FLAG=""
if [[ "${2:-}" == "--insecure" ]]; then
  INSEC_FLAG="--insecure"
fi

if ! command -v mc >/dev/null 2>&1; then
  echo "Error: mc (MinIO client) not found in PATH."
  exit 2
fi

# Try a simple listing on the alias root to verify connectivity.
# Suppress normal output; on failure re-run without suppression to show error details.
if mc "${INSEC_FLAG}" ls "${ALIAS}" >/dev/null 2>&1; then
  echo "OK: alias '${ALIAS}' reachable"
  exit 0
else
  echo "ERROR: alias '${ALIAS}' unreachable - showing details:"
  mc "${INSEC_FLAG}" ls "${ALIAS}" || true
  exit 3
fi
