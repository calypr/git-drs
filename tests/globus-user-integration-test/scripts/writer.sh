#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "$SCRIPT_DIR/common.sh"
load_env
require_tools
print_git_drs_version
require_vars TEST_REPO_URL TEST_REMOTE SOURCE_COLLECTION_ID WORK_ROOT \
  GLOBUS_DESTINATION_ROOT_PATH GIT_DRS_GLOBUS_DESTINATION_COLLECTION \
  SOURCE_RELEASE_PATH IMPORT_DESTINATION
[[ -f "$TEST_ROOT/fixtures/release.tsv" ]] || { echo "run scripts/prepare-fixtures.sh" >&2; exit 2; }

mode="${1:---dry-run}"
repo="$(new_clone writer)"
cp "$TEST_ROOT/fixtures/release.tsv" "$repo/release.tsv"
cd "$repo"

source_url="globus://${SOURCE_COLLECTION_ID}${SOURCE_RELEASE_PATH%/}/"
git drs add-url "$source_url" "$IMPORT_DESTINATION" \
  --recursive --manifest release.tsv --remote "$TEST_REMOTE" --dry-run
[[ ! -e "$IMPORT_DESTINATION/a.bin" ]] || { echo "dry-run wrote a pointer" >&2; exit 1; }
checkpoint "dry-run passed in $repo"

[[ "$mode" != "--dry-run" ]] || exit 0
[[ "$mode" == "--apply" || "$mode" == "--publish" ]] || { echo "usage: $0 [--dry-run|--apply|--publish]" >&2; exit 2; }
[[ "${ALLOW_WRITER_MUTATION:-no}" == "yes" ]] || { echo "set ALLOW_WRITER_MUTATION=yes to materialize pointers" >&2; exit 2; }

git drs add-url "$source_url" "$IMPORT_DESTINATION" \
  --recursive --manifest release.tsv --remote "$TEST_REMOTE"
[[ "$(find "$IMPORT_DESTINATION" -type f | wc -l | tr -d ' ')" == 3 ]]
grep -F "${IMPORT_DESTINATION}/** filter=drs" .gitattributes >/dev/null
grep -F "${IMPORT_DESTINATION}/** drs=ro" .gitattributes >/dev/null
checkpoint "inspect three pointers, local DRS objects, and .gitattributes in $repo"

[[ "$mode" != "--publish" ]] || {
  [[ "${ALLOW_DRS_PUSH:-no}" == "yes" ]] || { echo "set ALLOW_DRS_PUSH=yes to commit and push" >&2; exit 2; }
  git add -- "$IMPORT_DESTINATION" .gitattributes release.tsv
  git commit -m "test: publish Globus release fixtures"
  git drs push "$TEST_REMOTE"
  checkpoint "publication pushed; preserve it for scripts/reader.sh batch"
}
