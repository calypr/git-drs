#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
GIT_DRS_ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd)"

ENV_FILE="${ENV_FILE:-}"
if [[ -n "$ENV_FILE" && -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
elif [[ -z "$ENV_FILE" ]]; then
  # Default: load git-drs/.env only.
  if [[ -f "$GIT_DRS_ROOT/.env" ]]; then
    set -a
    # shellcheck disable=SC1090
    source "$GIT_DRS_ROOT/.env"
    set +a
  fi
fi

DRS_URL="${TEST_DRS_URL:-https://caliper-training.ohsu.edu}"
SERVER_MODE="${TEST_SERVER_MODE:-remote}"
GEN3_TOKEN="${TEST_GEN3_TOKEN:-}"
GEN3_PROFILE="${TEST_GEN3_PROFILE:-${GEN3_PROFILE:-}}"
GEN3_CONFIG_PATH="${TEST_GEN3_CONFIG_PATH:-${GEN3_CONFIG_PATH:-$HOME/.gen3/gen3_client_config.ini}}"

TEST_ORGANIZATION="${TEST_ORGANIZATION:-}"
TEST_PROJECT_ID="${TEST_PROJECT_ID:-}"
TEST_BUCKET="${TEST_BUCKET:-}"

CREATE_BUCKET_BEFORE_TEST="${TEST_CREATE_BUCKET_BEFORE_TEST:-false}"
DELETE_TEST_BUCKET_AFTER="${TEST_DELETE_BUCKET_AFTER:-false}"
TEST_BUCKET_NAME="${TEST_BUCKET_NAME:-$TEST_BUCKET}"
TEST_BUCKET_REGION="${TEST_BUCKET_REGION:-}"
TEST_BUCKET_ACCESS_KEY="${TEST_BUCKET_ACCESS_KEY:-}"
TEST_BUCKET_SECRET_KEY="${TEST_BUCKET_SECRET_KEY:-}"
TEST_BUCKET_ENDPOINT="${TEST_BUCKET_ENDPOINT:-}"
TEST_BUCKET_ORGANIZATION="${TEST_BUCKET_ORGANIZATION:-$TEST_ORGANIZATION}"
TEST_BUCKET_PROJECT_ID="${TEST_BUCKET_PROJECT_ID:-$TEST_PROJECT_ID}"

MONO_REMOTE_URL="${MONO_REMOTE_URL:-${TEST_REMOTE_URL:-https://github.com/calypr/git-drs-e2e-remote.git}}"
MONO_REMOTE_NAME="${MONO_REMOTE_NAME:-origin}"
MONO_REPO_NAME="${MONO_REPO_NAME:-git-drs-monorepo-e2e}"
MONO_WORK_ROOT="${MONO_WORK_ROOT:-$(mktemp -d -t drs-monorepo-XXXX)}"
MONO_KEEP_WORKDIR="${MONO_KEEP_WORKDIR:-false}"
MONO_TOP_LEVELS="${MONO_TOP_LEVELS:-TARGET-ALL-P2,TCGA-GBM,TCGA-LUAD}"
MONO_SUBDIRS="${MONO_SUBDIRS:-2}"
MONO_FILES_PER_SUBDIR="${MONO_FILES_PER_SUBDIR:-20}"
MONO_PUSH_PER_DIR="${MONO_PUSH_PER_DIR:-true}"
MONO_RUN_CLONE_VERIFY="${MONO_RUN_CLONE_VERIFY:-true}"
MONO_GIT_BRANCH="${MONO_GIT_BRANCH:-main}"
MONO_TRANSFERS="${MONO_TRANSFERS:-10}"
MONO_DELETE_REMOTE_AT_START="${MONO_DELETE_REMOTE_AT_START:-false}"
TEST_GITHUB_TOKEN="${TEST_GITHUB_TOKEN:-}"
MONO_MULTIPART_THRESHOLD_MB="${MONO_MULTIPART_THRESHOLD_MB:-64}"
MONO_RUN_MULTIPART_SMOKE="${MONO_RUN_MULTIPART_SMOKE:-true}"
MONO_MULTIPART_SMOKE_MB="${MONO_MULTIPART_SMOKE_MB:-96}"
MONO_CONTENT_SALT="${MONO_CONTENT_SALT:-run-$(date -u +%Y%m%dT%H%M%SZ)-$RANDOM}"

SOURCE_REPO="$MONO_WORK_ROOT/$MONO_REPO_NAME"
CLONE_REPO="$MONO_WORK_ROOT/${MONO_REPO_NAME}-clone"
BUCKET_API_BASE="${DRS_URL%/}/data/buckets"
CREATED_TEST_BUCKET=false
ACTIVE_BUCKET="$TEST_BUCKET"
MONO_REMOTE_URL_AUTH="$MONO_REMOTE_URL"
GITHUB_OWNER_REPO=""
DELETED_TEST_BUCKET=false
DELETED_REMOTE_REPO_AT_START=false

log() {
  printf '[drs-monorepo] %s\n' "$*"
}

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
  local curl_args=(-sS -o "$out" -w '%{http_code}' -X "$method" -H "Accept: application/json")
  if [[ "$SERVER_MODE" == "remote" ]]; then
    curl_args+=(-H "Authorization: Bearer $GEN3_TOKEN")
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
    return
  fi
  require_env GEN3_PROFILE "$GEN3_PROFILE"
  if [[ ! -f "$GEN3_CONFIG_PATH" ]]; then
    echo "error: GEN3 profile config file not found at $GEN3_CONFIG_PATH" >&2
    exit 1
  fi
  GEN3_TOKEN="$(load_profile_field "$GEN3_PROFILE" "access_token" "$GEN3_CONFIG_PATH")"
  if [[ -z "$GEN3_TOKEN" ]]; then
    echo "error: profile '$GEN3_PROFILE' does not contain access_token in $GEN3_CONFIG_PATH" >&2
    exit 1
  fi
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
  fi
}

cleanup() {
  if [[ "$CREATE_BUCKET_BEFORE_TEST" == "true" && "$DELETE_TEST_BUCKET_AFTER" == "true" && "$CREATED_TEST_BUCKET" == "true" ]]; then
    log "Deleting test bucket credential '$TEST_BUCKET_NAME'"
    api_json DELETE "$BUCKET_API_BASE/$TEST_BUCKET_NAME" "" "204,404" >/dev/null || true
    DELETED_TEST_BUCKET=true
  fi
  log "Cleanup summary: bucket_deleted=$DELETED_TEST_BUCKET remote_deleted_at_start=$DELETED_REMOTE_REPO_AT_START delete_bucket_after=$DELETE_TEST_BUCKET_AFTER delete_remote_at_start=$MONO_DELETE_REMOTE_AT_START"
  if [[ "$MONO_KEEP_WORKDIR" == "true" ]]; then
    log "Keeping working directory: $MONO_WORK_ROOT"
    return
  fi
  rm -rf "$MONO_WORK_ROOT"
}
trap cleanup EXIT

validate_config() {
  require_cmd git
  require_cmd git-lfs
  require_cmd jq
  require_cmd go
  require_cmd git-drs

  require_env TEST_ORGANIZATION "$TEST_ORGANIZATION"
  require_env TEST_PROJECT_ID "$TEST_PROJECT_ID"
  require_env TEST_BUCKET "$TEST_BUCKET"
  require_env MONO_REMOTE_URL "$MONO_REMOTE_URL"

  case "$SERVER_MODE" in
    remote|local) ;;
    *)
      echo "error: TEST_SERVER_MODE must be 'remote' or 'local'" >&2
      exit 1
      ;;
  esac

  if [[ "$CREATE_BUCKET_BEFORE_TEST" == "true" ]]; then
    require_env TEST_BUCKET_NAME "$TEST_BUCKET_NAME"
    require_env TEST_BUCKET_REGION "$TEST_BUCKET_REGION"
    require_env TEST_BUCKET_ACCESS_KEY "$TEST_BUCKET_ACCESS_KEY"
    require_env TEST_BUCKET_SECRET_KEY "$TEST_BUCKET_SECRET_KEY"
  fi
}

