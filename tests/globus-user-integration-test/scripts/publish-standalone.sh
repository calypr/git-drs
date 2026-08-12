#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "$SCRIPT_DIR/common.sh"
load_env
require_tools
print_git_drs_version
require_vars TEST_REPO_URL TEST_REMOTE TEST_DRS_ENDPOINT TEST_DRS_PROVIDER \
  TEST_DRS_AUTH TEST_DRS_SCOPE SOURCE_COLLECTION_ID SOURCE_STANDALONE_PATH \
  STANDALONE_HTTPS_BASE_URL WORK_ROOT GLOBUS_DESTINATION_ROOT_PATH \
  GIT_DRS_GLOBUS_DESTINATION_COLLECTION

mode="${1:---apply}"
[[ "$mode" == "--apply" || "$mode" == "--publish" ]] || {
  echo "usage: $0 [--apply|--publish]" >&2
  exit 2
}
[[ "${ALLOW_WRITER_MUTATION:-no}" == "yes" ]] || {
  echo "set ALLOW_WRITER_MUTATION=yes to create standalone fixture pointers" >&2
  exit 2
}
if [[ "${TEST_DRS_CREDENTIAL:-}" == profile:* && "$TEST_DRS_AUTH" != "provider-helper:gen3-profile" ]]; then
  echo "profile credentials require TEST_DRS_AUTH=provider-helper:gen3-profile" >&2
  exit 2
fi

repo="$(new_clone standalone-publisher no)"
remote_args=(
  "$TEST_REMOTE" "$TEST_DRS_ENDPOINT"
  --provider "$TEST_DRS_PROVIDER"
  --auth "$TEST_DRS_AUTH"
  --scope "$TEST_DRS_SCOPE"
)
[[ -z "${TEST_DRS_CREDENTIAL:-}" ]] || remote_args+=(--credential "$TEST_DRS_CREDENTIAL")
[[ -z "${TEST_DRS_STORAGE:-}" ]] || remote_args+=(--storage "$TEST_DRS_STORAGE")
git -C "$repo" drs remote add "${remote_args[@]}"
configure_globus_destination "$repo"
(
  cd "$REPO_ROOT"
  go run ./tests/globus-user-integration-test/cmd/materialize-standalone \
    --repo "$repo" \
    --fixtures "$TEST_ROOT/fixtures/standalone" \
    --source-collection "$SOURCE_COLLECTION_ID" \
    --source-path "$SOURCE_STANDALONE_PATH" \
    --https-base "$STANDALONE_HTTPS_BASE_URL"
)

cd "$repo"
[[ "$(find . -maxdepth 1 -name '*.bin' -type f | wc -l | tr -d ' ')" == 4 ]]
git drs ls-files --long
checkpoint "inspect four pointers, checksums, and access methods in $repo"

if [[ "$mode" == "--publish" ]]; then
  [[ "${ALLOW_DRS_PUSH:-no}" == "yes" ]] || {
    echo "set ALLOW_DRS_PUSH=yes to commit and push" >&2
    exit 2
  }
  git add -- globus-only.bin https-globus.bin https-only.bin broken-globus.bin .gitattributes
  git commit -m "test: publish standalone Globus fixtures"
  git drs push "$TEST_REMOTE"
  checkpoint "standalone fixture publication pushed"
fi

echo "standalone fixtures prepared in $repo"
