#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "$SCRIPT_DIR/common.sh"
load_env
export TUTORIAL_SYFON_ENDPOINT="${TUTORIAL_SYFON_ENDPOINT:-http://localhost:8080}"
export TUTORIAL_SYFON_SCOPE="${TUTORIAL_SYFON_SCOPE:-example/tutorial}"
export TUTORIAL_SYFON_BUCKET="${TUTORIAL_SYFON_BUCKET:-local-bucket}"
export TUTORIAL_SYFON_USERNAME="${TUTORIAL_SYFON_USERNAME:-drs-user}"
export TUTORIAL_SYFON_PASSWORD="${TUTORIAL_SYFON_PASSWORD:-drs-pass}"
require_tools
print_git_drs_version
require_vars TEST_REMOTE WORK_ROOT GLOBUS_DESTINATION_ROOT_PATH \
  GIT_DRS_GLOBUS_DESTINATION_COLLECTION TUTORIAL_SYFON_ENDPOINT \
  TUTORIAL_SYFON_SCOPE

repo="$(new_tutorial_repo)"
work_dir="$(dirname "$repo")"
stage_local="$work_dir/tutorial-source"
stage_globus="${GLOBUS_DESTINATION_ROOT_PATH%/}/$(basename "$work_dir")/tutorial-source"
manifest="$repo/tutorial.tsv"

mkdir -p "$stage_local"
(
  cd "$REPO_ROOT"
  go run ./tests/globus-user-integration-test/cmd/tutorial-manifest \
    --destination-collection "$GIT_DRS_GLOBUS_DESTINATION_COLLECTION" \
    --destination-path "$stage_globus" \
    --local-path "$stage_local" \
    --manifest "$manifest"
)

cd "$repo"
git drs add-url \
  globus://6c54cade-bde5-45c1-bdea-f4bd71dba2cc/home/share/godata/ \
  tutorial --recursive --manifest tutorial.tsv --remote "$TEST_REMOTE"
[[ "$(find tutorial -type f | wc -l | tr -d ' ')" == 3 ]]
git add .gitattributes tutorial
git -c user.name=git-drs-test -c user.email=git-drs-test@example.invalid \
  commit --quiet -m "test: add Globus tutorial pointers"
git drs push "$TEST_REMOTE"
echo "pointer files before hydration:"
while IFS=$'\t' read -r file _ _; do
  [[ "$file" == "path" ]] && continue
  printf '\n--- tutorial/%s ---\n' "$file"
  git show "HEAD:tutorial/$file"
done < tutorial.tsv
echo "DRS records before hydration:"
while IFS=$'\t' read -r file _ checksum; do
  [[ "$file" == "path" ]] && continue
  printf '\n--- tutorial/%s (%s) ---\n' "$file" "$checksum"
  git drs query --remote "$TEST_REMOTE" --checksum --pretty "$checksum"
done < tutorial.tsv
git drs pull "$TEST_REMOTE" --include 'tutorial/**' --access-method globus

while IFS=$'\t' read -r file _ expected; do
  [[ "$file" == "path" ]] && continue
  assert_sha256 "tutorial/$file" "$expected"
done < tutorial.tsv

echo "tutorial collection pointers created and hydrated in $repo"
