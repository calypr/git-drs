#!/usr/bin/env bash
set -euo pipefail
# set -x  # Uncomment for verbose command tracing

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
GIT_DRS_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"

# Optional env-file loading (default: <git-drs-root>/.env)
ENV_FILE="${ENV_FILE:-$GIT_DRS_ROOT/.env}"
if [[ -f "$ENV_FILE" ]]; then
  # shellcheck disable=SC1090
  set -a
  source "$ENV_FILE"
  set +a
fi

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
EXTRA_SMALL_FILES="${EXTRA_SMALL_FILES:-3}"
EXTRA_SMALL_FILE_KB="${EXTRA_SMALL_FILE_KB:-256}"
EXTRA_LARGE_FILES="${EXTRA_LARGE_FILES:-1}"
EXTRA_LARGE_FILE_MB="${EXTRA_LARGE_FILE_MB:-8}"
PUSH_MODE="${PUSH_MODE:-force}" # force|normal
CREATE_BUCKET_BEFORE_TEST="${CREATE_BUCKET_BEFORE_TEST:-false}"
TEST_BUCKET_NAME="${TEST_BUCKET_NAME:-$BUCKET}"
TEST_BUCKET_REGION="${TEST_BUCKET_REGION:-us-east-1}"
TEST_BUCKET_ACCESS_KEY="${TEST_BUCKET_ACCESS_KEY:-}"
TEST_BUCKET_SECRET_KEY="${TEST_BUCKET_SECRET_KEY:-}"
TEST_BUCKET_ENDPOINT="${TEST_BUCKET_ENDPOINT:-}"
DELETE_TEST_BUCKET_AFTER="${DELETE_TEST_BUCKET_AFTER:-false}"
ADMIN_AUTH_HEADER="${ADMIN_AUTH_HEADER:-}"

SOURCE_REPO="$WORK_ROOT/$REPO_NAME"
CLONE_REPO="$WORK_ROOT/${REPO_NAME}-clone"
API_BASE="${DRS_URL%/}/ga4gh/drs/v1"
INDEXD_BASE="${DRS_URL%/}/index/index"
BUCKET_API_BASE="${DRS_URL%/}/user/data/buckets"
FULL_SERVER_SWEEP="${FULL_SERVER_SWEEP:-true}"
CLEANUP_RECORDS="${CLEANUP_RECORDS:-true}"
STRICT_CLEANUP="${STRICT_CLEANUP:-true}"
CREATED_TEST_BUCKET=false
declare -a ALL_OIDS=()

log() {
  printf '[e2e-local] %s\n' "$*"
}

