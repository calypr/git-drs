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

new_tutorial_repo() {
  local work_dir repo_path
  mkdir -p "$WORK_ROOT"
  work_dir="$(mktemp -d "${WORK_ROOT%/}/git-drs-globus.tutorial.XXXXXX")"
  repo_path="$work_dir/repo"
  git init --quiet --initial-branch=main "$repo_path"
  git init --bare --quiet "$work_dir/git-origin.git"
  git -C "$repo_path" remote add "$TEST_REMOTE" "$work_dir/git-origin.git"
  configure_local_syfon_remote "$repo_path"
  configure_globus_destination "$repo_path"
  printf '%s\n' "$repo_path"
}

configure_local_syfon_remote() {
  local repo_path="$1" organization project
  require_vars TEST_REMOTE TUTORIAL_SYFON_ENDPOINT TUTORIAL_SYFON_SCOPE TUTORIAL_SYFON_BUCKET
  [[ "$TUTORIAL_SYFON_SCOPE" == */* ]] || { echo "TUTORIAL_SYFON_SCOPE must be organization/project" >&2; exit 2; }
  organization="${TUTORIAL_SYFON_SCOPE%%/*}"
  project="${TUTORIAL_SYFON_SCOPE#*/}"
  git -C "$repo_path" drs init
  git -C "$repo_path" config --local drs.default-remote "$TEST_REMOTE"
  git -C "$repo_path" config --local "drs.remote.$TEST_REMOTE.type" local
  git -C "$repo_path" config --local "drs.remote.$TEST_REMOTE.endpoint" "$TUTORIAL_SYFON_ENDPOINT"
  git -C "$repo_path" config --local "drs.remote.$TEST_REMOTE.organization" "$organization"
  git -C "$repo_path" config --local "drs.remote.$TEST_REMOTE.project" "$project"
  git -C "$repo_path" config --local "drs.remote.$TEST_REMOTE.bucket" "$TUTORIAL_SYFON_BUCKET"
  git -C "$repo_path" config --local "remote.$TEST_REMOTE.lfsurl" "${TUTORIAL_SYFON_ENDPOINT%/}/info/lfs"
  [[ -z "${TUTORIAL_SYFON_USERNAME:-}" ]] || git -C "$repo_path" config --local "drs.remote.$TEST_REMOTE.username" "$TUTORIAL_SYFON_USERNAME"
  [[ -z "${TUTORIAL_SYFON_PASSWORD:-}" ]] || git -C "$repo_path" config --local "drs.remote.$TEST_REMOTE.password" "$TUTORIAL_SYFON_PASSWORD"
}

configure_globus_destination() {
  local repo_path="$1" work_dir collection_path
  work_dir="$(dirname "$repo_path")"
  collection_path="${GLOBUS_DESTINATION_ROOT_PATH%/}/$(basename "$work_dir")/repo"
  git -C "$repo_path" config --local --add \
    "drs.remote.${TEST_REMOTE}.globus-destination-path" \
    "${GIT_DRS_GLOBUS_DESTINATION_COLLECTION}=${collection_path}"
}
