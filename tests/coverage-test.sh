#!/bin/bash
# coverage-test.sh
# Removes objects from the bucket and indexd records, then runs monorepo tests (clean, normal, clone) twice.
set -euo pipefail

# echo commands as they are executed
if [ -z "${GIT_TRACE:-}" ]; then
  echo "For more verbose git output, consider setting the following environment variables before re-running the script:" >&2
  echo "# export GIT_TRACE=1 GIT_TRANSFER_TRACE=1" >&2
else
  set -x
fi

# determine the script directory and cd to its parent (project root)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
if [ -z "${SCRIPT_DIR:-}" ]; then
  echo "error: unable to determine script directory" >&2
  exit 1
fi
cd "$SCRIPT_DIR/.." || { echo "error: failed to cd to parent of $SCRIPT_DIR" >&2; exit 1; }

# lint
go vet ./...
gofmt -s -w .

# build to ensure no compile errors
go build



# Accept named parameters (flags override environment variables)
POD="${POD:-}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-}"
RESOURCE="${RESOURCE:-}"
MINIO_ALIAS="${MINIO_ALIAS:-}"
BUCKET="${BUCKET:-}"

while [ $# -gt 0 ]; do
  case "$1" in
    --pod=*) POD="${1#*=}"; shift ;;
    --pod) POD="$2"; shift 2 ;;
    --postgres-password=*) POSTGRES_PASSWORD="${1#*=}"; shift ;;
    --postgres-password) POSTGRES_PASSWORD="$2"; shift 2 ;;
    --resource=*) RESOURCE="${1#*=}"; shift ;;
    --resource) RESOURCE="$2"; shift 2 ;;
    --minio-alias=*) MINIO_ALIAS="${1#*=}"; shift ;;
    --minio-alias) MINIO_ALIAS="$2"; shift 2 ;;
    --bucket=*) BUCKET="${1#*=}"; shift ;;
    --bucket) BUCKET="$2"; shift 2 ;;
    -h|--help)
      echo "Usage: $0 [--pod POD] [--postgres-password PASS] [--resource RES] [--minio-alias ALIAS] [--bucket BUCKET]"
      exit 0
      ;;
    *)
      break
      ;;
  esac
done


# Derive PROJECT_ID from RESOURCE (e.g. /programs/<prog>/projects/<proj> -> <prog>-<proj>)
_resource_clean="${RESOURCE#/}"        # drop leading slash if present
_resource_clean="${_resource_clean%/}" # drop trailing slash if present

IFS='/' read -r -a _parts <<< "$_resource_clean"