configure_remote_auth() {
  MONO_REMOTE_URL_AUTH="$MONO_REMOTE_URL"
  if [[ -n "$TEST_GITHUB_TOKEN" && "$MONO_REMOTE_URL" =~ ^https://github.com/ ]]; then
    MONO_REMOTE_URL_AUTH="${MONO_REMOTE_URL/https:\/\/github.com\//https:\/\/x-access-token:${TEST_GITHUB_TOKEN}@github.com/}"
  fi
}

parse_github_owner_repo() {
  local url="$1"
  local path
  path="${url#https://github.com/}"
  path="${path#http://github.com/}"
  path="${path%.git}"
  if [[ "$path" == */* ]]; then
    GITHUB_OWNER_REPO="$path"
    return 0
  fi
  return 1
}

ensure_github_repo_exists() {
  # Only applies to GitHub https remotes.
  if [[ ! "$MONO_REMOTE_URL" =~ ^https://github.com/ ]]; then
    return
  fi
  if ! parse_github_owner_repo "$MONO_REMOTE_URL"; then
    return
  fi
  if [[ -z "$TEST_GITHUB_TOKEN" ]]; then
    return
  fi
  require_cmd gh

  # If repo already exists (even if our current auth URL failed for other reasons), do nothing.
  if GH_TOKEN="$TEST_GITHUB_TOKEN" gh api "/repos/${GITHUB_OWNER_REPO}" >/dev/null 2>&1; then
    return
  fi

  local owner repo owner_type endpoint
  owner="${GITHUB_OWNER_REPO%%/*}"
  repo="${GITHUB_OWNER_REPO##*/}"
  owner_type="$(GH_TOKEN="$TEST_GITHUB_TOKEN" gh api "/users/${owner}" -q .type 2>/dev/null | tr '[:upper:]' '[:lower:]' || true)"
  if [[ "$owner_type" == "organization" ]]; then
    endpoint="/orgs/${owner}/repos"
  else
    endpoint="/user/repos"
  fi

  log "GitHub repo not found; creating ${GITHUB_OWNER_REPO}"
  GH_TOKEN="$TEST_GITHUB_TOKEN" gh api -X POST "$endpoint" \
    -f "name=${repo}" \
    -f "private=true" >/dev/null
}

delete_github_repo_if_requested() {
  if [[ "$MONO_DELETE_REMOTE_AT_START" != "true" ]]; then
    return
  fi
  if [[ ! "$MONO_REMOTE_URL" =~ ^https://github.com/ ]]; then
    return
  fi
  if [[ -z "$TEST_GITHUB_TOKEN" ]]; then
    return
  fi
  if ! parse_github_owner_repo "$MONO_REMOTE_URL"; then
    return
  fi

  require_cmd gh
  if GH_TOKEN="$TEST_GITHUB_TOKEN" gh api "/repos/${GITHUB_OWNER_REPO}" >/dev/null 2>&1; then
    log "Deleting existing GitHub repo ${GITHUB_OWNER_REPO} for clean test run"
    GH_TOKEN="$TEST_GITHUB_TOKEN" gh api -X DELETE "/repos/${GITHUB_OWNER_REPO}" >/dev/null
    DELETED_REMOTE_REPO_AT_START=true
    # Small wait to avoid eventual-consistency race with immediate recreation.
    sleep 2
  fi
}

preflight_remote_access() {
  if git ls-remote "$MONO_REMOTE_URL_AUTH" >/dev/null 2>&1; then
    return
  fi

  # If unreachable and we have a GitHub token for a GitHub URL, attempt repo creation.
  ensure_github_repo_exists
  if git ls-remote "$MONO_REMOTE_URL_AUTH" >/dev/null 2>&1; then
    return
  fi

  if ! git ls-remote "$MONO_REMOTE_URL_AUTH" >/dev/null 2>&1; then
    echo "error: unable to access git remote: $MONO_REMOTE_URL" >&2
    echo "hint: repo may not exist, or auth is missing for a private repo." >&2
    echo "hint: set TEST_GITHUB_TOKEN for private GitHub remotes." >&2
    exit 1
  fi
}

generate_fixtures() {
  local fixture_root="$SOURCE_REPO/fixtures"
  local generator_bin="$MONO_WORK_ROOT/generate-fixtures"

  mkdir -p "$SOURCE_REPO"
  pushd "$SOURCE_REPO" >/dev/null
  log "Building fixture generator"
  go build -o "$generator_bin" "$SCRIPT_DIR/generate-fixtures.go"

  mkdir -p "$fixture_root"
  pushd "$fixture_root" >/dev/null
  IFS=',' read -r -a top_levels <<< "$MONO_TOP_LEVELS"
  printf '%s\n' "${top_levels[@]}" | "$generator_bin" \
    --number-of-subdirectories="$MONO_SUBDIRS" \
    --number-of-files="$MONO_FILES_PER_SUBDIR" --
  # Ensure per-run unique content so OIDs don't collide with stale metadata from previous runs.
  while IFS= read -r -d '' file; do
    printf '\n%s\n' "$MONO_CONTENT_SALT" >>"$file"
  done < <(find . -type f -name '*.dat' -print0)
  popd >/dev/null
  popd >/dev/null
}

setup_repo() {
  pushd "$SOURCE_REPO" >/dev/null
  git init -b "$MONO_GIT_BRANCH"
  git remote add "$MONO_REMOTE_NAME" "$MONO_REMOTE_URL_AUTH"
  git drs init -t "$MONO_TRANSFERS"
  if [[ "$SERVER_MODE" == "remote" ]]; then
    git drs remote add gen3 "$MONO_REMOTE_NAME" \
      --token "$GEN3_TOKEN" \
      --bucket "$ACTIVE_BUCKET" \
      --organization "$TEST_ORGANIZATION" \
      --project "$TEST_PROJECT_ID"
  else
    git drs remote add local "$MONO_REMOTE_NAME" "$DRS_URL" \
      --bucket "$ACTIVE_BUCKET" \
      --organization "$TEST_ORGANIZATION" \
      --project "$TEST_PROJECT_ID"
  fi
  configure_lfs_endpoint_for_repo "$MONO_REMOTE_NAME"
  git config --local lfs.concurrenttransfers "$MONO_TRANSFERS"
  # Keep most files on single-part for scale tests; run one explicit multipart smoke upload separately.
  git config --local drs.multipart-threshold "$MONO_MULTIPART_THRESHOLD_MB"
  git config user.name "${GIT_AUTHOR_NAME:-drs-monorepo-e2e}"
  git config user.email "${GIT_AUTHOR_EMAIL:-drs-monorepo-e2e@local.invalid}"
  popd >/dev/null
}

push_dataset() {
  pushd "$SOURCE_REPO" >/dev/null
  git lfs track "*.dat"
  git add .gitattributes
  git commit -m "Initialize LFS tracking" || true
  # Ensure origin/main is established as upstream for subsequent git-drs pushes.
  git push --set-upstream "$MONO_REMOTE_NAME" "$MONO_GIT_BRANCH"

  if [[ "$MONO_RUN_MULTIPART_SMOKE" == "true" ]]; then
    mkdir -p fixtures/multipart-smoke
    dd if=/dev/urandom of=fixtures/multipart-smoke/multipart-smoke.dat bs=1048576 count="$MONO_MULTIPART_SMOKE_MB" status=none
    git add fixtures/multipart-smoke/multipart-smoke.dat
    git commit -m "Add multipart smoke file (${MONO_MULTIPART_SMOKE_MB}MB)" || true
    log "git drs push multipart smoke file"
    git drs push "$MONO_REMOTE_NAME"
  fi

  if [[ "$MONO_PUSH_PER_DIR" == "true" ]]; then
    while IFS= read -r -d '' dir; do
      rel="${dir#./}"
      git add "fixtures/${rel}"
      git commit -m "Add fixture tree fixtures/${rel}" || true
      log "git drs push for fixtures/${rel}"
      git drs push "$MONO_REMOTE_NAME"
    done < <(cd fixtures && find . -mindepth 1 -maxdepth 1 -type d -print0)
  else
    git add fixtures
    git commit -m "Add full monorepo fixture dataset" || true
    log "git drs push full dataset"
    git drs push "$MONO_REMOTE_NAME"
  fi

  log "source LFS pointers: $(git lfs ls-files | wc -l | tr -d ' ')"
  popd >/dev/null
}

clone_and_verify() {
  if [[ "$MONO_RUN_CLONE_VERIFY" != "true" ]]; then
    return
  fi
  rm -rf "$CLONE_REPO"
  git clone "$MONO_REMOTE_URL_AUTH" "$CLONE_REPO"
  pushd "$CLONE_REPO" >/dev/null
  git drs init -t "$MONO_TRANSFERS"
  if [[ "$SERVER_MODE" == "remote" ]]; then
    git drs remote add gen3 "$MONO_REMOTE_NAME" \
      --token "$GEN3_TOKEN" \
      --bucket "$ACTIVE_BUCKET" \
      --organization "$TEST_ORGANIZATION" \
      --project "$TEST_PROJECT_ID"
  else
    git drs remote add local "$MONO_REMOTE_NAME" "$DRS_URL" \
      --bucket "$ACTIVE_BUCKET" \
      --organization "$TEST_ORGANIZATION" \
      --project "$TEST_PROJECT_ID"
  fi
  configure_lfs_endpoint_for_repo "$MONO_REMOTE_NAME"
  git config --local lfs.concurrenttransfers "$MONO_TRANSFERS"
  log "Running git drs pull in clone"
  git drs pull

  local pointer_count
  pointer_count="$(grep -R --include='*.dat' -l 'https://git-lfs.github.com/spec/v1' fixtures | wc -l | tr -d ' ' || true)"
  if [[ "${pointer_count:-0}" != "0" ]]; then
    echo "error: expected hydrated fixture files, found ${pointer_count} unresolved LFS pointers in clone" >&2
    exit 1
  fi
  log "Clone verification passed"
  popd >/dev/null
}

main() {
  validate_config
  resolve_auth_from_profile_if_needed
  log "GitHub auth token detected: $([[ -n "$TEST_GITHUB_TOKEN" ]] && echo true || echo false)"
  configure_remote_auth
  delete_github_repo_if_requested
  preflight_remote_access
  if [[ "$CREATE_BUCKET_BEFORE_TEST" == "true" ]]; then
    log "Creating test bucket credential '$TEST_BUCKET_NAME'"
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
    api_json PUT "$BUCKET_API_BASE" "$create_bucket_body" "201,409" >/dev/null
    CREATED_TEST_BUCKET=true
    ACTIVE_BUCKET="$TEST_BUCKET_NAME"
  fi
  log "Working directory: $MONO_WORK_ROOT"
  log "Generating monorepo fixtures (top-levels: $MONO_TOP_LEVELS)"
  generate_fixtures
  log "Initializing source repository"
  setup_repo
  log "Pushing monorepo dataset"
  push_dataset
  log "Cloning and verifying hydrated pull path"
  clone_and_verify
  log "Monorepo E2E completed"
}

main "$@"
