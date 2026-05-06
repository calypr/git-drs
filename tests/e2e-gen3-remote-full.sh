#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
GIT_DRS_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"

# Optional shared env-file loading (default: <git-drs-root>/.env)
ENV_FILE="${ENV_FILE:-$GIT_DRS_ROOT/.env}"
if [[ -f "$ENV_FILE" ]]; then
  # shellcheck disable=SC1090
  set -a
  source "$ENV_FILE"
  set +a
fi

DRS_URL="${TEST_DRS_URL:-https://caliper-training.ohsu.edu}"
SERVER_MODE="${TEST_SERVER_MODE:-remote}"
LOG_PREFIX="${TEST_LOG_PREFIX:-}"
REAL_WORLD_GITHUB="${TEST_REAL_WORLD_GITHUB:-false}"
GEN3_TOKEN="${TEST_GEN3_TOKEN:-}"
GEN3_PROFILE="${TEST_GEN3_PROFILE:-${GEN3_PROFILE:-}}"
GEN3_CONFIG_PATH="${TEST_GEN3_CONFIG_PATH:-${GEN3_CONFIG_PATH:-$HOME/.gen3/gen3_client_config.ini}}"
ORGANIZATION="${TEST_ORGANIZATION:-}"
PROJECT_ID="${TEST_PROJECT_ID:-}"
BUCKET="${TEST_BUCKET:-}"
REMOTE_NAME="${TEST_REMOTE_NAME:-origin}"
REPO_NAME="${TEST_REPO_NAME:-git-drs-e2e-remote}"
WORK_ROOT="${TEST_WORK_ROOT:-$(mktemp -d -t git-drs-e2e-remote-XXXX)}"
REMOTE_URL="${TEST_REMOTE_URL:-$WORK_ROOT/${REPO_NAME}.git}"
KEEP_WORKDIR="${TEST_KEEP_WORKDIR:-false}"
LARGE_FILE_MB="${TEST_LARGE_FILE_MB:-12}"
EXTRA_SMALL_FILES="${TEST_EXTRA_SMALL_FILES:-3}"
EXTRA_SMALL_FILE_KB="${TEST_EXTRA_SMALL_FILE_KB:-256}"
EXTRA_LARGE_FILES="${TEST_EXTRA_LARGE_FILES:-1}"
EXTRA_LARGE_FILE_MB="${TEST_EXTRA_LARGE_FILE_MB:-8}"
MULTIPART_THRESHOLD_MB="${TEST_MULTIPART_THRESHOLD_MB:-5}"
UPLOAD_MULTIPART_THRESHOLD_MB="${TEST_UPLOAD_MULTIPART_THRESHOLD_MB:-$MULTIPART_THRESHOLD_MB}"
DOWNLOAD_MULTIPART_THRESHOLD_MB="${TEST_DOWNLOAD_MULTIPART_THRESHOLD_MB:-$MULTIPART_THRESHOLD_MB}"
RUN_OPTIONAL_MUTATIONS="${TEST_RUN_OPTIONAL_MUTATIONS:-false}"
PUSH_MODE="${TEST_PUSH_MODE:-drs}"
ENABLE_GIT_PUSH_COMPAT="${TEST_ENABLE_GIT_PUSH_COMPAT:-false}"
LFS_PULL_COMPAT="${TEST_LFS_PULL_COMPAT:-true}"
CREATE_BUCKET_BEFORE_TEST="${TEST_CREATE_BUCKET_BEFORE_TEST:-false}"
DELETE_TEST_BUCKET_AFTER="${TEST_DELETE_BUCKET_AFTER:-true}"
TEST_BUCKET_NAME="${TEST_BUCKET_NAME:-}"
TEST_BUCKET_REGION="${TEST_BUCKET_REGION:-}"
TEST_BUCKET_ACCESS_KEY="${TEST_BUCKET_ACCESS_KEY:-}"
TEST_BUCKET_SECRET_KEY="${TEST_BUCKET_SECRET_KEY:-}"
TEST_BUCKET_ENDPOINT="${TEST_BUCKET_ENDPOINT:-}"
ADMIN_AUTH_HEADER="${TEST_ADMIN_AUTH_HEADER:-${ADMIN_AUTH_HEADER:-}}"
LOCAL_PASSWORD="${TEST_LOCAL_PASSWORD:-${LOCAL_PASSWORD:-${DRS_BASIC_AUTH_PASSWORD:-}}}"
LOCAL_USERNAME="${TEST_LOCAL_USERNAME:-${LOCAL_USERNAME:-${DRS_BASIC_AUTH_USER:-}}}"
TEST_BUCKET_ORGANIZATION="${TEST_BUCKET_ORGANIZATION:-$ORGANIZATION}"
TEST_BUCKET_PROJECT_ID="${TEST_BUCKET_PROJECT_ID:-$PROJECT_ID}"
FULL_SERVER_SWEEP="${TEST_FULL_SERVER_SWEEP:-true}"
RUN_INTERNAL_API_CHECKS="${TEST_RUN_INTERNAL_API_CHECKS:-true}"
CLEANUP_RECORDS="${TEST_CLEANUP_RECORDS:-true}"
STRICT_CLEANUP="${TEST_STRICT_CLEANUP:-true}"
RUN_RESUME_E2E="${TEST_RUN_RESUME_E2E:-true}"
RESUME_FAIL_DOWNLOAD_AFTER_BYTES="${TEST_RESUME_FAIL_DOWNLOAD_AFTER_BYTES:-2097152}"
GITHUB_MODE="${TEST_GITHUB_MODE:-false}"
TEST_GITHUB_TOKEN="${TEST_GITHUB_TOKEN:-}"
TEST_GITHUB_OWNER="${TEST_GITHUB_OWNER:-calypr}"
TEST_GITHUB_IS_ORG="${TEST_GITHUB_IS_ORG:-auto}"
TEST_GITHUB_REPO_NAME="${TEST_GITHUB_REPO_NAME:-$REPO_NAME}"
TEST_DELETE_GITHUB_REPO_AFTER_RAW="${TEST_DELETE_GITHUB_REPO_AFTER-__UNSET__}"
TEST_DELETE_GITHUB_REPO_AFTER="${TEST_DELETE_GITHUB_REPO_AFTER:-true}"
TEST_COLLABORATOR_GRANT="${TEST_COLLABORATOR_GRANT:-auto}"
TEST_COLLAB_CMD="${TEST_COLLAB_CMD:-data-client}"
TEST_COLLAB_USER_EMAIL="${TEST_COLLAB_USER_EMAIL:-}"
TEST_COLLAB_PROJECT_ID="${TEST_COLLAB_PROJECT_ID:-${ORGANIZATION}-${PROJECT_ID}}"
TEST_COLLAB_WRITE="${TEST_COLLAB_WRITE:-true}"
TEST_COLLAB_APPROVE="${TEST_COLLAB_APPROVE:-true}"
TEST_COLLAB_STRICT="${TEST_COLLAB_STRICT:-false}"
TEST_ALLOW_BUCKET_PREFLIGHT_FORBIDDEN="${TEST_ALLOW_BUCKET_PREFLIGHT_FORBIDDEN:-false}"
TEST_DEBUG_AUTH="${TEST_DEBUG_AUTH:-true}"
TEST_PRINT_TOKEN="${TEST_PRINT_TOKEN:-false}"

if [[ "$REAL_WORLD_GITHUB" == "true" ]]; then
  SERVER_MODE="remote"
  GITHUB_MODE="true"
  # Real-world mode keeps the repo by default unless caller explicitly asked to delete it.
  if [[ "$TEST_DELETE_GITHUB_REPO_AFTER_RAW" == "__UNSET__" ]]; then
    TEST_DELETE_GITHUB_REPO_AFTER="false"
  fi
fi

# Convenience fallback: if TEST_BUCKET_NAME is omitted, reuse TEST_BUCKET.
if [[ -z "${TEST_BUCKET_NAME}" && -n "${BUCKET}" ]]; then
  TEST_BUCKET_NAME="${BUCKET}"
fi

SOURCE_REPO="$WORK_ROOT/$REPO_NAME"
CLONE_REPO="$WORK_ROOT/${REPO_NAME}-clone"
API_BASE="${DRS_URL%/}/ga4gh/drs/v1"
BUCKET_API_BASE="${DRS_URL%/}/data/buckets"
INDEXD_BASE="${DRS_URL%/}/index"
CREATED_TEST_BUCKET=false
HAS_DRS_API=false
HAS_DRS_BULK_DELETE=false
HAS_DRS_BULK_ACCESS=false
CREATED_GITHUB_REPO=false
GITHUB_OWNER_REPO=""
GITHUB_REPO_WEB_URL=""
GEN3_TOKEN_SOURCE="unset"
declare -a ALL_OIDS=()
DRS_ID_BY_OID_STORE=""
HASH_SRC_STORE=""
SIZE_SRC_STORE=""
CURRENT_PHASE="bootstrap"
TEST_OUTCOME="FAIL"
FAIL_LINE=""
FAIL_CMD=""

kv_store_get() {
  local store="$1"
  local key="$2"
  local line k v
  while IFS= read -r line; do
    [[ -n "$line" ]] || continue
    k="${line%%$'\t'*}"
    if [[ "$k" == "$key" ]]; then
      v="${line#*$'\t'}"
      printf '%s\n' "$v"
      return 0
    fi
  done <<<"$store"
  return 1
}

kv_store_set() {
  local store="$1"
  local key="$2"
  local value="$3"
  local line k
  local found=0
  local out=""
  while IFS= read -r line; do
    [[ -n "$line" ]] || continue
    k="${line%%$'\t'*}"
    if [[ "$k" == "$key" ]]; then
      out+="${key}"$'\t'"${value}"$'\n'
      found=1
    else
      out+="${line}"$'\n'
    fi
  done <<<"$store"
  if [[ "$found" -eq 0 ]]; then
    out+="${key}"$'\t'"${value}"$'\n'
  fi
  printf '%s' "$out"
}

map_get() {
  local map="$1"
  local key="$2"
  case "$map" in
    DRS_ID_BY_OID) kv_store_get "$DRS_ID_BY_OID_STORE" "$key" ;;
    HASH_SRC) kv_store_get "$HASH_SRC_STORE" "$key" ;;
    SIZE_SRC) kv_store_get "$SIZE_SRC_STORE" "$key" ;;
    *) return 1 ;;
  esac
}

map_set() {
  local map="$1"
  local key="$2"
  local value="$3"
  case "$map" in
    DRS_ID_BY_OID) DRS_ID_BY_OID_STORE="$(kv_store_set "$DRS_ID_BY_OID_STORE" "$key" "$value")" ;;
    HASH_SRC) HASH_SRC_STORE="$(kv_store_set "$HASH_SRC_STORE" "$key" "$value")" ;;
    SIZE_SRC) SIZE_SRC_STORE="$(kv_store_set "$SIZE_SRC_STORE" "$key" "$value")" ;;
    *) return 1 ;;
  esac
}

