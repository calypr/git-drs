#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_RESOURCE="/programs/cbds/projects/monorepos"

if [[ $# -eq 2 ]]; then
    exec "$SCRIPT_DIR/../scripts/utils/clean-drs-records.sh" "$@" "$DEFAULT_RESOURCE"
fi

exec "$SCRIPT_DIR/../scripts/utils/clean-drs-records.sh" "$@"
