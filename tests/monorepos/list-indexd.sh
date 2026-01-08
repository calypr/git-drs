#!/usr/bin/env bash

set -euo pipefail

show_help() {
    cat << 'EOF'
list-indexd.sh — list all Indexd records associated with a given resource path.

USAGE:
    list-indexd.sh <pod-name> <postgres-password> [resource-name]

PARAMETERS:
    pod-name
        The name of the Kubernetes pod running Postgres.
        Example: local-postgresql-0

    postgres-password
        The password for the Postgres user inside the pod.
        Example: <see default/local-postgresql>

    resource-name (optional)
        The Indexd resource path to list.
        Defaults to: /programs/cbds/projects/monorepos

OPTIONS:
    --help
        Show this documentation and exit.

EXAMPLE:
    list-indexd.sh local-postgresql-0 <see default/local-postgresql>
    list-indexd.sh local-postgresql-0 <see default/local-postgresql> "/programs/myproj/resource"

DESCRIPTION:
    This script:
      • connects to a Postgres pod via kubectl exec
      • runs SQL that lists all Indexd records associated with the resource

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

# echo "🔧 listing Indexd records for resource: $RESOURCE_NAME"

# SQL script with dynamic resource name (variables will be expanded)
SQL=$(cat <<EOF
-- List all records associated with the resource $RESOURCE_NAME
-- don't show totals or headers
\pset footer off
\pset tuples_only on
-- Select the relevant DIDs
SELECT did FROM index_record_authz WHERE resource = '$RESOURCE_NAME';
EOF
)

# echo "📝 Generated SQL for listing:"

# echo "🚀 Executing listing SQL in pod: $POD_NAME"


# Execute SQL inside the pod
# Bash - replace the psql invocation in `tests/monorepos/clean-indexd.sh`
# Pipe the SQL into the pod and run psql reading from stdin
printf '%s\n' "$SQL" | kubectl exec -i "$POD_NAME" -- \
    env PGPASSWORD="$POSTGRES_PASSWORD" \
    psql -U postgres -d "$DATABASE_NAME" -v ON_ERROR_STOP=1 -f -

# echo "✔ Listing complete for resource: $RESOURCE_NAME"