map_get_or_empty() {
  local map="$1"
  local key="$2"
  map_get "$map" "$key" 2>/dev/null || true
}

map_has() {
  local map="$1"
  local key="$2"
  map_get "$map" "$key" >/dev/null 2>&1
}

log() {
  if [[ -z "$LOG_PREFIX" ]]; then
    if [[ "$SERVER_MODE" == "local" ]]; then
      LOG_PREFIX="e2e-local-full"
    else
      LOG_PREFIX="e2e-gen3-remote"
    fi
  fi
  printf '[%s] %s\n' "$LOG_PREFIX" "$*"
}

log_warn() {
  if [[ -z "$LOG_PREFIX" ]]; then
    if [[ "$SERVER_MODE" == "local" ]]; then
      LOG_PREFIX="e2e-local-full"
    else
      LOG_PREFIX="e2e-gen3-remote"
    fi
  fi
  printf '[%s][warn] %s\n' "$LOG_PREFIX" "$*" >&2
}

phase() {
  CURRENT_PHASE="$1"
  log "PHASE: $CURRENT_PHASE"
}

on_error() {
  FAIL_LINE="${BASH_LINENO[0]:-unknown}"
  FAIL_CMD="${BASH_COMMAND:-unknown}"
}
trap on_error ERR

basic_auth_header() {
  local username="$1"
  local password="$2"
  if command -v base64 >/dev/null 2>&1; then
    printf 'Authorization: Basic %s' "$(printf '%s:%s' "$username" "$password" | base64 | tr -d '\n')"
  else
    printf 'Authorization: Basic %s' "$(printf '%s:%s' "$username" "$password" | openssl base64 -A)"
  fi
}

decode_base64() {
  local value="$1"
  if printf '%s' "$value" | base64 --decode >/dev/null 2>&1; then
    printf '%s' "$value" | base64 --decode
  else
    printf '%s' "$value" | base64 -D
  fi
}

configure_lfs_endpoint_for_repo() {
  local remote_name="$1"
  local endpoint="${DRS_URL%/}/info/lfs"
  git config --local lfs.url "$endpoint"
  git config --local "remote.${remote_name}.lfsurl" "$endpoint"
  git config --local "remote.${remote_name}.lfspushurl" "$endpoint"
  # Force preemptive auth for git-lfs so it doesn't intentionally probe
  # unauthenticated first (401) before retrying with credentials.
  git config --local "lfs.${endpoint}.access" "basic"
  if [[ -n "${GEN3_TOKEN:-}" ]]; then
    git config --local --unset-all "http.${endpoint}.extraheader" >/dev/null 2>&1 || true
    git config --local --add "http.${endpoint}.extraheader" "Authorization: Bearer ${GEN3_TOKEN}"
  elif [[ "$SERVER_MODE" == "local" && -n "$ADMIN_AUTH_HEADER" ]]; then
    git config --local --unset-all "http.${endpoint}.extraheader" >/dev/null 2>&1 || true
    git config --local --add "http.${endpoint}.extraheader" "$ADMIN_AUTH_HEADER"
  fi
}

log_lfs_endpoint_for_repo() {
  local remote_name="$1"
  local endpoint_default endpoint_remote
  endpoint_default="$(git config --local --get lfs.url || true)"
  endpoint_remote="$(git config --local --get "remote.${remote_name}.lfsurl" || true)"
  log "git-lfs endpoint (lfs.url): ${endpoint_default:-<unset>}"
  log "git-lfs endpoint (remote.${remote_name}.lfsurl): ${endpoint_remote:-<unset>}"
}

configure_local_credential_helper() {
  # Keep credential helper behavior deterministic for test logs and avoid
  # chaining into unrelated global helpers (e.g., gh helper) that can emit
  # noisy parse errors for bearer tokens.
  git config --local --unset-all credential.helper >/dev/null 2>&1 || true
  git config --local credential.helper "git drs credential-helper"
}

cleanup() {
  local exit_code=$?
  if [[ "$CLEANUP_RECORDS" == "true" && "${#ALL_OIDS[@]}" -gt 0 ]]; then
    log "Cleaning up ${#ALL_OIDS[@]} test records from drs-server"
    local bulk_delete_codes index_delete_codes verify_codes index_verify_codes
    if [[ "$STRICT_CLEANUP" == "true" ]]; then
      bulk_delete_codes="200,204,404"
      index_delete_codes="200,204,404"
      verify_codes="404"
      index_verify_codes="404"
    else
      bulk_delete_codes="200,204,401,403,404"
      index_delete_codes="200,204,401,403,404"
      verify_codes="200,401,403,404"
      index_verify_codes="200,401,403,404"
    fi
    if [[ "$HAS_DRS_API" == "true" && "$HAS_DRS_BULK_DELETE" == "true" ]]; then
      local drs_ids=() oid ids_json delete_body did
      for oid in "${ALL_OIDS[@]}"; do
        did="$(drs_id_from_oid "$oid")"
        if [[ -n "$did" ]]; then
          drs_ids+=("$did")
        fi
      done
      if [[ "${#drs_ids[@]}" -gt 0 ]]; then
        ids_json="$(printf '%s\n' "${drs_ids[@]}" | jq -Rsc 'split("\n") | map(select(length>0))')"
        delete_body="$(jq -n --argjson ids "$ids_json" '{bulk_object_ids:$ids, delete_storage_data:false}')"
        api_json POST "$API_BASE/objects/delete" "$delete_body" "$bulk_delete_codes" >/dev/null || true
      fi
    elif [[ "$HAS_DRS_API" == "true" ]]; then
      log "Skipping DRS bulk delete cleanup: endpoint /ga4gh/drs/v1/objects/delete not available"
    fi
    for oid in "${ALL_OIDS[@]}"; do
      local dids_raw did did_count did_list
      dids_raw="$(dids_from_oid "$oid" || true)"
      did_count=0
      did_list=""
      while IFS= read -r did; do
        [[ -n "$did" ]] || continue
        did_count=$((did_count + 1))
        if [[ -z "$did_list" ]]; then
          did_list="$did"
        else
          did_list="${did_list} ${did}"
        fi
        api_json_noexit DELETE "${INDEXD_BASE}/${did}" ""
        log "cleanup-delete: oid=${oid} did=${did} status=${API_HTTP_STATUS}"
        api_json_noexit GET "${INDEXD_BASE}/${did}" ""
        log "cleanup-verify: oid=${oid} did=${did} status=${API_HTTP_STATUS}"
      done <<<"$dids_raw"
      if [[ "$did_count" -eq 0 ]]; then
        log "cleanup: oid=${oid} resolved to 0 dids (already cleaned or no records)"
      else
        log "cleanup: oid=${oid} resolved did_count=${did_count} dids=${did_list}"
      fi
      if [[ "$HAS_DRS_API" == "true" ]]; then
        did="$(map_get_or_empty DRS_ID_BY_OID "$oid")"
        if [[ -n "$did" ]]; then
          api_json GET "$API_BASE/objects/${did}" "" "$verify_codes" >/dev/null || true
        fi
      fi
      api_json GET "${INDEXD_BASE}?hash=sha256:${oid}" "" "200" >/dev/null || true
    done
    log "Cleanup verification complete for ${#ALL_OIDS[@]} objects"
  fi

  if [[ "$CREATE_BUCKET_BEFORE_TEST" == "true" && "$DELETE_TEST_BUCKET_AFTER" == "true" && "$CREATED_TEST_BUCKET" == "true" ]]; then
    log "Deleting test bucket credential '$TEST_BUCKET_NAME'"
    api_json DELETE "$BUCKET_API_BASE/$TEST_BUCKET_NAME" "" "200,204,404" >/dev/null
  fi

  if [[ "$GITHUB_MODE" == "true" && "$TEST_DELETE_GITHUB_REPO_AFTER" == "true" && "$CREATED_GITHUB_REPO" == "true" ]]; then
    log "Deleting temporary GitHub repo ${GITHUB_OWNER_REPO}"
    if [[ -n "$TEST_GITHUB_TOKEN" ]]; then
      GH_TOKEN="$TEST_GITHUB_TOKEN" gh api -X DELETE "/repos/${GITHUB_OWNER_REPO}" >/dev/null 2>&1 || true
    else
      gh api -X DELETE "/repos/${GITHUB_OWNER_REPO}" >/dev/null 2>&1 || true
    fi
  fi

  if [[ "$KEEP_WORKDIR" == "true" ]]; then
    log "Keeping working directory: $WORK_ROOT"
    if [[ "$exit_code" -eq 0 && "$TEST_OUTCOME" == "PASS" ]]; then
      log "RESULT: PASS"
    else
      log_warn "RESULT: FAIL (phase=${CURRENT_PHASE}, line=${FAIL_LINE:-unknown})"
      if [[ -n "$FAIL_CMD" ]]; then
        log_warn "Failed command: $FAIL_CMD"
      fi
    fi
    return "$exit_code"
  fi
  rm -rf "$WORK_ROOT"
  if [[ "$exit_code" -eq 0 && "$TEST_OUTCOME" == "PASS" ]]; then
    log "RESULT: PASS"
  else
    log_warn "RESULT: FAIL (phase=${CURRENT_PHASE}, line=${FAIL_LINE:-unknown})"
    if [[ -n "$FAIL_CMD" ]]; then
      log_warn "Failed command: $FAIL_CMD"
    fi
  fi
  return "$exit_code"
}
trap cleanup EXIT

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: required command not found: $1" >&2
    exit 1
  fi
}

require_env() {
  local key="$1"
  local val="$2"
  if [[ -z "$val" ]]; then
    echo "error: required env var '$key' is not set" >&2
    exit 1
  fi
}

require_int_ge() {
  local key="$1"
  local val="$2"
  local min="$3"
  if ! [[ "$val" =~ ^[0-9]+$ ]]; then
    echo "error: env var '$key' must be an integer, got '$val'" >&2
    exit 1
  fi
  if (( val < min )); then
    echo "error: env var '$key' must be >= $min, got '$val'" >&2
    exit 1
  fi
}

expects_multipart_upload() {
  local largest="$LARGE_FILE_MB"
  if (( EXTRA_LARGE_FILES > 0 )) && (( EXTRA_LARGE_FILE_MB > largest )); then
    largest="$EXTRA_LARGE_FILE_MB"
  fi
  (( largest >= UPLOAD_MULTIPART_THRESHOLD_MB ))
}

