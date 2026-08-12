#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "$SCRIPT_DIR/common.sh"
load_env
require_tools
print_git_drs_version
require_vars TEST_REPO_URL TEST_REMOTE SOURCE_COLLECTION_ID \
  GIT_DRS_GLOBUS_CLIENT_ID GIT_DRS_GLOBUS_DESTINATION_COLLECTION \
  WORK_ROOT GLOBUS_DESTINATION_ROOT_PATH

[[ "$WORK_ROOT" = /* ]] || { echo "WORK_ROOT must be absolute" >&2; exit 2; }
[[ "$GLOBUS_DESTINATION_ROOT_PATH" = /* ]] || { echo "GLOBUS_DESTINATION_ROOT_PATH must be collection-absolute" >&2; exit 2; }
[[ -f "$TEST_ROOT/fixtures/release.tsv" ]] || { echo "run scripts/prepare-fixtures.sh" >&2; exit 2; }
git ls-remote "$TEST_REPO_URL" HEAD >/dev/null
git drs auth globus status
echo "preflight passed; globus-cli was not used"
