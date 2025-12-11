#!/usr/bin/env bash

set -euo pipefail

show_help() {
    cat << 'EOF'
cleanup_resource.sh — Delete all Indexd records associated with a given resource path.

USAGE:
    cleanup_resource.sh <pod-name> <postgres-password> [resource-name]

PARAMETERS:
    pod-name
        The name of the Kubernetes pod running Postgres.
        Example: local-postgresql-0

    postgres-password
        The password for the Postgres user inside the pod.
        Example: <see default/local-postgresql>

    resource-name (optional)
        The Indexd resource path to delete.
        Defaults to: /programs/cbds/projects/monorepos

OPTIONS:
    --help
        Show this documentation and exit.

EXAMPLE:
    cleanup_resource.sh local-postgresql-0 <see default/local-postgresql>
    cleanup_resource.sh local-postgresql-0 <see default/local-postgresql> "/programs/myproj/resource"

DESCRIPTION:
    This script:
      • connects to a Postgres pod via kubectl exec
      • runs SQL that deletes all Indexd records associated with the resource
      • cleans up metadata, hash, url, authz, and main index_record rows
      • uses dynamic SQL so the resource path is customizable

EOF
}

# Detect --help anywhere
for arg in "$@"; do
    if [[ "$arg" == "--help" ]]; then
        show_help
        exit 0
    fi
done

# Parameters
POD_NAME="${1:-}"
POSTGRES_PASSWORD="${2:-}"
RESOURCE_NAME="${3:-/programs/cbds/projects/monorepos}"
DATABASE_NAME="indexd_local"

if [[ -z "$POD_NAME" || -z "$POSTGRES_PASSWORD" ]]; then
    echo "Error: missing required parameters."
    echo "Run with --help for usage."
    exit 1
fi

echo "🔧 Cleaning Indexd records for resource: $RESOURCE_NAME"

# SQL script with dynamic resource name (variables will be expanded)
SQL=$(cat <<EOF
-- Delete all records associated with the resource $RESOURCE_NAME

-- Create a temporary table to hold DIDs for the resource
SELECT did INTO TEMP TABLE resource_did FROM index_record_authz WHERE resource = '$RESOURCE_NAME';

SELECT format('🗂 Found %s records to delete for resource: %s', COUNT(*), '$RESOURCE_NAME') FROM resource_did;

-- Delete related records from metadata, hash, and url tables
DELETE FROM index_record_metadata WHERE did IN (SELECT did FROM resource_did);
DELETE FROM index_record_hash     WHERE did IN (SELECT did FROM resource_did);
DELETE FROM index_record_url      WHERE did IN (SELECT did FROM resource_did);

-- Delete authz records for the resource
DELETE FROM index_record_authz WHERE resource = '$RESOURCE_NAME';

-- Finally, delete the main index records
DELETE FROM index_record WHERE did IN (SELECT did FROM resource_did);

-- Drop the temporary table
DROP TABLE IF EXISTS resource_did;
EOF
)

echo "📝 Generated SQL for cleanup:"

echo "🚀 Executing cleanup SQL in pod: $POD_NAME"


# Execute SQL inside the pod
# Bash - replace the psql invocation in `tests/monorepos/clean-indexd.sh`
# Pipe the SQL into the pod and run psql reading from stdin
printf '%s\n' "$SQL" | kubectl exec -i "$POD_NAME" -- \
    env PGPASSWORD="$POSTGRES_PASSWORD" \
    psql -U postgres -d "$DATABASE_NAME" -v ON_ERROR_STOP=1 -f -

echo "✔ Cleanup complete for resource: $RESOURCE_NAME"