cleanup() {
  if [[ "$CLEANUP_RECORDS" == "true" && "${#ALL_OIDS[@]}" -gt 0 ]]; then
    log "Cleaning up ${#ALL_OIDS[@]} test records from drs-server"
    local bulk_delete_codes index_delete_codes verify_codes index_verify_codes
    if [[ "$STRICT_CLEANUP" == "true" ]]; then
      bulk_delete_codes="200,204"
      index_delete_codes="200,204,404"
      verify_codes="404"
      index_verify_codes="404"
    else
      bulk_delete_codes="200,204,401,403,404"
      index_delete_codes="200,204,401,403,404"
      verify_codes="200,401,403,404"
      index_verify_codes="200,401,403,404"
    fi

    local ids_json delete_body
    ids_json="$(printf '%s\n' "${ALL_OIDS[@]}" | jq -Rsc 'split("\n") | map(select(length>0))')"
    delete_body="$(jq -n --argjson ids "$ids_json" '{bulk_object_ids:$ids, delete_storage_data:false}')"
    api_json POST "$API_BASE/objects/delete" "$delete_body" "$bulk_delete_codes" >/dev/null
    for oid in "${ALL_OIDS[@]}"; do
      api_json DELETE "${INDEXD_BASE}/${oid}" "" "$index_delete_codes" >/dev/null
      api_json GET "$API_BASE/objects/${oid}" "" "$verify_codes" >/dev/null
      api_json GET "${INDEXD_BASE}/${oid}" "" "$index_verify_codes" >/dev/null
    done
    log "Cleanup verification complete for ${#ALL_OIDS[@]} objects"
  fi

  if [[ "$DELETE_TEST_BUCKET_AFTER" == "true" && "$CREATED_TEST_BUCKET" == "true" ]]; then
    log "Deleting test bucket credential: $TEST_BUCKET_NAME"
    local curl_args=(-fsS -X DELETE)
    if [[ -n "$ADMIN_AUTH_HEADER" ]]; then
      curl_args+=(-H "$ADMIN_AUTH_HEADER")
    fi
    curl "${curl_args[@]}" "$DRS_URL/admin/credentials/$TEST_BUCKET_NAME" >/dev/null || true
  fi

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

status_in() {
  local status="$1"
  local accepted_csv="$2"
  IFS=',' read -r -a accepted <<<"$accepted_csv"
  for code in "${accepted[@]}"; do
    if [[ "$status" == "$code" ]]; then
      return 0
    fi
  done
  return 1
}

api_json() {
  local method="$1"
  local url="$2"
  local body="${3:-}"
  local expect_codes="${4:-200}"
  local out
  out="$(mktemp)"
  local status

  if [[ -n "$body" ]]; then
    status="$(curl -sS -o "$out" -w '%{http_code}' \
      -X "$method" \
      -H "Accept: application/json" \
      -H "Content-Type: application/json" \
      "$url" \
      -d "$body")"
  else
    status="$(curl -sS -o "$out" -w '%{http_code}' \
      -X "$method" \
      -H "Accept: application/json" \
      "$url")"
  fi

  if ! status_in "$status" "$expect_codes"; then
    echo "request failed: $method $url (status=$status, expected=$expect_codes)" >&2
    cat "$out" >&2
    rm -f "$out"
    exit 1
  fi
  cat "$out"
  rm -f "$out"
}

lfs_json() {
  local method="$1"
  local url="$2"
  local body="$3"
  local expect_codes="${4:-200}"
  local out
  out="$(mktemp)"
  local status
  status="$(curl -sS -o "$out" -w '%{http_code}' \
    -X "$method" \
    -H "Accept: application/vnd.git-lfs+json" \
    -H "Content-Type: application/vnd.git-lfs+json" \
    "$url" \
    -d "$body")"
  if ! status_in "$status" "$expect_codes"; then
    echo "LFS request failed: $method $url (status=$status, expected=$expect_codes)" >&2
    cat "$out" >&2
    rm -f "$out"
    exit 1
  fi
  cat "$out"
  rm -f "$out"
}

create_bucket_credential_if_requested() {
  if [[ "$CREATE_BUCKET_BEFORE_TEST" != "true" ]]; then
    return 0
  fi

  if [[ -z "$TEST_BUCKET_ACCESS_KEY" || -z "$TEST_BUCKET_SECRET_KEY" ]]; then
    echo "error: CREATE_BUCKET_BEFORE_TEST=true requires TEST_BUCKET_ACCESS_KEY and TEST_BUCKET_SECRET_KEY" >&2
    exit 1
  fi

  log "Creating/updating test bucket credential via $DRS_URL/admin/credentials (bucket=$TEST_BUCKET_NAME)"
  local payload
  payload="$(cat <<JSON
{"bucket":"$TEST_BUCKET_NAME","region":"$TEST_BUCKET_REGION","access_key":"$TEST_BUCKET_ACCESS_KEY","secret_key":"$TEST_BUCKET_SECRET_KEY","endpoint":"$TEST_BUCKET_ENDPOINT"}
JSON
)"

  local curl_args=(-fsS -X PUT "$DRS_URL/admin/credentials" -H "Content-Type: application/json" -d "$payload")
  if [[ -n "$ADMIN_AUTH_HEADER" ]]; then
    curl_args+=(-H "$ADMIN_AUTH_HEADER")
  fi
  curl "${curl_args[@]}" >/dev/null
  CREATED_TEST_BUCKET=true
}

