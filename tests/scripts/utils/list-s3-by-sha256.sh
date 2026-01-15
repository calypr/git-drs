#!/usr/bin/env bash
set -euo pipefail

show_help() {
    cat << 'HELP'
list-s3-by-sha256.sh — format sha256 hashes as s3 URLs and list via the MinIO client.

USAGE:
    list-s3-by-sha256.sh <mc-alias> <bucket> [prefix]

PARAMETERS:
    mc-alias
        The MinIO client alias (as configured by `mc alias set`).
        Example: minio

    bucket
        The bucket name containing the objects.
        Example: drs-objects

    prefix (optional)
        Optional object key prefix (no leading slash required).
        Example: uploads/ or gen3/

OPTIONS:
    --help
        Show this documentation and exit.

EXAMPLE:
    list-indexd-sha256.sh local-postgresql-0 <see default/local-postgresql> |
      list-s3-by-sha256.sh minio drs-objects uploads/

DESCRIPTION:
    This script:
      • reads sha256 hashes from stdin
      • formats each hash into an s3 URL
      • runs `mc ls` for each object to show metadata or an error
HELP
}

# Detect --help anywhere
for arg in "$@"; do
    if [[ "$arg" == "--help" || "$arg" == "-h" ]]; then
        show_help
        exit 0
    fi
done

MC_ALIAS="${1:-}"
BUCKET="${2:-}"
PREFIX="${3:-}"

if [[ -z "$MC_ALIAS" || -z "$BUCKET" ]]; then
    echo "Error: missing required parameters."
    echo "Run with --help for usage."
    exit 1
fi

if [[ -n "$PREFIX" && "$PREFIX" != */ ]]; then
    PREFIX="${PREFIX}/"
fi

if ! command -v mc >/dev/null 2>&1; then
    echo "Error: mc (MinIO client) not found in PATH."
    exit 2
fi


# Read 4 fields per line: separated by space or tab
while IFS=$' \t' read -r did hash file_name resource; do
    # skip empty lines
    if [[ -z "${did:-}" ]]; then
        echo "No DID found, skipping..."
        continue
    fi
    if [[ -z "${hash:-}" ]]; then
        echo "No HASH found, skipping..."
        continue
    fi

    object_key="${PREFIX}${did}/${hash}"
    s3_url="s3://${BUCKET}/${object_key}"

  # capture mc ls output, append file_name on success; on failure print error plus file_name
  if output=$(mc ls  "${MC_ALIAS}/${BUCKET}/${object_key}" 2>/dev/null); then
      printf '%s %s %s %s\n' "$output" "$file_name" "$resource" "$object_key"
  else
      printf 'ERROR: alias %s object %s not found or unreachable %s\n' "$MC_ALIAS" "$object_key" "$file_name"
  fi
done