_prog=""
_proj=""
for i in "${!_parts[@]}"; do
  if [ "${_parts[i]}" = "programs" ] && [ $((i+1)) -lt ${#_parts[@]} ]; then
    _prog="${_parts[i+1]}"
  fi
  if [ "${_parts[i]}" = "projects" ] && [ $((i+1)) -lt ${#_parts[@]} ]; then
    _proj="${_parts[i+1]}"
  fi
done

PROJECT_ID="${_prog}-${_proj}"

ROOT_DIR=$(git rev-parse --show-toplevel)
COVERAGE_ROOT="${COVERAGE_ROOT:-${ROOT_DIR}/coverage}"
INTEGRATION_COV_DIR="${INTEGRATION_COV_DIR:-${COVERAGE_ROOT}/integration/raw}"
INTEGRATION_PROFILE="${INTEGRATION_PROFILE:-${COVERAGE_ROOT}/integration/coverage.out}"
BUILD_DIR="${BUILD_DIR:-${ROOT_DIR}/build/coverage}"
mkdir -p $INTEGRATION_COV_DIR

UTIL_DIR="tests/scripts/utils"
MONOREPO_DIR="tests/monorepos"
RUN_TEST="./run-test.sh"

# helpers
err() { echo "error: $*" >&2; }
run_and_check() {
  echo "=== running: $* ===" >&2
  if ! "$@"; then
    err "command failed: $*"
    exit 1
  fi
}

# Validate required inputs
if [ -z "$POD" ] || [ -z "$POSTGRES_PASSWORD" ] || [ -z "$RESOURCE" ] || [ -z "$MINIO_ALIAS" ] || [ -z "$BUCKET" ]; then
  err "One or more required environment variables are missing. Please set: POD, POSTGRES_PASSWORD, RESOURCE, MINIO_ALIAS, BUCKET"
  exit 1
fi

# Ensure utilities exist
if [ ! -d "$UTIL_DIR" ]; then
  err "utils directory not found: \`$UTIL_DIR\`"
  exit 1
fi

# before running tests, build the executables with coverage instrumentation

go build -cover -covermode=atomic -coverpkg=./... -o "${BUILD_DIR}/git-drs" .

export PATH="${BUILD_DIR}:${PATH}"
export GOCOVERDIR="${INTEGRATION_COV_DIR}"

pushd "$UTIL_DIR" >/dev/null

# 1) Remove objects from bucket using indexd->s3 list/delete pipeline
echo "Removing bucket objects by sha256 via \`./list-indexd-sha256.sh $POD <POSTGRES_PASSWORD> $RESOURCE | ./delete-s3-by-sha256.sh $MINIO_ALIAS $BUCKET\`" >&2
if ! ./list-indexd-sha256.sh "$POD" "$POSTGRES_PASSWORD" "$RESOURCE" | ./delete-s3-by-sha256.sh "$MINIO_ALIAS" "$BUCKET"; then
  err "command failed: ./list-indexd-sha256.sh \"$POD\" \"$POSTGRES_PASSWORD\" \"$RESOURCE\" | ./delete-s3-by-sha256.sh \"$MINIO_ALIAS\" \"$BUCKET\""
  exit 1
fi
echo "Bucket object removal pipeline completed." >&2

# 2) Remove indexd records
echo "Removing indexd records via \`./clean-indexd.sh $POD <POSTGRES_PASSWORD>\`" >&2
run_and_check ./clean-indexd.sh "$POD" "$POSTGRES_PASSWORD" "$RESOURCE"
echo "Indexd cleanup completed." >&2

popd >/dev/null

# Ensure monorepo test runner exists
if [ ! -d "$MONOREPO_DIR" ]; then
  err "monorepo tests directory not found: \`$MONOREPO_DIR\`"
  exit 1
fi

pushd "$MONOREPO_DIR" >/dev/null

# Run sequence twice: (--clean, normal, --clone)
# The first sequence ensures a clean state, the second verifies idempotency.

for pass in 1 2; do
  echo "=== Test sequence pass #$pass ===" >&2

  # enable --upsert only on the second pass
  if [ "$pass" -eq 2 ]; then
    echo "-> Running: \`$RUN_TEST --clean --upsert\`" >&2
    run_and_check "$RUN_TEST" --clean --upsert  --bucket=$BUCKET --project=$PROJECT_ID
  else
    echo "-> Running: \`$RUN_TEST --clean\`" >&2
    run_and_check "$RUN_TEST" --clean  --bucket=$BUCKET --project=$PROJECT_ID
  fi

  # on the second pass, this will NOT replace existing indexd records
  echo "-> Running: \`$RUN_TEST\`" >&2
  run_and_check "$RUN_TEST"


  echo "-> Running: \`$RUN_TEST --clone\`" >&2
  run_and_check "$RUN_TEST" --clone

  echo "=== Test sequence pass #$pass completed ===" >&2
done

popd >/dev/null

echo "Listing bucket objects by sha256 via \`./list-indexd-sha256.sh $POD <POSTGRES_PASSWORD> $RESOURCE | ./list-s3-by-sha256.sh $MINIO_ALIAS $BUCKET\`" >&2
if ! $UTIL_DIR/list-indexd-sha256.sh "$POD" "$POSTGRES_PASSWORD" "$RESOURCE" | $UTIL_DIR/list-s3-by-sha256.sh "$MINIO_ALIAS" "$BUCKET"; then
  err "command failed: ./list-indexd-sha256.sh \"$POD\" \"$POSTGRES_PASSWORD\" \"$RESOURCE\" | ./list-s3-by-sha256.sh \"$MINIO_ALIAS\" \"$BUCKET\""
  exit 1
fi


echo "coverage-test.sh: all steps completed successfully." >&2
go tool covdata textfmt -i="${INTEGRATION_COV_DIR}" -o "${INTEGRATION_PROFILE}"

echo "Integration coverage profile saved to ${INTEGRATION_PROFILE}"

# unit tests
rm git-drs || true
which git-drs
#export GOCOVERDIR=coverage/unit/raw
#go test -cover -covermode=atomic -coverpkg=./... ./... || { echo "error: unit tests failed" >&2; exit 1; }

mkdir -p coverage/unit/raw
go test -v -race -coverprofile=coverage/unit/raw/coverage.out -covermode=atomic -coverpkg=./... $(go list ./... | grep -vE 'tests/integration/calypr|client/indexd/tests') || { echo "unit tests failed" >&2; exit 1; }


