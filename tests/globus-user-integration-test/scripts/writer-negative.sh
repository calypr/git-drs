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
  SOURCE_RELEASE_PATH

repo="$(new_clone writer-negative)"
cd "$repo"
source_url="globus://${SOURCE_COLLECTION_ID}${SOURCE_RELEASE_PATH%/}/"
sha="$(sha256_file "$TEST_ROOT/fixtures/release/a.bin")"

run_bad_manifest() {
  local name="$1" body="$2" destination="data/rejected-$1"
  printf 'path\tsize\tsha256\n%b' "$body" >"$name.tsv"
  expect_failure "$name.log" git drs add-url "$source_url" "$destination" \
    --recursive --manifest "$name.tsv" --remote "$TEST_REMOTE"
  [[ ! -e "$destination" ]] || { echo "$name created repository state" >&2; exit 1; }
  ! grep -F "$destination/**" .gitattributes 2>/dev/null
}

run_bad_manifest traversal "../outside.bin\t5\t${sha}\n"
run_bad_manifest malformed-sha "a.bin\t5\tnot-a-sha256\n"
run_bad_manifest duplicate-path "a.bin\t5\t${sha}\na.bin\t5\t${sha}\n"

echo "unsafe manifest cases passed in $repo"
