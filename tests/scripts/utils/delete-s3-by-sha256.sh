#!/usr/bin/env bash
set -euo pipefail

show_help() {
    cat << 'HELP'
delete-s3-by-sha256.sh — format sha256 hashes as s3 URLs and delete via the MinIO client.

USAGE:
    delete-s3-by-sha256.sh <mc-alias> <bucket> [prefix]

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
      delete-s3-by-sha256.sh minio drs-objects uploads/

DESCRIPTION:
    This script:
      • reads sha256 hashes from stdin
      • formats each hash into an s3 URL
      • deletes each object using `mc rm`

HELP
}

# Detect --help anywhere
for arg in "$@"; do
    if [[ "$arg" == "--help" ]]; then
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

    if [[ "${DEBUG:-}" == "1" ]]; then
        echo "$s3_url"
        echo "${MC_ALIAS}/${BUCKET}/${object_key}"
    fi
    # Run mc rm and show output; exit on error
    mc rm --force "${MC_ALIAS}/${BUCKET}/${object_key}" && echo "Deleted: ${resource} ${file_name}" || echo "ERROR: Failed to delete ${MC_ALIAS}/${BUCKET}/${object_key}"
done



