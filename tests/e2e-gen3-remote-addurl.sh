#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
GIT_DRS_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"

ENV_FILE="${ENV_FILE:-$GIT_DRS_ROOT/.env}"
if [[ -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
fi

DRS_URL="${TEST_DRS_URL:-${DRS_URL:-https://caliper-training.ohsu.edu}}"
SERVER_MODE="${TEST_SERVER_MODE:-${SERVER_MODE:-remote}}"
LOG_PREFIX="${TEST_LOG_PREFIX:-}"
GEN3_TOKEN="${TEST_GEN3_TOKEN:-${GEN3_TOKEN:-}}"
GEN3_PROFILE="${TEST_GEN3_PROFILE:-${GEN3_PROFILE:-}}"
GEN3_CONFIG_PATH="${TEST_GEN3_CONFIG_PATH:-${GEN3_CONFIG_PATH:-$HOME/.gen3/gen3_client_config.ini}}"
GEN3_TOKEN_SOURCE="unset"
TEST_DEBUG_AUTH="${TEST_DEBUG_AUTH:-true}"
TEST_PRINT_TOKEN="${TEST_PRINT_TOKEN:-false}"
ADMIN_AUTH_HEADER="${TEST_ADMIN_AUTH_HEADER:-${ADMIN_AUTH_HEADER:-}}"
LOCAL_USERNAME="${TEST_LOCAL_USERNAME:-${LOCAL_USERNAME:-${DRS_BASIC_AUTH_USER:-}}}"
LOCAL_PASSWORD="${TEST_LOCAL_PASSWORD:-${LOCAL_PASSWORD:-${DRS_BASIC_AUTH_PASSWORD:-}}}"

ORGANIZATION="${TEST_ORGANIZATION:-${ORGANIZATION:-}}"
PROJECT_ID="${TEST_PROJECT_ID:-${PROJECT:-}}"
BUCKET="${TEST_BUCKET:-${BUCKET:-}}"

REMOTE_NAME="${TEST_REMOTE_NAME:-origin}"
REPO_NAME="${TEST_REPO_NAME:-git-drs-e2e-addurl-remote}"
WORK_ROOT="${TEST_WORK_ROOT:-$(mktemp -d -t git-drs-e2e-addurl-XXXX)}"
REMOTE_URL="${TEST_REMOTE_URL:-$WORK_ROOT/${REPO_NAME}.git}"
KEEP_WORKDIR="${TEST_KEEP_WORKDIR:-false}"

CREATE_BUCKET_BEFORE_TEST="${TEST_CREATE_BUCKET_BEFORE_TEST:-true}"
DELETE_TEST_BUCKET_AFTER="${TEST_DELETE_BUCKET_AFTER:-true}"
TEST_BUCKET_NAME="${TEST_BUCKET_NAME:-$BUCKET}"
TEST_BUCKET_REGION="${TEST_BUCKET_REGION:-${BUCKET_REGION:-}}"
TEST_BUCKET_ACCESS_KEY="${TEST_BUCKET_ACCESS_KEY:-${BUCKET_ACCESS_KEY:-}}"
TEST_BUCKET_SECRET_KEY="${TEST_BUCKET_SECRET_KEY:-${BUCKET_SECRET_KEY:-}}"
TEST_BUCKET_ENDPOINT="${TEST_BUCKET_ENDPOINT:-${BUCKET_ENDPOINT:-}}"
TEST_BUCKET_ORGANIZATION="${TEST_BUCKET_ORGANIZATION:-$ORGANIZATION}"
TEST_BUCKET_PROJECT_ID="${TEST_BUCKET_PROJECT_ID:-$PROJECT_ID}"

CLEANUP_RECORDS="${TEST_CLEANUP_RECORDS:-true}"
STRICT_CLEANUP="${TEST_STRICT_CLEANUP:-true}"
TEST_HTTP_MAX_TIME="${TEST_HTTP_MAX_TIME:-60}"
TEST_CMD_TIMEOUT_SECONDS="${TEST_CMD_TIMEOUT_SECONDS:-120}"
CURRENT_PHASE="bootstrap"
TEST_OUTCOME="FAIL"
FAIL_LINE=""
FAIL_CMD=""

SOURCE_REPO="$WORK_ROOT/$REPO_NAME"
CLONE_REPO="$WORK_ROOT/${REPO_NAME}-clone"
BUCKET_API_BASE="${DRS_URL%/}/data/buckets"
INDEXD_BASE="${DRS_URL%/}/index"
API_BASE="${DRS_URL%/}/ga4gh/drs/v1"
CREATED_TEST_BUCKET=false
declare -a ALL_OIDS=()

log() {
  if [[ -z "$LOG_PREFIX" ]]; then
    if [[ "$SERVER_MODE" == "local" ]]; then
      LOG_PREFIX="e2e-addurl-local"
    else
      LOG_PREFIX="e2e-addurl-remote"
    fi
  fi
  printf '[%s] %s\n' "$LOG_PREFIX" "$*"
}

log_warn() {
  if [[ -z "$LOG_PREFIX" ]]; then
    if [[ "$SERVER_MODE" == "local" ]]; then
      LOG_PREFIX="e2e-addurl-local"
    else
      LOG_PREFIX="e2e-addurl-remote"
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
  local out status
  out="$(mktemp)"
  local curl_args=(
    -sS
    --connect-timeout 10
    --max-time "$TEST_HTTP_MAX_TIME"
    -o "$out"
    -w '%{http_code}'
    -X "$method"
    -H "Accept: application/json"
  )
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
    if [[ "$status" == "401" || "$status" == "403" ]]; then
      echo "permission/auth failure: $method $url (status=$status)" >&2
      echo "hint: token/profile likely lacks required permissions for this endpoint" >&2
    fi
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
  local curl_args=(
    -sS
    --connect-timeout 10
    --max-time "$TEST_HTTP_MAX_TIME"
    -o "$out"
    -w '%{http_code}'
    -X "$method"
    -H "Accept: application/json"
  )
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

debug_http_dump() {
  local method="$1"
  local url="$2"
  local body="${3:-}"
  local out status
  out="$(mktemp)"
  local curl_args=(
    -sS
    --connect-timeout 10
    --max-time "$TEST_HTTP_MAX_TIME"
    -o "$out"
    -w '%{http_code}'
    -X "$method"
    -H "Accept: application/json"
  )
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
  log "debug-http $method $url -> status=${status:-curl_error}"
  if [[ -s "$out" ]]; then
    sed 's/^/[debug-body] /' "$out" >&2
  else
    echo "[debug-body] <empty>" >&2
  fi
  rm -f "$out"
}

debug_pull_failure_context() {
  local oid="$1"
  log "pull-debug: collecting resolver context for oid=$oid"
  debug_http_dump GET "${INDEXD_BASE}?hash=sha256:${oid}"
  debug_http_dump GET "${API_BASE}/objects/checksum/${oid}"
}

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

run_cmd_with_timeout() {
  local timeout_s="$1"
  shift
  local desc="$1"
  shift

  local out pid start now elapsed rc
  out="$(mktemp)"
  start="$(date +%s)"

  "$@" >"$out" 2>&1 &
  pid=$!

  while kill -0 "$pid" >/dev/null 2>&1; do
    now="$(date +%s)"
    elapsed=$((now - start))
    if (( elapsed >= timeout_s )); then
      kill -TERM "$pid" >/dev/null 2>&1 || true
      sleep 1
      kill -KILL "$pid" >/dev/null 2>&1 || true
      echo "error: command timed out after ${timeout_s}s: $desc" >&2
      echo "hint: likely auth/permission retry loop; check token/profile scopes." >&2
      echo "last output:" >&2
      tail -n 40 "$out" >&2 || true
      rm -f "$out"
      exit 1
    fi
    sleep 1
  done

  wait "$pid"
  rc=$?
  if (( rc != 0 )); then
    echo "error: command failed: $desc (exit=$rc)" >&2
    cat "$out" >&2
    rm -f "$out"
    exit "$rc"
  fi
  rm -f "$out"
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

log_auth_context() {
  if [[ "$SERVER_MODE" != "remote" || "$TEST_DEBUG_AUTH" != "true" ]]; then
    return
  fi
  local fp masked
  fp="$(token_fingerprint "$GEN3_TOKEN")"
  masked="$(mask_token "$GEN3_TOKEN")"
  log "Auth context: source=$GEN3_TOKEN_SOURCE profile=${GEN3_PROFILE:-<none>} endpoint=${DRS_URL%/}"
  log "Auth token: masked=$masked fingerprint_sha256=$fp"
  if [[ "$TEST_PRINT_TOKEN" == "true" ]]; then
    log "Auth token raw: $GEN3_TOKEN"
  fi
}

sha256_file() {
  local path="$1"
  shasum -a 256 "$path" | awk '{print $1}'
}

file_size() {
  local path="$1"
  stat -f '%z' "$path"
}

configure_local_credential_helper() {
  git config --local --unset-all credential.helper >/dev/null 2>&1 || true
  git config --local credential.helper "git drs credential-helper"
}

configure_lfs_endpoint_for_repo() {
  local remote_name="$1"
  local endpoint="${DRS_URL%/}/info/lfs"
  git config --local lfs.url "$endpoint"
  git config --local "remote.${remote_name}.lfsurl" "$endpoint"
  git config --local "remote.${remote_name}.lfspushurl" "$endpoint"
  git config --local "lfs.${endpoint}.access" "basic"
  if [[ -n "${GEN3_TOKEN:-}" ]]; then
    git config --local --unset-all "http.${endpoint}.extraheader" >/dev/null 2>&1 || true
    git config --local --add "http.${endpoint}.extraheader" "Authorization: Bearer ${GEN3_TOKEN}"
  elif [[ "$SERVER_MODE" == "local" && -n "$ADMIN_AUTH_HEADER" ]]; then
    git config --local --unset-all "http.${endpoint}.extraheader" >/dev/null 2>&1 || true
    git config --local --add "http.${endpoint}.extraheader" "$ADMIN_AUTH_HEADER"
  fi
}

upload_fixture_to_bucket() {
  local oid="$1"
  local bucket="$2"
  local key="$3"
  local src="$4"
  local sign_url signed_url
  sign_url="${DRS_URL%/}/data/upload/${oid}?bucket=${bucket}&file_name=${key}"
  signed_url="$(api_json GET "$sign_url" "" "200" | jq -r '.url // .Url // empty')"
  if [[ -z "$signed_url" ]]; then
    echo "error: failed to obtain signed upload URL from $sign_url" >&2
    exit 1
  fi
  curl -sS -X PUT -T "$src" "$signed_url" >/dev/null
}

dids_from_oid() {
  local oid="$1"
  local out status resp
  out="$(mktemp)"
  local curl_args=(
    -sS
    --connect-timeout 10
    --max-time "$TEST_HTTP_MAX_TIME"
    -o "$out"
    -w '%{http_code}'
    -X GET
    -H "Accept: application/json"
  )
  if [[ "$SERVER_MODE" == "remote" ]]; then
    curl_args+=(-H "Authorization: Bearer $GEN3_TOKEN")
  elif [[ -n "$ADMIN_AUTH_HEADER" ]]; then
    curl_args+=(-H "$ADMIN_AUTH_HEADER")
  fi
  curl_args+=("${INDEXD_BASE}?hash=sha256:${oid}")
  status="$(curl "${curl_args[@]}" || true)"
  resp="$(cat "$out")"
  rm -f "$out"
  printf '[%s] cleanup-resolve: oid=%s status=%s\n' "${LOG_PREFIX:-e2e-addurl}" "$oid" "${status:-unknown}" >&2
  if [[ "$status" != "200" ]]; then
    echo "[cleanup-resolve-body] $resp" >&2
    return 1
  fi
  jq -r '((.records // []) | map(.did // .id // empty) | map(select(length>0) | tostring) | unique | .[]) // empty' <<<"$resp"
}

cleanup() {
  local exit_code=$?
  if [[ "$CLEANUP_RECORDS" == "true" && "${#ALL_OIDS[@]}" -gt 0 ]]; then
    log "Cleaning up ${#ALL_OIDS[@]} test records from drs-server"
    local delete_codes verify_codes
    if [[ "$STRICT_CLEANUP" == "true" ]]; then
      delete_codes="200,204,404"
      verify_codes="404"
    else
      delete_codes="200,204,401,403,404"
      verify_codes="200,401,403,404"
    fi
    for oid in "${ALL_OIDS[@]}"; do
      local -a dids
      dids=()
      while IFS= read -r did; do
        [[ -n "$did" ]] && dids+=("$did")
      done < <(dids_from_oid "$oid" || true)
      if [[ "${#dids[@]}" -eq 0 ]]; then
        log "cleanup: oid=${oid} resolved to 0 dids (already cleaned or no records)"
        continue
      fi
      log "cleanup: oid=${oid} resolved did_count=${#dids[@]} dids=${dids[*]}"
      local did
      for did in "${dids[@]}"; do
        api_json_noexit DELETE "${INDEXD_BASE}/${did}" ""
        log "cleanup-delete: oid=${oid} did=${did} status=${API_HTTP_STATUS}"
        api_json_noexit GET "${INDEXD_BASE}/${did}" ""
        log "cleanup-verify: oid=${oid} did=${did} status=${API_HTTP_STATUS}"
      done
    done
  fi

  if [[ "$CREATE_BUCKET_BEFORE_TEST" == "true" && "$DELETE_TEST_BUCKET_AFTER" == "true" && "$CREATED_TEST_BUCKET" == "true" ]]; then
    log "Deleting test bucket credential '$TEST_BUCKET_NAME'"
    api_json DELETE "$BUCKET_API_BASE/$TEST_BUCKET_NAME" "" "200,204,404" >/dev/null || true
  fi

  if [[ "$KEEP_WORKDIR" == "true" ]]; then
    log "Keeping working directory: $WORK_ROOT"
  else
    rm -rf "$WORK_ROOT"
  fi

  if [[ "$exit_code" -eq 0 && "$TEST_OUTCOME" == "PASS" ]]; then
    log "RESULT: PASS"
  else
    log_warn "RESULT: FAIL (phase=${CURRENT_PHASE}, line=${FAIL_LINE:-unknown})"
    if [[ -n "$FAIL_CMD" ]]; then
      log_warn "Failed command: $FAIL_CMD"
    fi
  fi
}
trap cleanup EXIT

main() {
  phase "validation"
  require_cmd git
  require_cmd git-lfs
  require_cmd git-drs
  require_cmd curl
  require_cmd jq
  require_cmd shasum

  require_env TEST_ORGANIZATION "$ORGANIZATION"
  require_env TEST_PROJECT_ID "$PROJECT_ID"
  require_env TEST_BUCKET "$BUCKET"
  log "Using git-drs binary: $(command -v git-drs)"
  log "git-drs version: $(git-drs version 2>/dev/null | head -n 1 || echo unknown)"

  phase "auth-setup"
  resolve_auth_from_profile_if_needed
  if [[ "$SERVER_MODE" == "local" ]]; then
    if [[ -n "$LOCAL_USERNAME" && -z "$LOCAL_PASSWORD" ]]; then
      echo "error: TEST_LOCAL_PASSWORD is required when TEST_LOCAL_USERNAME is set" >&2
      exit 1
    fi
    if [[ -z "$LOCAL_USERNAME" && -n "$LOCAL_PASSWORD" ]]; then
      echo "error: TEST_LOCAL_USERNAME is required when TEST_LOCAL_PASSWORD is set" >&2
      exit 1
    fi
    if [[ -z "$ADMIN_AUTH_HEADER" && -n "$LOCAL_USERNAME" && -n "$LOCAL_PASSWORD" ]]; then
      ADMIN_AUTH_HEADER="$(basic_auth_header "$LOCAL_USERNAME" "$LOCAL_PASSWORD")"
    fi
    if [[ -z "$LOCAL_USERNAME" && -z "$LOCAL_PASSWORD" && "$ADMIN_AUTH_HEADER" =~ ^[Aa]uthorization:[[:space:]]*[Bb]asic[[:space:]]+(.+)$ ]]; then
      local basic_b64 basic_decoded
      basic_b64="${BASH_REMATCH[1]}"
      basic_decoded="$(decode_base64 "$basic_b64" 2>/dev/null || true)"
      if [[ "$basic_decoded" == *:* ]]; then
        LOCAL_USERNAME="${basic_decoded%%:*}"
        LOCAL_PASSWORD="${basic_decoded#*:}"
      fi
    fi
    if [[ -n "$LOCAL_USERNAME" && -n "$LOCAL_PASSWORD" ]]; then
      log "Local auth configured for git-drs remote add (username provided)"
    else
      log "Local auth not provided to git-drs remote add (TEST_LOCAL_USERNAME/TEST_LOCAL_PASSWORD unset)"
    fi
  fi
  log_auth_context
  if [[ "$SERVER_MODE" == "remote" && -z "$GEN3_TOKEN" ]]; then
    echo "error: remote mode requires TEST_GEN3_TOKEN or GEN3_PROFILE" >&2
    exit 1
  fi

  if [[ "$CREATE_BUCKET_BEFORE_TEST" == "true" ]]; then
    require_env TEST_BUCKET_NAME "$TEST_BUCKET_NAME"
    require_env TEST_BUCKET_REGION "$TEST_BUCKET_REGION"
    require_env TEST_BUCKET_ACCESS_KEY "$TEST_BUCKET_ACCESS_KEY"
    require_env TEST_BUCKET_SECRET_KEY "$TEST_BUCKET_SECRET_KEY"
  fi

  phase "repository-setup"
  log "Working directory: $WORK_ROOT"
  mkdir -p "$SOURCE_REPO" "$CLONE_REPO"

  if [[ "$REMOTE_URL" != git@* && "$REMOTE_URL" != http* ]]; then
    log "Initializing local bare git remote at $REMOTE_URL"
    rm -rf "$REMOTE_URL"
    git init --bare "$REMOTE_URL" >/dev/null
    git --git-dir="$REMOTE_URL" symbolic-ref HEAD refs/heads/main >/dev/null
  fi

  cd "$SOURCE_REPO"
  git init -b main >/dev/null
  git config user.email "git-drs-e2e@example.local"
  git config user.name "git-drs-e2e"
  git remote add "$REMOTE_NAME" "$REMOTE_URL"

  git drs init
  configure_local_credential_helper
  configure_lfs_endpoint_for_repo "$REMOTE_NAME"

  if [[ "$CREATE_BUCKET_BEFORE_TEST" == "true" ]]; then
    log "Creating bucket credential + scope via bucket API"
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
    CREATED_TEST_BUCKET=true
  fi

  if [[ "$SERVER_MODE" == "remote" ]]; then
    log "Configuring gen3 remote"
    run_cmd_with_timeout "$TEST_CMD_TIMEOUT_SECONDS" "git drs remote add gen3 (source repo)" \
      git drs remote add gen3 "$REMOTE_NAME" \
        --token "$GEN3_TOKEN" \
        --organization "$ORGANIZATION" \
        --project "$PROJECT_ID"
  else
    log "Configuring local remote"
    local -a local_add_args
    local_add_args=(
      git drs remote add local "$REMOTE_NAME" "$DRS_URL"
      --bucket "$TEST_BUCKET_NAME"
      --organization "$ORGANIZATION"
      --project "$PROJECT_ID"
    )
    if [[ -n "$LOCAL_USERNAME" && -n "$LOCAL_PASSWORD" ]]; then
      local_add_args+=(--username "$LOCAL_USERNAME" --password "$LOCAL_PASSWORD")
    fi
    run_cmd_with_timeout "$TEST_CMD_TIMEOUT_SECONDS" "git drs remote add local (source repo)" "${local_add_args[@]}"
    if [[ -n "$LOCAL_USERNAME" && -n "$LOCAL_PASSWORD" ]]; then
      local cfg_user cfg_pass
      cfg_user="$(git config --local --get "drs.remote.${REMOTE_NAME}.username" || true)"
      cfg_pass="$(git config --local --get "drs.remote.${REMOTE_NAME}.password" || true)"
      if [[ -z "$cfg_user" || -z "$cfg_pass" ]]; then
        echo "error: git drs remote add local did not persist local basic auth credentials" >&2
        exit 1
      fi
    fi
  fi

  mkdir -p data
  printf 'add-url remote object payload %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > data/seed-upload.bin
  local_oid="$(sha256_file data/seed-upload.bin)"
  local_size="$(file_size data/seed-upload.bin)"
  object_key="${ORGANIZATION}/${PROJECT_ID}/addurl/${local_oid}"
  ALL_OIDS+=("$local_oid")

  phase "seed-upload"
  log "Uploading seed object directly to bucket via signed URL (outside git-drs push)"
  upload_fixture_to_bucket "$local_oid" "$TEST_BUCKET_NAME" "$object_key" "data/seed-upload.bin"
  rm -f data/seed-upload.bin

  printf 'add-url remote unknown-sha payload %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > data/seed-upload-unknown.bin
  unknown_real_oid="$(sha256_file data/seed-upload-unknown.bin)"
  unknown_size="$(file_size data/seed-upload-unknown.bin)"
  unknown_key="${ORGANIZATION}/${PROJECT_ID}/addurl/${unknown_real_oid}"

  log "Uploading second seed object directly to bucket (unknown-sha add-url case)"
  upload_fixture_to_bucket "$unknown_real_oid" "$TEST_BUCKET_NAME" "$unknown_key" "data/seed-upload-unknown.bin"
  rm -f data/seed-upload-unknown.bin

  phase "addurl-register"
  log "Creating pointer+DRS mapping via add-url for pre-existing bucket object (known sha256)"
  git drs add-url "s3://${TEST_BUCKET_NAME}/${object_key}" "data/from-bucket.bin" --sha256 "$local_oid"
  known_pointer_oid="$(awk '/^oid sha256:/{print $2}' data/from-bucket.bin | sed 's/^sha256://')"
  if [[ "$known_pointer_oid" != "$local_oid" ]]; then
    echo "error: expected known-sha pointer oid to equal real sha256: expected $local_oid got $known_pointer_oid" >&2
    exit 1
  fi

  log "Creating pointer+DRS mapping via add-url for pre-existing bucket object (unknown sha256)"
  git drs add-url "s3://${TEST_BUCKET_NAME}/${unknown_key}" "data/from-bucket-unknown.bin"
  unknown_pointer_oid="$(awk '/^oid sha256:/{print $2}' data/from-bucket-unknown.bin | sed 's/^sha256://')"
  if [[ -z "$unknown_pointer_oid" ]]; then
    echo "error: unknown-sha add-url produced empty pointer oid" >&2
    exit 1
  fi
  if [[ "$unknown_pointer_oid" == "$unknown_real_oid" ]]; then
    echo "error: unknown-sha add-url unexpectedly used real sha256 (expected placeholder oid)" >&2
    exit 1
  fi
  ALL_OIDS+=("$unknown_pointer_oid")

  git add .gitattributes data/from-bucket.bin data/from-bucket-unknown.bin
  git commit -m "e2e(add-url): register pre-existing bucket objects (known+unknown sha)" >/dev/null

  # Mirror the primary remote e2e behavior so first push doesn't require
  # an upstream tracking branch.
  git config --local push.default current
  if [[ "$SERVER_MODE" == "local" ]]; then
    local cfg_user_before_push cfg_pass_before_push
    cfg_user_before_push="$(git config --local --get "drs.remote.${REMOTE_NAME}.username" || true)"
    cfg_pass_before_push="$(git config --local --get "drs.remote.${REMOTE_NAME}.password" || true)"
    if [[ -n "$cfg_user_before_push" && -n "$cfg_pass_before_push" ]]; then
      log "Verified local auth present in repo config for remote '$REMOTE_NAME' before push"
    else
      echo "error: local auth missing in repo config before push for remote '$REMOTE_NAME'" >&2
      echo "hint: set TEST_LOCAL_USERNAME/TEST_LOCAL_PASSWORD so git drs remote add local persists credentials" >&2
      exit 1
    fi
  fi
  log "Pushing add-url commit + metadata registration via git-drs push"
  git drs push "$REMOTE_NAME"

  phase "download-and-verify"
  log "Cloning fresh repository for pull verification"
  rm -rf "$CLONE_REPO"
  GIT_LFS_SKIP_SMUDGE=1 git clone --branch main "$REMOTE_URL" "$CLONE_REPO" >/dev/null
  cd "$CLONE_REPO"
  git config user.email "git-drs-e2e@example.local"
  git config user.name "git-drs-e2e"
  git drs init
  configure_local_credential_helper
  configure_lfs_endpoint_for_repo "$REMOTE_NAME"
  if [[ "$SERVER_MODE" == "remote" ]]; then
    run_cmd_with_timeout "$TEST_CMD_TIMEOUT_SECONDS" "git drs remote add gen3 (clone repo)" \
      git drs remote add gen3 "$REMOTE_NAME" \
        --token "$GEN3_TOKEN" \
        --organization "$ORGANIZATION" \
        --project "$PROJECT_ID"
  else
    local -a local_add_args_clone
    local_add_args_clone=(
      git drs remote add local "$REMOTE_NAME" "$DRS_URL"
      --bucket "$TEST_BUCKET_NAME"
      --organization "$ORGANIZATION"
      --project "$PROJECT_ID"
    )
    if [[ -n "$LOCAL_USERNAME" && -n "$LOCAL_PASSWORD" ]]; then
      local_add_args_clone+=(--username "$LOCAL_USERNAME" --password "$LOCAL_PASSWORD")
    fi
    run_cmd_with_timeout "$TEST_CMD_TIMEOUT_SECONDS" "git drs remote add local (clone repo)" "${local_add_args_clone[@]}"
    if [[ -n "$LOCAL_USERNAME" && -n "$LOCAL_PASSWORD" ]]; then
      local cfg_user_clone cfg_pass_clone
      cfg_user_clone="$(git config --local --get "drs.remote.${REMOTE_NAME}.username" || true)"
      cfg_pass_clone="$(git config --local --get "drs.remote.${REMOTE_NAME}.password" || true)"
      if [[ -z "$cfg_user_clone" || -z "$cfg_pass_clone" ]]; then
        echo "error: git drs remote add local (clone) did not persist local basic auth credentials" >&2
        exit 1
      fi
    fi
  fi

  log "Pulling object via git-drs pull"
  if ! git drs pull "$REMOTE_NAME"; then
    debug_pull_failure_context "$unknown_pointer_oid"
    debug_pull_failure_context "$local_oid"
    exit 1
  fi

  pulled_oid="$(sha256_file data/from-bucket.bin)"
  pulled_size="$(file_size data/from-bucket.bin)"
  if [[ "$pulled_oid" != "$local_oid" ]]; then
    echo "error: pulled content hash mismatch: expected $local_oid got $pulled_oid" >&2
    exit 1
  fi
  if [[ "$pulled_size" != "$local_size" ]]; then
    echo "error: pulled size mismatch: expected $local_size got $pulled_size" >&2
    exit 1
  fi

  pulled_unknown_oid="$(sha256_file data/from-bucket-unknown.bin)"
  pulled_unknown_size="$(file_size data/from-bucket-unknown.bin)"
  if [[ "$pulled_unknown_oid" != "$unknown_real_oid" ]]; then
    echo "error: pulled unknown-sha content hash mismatch: expected $unknown_real_oid got $pulled_unknown_oid" >&2
    exit 1
  fi
  if [[ "$pulled_unknown_size" != "$unknown_size" ]]; then
    echo "error: pulled unknown-sha size mismatch: expected $unknown_size got $pulled_unknown_size" >&2
    exit 1
  fi

  if [[ "$SERVER_MODE" == "remote" ]]; then
    log "Validating DRS API object lookup by checksum (known-sha case)"
    resp="$(api_json GET "${API_BASE}/objects/checksum/${local_oid}" "" "200")"
    if [[ "$(jq -r '.id // empty' <<<"$resp")" == "" ]]; then
      echo "error: DRS checksum lookup returned empty id for $local_oid" >&2
      exit 1
    fi
    log "Validating DRS API object lookup by checksum (unknown-sha synthetic oid case)"
    resp_unknown="$(api_json GET "${API_BASE}/objects/checksum/${unknown_pointer_oid}" "" "200")"
    if [[ "$(jq -r '.id // empty' <<<"$resp_unknown")" == "" ]]; then
      echo "error: DRS checksum lookup returned empty id for synthetic oid $unknown_pointer_oid" >&2
      exit 1
    fi
  fi

  log "SUCCESS: add-url e2e flow passed (known+unknown sha256)"
  log "- DRS URL:      ${DRS_URL%/}"
  log "- Organization: $ORGANIZATION"
  log "- Project ID:   $PROJECT_ID"
  log "- Bucket:       $TEST_BUCKET_NAME"
  log "- Known OID:    $local_oid"
  log "- Unknown real: $unknown_real_oid"
  log "- Unknown ptr:  $unknown_pointer_oid"
  TEST_OUTCOME="PASS"
}

main "$@"
