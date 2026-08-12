#!/usr/bin/env bash
set -euo pipefail

TEST_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd "$TEST_ROOT/../.." && pwd)"

load_env() {
  if [[ ! -f "$TEST_ROOT/.env" ]]; then
    echo "copy $TEST_ROOT/.env.example to $TEST_ROOT/.env and configure it" >&2
    exit 2
  fi
  set -a
  # shellcheck disable=SC1091
  source "$TEST_ROOT/.env"
  if [[ -f "$TEST_ROOT/fixtures/checksums.env" ]]; then
    # shellcheck disable=SC1091
    source "$TEST_ROOT/fixtures/checksums.env"
  fi
  set +a
}

require_vars() {
  local name
  for name in "$@"; do
    if [[ -z "${!name:-}" ]]; then
      echo "required setting $name is empty" >&2
      exit 2
    fi
  done
}

require_tools() {
  local tool
  for tool in git mktemp; do
    command -v "$tool" >/dev/null || { echo "$tool is required" >&2; exit 2; }
  done
  git drs version >/dev/null
  git lfs version >/dev/null
}

print_git_drs_version() {
  printf 'git-drs under test: '
  git drs version
}

sha256_file() {
  if command -v sha256sum >/dev/null; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

assert_sha256() {
  local actual
  actual="$(sha256_file "$1")"
  [[ "$actual" == "$2" ]] || { echo "SHA-256 mismatch for $1: $actual != $2" >&2; exit 1; }
}

new_clone() {
  local case_name="$1" configure_destination="${2:-yes}" work_dir repo_path
  mkdir -p "$WORK_ROOT"
  work_dir="$(mktemp -d "${WORK_ROOT%/}/git-drs-globus.${case_name}.XXXXXX")"
  repo_path="$work_dir/repo"
  git clone --quiet "$TEST_REPO_URL" "$repo_path"
  git -C "$repo_path" config --local drs.default-remote "$TEST_REMOTE"
  [[ "$configure_destination" != "yes" ]] || configure_globus_destination "$repo_path"
  printf '%s\n' "$repo_path"
}

configure_globus_destination() {
  local repo_path="$1" work_dir collection_path
  work_dir="$(dirname "$repo_path")"
  collection_path="${GLOBUS_DESTINATION_ROOT_PATH%/}/$(basename "$work_dir")/repo"
  git -C "$repo_path" config --local --add \
    "drs.remote.${TEST_REMOTE}.globus-destination-path" \
    "${GIT_DRS_GLOBUS_DESTINATION_COLLECTION}=${collection_path}"
}

expect_failure() {
  local output_file="$1"
  shift
  if "$@" >"$output_file" 2>&1; then
    echo "command unexpectedly succeeded: $*" >&2
    exit 1
  fi
}

checkpoint() {
  printf '\nCHECKPOINT: %s\n' "$*"
}
