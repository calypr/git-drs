#!/usr/bin/env bash
set -euo pipefail
# set -x  # Uncomment for verbose command tracing

# Local end-to-end test for git-drs + drs-server local mode.
# Covers:
# - single-part upload/download
# - multipart upload/download (forced via low multipart thresholds)
#
# Requirements:
# - git, git-lfs, git-drs in PATH
# - drs-server running and reachable (default: http://localhost:8080)
# - drs-server configured with at least one writable S3 bucket credential

# Defaults aligned with legacy local e2e script values.
REPO_NAME="${REPO_NAME:-git-drs-e2e-test}"
GIT_USER="${GIT_USER:-cbds}"
DRS_URL="${DRS_URL:-http://localhost:8080}"
BUCKET="${BUCKET:-cbds}"
ORGANIZATION="${ORGANIZATION:-cbdsTest}"
PROJECT="${PROJECT:-git_drs_e2e_test}"
WORK_ROOT="${WORK_ROOT:-$(mktemp -d -t git-drs-e2e-local-XXXX)}"
REMOTE_URL="${REMOTE_URL:-$WORK_ROOT/${REPO_NAME}.git}"
KEEP_WORKDIR="${KEEP_WORKDIR:-false}"
MULTIPART_THRESHOLD_MB="${MULTIPART_THRESHOLD_MB:-5}"
UPLOAD_MULTIPART_THRESHOLD_MB="${UPLOAD_MULTIPART_THRESHOLD_MB:-$MULTIPART_THRESHOLD_MB}"
DOWNLOAD_MULTIPART_THRESHOLD_MB="${DOWNLOAD_MULTIPART_THRESHOLD_MB:-$MULTIPART_THRESHOLD_MB}"
LARGE_FILE_MB="${LARGE_FILE_MB:-12}"
PUSH_MODE="${PUSH_MODE:-force}" # force|normal

SOURCE_REPO="$WORK_ROOT/$REPO_NAME"
CLONE_REPO="$WORK_ROOT/${REPO_NAME}-clone"

log() {
  printf '[e2e-local] %s\n' "$*"
}

cleanup() {
  if [[ "$KEEP_WORKDIR" == "true" ]]; then
    log "Keeping working directory: $WORK_ROOT"
    return
  fi
  rm -rf "$WORK_ROOT"
}
trap cleanup EXIT

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: required command not found: $1" >&2
    exit 1
  fi
}

sha256_file() {
  local path="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$path" | awk '{print $1}'
  else
    shasum -a 256 "$path" | awk '{print $1}'
  fi
}

assert_eq() {
  local expected="$1"
  local actual="$2"
  local message="$3"
  if [[ "$expected" != "$actual" ]]; then
    echo "assertion failed: $message" >&2
    echo "expected: $expected" >&2
    echo "actual:   $actual" >&2
    exit 1
  fi
}

main() {
  require_cmd git
  require_cmd git-lfs
  require_cmd git-drs
  require_cmd curl

  log "Checking drs-server health at $DRS_URL/healthz"
  curl -fsS "$DRS_URL/healthz" >/dev/null

  log "Using REMOTE_URL=$REMOTE_URL"
  log "Working directory: $WORK_ROOT"
  mkdir -p "$SOURCE_REPO" "$CLONE_REPO"
  if [[ "$REMOTE_URL" != git@* && "$REMOTE_URL" != http* ]]; then
    log "Initializing local bare remote at $REMOTE_URL"
    rm -rf "$REMOTE_URL"
    git init --bare "$REMOTE_URL" >/dev/null
    git --git-dir="$REMOTE_URL" symbolic-ref HEAD refs/heads/main >/dev/null
  fi

  log "Initializing source repository"
  cd "$SOURCE_REPO"
  git init -b main >/dev/null
  git config user.email "git-drs-e2e@example.local"
  git config user.name "git-drs-e2e"
  git remote add origin "$REMOTE_URL"

  log "Setting up git-drs"
  git drs init
  git config --local lfs.basictransfersonly true
  git drs remote add local origin "$DRS_URL" --bucket "$BUCKET" --organization "$ORGANIZATION" --project "$PROJECT"
  git config --local drs.multipart-threshold "$UPLOAD_MULTIPART_THRESHOLD_MB"

  log "Creating test payloads"
  mkdir -p data
  printf 'single-part payload %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > data/single.bin
  dd if=/dev/urandom of=data/multipart.bin bs=1048576 count="$LARGE_FILE_MB" status=none

  local single_hash_src
  local multi_hash_src
  single_hash_src="$(sha256_file data/single.bin)"
  multi_hash_src="$(sha256_file data/multipart.bin)"

  log "Tracking files with git-lfs"
  git lfs track "*.bin"
  git add .gitattributes data/single.bin data/multipart.bin
  git commit -m "e2e: add single and multipart test files" >/dev/null

  log "Pushing source repo via git-drs push (register + upload; multipart expected for large file)"
  if [[ "$PUSH_MODE" == "force" ]]; then
    git config --local push.default current
    git drs push origin
  else
    git config --local push.default current
    git drs push origin
  fi

  log "Cloning fresh repository"
  rm -rf "$CLONE_REPO"
  GIT_LFS_SKIP_SMUDGE=1 git clone --branch main "$REMOTE_URL" "$CLONE_REPO" >/dev/null

  cd "$CLONE_REPO"
  git config user.email "git-drs-e2e@example.local"
  git config user.name "git-drs-e2e"

  log "Setting up git-drs in clone"
  git drs init
  git config --local lfs.basictransfersonly true
  git drs remote add local origin "$DRS_URL" --bucket "$BUCKET" --organization "$ORGANIZATION" --project "$PROJECT"
  git config --local drs.multipart-threshold "$DOWNLOAD_MULTIPART_THRESHOLD_MB"

  log "Pulling via git-drs pull (download path; multipart expected for large file)"
  git drs pull origin

  log "Verifying downloaded content"
  local single_hash_clone
  local multi_hash_clone
  single_hash_clone="$(sha256_file data/single.bin)"
  multi_hash_clone="$(sha256_file data/multipart.bin)"

  assert_eq "$single_hash_src" "$single_hash_clone" "single-part file hash mismatch"
  assert_eq "$multi_hash_src" "$multi_hash_clone" "multipart file hash mismatch"

  if grep -q 'https://git-lfs.github.com/spec/v1' data/single.bin; then
    echo "assertion failed: single.bin is still an LFS pointer" >&2
    exit 1
  fi
  if grep -q 'https://git-lfs.github.com/spec/v1' data/multipart.bin; then
    echo "assertion failed: multipart.bin is still an LFS pointer" >&2
    exit 1
  fi

  log "SUCCESS: single + multipart upload/download passed through git-drs push/pull workflow"
  log "- upload multipart threshold (MB):   $UPLOAD_MULTIPART_THRESHOLD_MB"
  log "- download multipart threshold (MB): $DOWNLOAD_MULTIPART_THRESHOLD_MB"
  log "- large file size (MB):              $LARGE_FILE_MB"
  log "- source single sha256:    $single_hash_src"
  log "- source multipart sha256: $multi_hash_src"
}

main "$@"