validate_required_config() {
  case "$SERVER_MODE" in
    remote|local) ;;
    *)
      echo "error: TEST_SERVER_MODE must be 'remote' or 'local', got '$SERVER_MODE'" >&2
      exit 1
      ;;
  esac

  if [[ "$SERVER_MODE" == "remote" ]]; then
    case "${DRS_URL%/}" in
      http://localhost*|https://localhost*|http://127.0.0.1*|https://127.0.0.1*|http://[::1]*|https://[::1]*)
        echo "error: TEST_SERVER_MODE=remote cannot use TEST_DRS_URL=${DRS_URL%/}" >&2
        echo "       remote mode expects Gen3 auth/profile flow for the DRS endpoint." >&2
        echo "       Use TEST_SERVER_MODE=local for localhost DRS, or set TEST_DRS_URL to your remote Gen3 host." >&2
        exit 1
        ;;
    esac
  fi

  case "$PUSH_MODE" in
    drs|git|both) ;;
    *)
      echo "error: TEST_PUSH_MODE must be one of: drs, git, both (got '$PUSH_MODE')" >&2
      exit 1
      ;;
  esac

  case "$ENABLE_GIT_PUSH_COMPAT" in
    true|false) ;;
    *)
      echo "error: TEST_ENABLE_GIT_PUSH_COMPAT must be 'true' or 'false', got '$ENABLE_GIT_PUSH_COMPAT'" >&2
      exit 1
      ;;
  esac
  case "$RUN_RESUME_E2E" in
    true|false) ;;
    *)
      echo "error: TEST_RUN_RESUME_E2E must be 'true' or 'false', got '$RUN_RESUME_E2E'" >&2
      exit 1
      ;;
  esac

  # Keep compatibility path opt-in so default runs exercise git-drs push only.
  if [[ "$ENABLE_GIT_PUSH_COMPAT" != "true" ]]; then
    if [[ "$PUSH_MODE" == "git" ]]; then
      echo "error: TEST_PUSH_MODE=git requires TEST_ENABLE_GIT_PUSH_COMPAT=true" >&2
      exit 1
    fi
    if [[ "$PUSH_MODE" == "both" ]]; then
      log_warn "TEST_PUSH_MODE=both requested but TEST_ENABLE_GIT_PUSH_COMPAT=false; using drs-only push path"
      PUSH_MODE="drs"
    fi
  fi

  require_env TEST_ORGANIZATION "$ORGANIZATION"
  require_env TEST_PROJECT_ID "$PROJECT_ID"
  require_env TEST_BUCKET "$BUCKET"

  require_int_ge TEST_LARGE_FILE_MB "$LARGE_FILE_MB" 1
  require_int_ge TEST_EXTRA_SMALL_FILES "$EXTRA_SMALL_FILES" 0
  require_int_ge TEST_EXTRA_SMALL_FILE_KB "$EXTRA_SMALL_FILE_KB" 1
  require_int_ge TEST_EXTRA_LARGE_FILES "$EXTRA_LARGE_FILES" 0
  require_int_ge TEST_EXTRA_LARGE_FILE_MB "$EXTRA_LARGE_FILE_MB" 1
  require_int_ge TEST_MULTIPART_THRESHOLD_MB "$MULTIPART_THRESHOLD_MB" 1
  require_int_ge TEST_UPLOAD_MULTIPART_THRESHOLD_MB "$UPLOAD_MULTIPART_THRESHOLD_MB" 1
  require_int_ge TEST_DOWNLOAD_MULTIPART_THRESHOLD_MB "$DOWNLOAD_MULTIPART_THRESHOLD_MB" 1
  require_int_ge TEST_RESUME_FAIL_DOWNLOAD_AFTER_BYTES "$RESUME_FAIL_DOWNLOAD_AFTER_BYTES" 1

  if [[ "$SERVER_MODE" == "remote" && -z "$GEN3_TOKEN" && -z "$GEN3_PROFILE" ]]; then
    echo "error: remote mode requires TEST_GEN3_TOKEN or GEN3_PROFILE/TEST_GEN3_PROFILE" >&2
    exit 1
  fi
  if [[ "$SERVER_MODE" == "local" ]]; then
    if [[ -n "$LOCAL_USERNAME" && -z "$LOCAL_PASSWORD" ]]; then
      echo "error: TEST_LOCAL_PASSWORD is required when TEST_LOCAL_USERNAME is set" >&2
      exit 1
    fi
    if [[ -z "$LOCAL_USERNAME" && -n "$LOCAL_PASSWORD" ]]; then
      echo "error: TEST_LOCAL_USERNAME is required when TEST_LOCAL_PASSWORD is set" >&2
      exit 1
    fi
  fi
  if [[ "$GITHUB_MODE" == "true" ]]; then
    require_cmd gh
  fi
  if [[ "$CREATE_BUCKET_BEFORE_TEST" == "true" ]]; then
    require_env TEST_BUCKET_NAME "$TEST_BUCKET_NAME"
    require_env TEST_BUCKET_REGION "$TEST_BUCKET_REGION"
    require_env TEST_BUCKET_ACCESS_KEY "$TEST_BUCKET_ACCESS_KEY"
    require_env TEST_BUCKET_SECRET_KEY "$TEST_BUCKET_SECRET_KEY"
  fi

  case "$TEST_COLLABORATOR_GRANT" in
    auto|true|false) ;;
    *)
      echo "error: TEST_COLLABORATOR_GRANT must be one of: auto, true, false (got '$TEST_COLLABORATOR_GRANT')" >&2
      exit 1
      ;;
  esac
}

