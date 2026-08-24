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
require_vars TEST_REMOTE WORK_ROOT GLOBUS_DESTINATION_ROOT_PATH \
  GIT_DRS_GLOBUS_DESTINATION_COLLECTION TUTORIAL_SYFON_ENDPOINT \
  TUTORIAL_SYFON_SCOPE

test_bin_dir="$(mktemp -d)"
trap 'rm -rf "$test_bin_dir"' EXIT
(
  cd "$REPO_ROOT"
  go build -o "$test_bin_dir/git-drs" .
)
export PATH="$test_bin_dir:$PATH"
print_git_drs_version

repo="$(new_tutorial_repo)"
cd "$repo"
verification_file="$test_bin_dir/verification.tsv"
git drs add-url \
  'globus://6c54cade-bde5-45c1-bdea-f4bd71dba2cc/home/share/godata/file*.txt' \
  tutorial --remote "$TEST_REMOTE"
tutorial_files="$(find tutorial -type f | sort)"
expected_files="$(printf '%s\n' tutorial/file1.txt tutorial/file2.txt tutorial/file3.txt)"
[[ "$tutorial_files" == "$expected_files" ]] || {
  printf 'wildcard selected unexpected files:\n%s\n' "$tutorial_files" >&2
  exit 1
}
printf 'PASS wildcard selected the three tutorial files\n'
echo "local DRS records after add-url:"
while IFS= read -r file; do
  temporary_oid="$(awk '/^oid sha256:/{sub(/^oid sha256:/, ""); print; exit}' "$file")"
  record_path=".git/drs/lfs/objects/${temporary_oid:0:2}/${temporary_oid:2:2}/$temporary_oid"
  printf '\n--- %s (%s) ---\n' "$file" "$temporary_oid"
  jq . "$record_path"
done < <(find tutorial -type f | sort)
git add .gitattributes tutorial
git -c user.name=git-drs-test -c user.email=git-drs-test@example.invalid \
  commit --quiet -m "test: add Globus tutorial pointers"
git drs push "$TEST_REMOTE"
echo "pointer files before hydration:"
while IFS= read -r file; do
  pointer="$(git show "HEAD:$file")"
  printf '\n--- %s ---\n' "$file"
  printf '%s\n' "$pointer"
  temporary_oid="$(printf '%s\n' "$pointer" | awk '/^oid sha256:/{sub(/^oid sha256:/, ""); print; exit}')"
  placeholder_oid="$(printf '%s\n' "$pointer" | awk '/^ext-0-gitdrsplaceholder sha256:/{sub(/^ext-0-gitdrsplaceholder sha256:/, ""); print; exit}')"
  [[ "$temporary_oid" =~ ^[[:xdigit:]]{64}$ && "$placeholder_oid" == "$temporary_oid" ]] || {
    echo "invalid temporary oid in $file" >&2
    exit 1
  }
  object_json="$(git drs query --remote "$TEST_REMOTE" --checksum "$temporary_oid")"
  printf '\nSyfon DRS record after push:\n'
  printf '%s\n' "$object_json" | jq .
  printf '%s\n' "$object_json" | jq -e -s --arg oid "$temporary_oid" \
    'length == 1 and any(.[0].checksums[]?; .type == "git-drs-placeholder" and .checksum == $oid)' >/dev/null || {
      echo "Syfon did not save temporary oid $temporary_oid for $file" >&2
      exit 1
    }
  did="$(printf '%s\n' "$object_json" | jq -er -s 'if length == 1 then .[0].id else empty end')"
  globus_url="$(printf '%s\n' "$object_json" | jq -er -s 'if length == 1 then (.[0].access_methods[]? | select(.type == "globus") | .access_url.url) else empty end')"
  expected_url="globus://6c54cade-bde5-45c1-bdea-f4bd71dba2cc/home/share/godata/${file#tutorial/}"
  [[ "$globus_url" == "$expected_url" ]] || {
    echo "Syfon changed Globus URL for $file: $globus_url" >&2
    exit 1
  }
  printf 'PASS 1 temporary oid saved to Syfon: %s oid=%s\n' "$file" "$temporary_oid"
  printf 'PASS 3 Syfon kept unsigned Globus URL: %s url=%s\n' "$file" "$globus_url"
  printf '%s\t%s\t%s\n' "$file" "$temporary_oid" "$did" >>"$verification_file"
done < <(find tutorial -type f | sort)
git drs pull "$TEST_REMOTE" --include 'tutorial/**' --access-method globus

echo "hydrated file checksums and Syfon verification:"
while IFS=$'\t' read -r file temporary_oid did; do
  grep -q '^version https://git-lfs.github.com/spec/v1$' "$file" && {
    echo "$file was not hydrated" >&2
    exit 1
  }
  real_oid="$(sha256_file "$file")"
  [[ "$real_oid" != "$temporary_oid" ]] || {
    echo "$file still uses its temporary oid after download" >&2
    exit 1
  }
  object_json="$(git drs query --remote "$TEST_REMOTE" --checksum "$real_oid")"
  printf '\n--- Syfon DRS record after download: %s (%s) ---\n' "$file" "$real_oid"
  printf '%s\n' "$object_json" | jq .
  printf '%s\n' "$object_json" | jq -e -s --arg did "$did" --arg oid "$real_oid" \
    'any(.[]; .id == $did and any(.checksums[]?; .type == "sha256" and .checksum == $oid))' >/dev/null || {
      echo "Syfon did not save real sha256 $real_oid for $file" >&2
      exit 1
    }
  printf '%s  %s\n' "$real_oid" "$file"
  printf 'PASS 2 downloaded sha256 saved to Syfon: %s sha256=%s\n' "$file" "$real_oid"
done <"$verification_file"

echo "tutorial collection pointers created and hydrated in $repo"
