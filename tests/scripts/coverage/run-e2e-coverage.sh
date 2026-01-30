#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(git rev-parse --show-toplevel)
COVERAGE_ROOT="${COVERAGE_ROOT:-${ROOT_DIR}/coverage}"
BUILD_DIR="${BUILD_DIR:-${ROOT_DIR}/build/coverage}"
export PATH="${BUILD_DIR}:${PATH}"

INTEGRATION_COV_DIR="${INTEGRATION_COV_DIR:-${COVERAGE_ROOT}/integration/raw}"
INTEGRATION_PROFILE="${INTEGRATION_PROFILE:-${COVERAGE_ROOT}/integration/coverage.out}"
export GOCOVERDIR="${INTEGRATION_COV_DIR}"

mkdir -p "${BUILD_DIR}" "${INTEGRATION_COV_DIR}" "$(dirname "${INTEGRATION_PROFILE}")"
go build "${GOFLAGS[@]}" -cover -covermode=atomic -coverpkg=./... -o "${BUILD_DIR}/git-drs" .


"${E2E_SCRIPT}"

go tool covdata textfmt -i="${INTEGRATION_COV_DIR}" -o "${INTEGRATION_PROFILE}"
echo "Integration coverage profile saved to ${INTEGRATION_PROFILE}"