load_profile_field() {
  local profile="$1"
  local key="$2"
  local file="$3"
  awk -F'=' -v section="$profile" -v wanted="$key" '
    /^[[:space:]]*\[/ {
      current=$0
      gsub(/^[[:space:]]*\[/, "", current)
      gsub(/\][[:space:]]*$/, "", current)
      in_section = (current == section)
      next
    }
    in_section {
      line=$0
      sub(/[;#].*$/, "", line)
      if (line !~ /=/) next
      k=line
      sub(/=.*/, "", k)
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", k)
      if (k == wanted) {
        v=line
        sub(/^[^=]*=/, "", v)
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", v)
        print v
        exit
      }
    }
  ' "$file"
}

resolve_auth_from_profile_if_needed() {
  if [[ "$SERVER_MODE" == "local" ]]; then
    return
  fi
  if [[ -n "$GEN3_TOKEN" ]]; then
    GEN3_TOKEN_SOURCE="env:TEST_GEN3_TOKEN"
    return
  fi
  require_env GEN3_PROFILE "$GEN3_PROFILE"
  if [[ ! -f "$GEN3_CONFIG_PATH" ]]; then
    echo "error: GEN3_PROFILE was set, but config file not found at $GEN3_CONFIG_PATH" >&2
    exit 1
  fi

  local profile_token profile_endpoint profile_api_key
  profile_token="$(load_profile_field "$GEN3_PROFILE" "access_token" "$GEN3_CONFIG_PATH")"
  profile_endpoint="$(load_profile_field "$GEN3_PROFILE" "api_endpoint" "$GEN3_CONFIG_PATH")"
  profile_api_key="$(load_profile_field "$GEN3_PROFILE" "api_key" "$GEN3_CONFIG_PATH")"

  if [[ -z "${TEST_DRS_URL:-}" && -n "$profile_endpoint" ]]; then
    DRS_URL="$profile_endpoint"
  fi

  if [[ -n "$profile_api_key" ]]; then
    local refresh_url refresh_body refresh_out refresh_status refreshed
    refresh_url="${DRS_URL%/}/user/credentials/api/access_token"
    refresh_body="$(jq -n --arg api_key "$profile_api_key" '{api_key:$api_key}')"
    refresh_out="$(mktemp)"
    refresh_status="$(curl -sS -o "$refresh_out" -w '%{http_code}' \
      -X POST \
      -H "Accept: application/json" \
      -H "Content-Type: application/json" \
      "$refresh_url" \
      -d "$refresh_body" || true)"
    if [[ "$refresh_status" == "200" ]]; then
      refreshed="$(jq -r '.access_token // empty' "$refresh_out" 2>/dev/null || true)"
      if [[ -n "$refreshed" ]]; then
        GEN3_TOKEN="$refreshed"
        GEN3_TOKEN_SOURCE="profile:${GEN3_PROFILE}:api_key_refresh"
        rm -f "$refresh_out"
        return
      fi
    fi
    rm -f "$refresh_out"
  fi

  if [[ -z "$profile_token" ]]; then
    echo "error: profile '$GEN3_PROFILE' does not contain access_token in $GEN3_CONFIG_PATH" >&2
    exit 1
  fi
  GEN3_TOKEN="$profile_token"
  GEN3_TOKEN_SOURCE="profile:${GEN3_PROFILE}:access_token"
}

token_fingerprint() {
  local token="$1"
  if [[ -z "$token" ]]; then
    printf 'none'
    return
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    printf '%s' "$token" | sha256sum | awk '{print $1}'
  else
    printf '%s' "$token" | shasum -a 256 | awk '{print $1}'
  fi
}

mask_token() {
  local token="$1"
  local len=${#token}
  if (( len <= 12 )); then
    printf '%s' "$token"
    return
  fi
  printf '%s...%s' "${token:0:8}" "${token:len-4:4}"
}

sanitize_remote_url() {
  local raw="$1"
  local parsed rest creds host
  if [[ "$raw" != http://* && "$raw" != https://* ]]; then
    printf '%s' "$raw"
    return
  fi
  parsed="${raw#*://}"
  if [[ "$parsed" != *"@"* ]]; then
    printf '%s' "$raw"
    return
  fi
  rest="${parsed#*@}"
  creds="${parsed%@*}"
  host="${rest%%/*}"
  if [[ "$creds" == *:* ]]; then
    printf '%s://%s:[REDACTED]@%s%s' "${raw%%://*}" "${creds%%:*}" "$host" "${rest#"$host"}"
    return
  fi
  printf '%s://[REDACTED]@%s%s' "${raw%%://*}" "$host" "${rest#"$host"}"
}

log_auth_context() {
  if [[ "$SERVER_MODE" != "remote" ]]; then
    return
  fi
  if [[ "$TEST_DEBUG_AUTH" != "true" ]]; then
    return
  fi
  local fp masked
  fp="$(token_fingerprint "$GEN3_TOKEN")"
  masked="$(mask_token "$GEN3_TOKEN")"
  log "Auth context: source=$GEN3_TOKEN_SOURCE profile=${GEN3_PROFILE:-<none>} endpoint=${DRS_URL%/}"
  log "Auth token: masked=$masked fingerprint_sha256=$fp"
  if [[ "$TEST_PRINT_TOKEN" == "true" ]]; then
    log_warn "TEST_PRINT_TOKEN=true token=$GEN3_TOKEN"
  fi
}

github_create_repo_if_needed() {
  if [[ "$GITHUB_MODE" != "true" ]]; then
    return
  fi
  require_cmd gh
  if [[ -z "$TEST_GITHUB_OWNER" ]]; then
    if [[ -n "$TEST_GITHUB_TOKEN" ]]; then
      TEST_GITHUB_OWNER="$(GH_TOKEN="$TEST_GITHUB_TOKEN" gh api /user -q .login)"
    else
      TEST_GITHUB_OWNER="$(gh api /user -q .login)"
    fi
  fi
  require_env TEST_GITHUB_OWNER "$TEST_GITHUB_OWNER"

  local github_owner_type endpoint
  github_owner_type="$TEST_GITHUB_IS_ORG"
  if [[ "$github_owner_type" == "auto" ]]; then
    if [[ -n "$TEST_GITHUB_TOKEN" ]]; then
      github_owner_type="$(GH_TOKEN="$TEST_GITHUB_TOKEN" gh api "/users/${TEST_GITHUB_OWNER}" -q .type | tr '[:upper:]' '[:lower:]')"
    else
      github_owner_type="$(gh api "/users/${TEST_GITHUB_OWNER}" -q .type | tr '[:upper:]' '[:lower:]')"
    fi
    case "$github_owner_type" in
      organization) github_owner_type="true" ;;
      user) github_owner_type="false" ;;
      *)
        echo "error: unable to infer GitHub owner type for '$TEST_GITHUB_OWNER'" >&2
        exit 1
        ;;
    esac
  fi

  local is_org="false"
  if [[ "$github_owner_type" == "true" ]]; then
    is_org="true"
  fi

  if [[ "$is_org" == "true" ]]; then
    endpoint="/orgs/${TEST_GITHUB_OWNER}/repos"
  else
    endpoint="/user/repos"
  fi

  # Hard reset behavior for temp E2E repos: delete any pre-existing repo with the same name.
  # Ignore errors so 404/non-existent remains a no-op.
  if [[ -n "$TEST_GITHUB_TOKEN" ]]; then
    GH_TOKEN="$TEST_GITHUB_TOKEN" gh api -X DELETE "/repos/${TEST_GITHUB_OWNER}/${TEST_GITHUB_REPO_NAME}" >/dev/null 2>&1 || true
  else
    gh api -X DELETE "/repos/${TEST_GITHUB_OWNER}/${TEST_GITHUB_REPO_NAME}" >/dev/null 2>&1 || true
  fi

  log "Creating temporary GitHub repo ${TEST_GITHUB_OWNER}/${TEST_GITHUB_REPO_NAME} (owner_is_org=$is_org)"
  if [[ -n "$TEST_GITHUB_TOKEN" ]]; then
    GH_TOKEN="$TEST_GITHUB_TOKEN" gh api -X POST "$endpoint" \
      -f "name=${TEST_GITHUB_REPO_NAME}" \
      -f "private=true" >/dev/null
  else
    gh api -X POST "$endpoint" \
      -f "name=${TEST_GITHUB_REPO_NAME}" \
      -f "private=true" >/dev/null
  fi

  CREATED_GITHUB_REPO=true
  GITHUB_OWNER_REPO="${TEST_GITHUB_OWNER}/${TEST_GITHUB_REPO_NAME}"
  if [[ -n "$TEST_GITHUB_TOKEN" ]]; then
    REMOTE_URL="https://x-access-token:${TEST_GITHUB_TOKEN}@github.com/${GITHUB_OWNER_REPO}.git"
  else
    REMOTE_URL="https://github.com/${GITHUB_OWNER_REPO}.git"
  fi
  log "GitHub remote created: https://github.com/${GITHUB_OWNER_REPO}.git"
  local repo_id attempt
  repo_id=""
  for attempt in {1..10}; do
    if [[ -n "$TEST_GITHUB_TOKEN" ]]; then
      repo_id="$(GH_TOKEN="$TEST_GITHUB_TOKEN" gh api "/repos/${GITHUB_OWNER_REPO}" -q .id 2>/dev/null || true)"
    else
      repo_id="$(gh api "/repos/${GITHUB_OWNER_REPO}" -q .id 2>/dev/null || true)"
    fi
    if [[ "$repo_id" =~ ^[0-9]+$ ]]; then
      break
    fi
    repo_id=""
    sleep 1
  done
  if [[ -z "$repo_id" || ! "$repo_id" =~ ^[0-9]+$ ]]; then
    echo "error: failed to verify GitHub repo ${GITHUB_OWNER_REPO} after creation (still not visible via API)" >&2
    exit 1
  fi
  log "GitHub repo id confirmed: ${repo_id}"
}

github_verify_lfs_pointer() {
  local file_path="$1"
  if [[ "$GITHUB_MODE" != "true" ]]; then
    return
  fi

  # Validate the blob in the commit we just pushed is an LFS pointer.
  local local_blob_sha local_content
  local_blob_sha="$(git rev-parse "HEAD:${file_path}")"
  local_content="$(git show "HEAD:${file_path}")"
  if ! grep -q "https://git-lfs.github.com/spec/v1" <<<"$local_content"; then
    echo "assertion failed: expected ${file_path} in local HEAD to be an LFS pointer" >&2
    exit 1
  fi

  # Validate GitHub main references the same blob SHA; this confirms the remote
  # repository received the same pointer blob that we validated locally.
  local remote_blob_sha
  if [[ -n "$TEST_GITHUB_TOKEN" ]]; then
    remote_blob_sha="$(GH_TOKEN="$TEST_GITHUB_TOKEN" gh api "/repos/${GITHUB_OWNER_REPO}/contents/${file_path}?ref=main" -q .sha)"
  else
    remote_blob_sha="$(gh api "/repos/${GITHUB_OWNER_REPO}/contents/${file_path}?ref=main" -q .sha)"
  fi
  if [[ -z "$remote_blob_sha" ]]; then
    echo "assertion failed: could not resolve GitHub blob sha for ${file_path}" >&2
    exit 1
  fi
  if [[ "$local_blob_sha" != "$remote_blob_sha" ]]; then
    echo "assertion failed: blob sha mismatch for ${file_path}" >&2
    echo "local:  $local_blob_sha" >&2
    echo "remote: $remote_blob_sha" >&2
    exit 1
  fi
}

detect_api_capabilities() {
  if [[ "$SERVER_MODE" == "local" ]]; then
    HAS_DRS_API=true
    HAS_DRS_BULK_DELETE=true
    HAS_DRS_BULK_ACCESS=true
    return
  fi
  local out status delete_out delete_status delete_body access_out access_status access_body
  out="$(mktemp)"
  status="$(curl -sS -o "$out" -w '%{http_code}' \
    -H "Authorization: Bearer $GEN3_TOKEN" \
    -H "Accept: application/json" \
    "${API_BASE}/service-info" || true)"
  if [[ "$status" == "200" ]]; then
    HAS_DRS_API=true
  else
    HAS_DRS_API=false
  fi
  rm -f "$out"

  HAS_DRS_BULK_DELETE=false
  HAS_DRS_BULK_ACCESS=false
  if [[ "$HAS_DRS_API" == "true" ]]; then
    delete_body='{"bulk_object_ids":[],"delete_storage_data":false}'
    delete_out="$(mktemp)"
    delete_status="$(curl -sS -o "$delete_out" -w '%{http_code}' \
      -X POST \
      -H "Authorization: Bearer $GEN3_TOKEN" \
      -H "Accept: application/json" \
      -H "Content-Type: application/json" \
      "${API_BASE}/objects/delete" \
      -d "$delete_body" || true)"
    rm -f "$delete_out"
    case "$delete_status" in
      200|204|400|401|403|422) HAS_DRS_BULK_DELETE=true ;;
      *) HAS_DRS_BULK_DELETE=false ;;
    esac

    access_body='{"bulk_object_access_ids":[]}'
    access_out="$(mktemp)"
    access_status="$(curl -sS -o "$access_out" -w '%{http_code}' \
      -X POST \
      -H "Authorization: Bearer $GEN3_TOKEN" \
      -H "Accept: application/json" \
      -H "Content-Type: application/json" \
      "${API_BASE}/objects/access" \
      -d "$access_body" || true)"
    rm -f "$access_out"
    case "$access_status" in
      200|400|401|403|422) HAS_DRS_BULK_ACCESS=true ;;
      *) HAS_DRS_BULK_ACCESS=false ;;
    esac
  fi

  log "API capability: DRS=$HAS_DRS_API bulk_delete=$HAS_DRS_BULK_DELETE bulk_access=$HAS_DRS_BULK_ACCESS"
}

auth_preflight() {
  if [[ "$SERVER_MODE" == "local" ]]; then
    return
  fi
  local probe_oid probe status out body
  probe_oid="e2e-auth-probe"
  probe="${DRS_URL%/}/data/upload/${probe_oid}?bucket=${BUCKET}&file_name=${probe_oid}"
  out="$(mktemp)"
  status="$(curl -sS -o "$out" -w '%{http_code}' \
    -H "Authorization: Bearer $GEN3_TOKEN" \
    -H "Accept: application/json" \
    "$probe" || true)"
  body="$(cat "$out")"
  rm -f "$out"
  case "$status" in
    200|400|404) return ;;
    401|403)
      echo "error: auth preflight failed (status=$status) for upload-signing endpoint." >&2
      echo "hint: verify token permissions, or use GEN3_PROFILE with a valid api_key for token refresh." >&2
      exit 1
      ;;
    500)
      if grep -qi "credential not found" <<<"$body"; then
        echo "error: upload-signing preflight failed: bucket credential for '$BUCKET' is missing on drs-server." >&2
        echo "hint: add/map bucket credentials first, or set TEST_CREATE_BUCKET_BEFORE_TEST=true with TEST_BUCKET_* values." >&2
        exit 1
      fi
      echo "error: upload-signing preflight failed (status=500) body=$body" >&2
      exit 1
      ;;
    *) return ;;
  esac
}

bucket_preflight() {
  if [[ "$SERVER_MODE" != "remote" ]]; then
    return
  fi
  local out status
  out="$(mktemp)"
  status="$(curl -sS -o "$out" -w '%{http_code}' \
    -H "Authorization: Bearer $GEN3_TOKEN" \
    -H "Accept: application/json" \
    "$BUCKET_API_BASE" || true)"

  case "$status" in
    200)
      if ! jq -e --arg b "$BUCKET" '.S3_BUCKETS[$b] or .s3_buckets[$b]' "$out" >/dev/null 2>&1; then
        echo "error: bucket '$BUCKET' is not configured in server bucket credentials for this token context." >&2
        echo "hint: run bucket add first (or set TEST_CREATE_BUCKET_BEFORE_TEST=true with TEST_BUCKET_* vars)." >&2
        rm -f "$out"
        exit 1
      fi
      ;;
    401|403)
      if [[ "$TEST_ALLOW_BUCKET_PREFLIGHT_FORBIDDEN" == "true" ]]; then
        log_warn "Bucket preflight could not list buckets (status=$status). Continuing because TEST_ALLOW_BUCKET_PREFLIGHT_FORBIDDEN=true."
      else
        echo "error: bucket preflight denied (status=$status) on $BUCKET_API_BASE" >&2
        echo "hint: token lacks permission to read bucket mappings; fix token scope/permissions or set TEST_ALLOW_BUCKET_PREFLIGHT_FORBIDDEN=true to bypass." >&2
        rm -f "$out"
        exit 1
      fi
      ;;
    *)
      log_warn "Bucket preflight unexpected status=$status on $BUCKET_API_BASE. Continuing."
      ;;
  esac
  rm -f "$out"
}

