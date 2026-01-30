#!/usr/bin/env bash
set -euo pipefail
 
ROOT_DIR=$(git rev-parse --show-toplevel)
COVERAGE_ROOT="${COVERAGE_ROOT:-${ROOT_DIR}/coverage}"
 
UNIT_COV_DIR="${1:-${UNIT_COV_DIR:-${COVERAGE_ROOT}/unit/raw}}"
INTEGRATION_COV_DIR="${2:-${INTEGRATION_COV_DIR:-${COVERAGE_ROOT}/integration/raw}}"
MERGED_COV_DIR="${3:-${MERGED_COV_DIR:-${COVERAGE_ROOT}/merged/raw}}"
COMBINED_PROFILE="${4:-${COMBINED_PROFILE:-${COVERAGE_ROOT}/combined.out}}"
 
if [[ ! -d "${UNIT_COV_DIR}" ]]; then
echo "Unit coverage directory not found: ${UNIT_COV_DIR}" >&2
echo "Run unit tests with GOCOVERDIR set, e.g.:" >&2
echo "  GOCOVERDIR=${COVERAGE_ROOT}/unit/raw go test -cover ./..." >&2
exit 1
fi
 
if [[ ! -d "${INTEGRATION_COV_DIR}" ]]; then
echo "Integration coverage directory not found: ${INTEGRATION_COV_DIR}" >&2
exit 1
fi
 
mkdir -p "${MERGED_COV_DIR}" "$(dirname "${COMBINED_PROFILE}")"
 
rm -rf "${MERGED_COV_DIR:?}"/*
 
go tool covdata merge -i="${UNIT_COV_DIR},${INTEGRATION_COV_DIR}" -o "${MERGED_COV_DIR}"
go tool covdata textfmt -i="${MERGED_COV_DIR}" -o "${COMBINED_PROFILE}"
 
echo "Combined coverage profile saved to ${COMBINED_PROFILE}"
