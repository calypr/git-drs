#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
GIT_DRS_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"

# Optional env-file loading (default: <git-drs-root>/.env)
ENV_FILE="${ENV_FILE:-$GIT_DRS_ROOT/.env}"
if [[ -f "$ENV_FILE" ]]; then
  # shellcheck disable=SC1090
  set -a
  source "$ENV_FILE"
  set +a
fi

# Optional positional remote name override for compatibility:
#   bash tests/e2e-local-full.sh origin
if [[ $# -gt 1 ]]; then
  echo "error: accepts at most 1 argument (remote name), received $#" >&2
  echo "usage: bash tests/e2e-local-full.sh [remote-name]" >&2
  exit 1
fi
if [[ $# -eq 1 ]]; then
  export TEST_REMOTE_NAME="$1"
fi

# Compatibility wrapper:
# forwards legacy local E2E env var names into the unified remote/local script.
# Note: TEST_GITHUB_MODE=true is supported here (localhost DRS + GitHub git remote).

export TEST_SERVER_MODE=local
export TEST_DRS_URL="${TEST_DRS_URL:-${DRS_URL:-http://localhost:8080}}"
export TEST_REMOTE_NAME="${TEST_REMOTE_NAME:-origin}"
export TEST_REPO_NAME="${TEST_REPO_NAME:-${REPO_NAME:-git-drs-e2e-test}}"
export TEST_WORK_ROOT="${TEST_WORK_ROOT:-${WORK_ROOT:-$(mktemp -d -t git-drs-e2e-local-XXXX)}}"
export TEST_REMOTE_URL="${TEST_REMOTE_URL:-${REMOTE_URL:-$TEST_WORK_ROOT/${TEST_REPO_NAME}.git}}"
export TEST_KEEP_WORKDIR="${TEST_KEEP_WORKDIR:-${KEEP_WORKDIR:-false}}"
export TEST_ORGANIZATION="${TEST_ORGANIZATION:-${ORGANIZATION:-cbdsTest}}"
export TEST_PROJECT_ID="${TEST_PROJECT_ID:-${PROJECT_ID:-${PROJECT:-git_drs_e2e_test}}}"
export TEST_BUCKET="${TEST_BUCKET:-${BUCKET:-cbds}}"
export TEST_MULTIPART_THRESHOLD_MB="${TEST_MULTIPART_THRESHOLD_MB:-${MULTIPART_THRESHOLD_MB:-5}}"
export TEST_UPLOAD_MULTIPART_THRESHOLD_MB="${TEST_UPLOAD_MULTIPART_THRESHOLD_MB:-${UPLOAD_MULTIPART_THRESHOLD_MB:-$TEST_MULTIPART_THRESHOLD_MB}}"
export TEST_DOWNLOAD_MULTIPART_THRESHOLD_MB="${TEST_DOWNLOAD_MULTIPART_THRESHOLD_MB:-${DOWNLOAD_MULTIPART_THRESHOLD_MB:-$TEST_MULTIPART_THRESHOLD_MB}}"
export TEST_LARGE_FILE_MB="${TEST_LARGE_FILE_MB:-${LARGE_FILE_MB:-12}}"
export TEST_EXTRA_SMALL_FILES="${TEST_EXTRA_SMALL_FILES:-${EXTRA_SMALL_FILES:-3}}"
export TEST_EXTRA_SMALL_FILE_KB="${TEST_EXTRA_SMALL_FILE_KB:-${EXTRA_SMALL_FILE_KB:-256}}"
export TEST_EXTRA_LARGE_FILES="${TEST_EXTRA_LARGE_FILES:-${EXTRA_LARGE_FILES:-1}}"
export TEST_EXTRA_LARGE_FILE_MB="${TEST_EXTRA_LARGE_FILE_MB:-${EXTRA_LARGE_FILE_MB:-8}}"
export TEST_PUSH_MODE="${TEST_PUSH_MODE:-${PUSH_MODE:-drs}}"
export TEST_CREATE_BUCKET_BEFORE_TEST="${TEST_CREATE_BUCKET_BEFORE_TEST:-${CREATE_BUCKET_BEFORE_TEST:-false}}"
export TEST_BUCKET_NAME="${TEST_BUCKET_NAME:-$TEST_BUCKET}"
export TEST_BUCKET_REGION="${TEST_BUCKET_REGION:-us-east-1}"
export TEST_BUCKET_ACCESS_KEY="${TEST_BUCKET_ACCESS_KEY:-}"
export TEST_BUCKET_SECRET_KEY="${TEST_BUCKET_SECRET_KEY:-}"
export TEST_BUCKET_ENDPOINT="${TEST_BUCKET_ENDPOINT:-}"
export TEST_DELETE_BUCKET_AFTER="${TEST_DELETE_BUCKET_AFTER:-${DELETE_TEST_BUCKET_AFTER:-false}}"
export TEST_ADMIN_AUTH_HEADER="${TEST_ADMIN_AUTH_HEADER:-${ADMIN_AUTH_HEADER:-}}"
export TEST_LOCAL_USERNAME="${TEST_LOCAL_USERNAME:-${LOCAL_USERNAME:-${DRS_BASIC_AUTH_USER:-}}}"
export TEST_LOCAL_PASSWORD="${TEST_LOCAL_PASSWORD:-${LOCAL_PASSWORD:-${DRS_BASIC_AUTH_PASSWORD:-}}}"
export TEST_FULL_SERVER_SWEEP="${TEST_FULL_SERVER_SWEEP:-${FULL_SERVER_SWEEP:-true}}"
export TEST_CLEANUP_RECORDS="${TEST_CLEANUP_RECORDS:-${CLEANUP_RECORDS:-true}}"
export TEST_STRICT_CLEANUP="${TEST_STRICT_CLEANUP:-${STRICT_CLEANUP:-true}}"
export TEST_LFS_PULL_COMPAT="${TEST_LFS_PULL_COMPAT:-true}"

exec bash "$SCRIPT_DIR/e2e-gen3-remote-full.sh" "$@"