resolve_bucket_api_base() {
  if [[ "$SERVER_MODE" != "remote" ]]; then
    return
  fi

  local -a candidates=("${DRS_URL%/}/data/buckets")
  local candidate out status

  for candidate in "${candidates[@]}"; do
    out="$(mktemp)"
    status="$(curl -sS -o "$out" -w '%{http_code}' \
      -H "Authorization: Bearer $GEN3_TOKEN" \
      -H "Accept: application/json" \
      "$candidate" || true)"
    rm -f "$out"

    case "$status" in
      200|401|403)
        BUCKET_API_BASE="$candidate"
        log "Resolved bucket endpoint: $BUCKET_API_BASE (status=$status)"
        return
        ;;
      404)
        ;;
      *)
        log_warn "Bucket endpoint probe returned status=$status for $candidate"
        ;;
    esac
  done

  log_warn "Unable to auto-resolve bucket endpoint; continuing with default $BUCKET_API_BASE"
}

multipart_preflight() {
  if [[ "$SERVER_MODE" != "remote" ]]; then
    return
  fi
  if ! expects_multipart_upload; then
    return
  fi

  local url status out body
  url="${DRS_URL%/}/data/multipart/init"
  out="$(mktemp)"
  status="$(curl -sS -o "$out" -w '%{http_code}' \
    -X POST \
    -H "Authorization: Bearer $GEN3_TOKEN" \
    -H "Accept: application/json" \
    -H "Content-Type: application/json" \
    "$url" \
    -d "{\"file_name\":\"e2e-multipart-preflight.bin\",\"bucket\":\"$BUCKET\"}" || true)"
  body="$(cat "$out")"
  rm -f "$out"

  case "$status" in
    200|201|400|422) return ;;
    401|403)
      echo "error: multipart preflight failed (status=$status) on /data/multipart/init." >&2
      echo "hint: token is present but lacks permissions for multipart upload init." >&2
      exit 1
      ;;
    500)
      if grep -qi "credential not found" <<<"$body"; then
        echo "error: multipart preflight failed: bucket credential for '$BUCKET' is missing on drs-server." >&2
        echo "hint: add/map bucket credentials first, or set TEST_CREATE_BUCKET_BEFORE_TEST=true with TEST_BUCKET_* values." >&2
        exit 1
      fi
      echo "error: multipart preflight failed (status=500) on /data/multipart/init: $body" >&2
      exit 1
      ;;
    404)
      echo "error: multipart preflight failed (status=404). Endpoint /data/multipart/init is unavailable." >&2
      echo "hint: enable compatible multipart routes on drs-server for remote Gen3 workflows." >&2
      exit 1
      ;;
    *)
      echo "error: multipart preflight returned unexpected status=$status on /data/multipart/init" >&2
      exit 1
      ;;
  esac
}

grant_collaborator_access_if_needed() {
  if [[ "$SERVER_MODE" != "remote" ]]; then
    return
  fi

  local should_run=false
  case "$TEST_COLLABORATOR_GRANT" in
    true) should_run=true ;;
    false) should_run=false ;;
    auto)
      if [[ -n "$GEN3_PROFILE" && -n "$TEST_COLLAB_USER_EMAIL" ]]; then
        should_run=true
      fi
      ;;
  esac

  if [[ "$should_run" != "true" ]]; then
    return
  fi

  if [[ -z "$GEN3_PROFILE" ]]; then
    local m="collaborator grant requested, but GEN3_PROFILE is not set"
    if [[ "$TEST_COLLAB_STRICT" == "true" || "$TEST_COLLABORATOR_GRANT" == "true" ]]; then
      echo "error: $m" >&2
      exit 1
    fi
    log_warn "$m (skipping collaborator grant)"
    return
  fi
  if [[ -z "$TEST_COLLAB_USER_EMAIL" ]]; then
    local m2="collaborator grant requested, but TEST_COLLAB_USER_EMAIL is not set"
    if [[ "$TEST_COLLAB_STRICT" == "true" || "$TEST_COLLABORATOR_GRANT" == "true" ]]; then
      echo "error: $m2" >&2
      exit 1
    fi
    log_warn "$m2 (skipping collaborator grant)"
    return
  fi
  if [[ -z "$TEST_COLLAB_PROJECT_ID" ]]; then
    local m3="collaborator grant requested, but TEST_COLLAB_PROJECT_ID resolved empty"
    if [[ "$TEST_COLLAB_STRICT" == "true" || "$TEST_COLLABORATOR_GRANT" == "true" ]]; then
      echo "error: $m3" >&2
      exit 1
    fi
    log_warn "$m3 (skipping collaborator grant)"
    return
  fi

  require_cmd "$TEST_COLLAB_CMD"

  local existing existing_reader existing_writer
  log "Checking collaborator access before grant: project=$TEST_COLLAB_PROJECT_ID user=$TEST_COLLAB_USER_EMAIL"
  if existing="$("$TEST_COLLAB_CMD" --profile "$GEN3_PROFILE" collaborator ls --active --username "$TEST_COLLAB_USER_EMAIL" 2>&1)"; then
    existing_reader="policy_id: programs.${ORGANIZATION}.projects.${PROJECT_ID}_reader"
    existing_writer="policy_id: programs.${ORGANIZATION}.projects.${PROJECT_ID}_writer"
    if [[ "$TEST_COLLAB_WRITE" == "true" ]]; then
      if grep -Fq "$existing_reader" <<<"$existing" && grep -Fq "$existing_writer" <<<"$existing"; then
        log "Collaborator already has reader+writer access; skipping add/approve"
        return
      fi
    else
      if grep -Fq "$existing_reader" <<<"$existing"; then
        log "Collaborator already has reader access; skipping add/approve"
        return
      fi
    fi
    log "Collaborator access not present yet; proceeding with add/approve"
  else
    log_warn "Could not list existing collaborator access before grant; continuing with add/approve"
  fi

  local args=(
    --profile "$GEN3_PROFILE"
    collaborator add "$TEST_COLLAB_PROJECT_ID" "$TEST_COLLAB_USER_EMAIL"
  )
  if [[ "$TEST_COLLAB_WRITE" == "true" ]]; then
    args+=(--write)
  fi
  if [[ "$TEST_COLLAB_APPROVE" == "true" ]]; then
    args+=(--approve)
  fi

  log "Granting collaborator access: cmd=$TEST_COLLAB_CMD project=$TEST_COLLAB_PROJECT_ID user=$TEST_COLLAB_USER_EMAIL write=$TEST_COLLAB_WRITE approve=$TEST_COLLAB_APPROVE"
  if "$TEST_COLLAB_CMD" "${args[@]}"; then
    log "Collaborator grant command completed"
    return
  fi

  local emsg="collaborator grant command failed (project=$TEST_COLLAB_PROJECT_ID user=$TEST_COLLAB_USER_EMAIL)"
  if [[ "$TEST_COLLAB_STRICT" == "true" || "$TEST_COLLABORATOR_GRANT" == "true" ]]; then
    echo "error: $emsg" >&2
    exit 1
  fi
  log_warn "$emsg; continuing because TEST_COLLAB_STRICT=false"
}

sha256_file() {
  local path="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$path" | awk '{print $1}'
  else
    shasum -a 256 "$path" | awk '{print $1}'
  fi
}

file_size() {
  wc -c <"$1" | tr -d '[:space:]'
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

  local curl_args=(-sS -o "$out" -w '%{http_code}' -X "$method" -H "Accept: application/json")
  if [[ "$SERVER_MODE" == "remote" ]]; then
    curl_args+=(-H "Authorization: Bearer $GEN3_TOKEN")
  elif [[ -n "$ADMIN_AUTH_HEADER" ]]; then
    curl_args+=(-H "$ADMIN_AUTH_HEADER")
  fi
  if [[ -n "$body" ]]; then
    curl_args+=(-H "Content-Type: application/json" "$url" -d "$body")
  else
    curl_args+=("$url")
  fi
  status="$(curl "${curl_args[@]}")"

  if ! status_in "$status" "$expect_codes"; then
    echo "request failed: $method $url (status=$status, expected=$expect_codes)" >&2
    cat "$out" >&2
    rm -f "$out"
    exit 1
  fi
  cat "$out"
  rm -f "$out"
}

API_HTTP_STATUS=""
api_json_noexit() {
  local method="$1"
  local url="$2"
  local body="${3:-}"
  local out status
  out="$(mktemp)"
  local curl_args=(-sS -o "$out" -w '%{http_code}' -X "$method" -H "Accept: application/json")
  if [[ "$SERVER_MODE" == "remote" ]]; then
    curl_args+=(-H "Authorization: Bearer $GEN3_TOKEN")
  elif [[ -n "$ADMIN_AUTH_HEADER" ]]; then
    curl_args+=(-H "$ADMIN_AUTH_HEADER")
  fi
  if [[ -n "$body" ]]; then
    curl_args+=(-H "Content-Type: application/json" "$url" -d "$body")
  else
    curl_args+=("$url")
  fi
  status="$(curl "${curl_args[@]}" || true)"
  API_HTTP_STATUS="$status"
  cat "$out"
  rm -f "$out"
}

drs_id_from_oid() {
  local oid="$1"
  local cached
  cached="$(map_get_or_empty DRS_ID_BY_OID "$oid")"
  if [[ -n "$cached" ]]; then
    printf '%s\n' "$cached"
    return 0
  fi

  local resp did
  resp="$(api_json GET "${INDEXD_BASE}?hash=sha256:${oid}" "" "200")"
  did="$(echo "$resp" | jq -r 'first((.records // [])[] | (.did // .id // empty)) // empty')"
  if [[ -z "$did" || "$did" == "null" ]]; then
    echo "error: failed to resolve DRS id for oid ${oid} via ${INDEXD_BASE}?hash=sha256:${oid}" >&2
    echo "$resp" >&2
    exit 1
  fi

  map_set DRS_ID_BY_OID "$oid" "$did"
  printf '%s\n' "$did"
}

dids_from_oid() {
  local oid="$1"
  local out status resp
  out="$(mktemp)"
  local curl_args=(-sS -o "$out" -w '%{http_code}' -X GET -H "Accept: application/json")
  if [[ "$SERVER_MODE" == "remote" ]]; then
    curl_args+=(-H "Authorization: Bearer $GEN3_TOKEN")
  elif [[ -n "$ADMIN_AUTH_HEADER" ]]; then
    curl_args+=(-H "$ADMIN_AUTH_HEADER")
  fi
  curl_args+=("${INDEXD_BASE}?hash=sha256:${oid}")
  status="$(curl "${curl_args[@]}" || true)"
  resp="$(cat "$out")"
  rm -f "$out"
  printf '[%s] cleanup-resolve: oid=%s status=%s\n' "${LOG_PREFIX:-e2e-full}" "$oid" "${status:-unknown}" >&2
  if [[ "$status" != "200" ]]; then
    echo "[cleanup-resolve-body] $resp" >&2
    return 1
  fi
  jq -r '((.records // []) | map(.did // .id // empty) | map(select(length>0) | tostring) | unique | .[]) // empty' <<<"$resp"
}

