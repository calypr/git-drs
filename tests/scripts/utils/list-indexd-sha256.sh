#!/usr/bin/env bash
set -euo pipefail

show_help() {
    cat << 'HELP'
list-indexd-sha256.sh — list sha256 hashes for Indexd records associated with a given resource path.

USAGE:
    list-indexd-sha256.sh <pod-name> <postgres-password> [resource-name]

PARAMETERS:
    pod-name
        The name of the Kubernetes pod running Postgres.
        Example: local-postgresql-0

    postgres-password
        The password for the Postgres user inside the pod.
        Example: <see default/local-postgresql>

    resource-name (optional)
        The Indexd resource path to list.
        Example: /programs/cbds/projects/monorepos

OPTIONS:
    --help
        Show this documentation and exit.

EXAMPLE:
    list-indexd-sha256.sh local-postgresql-0 <see default/local-postgresql>
    list-indexd-sha256.sh local-postgresql-0 <see default/local-postgresql> "/programs/myproj/resource"

DESCRIPTION:
    This script:
      • connects to a Postgres pod via kubectl exec
      • runs SQL that lists all sha256 hashes for Indexd records associated with the resource

HELP
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
RESOURCE_NAME="${3:-}"
DATABASE_NAME="indexd_local"

if [[ -z "$POD_NAME" || -z "$POSTGRES_PASSWORD" || -z "$RESOURCE_NAME" ]]; then
    echo "Error: missing required parameters."
    echo "Run with --help for usage."
    exit 1
fi

# SQL script using psql variable substitution (do not expand $RESOURCE_NAME here)
SQL=$(cat <<'SQL_EOF'
-- List all sha256 hashes associated with the resource (use psql variable substitution)
-- dont show totals or headers
\pset footer off
\pset tuples_only on
-- output as unaligned with no field separator
\pset format unaligned
\pset fieldsep ' '
-- Select sha256 hashes for the relevant DIDs; :'resource_name' is substituted as a SQL string literal
SELECT h.did, h.hash_value, r.file_name, a.resource
  FROM index_record_hash AS h
  JOIN index_record_authz AS a
    ON a.did = h.did
  JOIN index_record AS r
    ON r.did = h.did
 WHERE a.resource = :'resource_name'
   AND h.hash_type = 'sha256';
SQL_EOF
)

# Execute SQL inside the pod, passing resource_name via -v so psql does safe quoting
printf '%s\n' "$SQL" | kubectl exec -c postgresql -i "$POD_NAME" -- \
    env PGPASSWORD="$POSTGRES_PASSWORD" \
    psql -U postgres -d "$DATABASE_NAME" -q -v resource_name="$RESOURCE_NAME" -v ON_ERROR_STOP=1 -f -
