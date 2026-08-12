#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "$SCRIPT_DIR/common.sh"
load_env
require_tools
print_git_drs_version
require_vars TEST_REPO_URL TEST_REMOTE WORK_ROOT GLOBUS_DESTINATION_ROOT_PATH \
  GIT_DRS_GLOBUS_DESTINATION_COLLECTION SOURCE_COLLECTION_ID

case_name="${1:-strict}"
repo="$(new_clone "$case_name")"
cd "$repo"

case "$case_name" in
  strict)
    require_vars GLOBUS_ONLY_PATH GLOBUS_ONLY_SHA256
    git drs pull "$TEST_REMOTE" --include "$GLOBUS_ONLY_PATH" --access-method globus
    assert_sha256 "$GLOBUS_ONLY_PATH" "$GLOBUS_ONLY_SHA256"
    checkpoint "confirm one successful Globus task with the committed source path"
    ;;
  auto)
    require_vars HTTPS_GLOBUS_PATH HTTPS_GLOBUS_SHA256
    git drs pull "$TEST_REMOTE" --include "$HTTPS_GLOBUS_PATH"
    assert_sha256 "$HTTPS_GLOBUS_PATH" "$HTTPS_GLOBUS_SHA256"
    checkpoint "confirm no Globus task was created; auto selected HTTPS"
    ;;
  prefer)
    require_vars HTTPS_GLOBUS_PATH HTTPS_GLOBUS_SHA256
    GIT_DRS_ACCESS_METHOD=prefer:globus git drs pull "$TEST_REMOTE" --include "$HTTPS_GLOBUS_PATH"
    assert_sha256 "$HTTPS_GLOBUS_PATH" "$HTTPS_GLOBUS_SHA256"
    checkpoint "confirm Globus was selected"
    ;;
  precedence)
    require_vars HTTPS_GLOBUS_PATH HTTPS_GLOBUS_SHA256
    git config --local "drs.remote.${TEST_REMOTE}.access-method" prefer:globus
    GIT_DRS_ACCESS_METHOD=prefer:https git drs pull "$TEST_REMOTE" --include "$HTTPS_GLOBUS_PATH"
    assert_sha256 "$HTTPS_GLOBUS_PATH" "$HTTPS_GLOBUS_SHA256"
    cli_repo="$(new_clone precedence-cli)"
    cd "$cli_repo"
    GIT_DRS_ACCESS_METHOD=prefer:https git drs pull "$TEST_REMOTE" \
      --include "$HTTPS_GLOBUS_PATH" --access-method globus
    assert_sha256 "$HTTPS_GLOBUS_PATH" "$HTTPS_GLOBUS_SHA256"
    checkpoint "confirm the first pull used HTTPS and the strict CLI pull used Globus"
    ;;
  batch)
    require_vars RELEASE_A_SHA256 RELEASE_B_SHA256 RELEASE_C_SHA256 IMPORT_DESTINATION
    git drs pull "$TEST_REMOTE" --include "${IMPORT_DESTINATION}/**" --access-method globus
    assert_sha256 "$IMPORT_DESTINATION/a.bin" "$RELEASE_A_SHA256"
    assert_sha256 "$IMPORT_DESTINATION/nested/b.bin" "$RELEASE_B_SHA256"
    assert_sha256 "$IMPORT_DESTINATION/nested/c.bin" "$RELEASE_C_SHA256"
    checkpoint "confirm exactly one task contains the three explicit transfer items"
    ;;
  routing)
    require_vars GLOBUS_ONLY_PATH GLOBUS_ONLY_SHA256
    destination_collection="$GIT_DRS_GLOBUS_DESTINATION_COLLECTION"
    unset GIT_DRS_GLOBUS_DESTINATION_COLLECTION
    git config --local --add "drs.remote.${TEST_REMOTE}.globus-collection" \
      "${SOURCE_COLLECTION_ID}=${destination_collection}"
    git drs pull "$TEST_REMOTE" --include "$GLOBUS_ONLY_PATH" --access-method globus
    assert_sha256 "$GLOBUS_ONLY_PATH" "$GLOBUS_ONLY_SHA256"
    checkpoint "confirm the task used the clone-local source route and repository path"
    ;;
  fallback)
    require_vars HTTPS_GLOBUS_PATH HTTPS_GLOBUS_SHA256
    unset GIT_DRS_GLOBUS_DESTINATION_COLLECTION
    git config --local --unset-all "drs.remote.${TEST_REMOTE}.globus-collection" 2>/dev/null || true
    GIT_DRS_ACCESS_METHOD=prefer:globus git drs pull "$TEST_REMOTE" --include "$HTTPS_GLOBUS_PATH"
    assert_sha256 "$HTTPS_GLOBUS_PATH" "$HTTPS_GLOBUS_SHA256"
    checkpoint "confirm planning fell back to HTTPS and submitted no Globus task"
    ;;
  diagnostics)
    require_vars GLOBUS_ONLY_PATH HTTPS_ONLY_PATH
    unset GIT_DRS_GLOBUS_DESTINATION_COLLECTION
    git config --local --unset-all "drs.remote.${TEST_REMOTE}.globus-collection" 2>/dev/null || true
    expect_failure "$repo/diagnostics.log" git drs pull "$TEST_REMOTE" \
      --include "$GLOBUS_ONLY_PATH" --include "$HTTPS_ONLY_PATH" --access-method globus
    grep -E 'disabled|destination_collection_unmapped' "$repo/diagnostics.log" >/dev/null
    checkpoint "inspect diagnostics.log for both object IDs and confirm it contains no credential"
    ;;
  rejected)
    require_vars GLOBUS_ONLY_PATH
    expect_failure "$repo/rejected.log" env GIT_DRS_GLOBUS_TRANSFER_TOKEN=invalid-test-token \
      git drs pull "$TEST_REMOTE" --include "$GLOBUS_ONLY_PATH" --access-method globus
    grep -i 'auth' "$repo/rejected.log" >/dev/null
    checkpoint "confirm no task was submitted and inspect rejected.log"
    ;;
  broken)
    require_vars BROKEN_GLOBUS_PATH
    expect_failure "$repo/broken.log" env GIT_DRS_ACCESS_METHOD=prefer:globus \
      git drs pull "$TEST_REMOTE" --include "$BROKEN_GLOBUS_PATH"
    checkpoint "confirm the submitted Globus task failed and HTTPS fallback was not attempted"
    ;;
  *)
    echo "usage: $0 {strict|auto|prefer|precedence|batch|routing|fallback|diagnostics|rejected|broken}" >&2
    exit 2
    ;;
esac

echo "case $case_name passed local assertions in $repo"