lfs_json() {
  local method="$1"
  local url="$2"
  local body="$3"
  local expect_codes="${4:-200}"
  local out
  out="$(mktemp)"
  local status
  local curl_args=(-sS -o "$out" -w '%{http_code}' -X "$method" -H "Accept: application/vnd.git-lfs+json" -H "Content-Type: application/vnd.git-lfs+json")
  if [[ "$SERVER_MODE" == "remote" ]]; then
    curl_args+=(-H "Authorization: Bearer $GEN3_TOKEN")
  elif [[ -n "$ADMIN_AUTH_HEADER" ]]; then
    curl_args+=(-H "$ADMIN_AUTH_HEADER")
  fi
  curl_args+=("$url" -d "$body")
  status="$(curl "${curl_args[@]}")"
  if ! status_in "$status" "$expect_codes"; then
    echo "LFS request failed: $method $url (status=$status, expected=$expect_codes)" >&2
    cat "$out" >&2
    rm -f "$out"
    exit 1
  fi
  cat "$out"
  rm -f "$out"
}

lfs_batch_diag() {
  local endpoint="$1"
  shift
  local -a paths=("$@")
  local -a object_rows=()
  local p oid size

  for p in "${paths[@]}"; do
    oid="$(map_get_or_empty HASH_SRC "$p")"
    size="$(map_get_or_empty SIZE_SRC "$p")"
    if [[ -z "$oid" || -z "$size" ]]; then
      continue
    fi
    object_rows+=("{\"oid\":\"${oid}\",\"size\":${size}}")
  done

  if [[ "${#object_rows[@]}" -eq 0 ]]; then
    log_warn "Skipping LFS batch diagnostic: no objects available"
    return 0
  fi

  local body out status
  body="$(
    printf '%s\n' "${object_rows[@]}" | jq -s \
      '{operation:"download",transfers:["basic"],ref:{name:"refs/heads/main"},objects:.}'
  )"

  out="$(mktemp)"
  local curl_args=(-sS -o "$out" -w '%{http_code}' -X POST \
    -H "Accept: application/vnd.git-lfs+json" \
    -H "Content-Type: application/vnd.git-lfs+json")
  if [[ "$SERVER_MODE" == "remote" ]]; then
    curl_args+=(-H "Authorization: Bearer $GEN3_TOKEN")
  elif [[ -n "$ADMIN_AUTH_HEADER" ]]; then
    curl_args+=(-H "$ADMIN_AUTH_HEADER")
  fi
  curl_args+=("${endpoint%/}/objects/batch" -d "$body")
  status="$(curl "${curl_args[@]}")"

  log "LFS batch diagnostic request endpoint: ${endpoint%/}/objects/batch"
  log "LFS batch diagnostic request body: $body"
  log "LFS batch diagnostic response status: $status"
  log "LFS batch diagnostic response body:"
  if [[ -z "$LOG_PREFIX" ]]; then
    if [[ "$SERVER_MODE" == "local" ]]; then
      LOG_PREFIX="e2e-local-full"
    else
      LOG_PREFIX="e2e-gen3-remote"
    fi
  fi
  sed "s/^/[${LOG_PREFIX}][lfs-batch] /" "$out"
  rm -f "$out"
}

