#!/usr/bin/env bash
set -euo pipefail
 
ROOT_DIR=$(git rev-parse --show-toplevel)
COVERAGE_ROOT="${COVERAGE_ROOT:-${ROOT_DIR}/coverage}"
INTEGRATION_COV_DIR="${INTEGRATION_COV_DIR:-${COVERAGE_ROOT}/integration/raw}"
INTEGRATION_PROFILE="${INTEGRATION_PROFILE:-${COVERAGE_ROOT}/integration/coverage.out}"
BUILD_DIR="${BUILD_DIR:-${ROOT_DIR}/build/coverage}"
 
E2E_SCRIPT="${ROOT_DIR}/tests/scripts/end-2-end/e2e.sh"
if [[ ! -x "${E2E_SCRIPT}" ]]; then
ALT_E2E_SCRIPT="${ROOT_DIR}/tests/scripts/end-to-end/e2e.sh"
if [[ -x "${ALT_E2E_SCRIPT}" ]]; then
E2E_SCRIPT="${ALT_E2E_SCRIPT}"
else
echo "Unable to find executable e2e.sh at ${E2E_SCRIPT} or ${ALT_E2E_SCRIPT}." >&2
exit 1
fi
fi
 
mkdir -p "${BUILD_DIR}" "${INTEGRATION_COV_DIR}" "$(dirname "${INTEGRATION_PROFILE}")"
 
GOFLAGS=()
if [[ -n "${GOFLAGS_EXTRA:-}" ]]; then
GOFLAGS+=("${GOFLAGS_EXTRA}")
fi
 
go build "${GOFLAGS[@]}" -cover -covermode=atomic -coverpkg=./... -o "${BUILD_DIR}/git-drs" .
 
export PATH="${BUILD_DIR}:${PATH}"
export GOCOVERDIR="${INTEGRATION_COV_DIR}"
 
"${E2E_SCRIPT}"
 
go tool covdata textfmt -i="${INTEGRATION_COV_DIR}" -o "${INTEGRATION_PROFILE}"
 
echo "Integration coverage profile saved to ${INTEGRATION_PROFILE}"
