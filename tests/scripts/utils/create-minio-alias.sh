#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'HELP'
create-minio-alias.sh — set a MinIO client alias using parameters

USAGE:
    ./create-minio-alias.sh <alias> <endpoint> <access_key> <secret_key> [--insecure]

EXAMPLE:
    ./create-minio-alias.sh myminio https://127.0.0.1:9000 minioadmin minioadmin --insecure
HELP
}

# Detect help anywhere
for a in "$@"; do
  if [[ "$a" == "--help" || "$a" == "-h" ]]; then
    usage
    exit 0
  fi
done

if (( $# < 4 )); then
  echo "Error: missing required parameters."
  usage
  exit 1
fi

ALIAS="$1"
ENDPOINT="$2"
ACCESS_KEY="$3"
SECRET_KEY="$4"

# optional fifth arg may be --insecure
INSECURE=false
if (( $# >= 5 )) && [[ "${5}" == "--insecure" ]]; then
  INSECURE=true
fi

if ! command -v mc >/dev/null 2>&1; then
  echo "Error: mc (MinIO client) not found in PATH."
  exit 1
fi

if [[ "$INSECURE" == "true" ]]; then
  mc alias set "$ALIAS" "$ENDPOINT" "$ACCESS_KEY" "$SECRET_KEY" --insecure --api S3v4
else
  mc alias set "$ALIAS" "$ENDPOINT" "$ACCESS_KEY" "$SECRET_KEY" --api S3v4
fi

echo "Alias '$ALIAS' configured for endpoint '$ENDPOINT'."