main() {
  phase "validation"
  require_cmd git
  require_cmd git-drs
  require_cmd curl
  require_cmd jq
  if [[ "$LFS_PULL_COMPAT" == "true" ]]; then
    require_cmd git-lfs
  fi
  validate_required_config
  phase "auth-setup"
  resolve_auth_from_profile_if_needed
  if [[ "$SERVER_MODE" == "local" && -z "$ADMIN_AUTH_HEADER" && -n "$LOCAL_USERNAME" && -n "$LOCAL_PASSWORD" ]]; then
    ADMIN_AUTH_HEADER="$(basic_auth_header "$LOCAL_USERNAME" "$LOCAL_PASSWORD")"
  fi
  if [[ "$SERVER_MODE" == "local" && -z "$LOCAL_USERNAME" && -z "$LOCAL_PASSWORD" && "$ADMIN_AUTH_HEADER" =~ ^[Aa]uthorization:[[:space:]]*[Bb]asic[[:space:]]+(.+)$ ]]; then
    local basic_b64 basic_decoded
    basic_b64="${BASH_REMATCH[1]}"
    basic_decoded="$(decode_base64 "$basic_b64" 2>/dev/null || true)"
    if [[ "$basic_decoded" == *:* ]]; then
      LOCAL_USERNAME="${basic_decoded%%:*}"
      LOCAL_PASSWORD="${basic_decoded#*:}"
    fi
  fi
  if [[ "$SERVER_MODE" == "remote" && -z "$GEN3_TOKEN" ]]; then
    echo "error: auth is required. set TEST_GEN3_TOKEN or GEN3_PROFILE" >&2
    exit 1
  fi
  log_auth_context
  phase "repo-provisioning"
  github_create_repo_if_needed
  local safe_remote
  safe_remote="$(sanitize_remote_url "$REMOTE_URL")"
  log "Run config: mode=$SERVER_MODE push_mode=$PUSH_MODE github_mode=$GITHUB_MODE drs_url=${DRS_URL%/} remote=$safe_remote"
  if [[ "$SERVER_MODE" == "remote" && "$GITHUB_MODE" != "true" && "$REMOTE_URL" != git@* && "$REMOTE_URL" != http* ]]; then
    log_warn "Using a local bare git remote in remote mode. Set TEST_GITHUB_MODE=true (or TEST_REMOTE_URL) to exercise hosted Git flow."
  fi
  declare -a ALL_FILES=()

  local active_bucket="$BUCKET"
  phase "preflight"
  resolve_bucket_api_base

  # If requested, create bucket credentials/mapping before any auth/upload preflight checks.
  # Otherwise preflights can fail with "credential not found" before we even attempt creation.
  if [[ "$CREATE_BUCKET_BEFORE_TEST" == "true" ]]; then
    log "Creating bucket credential '$TEST_BUCKET_NAME' via bucket API"
    local create_bucket_body
    create_bucket_body="$(jq -n \
      --arg bucket "$TEST_BUCKET_NAME" \
      --arg region "$TEST_BUCKET_REGION" \
      --arg access_key "$TEST_BUCKET_ACCESS_KEY" \
      --arg secret_key "$TEST_BUCKET_SECRET_KEY" \
      --arg endpoint "$TEST_BUCKET_ENDPOINT" \
      --arg organization "$TEST_BUCKET_ORGANIZATION" \
      --arg project_id "$TEST_BUCKET_PROJECT_ID" \
      '{bucket:$bucket, region:$region, access_key:$access_key, secret_key:$secret_key}
      + (if $endpoint == "" then {} else {endpoint:$endpoint} end)
      + (if $organization == "" then {} else {organization:$organization} end)
      + (if $project_id == "" then {} else {project_id:$project_id} end)')"
    api_json PUT "$BUCKET_API_BASE" "$create_bucket_body" "200,201" >/dev/null

    local buckets_resp
    buckets_resp="$(api_json GET "$BUCKET_API_BASE" "" "200")"
    echo "$buckets_resp" | jq -e --arg bucket "$TEST_BUCKET_NAME" '.S3_BUCKETS[$bucket]' >/dev/null

    active_bucket="$TEST_BUCKET_NAME"
    CREATED_TEST_BUCKET=true
    log "Using dynamically configured bucket: $active_bucket"
  else
    log "Bucket creation disabled (TEST_CREATE_BUCKET_BEFORE_TEST=false); expecting preconfigured bucket='$active_bucket' on server"
  fi

  detect_api_capabilities
  bucket_preflight
  auth_preflight
  grant_collaborator_access_if_needed
  multipart_preflight

  phase "repository-setup"
  log "Working directory: $WORK_ROOT"
  mkdir -p "$SOURCE_REPO" "$CLONE_REPO"

  if [[ "$REMOTE_URL" != git@* && "$REMOTE_URL" != http* ]]; then
    log "Initializing local bare git remote at $REMOTE_URL"
    rm -rf "$REMOTE_URL"
    git init --bare "$REMOTE_URL" >/dev/null
    git --git-dir="$REMOTE_URL" symbolic-ref HEAD refs/heads/main >/dev/null
  fi

  phase "upload-and-register"
  log "Initializing source repository"
  cd "$SOURCE_REPO"
  git init -b main >/dev/null
  git config user.email "git-drs-e2e@example.local"
  git config user.name "git-drs-e2e"
  git remote add "$REMOTE_NAME" "$REMOTE_URL"

  log "Configuring git-drs remote ($SERVER_MODE mode)"
  git drs init
  configure_local_credential_helper
  git config --local lfs.basictransfersonly true
  if [[ "$SERVER_MODE" == "remote" ]]; then
    git drs remote add gen3 "$REMOTE_NAME" --token "$GEN3_TOKEN" --organization "$ORGANIZATION" --project "$PROJECT_ID"
  else
    local -a local_add_args
    local_add_args=(git drs remote add local "$REMOTE_NAME" "$DRS_URL" --bucket "$active_bucket" --organization "$ORGANIZATION" --project "$PROJECT_ID")
    if [[ -n "$LOCAL_USERNAME" && -n "$LOCAL_PASSWORD" ]]; then
      local_add_args+=(--username "$LOCAL_USERNAME" --password "$LOCAL_PASSWORD")
    fi
    "${local_add_args[@]}"
  fi
  git config --local drs.multipart-threshold "$UPLOAD_MULTIPART_THRESHOLD_MB"
  log "Configured git-drs remote '$REMOTE_NAME' with bucket='$active_bucket' organization='$ORGANIZATION' project='$PROJECT_ID'"

  log "Creating test payloads"
  mkdir -p data
  printf 'single-part payload %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > data/single.bin
  dd if=/dev/urandom of=data/multipart.bin bs=1048576 count="$LARGE_FILE_MB" status=none
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

  local single_oid multi_oid single_size multi_size
  single_oid="$(sha256_file data/single.bin)"
  multi_oid="$(sha256_file data/multipart.bin)"
  single_size="$(file_size data/single.bin)"
  multi_size="$(file_size data/multipart.bin)"
  map_set HASH_SRC "data/single.bin" "$single_oid"
  map_set HASH_SRC "data/multipart.bin" "$multi_oid"
  map_set SIZE_SRC "data/single.bin" "$single_size"
  map_set SIZE_SRC "data/multipart.bin" "$multi_size"
  ALL_OIDS+=("$single_oid" "$multi_oid")
  ALL_FILES+=("data/single.bin" "data/multipart.bin")
  for f in data/extra-small-*.bin data/extra-large-*.bin; do
    [[ -f "$f" ]] || continue
    local h s
    h="$(sha256_file "$f")"
    s="$(file_size "$f")"
    map_set HASH_SRC "$f" "$h"
    map_set SIZE_SRC "$f" "$s"
    ALL_OIDS+=("$h")
    ALL_FILES+=("$f")
  done

  log "Tracking and committing DRS files"
  git drs track "*.bin"
  git add .gitattributes data/*.bin
  git commit -m "e2e(remote): add expanded dataset (single, multipart, extras)" >/dev/null

  git config --local push.default current
  configure_lfs_endpoint_for_repo "$REMOTE_NAME"
  log_lfs_endpoint_for_repo "$REMOTE_NAME"
  if [[ "$PUSH_MODE" == "drs" || "$PUSH_MODE" == "both" ]]; then
    log "Pushing via git-drs push (register + upload)"
    git drs push "$REMOTE_NAME"
    log "git-drs push completed"
  fi

  if [[ "$RUN_RESUME_E2E" == "true" && ("$PUSH_MODE" == "drs" || "$PUSH_MODE" == "both") ]]; then
    phase "resumable-upload"
    log "Creating resumable multipart upload payload"
    dd if=/dev/urandom of=data/resume-upload.bin bs=1048576 count="$LARGE_FILE_MB" status=none
    local resume_upload_oid resume_upload_size
    resume_upload_oid="$(sha256_file data/resume-upload.bin)"
    resume_upload_size="$(file_size data/resume-upload.bin)"
    map_set HASH_SRC "data/resume-upload.bin" "$resume_upload_oid"
    map_set SIZE_SRC "data/resume-upload.bin" "$resume_upload_size"
    ALL_OIDS+=("$resume_upload_oid")
    ALL_FILES+=("data/resume-upload.bin")
    git add data/resume-upload.bin
    git commit -m "e2e(remote): resumable multipart upload payload" >/dev/null

    log "Simulating interrupted multipart upload (expected failure)"
    if DATA_CLIENT_TEST_FAIL_UPLOAD_PART_ONCE=1 git drs push "$REMOTE_NAME"; then
      echo "error: expected first resumable multipart upload attempt to fail, but it succeeded" >&2
      echo "hint: test fault injection DATA_CLIENT_TEST_FAIL_UPLOAD_PART_ONCE may not be wired into current upload path" >&2
      exit 1
    fi
    log "Retrying multipart upload after interruption"
    git drs push "$REMOTE_NAME"
    log "Resumable multipart upload retry completed"
  fi

  if [[ "$PUSH_MODE" == "git" || "$PUSH_MODE" == "both" ]]; then
    phase "git-push-compat"
    log "Creating git-push compatibility payload"
    printf 'git-push compatibility payload %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > data/gitpush.bin
    local gitpush_oid gitpush_size
    gitpush_oid="$(sha256_file data/gitpush.bin)"
    gitpush_size="$(file_size data/gitpush.bin)"
    map_set HASH_SRC "data/gitpush.bin" "$gitpush_oid"
    map_set SIZE_SRC "data/gitpush.bin" "$gitpush_size"
    ALL_OIDS+=("$gitpush_oid")
    ALL_FILES+=("data/gitpush.bin")

    git add data/gitpush.bin
    git commit -m "e2e(remote): git push compatibility path" >/dev/null
    log "Pushing via plain git push (hook + git-lfs compatibility path)"
    git push "$REMOTE_NAME" main
    log "plain git push completed"
  fi

  github_verify_lfs_pointer "data/single.bin"
  github_verify_lfs_pointer "data/multipart.bin"
  if map_has HASH_SRC "data/resume-upload.bin"; then
    github_verify_lfs_pointer "data/resume-upload.bin"
  fi
  if [[ "$PUSH_MODE" == "git" || "$PUSH_MODE" == "both" ]]; then
    github_verify_lfs_pointer "data/gitpush.bin"
  fi

  phase "download-and-verify"
  log "Cloning fresh repository for download path"
  rm -rf "$CLONE_REPO"
  GIT_LFS_SKIP_SMUDGE=1 git clone --branch main "$REMOTE_URL" "$CLONE_REPO" >/dev/null

  cd "$CLONE_REPO"
  git config user.email "git-drs-e2e@example.local"
  git config user.name "git-drs-e2e"
  git drs init
  configure_local_credential_helper
  git config --local lfs.basictransfersonly true
  if [[ "$SERVER_MODE" == "remote" ]]; then
    git drs remote add gen3 "$REMOTE_NAME" --token "$GEN3_TOKEN" --organization "$ORGANIZATION" --project "$PROJECT_ID"
  else
    local -a local_add_args_clone
    local_add_args_clone=(git drs remote add local "$REMOTE_NAME" "$DRS_URL" --bucket "$active_bucket" --organization "$ORGANIZATION" --project "$PROJECT_ID")
    if [[ -n "$LOCAL_USERNAME" && -n "$LOCAL_PASSWORD" ]]; then
      local_add_args_clone+=(--username "$LOCAL_USERNAME" --password "$LOCAL_PASSWORD")
    fi
    "${local_add_args_clone[@]}"
  fi
  git config --local drs.multipart-threshold "$DOWNLOAD_MULTIPART_THRESHOLD_MB"

  log "Pulling via git-drs pull"
  git drs pull "$REMOTE_NAME"

  log "Verifying downloaded content hashes"
  local single_hash_clone multi_hash_clone
  single_hash_clone="$(sha256_file data/single.bin)"
  multi_hash_clone="$(sha256_file data/multipart.bin)"
  assert_eq "$single_oid" "$single_hash_clone" "single-part file hash mismatch"
  assert_eq "$multi_oid" "$multi_hash_clone" "multipart file hash mismatch"
  for f in "${ALL_FILES[@]}"; do
    local h_clone
    h_clone="$(sha256_file "$f")"
    assert_eq "$(map_get_or_empty HASH_SRC "$f")" "$h_clone" "hash mismatch for $f"
  done

  if [[ "$RUN_RESUME_E2E" == "true" ]]; then
    phase "resumable-download"
    log "Simulating interrupted download for multipart-sized object (expected failure)"
    local lfs_obj_path old_download_threshold resumed_hash
    lfs_obj_path=".git/lfs/objects/${multi_oid:0:2}/${multi_oid:2:2}/${multi_oid}"
    rm -f data/multipart.bin "$lfs_obj_path"

    old_download_threshold="$(git config --local --get drs.multipart-threshold || true)"
    # Force single-stream path so retry resumes via byte ranges from a partial file.
    git config --local drs.multipart-threshold 999999
    if DATA_CLIENT_TEST_FAIL_DOWNLOAD_AFTER_BYTES="$RESUME_FAIL_DOWNLOAD_AFTER_BYTES" git drs pull "$REMOTE_NAME"; then
      echo "error: expected first resumable download attempt to fail" >&2
      exit 1
    fi
    log "Retrying download after interruption"
    git drs pull "$REMOTE_NAME"
    if [[ -n "$old_download_threshold" ]]; then
      git config --local drs.multipart-threshold "$old_download_threshold"
    else
      git config --local --unset drs.multipart-threshold >/dev/null 2>&1 || true
    fi
    resumed_hash="$(sha256_file data/multipart.bin)"
    assert_eq "$multi_oid" "$resumed_hash" "resumable download hash mismatch"
    log "Resumable download retry completed"
  fi

  if [[ "$LFS_PULL_COMPAT" == "true" ]]; then
    phase "lfs-compat"
    log "Running stock git-lfs pull compatibility check"
    rm -rf "${CLONE_REPO}-lfs"
    GIT_LFS_SKIP_SMUDGE=1 git clone --branch main "$REMOTE_URL" "${CLONE_REPO}-lfs" >/dev/null
    cd "${CLONE_REPO}-lfs"
    git config user.email "git-drs-e2e@example.local"
    git config user.name "git-drs-e2e"
    git drs init
    configure_local_credential_helper
    git config --local lfs.basictransfersonly true
    if [[ "$SERVER_MODE" == "remote" ]]; then
      git drs remote add gen3 "$REMOTE_NAME" --token "$GEN3_TOKEN" --organization "$ORGANIZATION" --project "$PROJECT_ID"
    else
      local -a local_add_args_lfs
      local_add_args_lfs=(git drs remote add local "$REMOTE_NAME" "$DRS_URL" --bucket "$active_bucket" --organization "$ORGANIZATION" --project "$PROJECT_ID")
      if [[ -n "$LOCAL_USERNAME" && -n "$LOCAL_PASSWORD" ]]; then
        local_add_args_lfs+=(--username "$LOCAL_USERNAME" --password "$LOCAL_PASSWORD")
      fi
      "${local_add_args_lfs[@]}"
    fi
    # Force git-lfs compatibility check to use the test DRS server endpoint,
    # regardless of inherited/global config or tracked .lfsconfig values.
    configure_lfs_endpoint_for_repo "$REMOTE_NAME"
    log_lfs_endpoint_for_repo "$REMOTE_NAME"
    local -a lfs_diag_paths
    lfs_diag_paths=("data/single.bin" "data/multipart.bin")
    if map_has HASH_SRC "data/resume-upload.bin"; then
      lfs_diag_paths+=("data/resume-upload.bin")
    fi
    if map_has HASH_SRC "data/gitpush.bin"; then
      lfs_diag_paths+=("data/gitpush.bin")
    fi
    lfs_batch_diag "${DRS_URL%/}/info/lfs" "${lfs_diag_paths[@]}"
    local lfs_pull_include
    lfs_pull_include="data/single.bin,data/multipart.bin"
    if map_has HASH_SRC "data/gitpush.bin"; then
      lfs_pull_include="${lfs_pull_include},data/gitpush.bin"
    fi
    git -c "lfs.url=${DRS_URL%/}/info/lfs" \
        -c "remote.${REMOTE_NAME}.lfsurl=${DRS_URL%/}/info/lfs" \
        -c "remote.${REMOTE_NAME}.lfspushurl=${DRS_URL%/}/info/lfs" \
        lfs pull -I "$lfs_pull_include"
    log "git-lfs pull completed"
    assert_eq "$single_oid" "$(sha256_file data/single.bin)" "git-lfs single-part hash mismatch"
    assert_eq "$multi_oid" "$(sha256_file data/multipart.bin)" "git-lfs multipart hash mismatch"
    if [[ -f data/gitpush.bin ]]; then
      assert_eq "$(map_get_or_empty HASH_SRC "data/gitpush.bin")" "$(sha256_file data/gitpush.bin)" "git-lfs gitpush-path hash mismatch"
    fi
    cd "$CLONE_REPO"
  fi

  phase "api-validation"
  log "Running DRS API checks"
  local service_info single_obj multi_obj all_oids_json single_drs_id multi_drs_id all_drs_ids_json
  all_oids_json="$(printf '%s\n' "${ALL_OIDS[@]}" | jq -Rsc 'split("\n") | map(select(length>0))')"
  single_drs_id="$(drs_id_from_oid "$single_oid")"
  multi_drs_id="$(drs_id_from_oid "$multi_oid")"
  all_drs_ids_json="$(printf '%s\n' "${ALL_OIDS[@]}" | while read -r oid; do
    [[ -n "$oid" ]] || continue
    drs_id_from_oid "$oid"
  done | jq -Rsc 'split("\n") | map(select(length>0))')"
  service_info="$(api_json GET "$API_BASE/service-info" "" "200")"
  echo "$service_info" | jq -e '.name and .version' >/dev/null

  single_obj="$(api_json GET "$API_BASE/objects/$single_drs_id" "" "200")"
  multi_obj="$(api_json GET "$API_BASE/objects/$multi_drs_id" "" "200")"
  echo "$single_obj" | jq -e --arg did "$single_drs_id" '.id == $did' >/dev/null
  echo "$multi_obj" | jq -e --arg did "$multi_drs_id" '.id == $did' >/dev/null
  echo "$single_obj" | jq -e --arg bucket "$active_bucket" --arg org "$ORGANIZATION" --arg proj "$PROJECT_ID" '
    any(.auth[$org][$proj][]?; startswith("s3://" + $bucket + "/"))
  ' >/dev/null
  echo "$multi_obj" | jq -e --arg bucket "$active_bucket" --arg org "$ORGANIZATION" --arg proj "$PROJECT_ID" '
    any(.auth[$org][$proj][]?; startswith("s3://" + $bucket + "/"))
  ' >/dev/null

  api_json OPTIONS "$API_BASE/objects/$single_drs_id" "" "200,204" >/dev/null
  api_json OPTIONS "$API_BASE/objects/$multi_drs_id" "" "200,204" >/dev/null

  api_json GET "$API_BASE/objects/checksum/$single_oid" "" "200" >/dev/null
  api_json GET "$API_BASE/objects/checksum/$multi_oid" "" "200" >/dev/null

  local bulk_body bulk_resp
  bulk_body="$(jq -n --argjson ids "$all_drs_ids_json" '{bulk_object_ids: $ids}')"
  bulk_resp="$(api_json POST "$API_BASE/objects" "$bulk_body" "200")"
  echo "$bulk_resp" | jq -e '.summary.requested >= 2 and .summary.resolved >= 2' >/dev/null

  local single_access_id multi_access_id
  single_access_id="$(echo "$single_obj" | jq -r '.access_methods[0].access_id // empty')"
  multi_access_id="$(echo "$multi_obj" | jq -r '.access_methods[0].access_id // empty')"

  if [[ -n "$single_access_id" ]]; then
    api_json GET "$API_BASE/objects/$single_drs_id/access/$single_access_id" "" "200" >/dev/null
    api_json POST "$API_BASE/objects/$single_drs_id/access/$single_access_id" "{}" "200" >/dev/null
  else
    log "Skipping single-object /access check (no access_id present)"
  fi

  if [[ "$HAS_DRS_BULK_ACCESS" == "true" && -n "$single_access_id" && -n "$multi_access_id" ]]; then
    local bulk_access_body bulk_access_resp
    bulk_access_body="$(jq -n \
      --arg s "$single_drs_id" --arg sid "$single_access_id" \
      --arg m "$multi_drs_id" --arg mid "$multi_access_id" \
      '{bulk_object_access_ids: [{bulk_object_id:$s, bulk_access_ids:[$sid]}, {bulk_object_id:$m, bulk_access_ids:[$mid]}]}')"
    bulk_access_resp="$(api_json POST "$API_BASE/objects/access" "$bulk_access_body" "200")"
    echo "$bulk_access_resp" | jq -e '.summary.requested >= 2 and .summary.resolved >= 2' >/dev/null
  elif [[ "$HAS_DRS_BULK_ACCESS" != "true" ]]; then
    log "Skipping bulk /objects/access check: endpoint not available"
  else
    log "Skipping bulk /objects/access check (missing access_id on at least one object)"
  fi

  local upload_req
  upload_req="$(jq -n --arg oid "$single_oid" --argjson size "$single_size" '{
    requests: [{
      name: "dryrun-upload-request.bin",
      size: $size,
      mime_type: "application/octet-stream",
      checksums: [{type: "sha256", checksum: $oid}]
    }]
  }')"
  api_json POST "$API_BASE/upload-request" "$upload_req" "200" >/dev/null

  log "Running LFS API checks"
  local lfs_batch_download lfs_batch_upload
  lfs_batch_download="$(jq -n \
    --arg soid "$single_oid" --argjson ssize "$single_size" \
    --arg moid "$multi_oid" --argjson msize "$multi_size" \
    '{operation:"download", transfers:["basic"], objects:[{oid:$soid,size:$ssize},{oid:$moid,size:$msize}], hash_algo:"sha256"}')"
  lfs_json POST "${DRS_URL%/}/info/lfs/objects/batch" "$lfs_batch_download" "200" >/dev/null

  lfs_batch_upload="$(jq -n \
    --arg soid "$single_oid" --argjson ssize "$single_size" \
    --arg moid "$multi_oid" --argjson msize "$multi_size" \
    '{operation:"upload", transfers:["basic"], objects:[{oid:$soid,size:$ssize},{oid:$moid,size:$msize}], hash_algo:"sha256"}')"
  lfs_json POST "${DRS_URL%/}/info/lfs/objects/batch" "$lfs_batch_upload" "200" >/dev/null

  lfs_json POST "${DRS_URL%/}/info/lfs/verify" "$(jq -n --arg oid "$single_oid" --argjson size "$single_size" '{oid:$oid,size:$size}')" "200" >/dev/null
  lfs_json POST "${DRS_URL%/}/info/lfs/verify" "$(jq -n --arg oid "$multi_oid" --argjson size "$multi_size" '{oid:$oid,size:$size}')" "200" >/dev/null

  local validity_body validity_resp
  validity_body="$(jq -n --arg s "$single_oid" --arg m "$multi_oid" '{sha256: [$s, $m]}')"
  if [[ "$RUN_INTERNAL_API_CHECKS" == "true" ]]; then
    log "Running internal API checks"
    validity_resp="$(api_json POST "${INDEXD_BASE}/bulk/sha256/validity" "$validity_body" "200")"
    echo "$validity_resp" | jq -e --arg s "$single_oid" --arg m "$multi_oid" '.[$s] == true and .[$m] == true' >/dev/null

    api_json GET "${DRS_URL%/}/index/v1/metrics/files/$single_oid" "" "200,404" >/dev/null
    api_json GET "${DRS_URL%/}/index/v1/metrics/files/$multi_oid" "" "200,404" >/dev/null
    api_json GET "${DRS_URL%/}/index/v1/metrics/summary" "" "200,401,403" >/dev/null
  else
    log "Skipping internal API checks (TEST_RUN_INTERNAL_API_CHECKS=false)"
  fi

  if [[ "$FULL_SERVER_SWEEP" == "true" ]]; then
    phase "server-sweep"
    log "Running additional server sweep checks"
    api_json GET "${INDEXD_BASE}?hash=sha256:$single_oid" "" "200" >/dev/null
    api_json POST "${INDEXD_BASE}/bulk/hashes" "$(jq -n --argjson ids "$all_oids_json" '{hashes: ($ids | map("sha256:"+.))}')" "200" >/dev/null
    api_json POST "${INDEXD_BASE}/bulk/sha256/validity" "$validity_body" "200" >/dev/null
    api_json POST "${DRS_URL%/}/index/bulk/documents" "$(jq -n --argjson ids "$all_oids_json" '$ids')" "200" >/dev/null

    api_json GET "${DRS_URL%/}/data/upload/$single_oid?bucket=$active_bucket&file_name=$single_oid" "" "200,401,403,404" >/dev/null
    api_json GET "${DRS_URL%/}/data/download/$single_oid" "" "200,302,307,401,403,404" >/dev/null
    api_json GET "${DRS_URL%/}/data/upload/$single_oid?bucket=$active_bucket&file_name=$single_oid" "" "200,401,403,404" >/dev/null
    api_json GET "${DRS_URL%/}/data/download/$single_oid" "" "200,302,307,401,403,404" >/dev/null

    api_json GET "$BUCKET_API_BASE" "" "200,401,403" >/dev/null
    api_json GET "${DRS_URL%/}/index/openapi.yaml" "" "200" >/dev/null
  fi

  if [[ "$RUN_OPTIONAL_MUTATIONS" == "true" ]]; then
    phase "optional-mutations"
    log "Running optional mutation checks (requires broader write permissions)"
    local same_access_methods
    same_access_methods="$(echo "$single_obj" | jq '{access_methods: .access_methods}')"
    api_json PUT "$API_BASE/objects/$single_drs_id/access-methods" "$same_access_methods" "200,401,403,404" >/dev/null

    local bulk_access_update
    bulk_access_update="$(jq -n --arg oid "$single_drs_id" --arg aid "${single_access_id:-s3}" --arg url "$(echo "$single_obj" | jq -r '.access_methods[0].access_url.url')" \
      '{updates:[{object_id:$oid, access_methods:[{type:"s3", access_id:$aid, access_url:{url:$url}}]}]}')"
    api_json PUT "$API_BASE/objects/access-methods" "$bulk_access_update" "200,401,403,404" >/dev/null
  fi

  log "SUCCESS: comprehensive remote Gen3 e2e checks passed"
  log "- DRS URL:                          $DRS_URL"
  log "- Organization:                     $ORGANIZATION"
  log "- Project ID:                       $PROJECT_ID"
  log "- Bucket:                           $active_bucket"
  log "- Upload multipart threshold (MB):  $UPLOAD_MULTIPART_THRESHOLD_MB"
  log "- Download multipart threshold (MB):$DOWNLOAD_MULTIPART_THRESHOLD_MB"
  log "- Push mode:                        $PUSH_MODE"
  log "- Git push compatibility enabled:   $ENABLE_GIT_PUSH_COMPAT"
  log "- Resume E2E enabled:               $RUN_RESUME_E2E"
  log "- Resume fail-after bytes:          $RESUME_FAIL_DOWNLOAD_AFTER_BYTES"
  log "- Large file size (MB):             $LARGE_FILE_MB"
  log "- single oid:                       $single_oid"
  log "- multipart oid:                    $multi_oid"
  log "- total objects uploaded:           ${#ALL_OIDS[@]}"
  if [[ "$GITHUB_MODE" == "true" ]]; then
    log "- github repo:                      ${GITHUB_OWNER_REPO}"
  fi
  TEST_OUTCOME="PASS"
}

main "$@"