main() {
  require_cmd git
  require_cmd git-lfs
  require_cmd git-drs
  require_cmd curl
  require_cmd jq

  log "Checking drs-server health at $DRS_URL/healthz"
  curl -fsS "$DRS_URL/healthz" >/dev/null

  create_bucket_credential_if_requested
  declare -A HASH_SRC=()
  declare -A SIZE_SRC=()
  declare -a ALL_FILES=()

  local effective_bucket="$BUCKET"
  if [[ "$CREATE_BUCKET_BEFORE_TEST" == "true" ]]; then
    effective_bucket="$TEST_BUCKET_NAME"
  fi

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
  git drs remote add local origin "$DRS_URL" --bucket "$effective_bucket" --organization "$ORGANIZATION" --project "$PROJECT"
  git config --local drs.multipart-threshold "$UPLOAD_MULTIPART_THRESHOLD_MB"

  log "Creating test payloads"
  mkdir -p data
  printf 'single-part payload %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > data/single.bin
  dd if=/dev/urandom of=data/multipart.bin bs=1048576 count="$LARGE_FILE_MB" status=none

  # Add more payloads so endpoint sweeps run against realistic non-trivial DB state.
  if [[ "$EXTRA_SMALL_FILES" -gt 0 ]]; then
    for i in $(seq 1 "$EXTRA_SMALL_FILES"); do
      dd if=/dev/urandom of="data/extra-small-${i}.bin" bs=1024 count="$EXTRA_SMALL_FILE_KB" status=none
    done
  fi
  if [[ "$EXTRA_LARGE_FILES" -gt 0 ]]; then
    for i in $(seq 1 "$EXTRA_LARGE_FILES"); do
      dd if=/dev/urandom of="data/extra-large-${i}.bin" bs=1048576 count="$EXTRA_LARGE_FILE_MB" status=none
    done
  fi

  local single_hash_src
  local multi_hash_src
  single_hash_src="$(sha256_file data/single.bin)"
  multi_hash_src="$(sha256_file data/multipart.bin)"
  HASH_SRC["data/single.bin"]="$single_hash_src"
  HASH_SRC["data/multipart.bin"]="$multi_hash_src"
  SIZE_SRC["data/single.bin"]="$(wc -c < data/single.bin | tr -d '[:space:]')"
  SIZE_SRC["data/multipart.bin"]="$(wc -c < data/multipart.bin | tr -d '[:space:]')"
  ALL_OIDS+=("$single_hash_src" "$multi_hash_src")
  ALL_FILES+=("data/single.bin" "data/multipart.bin")
  for f in data/extra-small-*.bin data/extra-large-*.bin; do
    [[ -f "$f" ]] || continue
    local h
    h="$(sha256_file "$f")"
    HASH_SRC["$f"]="$h"
    SIZE_SRC["$f"]="$(wc -c < "$f" | tr -d '[:space:]')"
    ALL_OIDS+=("$h")
    ALL_FILES+=("$f")
  done

  log "Tracking files with git-lfs"
  git lfs track "*.bin"
  git add .gitattributes data/*.bin
  git commit -m "e2e: add expanded dataset (single, multipart, extras)" >/dev/null

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
  git drs remote add local origin "$DRS_URL" --bucket "$effective_bucket" --organization "$ORGANIZATION" --project "$PROJECT"
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
  for f in "${ALL_FILES[@]}"; do
    local h_clone
    h_clone="$(sha256_file "$f")"
    assert_eq "${HASH_SRC[$f]}" "$h_clone" "hash mismatch for $f"
  done

  if grep -q 'https://git-lfs.github.com/spec/v1' data/single.bin; then
    echo "assertion failed: single.bin is still an LFS pointer" >&2
    exit 1
  fi
  if grep -q 'https://git-lfs.github.com/spec/v1' data/multipart.bin; then
    echo "assertion failed: multipart.bin is still an LFS pointer" >&2
    exit 1
  fi

  if [[ "$FULL_SERVER_SWEEP" == "true" ]]; then
    log "Running full server method sweep"
    local single_obj multi_obj
    local single_access_id multi_access_id
    local single_size multi_size
    local all_oids_json
    single_size="${SIZE_SRC["data/single.bin"]}"
    multi_size="${SIZE_SRC["data/multipart.bin"]}"
    all_oids_json="$(printf '%s\n' "${ALL_OIDS[@]}" | jq -Rsc 'split("\n") | map(select(length>0))')"

    api_json GET "$API_BASE/service-info" "" "200" | jq -e '.name and .version' >/dev/null

    single_obj="$(api_json GET "$API_BASE/objects/$single_hash_src" "" "200")"
    multi_obj="$(api_json GET "$API_BASE/objects/$multi_hash_src" "" "200")"
    echo "$single_obj" | jq -e --arg oid "$single_hash_src" '.id == $oid' >/dev/null
    echo "$multi_obj" | jq -e --arg oid "$multi_hash_src" '.id == $oid' >/dev/null

    api_json OPTIONS "$API_BASE/objects/$single_hash_src" "" "200,204" >/dev/null
    api_json OPTIONS "$API_BASE/objects/$multi_hash_src" "" "200,204" >/dev/null
    api_json GET "$API_BASE/objects/checksum/$single_hash_src" "" "200" >/dev/null
    api_json GET "$API_BASE/objects/checksum/$multi_hash_src" "" "200" >/dev/null
    api_json POST "$API_BASE/objects" "$(jq -n --argjson ids "$all_oids_json" '{bulk_object_ids: $ids}')" "200" \
      | jq -e '.summary.requested >= 2 and .summary.resolved >= 2' >/dev/null

    single_access_id="$(echo "$single_obj" | jq -r '.access_methods[0].access_id // .access_methods[0].type')"
    multi_access_id="$(echo "$multi_obj" | jq -r '.access_methods[0].access_id // .access_methods[0].type')"
    [[ -n "$single_access_id" && "$single_access_id" != "null" ]] || { echo "error: missing single access id" >&2; exit 1; }
    [[ -n "$multi_access_id" && "$multi_access_id" != "null" ]] || { echo "error: missing multipart access id" >&2; exit 1; }

    api_json GET "$API_BASE/objects/$single_hash_src/access/$single_access_id" "" "200" >/dev/null
    api_json POST "$API_BASE/objects/$single_hash_src/access/$single_access_id" "{}" "200" >/dev/null
    api_json POST "$API_BASE/objects/access" "$(jq -n \
      --arg s "$single_hash_src" --arg sid "$single_access_id" \
      --arg m "$multi_hash_src" --arg mid "$multi_access_id" \
      '{bulk_object_access_ids: [{bulk_object_id:$s, bulk_access_ids:[$sid]}, {bulk_object_id:$m, bulk_access_ids:[$mid]}]}')" "200" \
      | jq -e '.summary.requested >= 2 and .summary.resolved >= 2' >/dev/null

    api_json POST "$API_BASE/upload-request" "$(jq -n --arg oid "$single_hash_src" --argjson size "$single_size" '{
      requests: [{name: "local-e2e-upload-request.bin", size: $size, mime_type: "application/octet-stream", checksums: [{type:"sha256", checksum:$oid}]}]
    }')" "200" >/dev/null

    lfs_json POST "${DRS_URL%/}/info/lfs/objects/batch" "$(jq -n \
      --arg soid "$single_hash_src" --argjson ssize "$single_size" \
      --arg moid "$multi_hash_src" --argjson msize "$multi_size" \
      '{operation:"download", transfers:["basic"], objects:[{oid:$soid,size:$ssize},{oid:$moid,size:$msize}], hash_algo:"sha256"}')" "200" >/dev/null
    lfs_json POST "${DRS_URL%/}/info/lfs/objects/batch" "$(jq -n \
      --arg soid "$single_hash_src" --argjson ssize "$single_size" \
      --arg moid "$multi_hash_src" --argjson msize "$multi_size" \
      '{operation:"upload", transfers:["basic"], objects:[{oid:$soid,size:$ssize},{oid:$moid,size:$msize}], hash_algo:"sha256"}')" "200" >/dev/null
    lfs_json POST "${DRS_URL%/}/info/lfs/verify" "$(jq -n --arg oid "$single_hash_src" --argjson size "$single_size" '{oid:$oid,size:$size}')" "200" >/dev/null
    lfs_json POST "${DRS_URL%/}/info/lfs/verify" "$(jq -n --arg oid "$multi_hash_src" --argjson size "$multi_size" '{oid:$oid,size:$size}')" "200" >/dev/null

    local validity_body validity_resp
    validity_body="$(jq -n --arg s "$single_hash_src" --arg m "$multi_hash_src" '{sha256: [$s, $m]}')"
    validity_resp="$(api_json POST "${DRS_URL%/}/internal/v1/sha256/validity" "$validity_body" "200")"
    echo "$validity_resp" | jq -e --arg s "$single_hash_src" --arg m "$multi_hash_src" '.[$s] == true and .[$m] == true' >/dev/null

    api_json GET "${DRS_URL%/}/internal/v1/metrics/files/$single_hash_src" "" "200,404" >/dev/null
    api_json GET "${DRS_URL%/}/internal/v1/metrics/files/$multi_hash_src" "" "200,404" >/dev/null
    api_json GET "${DRS_URL%/}/internal/v1/metrics/summary" "" "200,401,403" >/dev/null

    api_json GET "${INDEXD_BASE}?hash=sha256:$single_hash_src" "" "200" >/dev/null
    api_json POST "${INDEXD_BASE}/bulk/hashes" "$(jq -n --argjson ids "$all_oids_json" '{hashes: ($ids | map("sha256:"+.))}')" "200" >/dev/null
    api_json POST "${INDEXD_BASE}/bulk/sha256/validity" "$validity_body" "200" >/dev/null
    api_json POST "${DRS_URL%/}/bulk/documents" "$(jq -n --argjson ids "$all_oids_json" '$ids')" "200" >/dev/null

    api_json GET "${DRS_URL%/}/data/upload/$single_hash_src?bucket=$effective_bucket&file_name=$single_hash_src" "" "200,401,403,404" >/dev/null
    api_json GET "${DRS_URL%/}/data/download/$single_hash_src" "" "200,302,307,401,403,404" >/dev/null
    api_json GET "${DRS_URL%/}/user/data/upload/$single_hash_src?bucket=$effective_bucket&file_name=$single_hash_src" "" "200,401,403,404" >/dev/null
    api_json GET "${DRS_URL%/}/user/data/download/$single_hash_src" "" "200,302,307,401,403,404" >/dev/null
    api_json GET "$BUCKET_API_BASE" "" "200,401,403" >/dev/null
    api_json GET "${DRS_URL%/}/openapi.yaml" "" "200" >/dev/null
  fi

  log "SUCCESS: single + multipart upload/download passed through git-drs push/pull workflow"
  log "- upload multipart threshold (MB):   $UPLOAD_MULTIPART_THRESHOLD_MB"
  log "- download multipart threshold (MB): $DOWNLOAD_MULTIPART_THRESHOLD_MB"
  log "- large file size (MB):              $LARGE_FILE_MB"
  log "- bucket used:                       $effective_bucket"
  log "- source single sha256:    $single_hash_src"
  log "- source multipart sha256: $multi_hash_src"
  log "- total objects uploaded:            ${#ALL_OIDS[@]}"
}

main "$@"
