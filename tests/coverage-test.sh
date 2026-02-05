#!/bin/bash
# coverage-test.sh
# Removes objects from the bucket and indexd records, then runs monorepo tests (clean, normal, clone) twice.
set -euo pipefail

# uncomment the following line to enable debug output for git commands (e.g. to troubleshoot large file uploads/downloads)
# set -x

usage() {
  cat <<-EOF
Usage: $0 [options]

Options:
  --pod POD                 Pod name (env: POD)
  --postgres-password PASS  Postgres password (env: POSTGRES_PASSWORD)
  --resource RES            Resource path, e.g. /programs/<prog>/projects/<proj> (env: RESOURCE)
  --minio-alias ALIAS       MinIO alias (env: MINIO_ALIAS)
  --bucket BUCKET           Bucket name (env: BUCKET)
  --prefix PREFIX           Object key prefix (env: PREFIX)
  -h, --help                Show this help and exit

Environment / defaults:
  COVERAGE_ROOT             ${COVERAGE_ROOT:-<root>/coverage}
  INTEGRATION_COV_DIR       ${INTEGRATION_COV_DIR:-<coverage>/integration/raw}
  INTEGRATION_PROFILE       ${INTEGRATION_PROFILE:-<coverage>/integration/coverage.out}
  BUILD_DIR                 ${BUILD_DIR:-<root>/build/coverage}

Notes:
  - Flags override environment variables.
  - PROJECT_ID is derived from RESOURCE (program-project).
  - Script must be run from repository (it cds to the project root).

Example:
  tests/coverage-test.sh --pod mypod --postgres-password secret --resource /programs/prog/projects/proj --minio-alias minio --bucket my-bucket

More:
  See:
  - tests/monorepos/README-run-test.md for details on the monorepo test runner.
  - tests/scripts/coverage/combine-coverage.sh for combining coverage profiles.
  - tests/scripts/coverage/assert-coverage-timestamp.sh for verifying coverage timestamp.

EOF
  exit 0
}


# Accept named parameters (flags override environment variables)
POD="${POD:-}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-}"
RESOURCE="${RESOURCE:-}"
MINIO_ALIAS="${MINIO_ALIAS:-}"
BUCKET="${BUCKET:-}"
PREFIX="${PREFIX:-}"

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
    --prefix=*) PREFIX="${1#*=}"; shift ;;
    --prefix) PREFIX="$2"; shift 2 ;;
    -h|--help)
      usage
      ;;
    *)
      break
      ;;
  esac
done


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

# Normalize PREFIX to have trailing slash if set
if [ -n "$PREFIX" ]; then PREFIX="${PREFIX%/}/"
fi

# Ensure utilities exist
if [ ! -d "$UTIL_DIR" ]; then
  err "utils directory not found: \`$UTIL_DIR\`"
  exit 1
fi

# before running tests, build the executables with coverage instrumentation


# build git-drs with coverage instrumentation
go build -cover -covermode=atomic -coverpkg=./... -o "${BUILD_DIR}/git-drs" .

export PATH="${BUILD_DIR}:${PATH}"

# get rid of old binary if exists
rm git-drs || true
#go build
# unit tests
which git-drs
rm -rf coverage/unit
mkdir -p coverage/unit
go test  -cover -covermode=atomic -coverprofile=coverage/unit/coverage.out -coverpkg=./... ./... || { echo "error: unit tests failed" >&2; exit 1; }
#
echo "Unit tests completed successfully. Coverage profile saved to coverage/unit/coverage.out" >&2

# set coverage directory for integration tests
export GOCOVERDIR="${INTEGRATION_COV_DIR}"

rm -rf coverage/integration/raw
mkdir -p coverage/integration/raw

pushd "$UTIL_DIR" >/dev/null

# 1) Remove objects from bucket using indexd->s3 list/delete pipeline
echo "Removing bucket objects by sha256 via \`./list-indexd-sha256.sh $POD <POSTGRES_PASSWORD> $RESOURCE | ./delete-s3-by-sha256.sh $MINIO_ALIAS $BUCKET\`" >&2
if ! ./list-indexd-sha256.sh "$POD" "$POSTGRES_PASSWORD" "$RESOURCE" | ./delete-s3-by-sha256.sh "$MINIO_ALIAS" "$PREFIX$BUCKET"; then
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

# remove old fixtures if any, to eliminate stale data, renames, etc
rm -rf fixtures/TARGET-ALL-P2 || true
rm -rf fixtures/data || true
# build fixtures
make test-monorepos


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

#
# After tests, upload a simple test file to the bucket via mc
# ensure git-drs can add-url it correctly
#
cd fixtures

# create a simple test file, place it in the bucket via mc
test_string='simple test'
test_string2='simple test2'
echo $test_string > /tmp/simple_test_file.txt
echo $test_string2 > /tmp/simple_test_file2.txt
sha256=$(sha256sum /tmp/simple_test_file.txt | cut -d' ' -f1)
sha2562=$(sha256sum /tmp/simple_test_file2.txt | cut -d' ' -f1)
echo "Uploading a simple test file to the bucket via \`mc\`" >&2


mc cp /tmp/simple_test_file.txt "$MINIO_ALIAS/$PREFIX$BUCKET/simple_test_file.txt"
mc cp /tmp/simple_test_file2.txt "$MINIO_ALIAS/$PREFIX$BUCKET/simple_test_file2.txt"

# get the s3 parameters from the mc alias
MC_ALIAS_INFO_JSON=$(mc alias ls "$MINIO_ALIAS" --json)
if [ $? -ne 0 ]; then
  err "failed to get mc alias info for alias: $MINIO_ALIAS"
  exit 1
fi
ENDPOINT=$(echo "$MC_ALIAS_INFO_JSON" | jq -r '.URL')
ACCESS_KEY=$(echo "$MC_ALIAS_INFO_JSON" | jq -r '.accessKey')
SECRET_KEY=$(echo "$MC_ALIAS_INFO_JSON" | jq -r '.secretKey')

# use the add-url command to add the file to project
# we are not providing the sha256, so git-drs must compute it and verify it matches
git drs add-url s3://$PREFIX$BUCKET/simple_test_file.txt data/simple_test_file.txt  \
  --aws-access-key-id  $ACCESS_KEY \
  --aws-secret-access-key  $SECRET_KEY  \
  --endpoint-url $ENDPOINT \
  --region us-east-1

# set the .gitattributes to track the file
git lfs track data/simple_test_file.txt
#
git add .gitattributes data/simple_test_file.txt
git commit -m "add-url simple_test_file.txt to git lfs"

echo "verify the sha256 matches"
grep $sha256 data/simple_test_file.txt
echo "data/simple_test_file.txt should show as a pointer file"
git lfs ls-files | grep " - data/simple_test_file.txt"

echo "Pulling the file via git lfs pull"
git lfs pull origin main

echo "verify the file is now tracked as a local data file"
git lfs ls-files | grep " * data/simple_test_file.txt"
echo "verify the file contents after pull"
cat data/simple_test_file.txt | grep "$test_string"

echo "checking the original oid"
original_add_url_oid=$(git lfs ls-files -l | awk -v path="data/simple_test_file.txt" '$0 ~ (" " path "$") {print $1; exit}')
if [ -z "$original_add_url_oid" ]; then
  err "unable to find LFS OID for data/simple_test_file.txt"
  exit 1
fi

updated_test_string='simple test updated'
echo "$updated_test_string" > /tmp/simple_test_file_updated.txt
updated_sha256=$(sha256sum /tmp/simple_test_file_updated.txt | cut -d' ' -f1)
mc cp /tmp/simple_test_file_updated.txt "$MINIO_ALIAS/$PREFIX$BUCKET/simple_test_file.txt"

git drs add-url s3://$PREFIX$BUCKET/simple_test_file.txt data/simple_test_file.txt  \
  --aws-access-key-id  $ACCESS_KEY \
  --aws-secret-access-key  $SECRET_KEY  \
  --endpoint-url $ENDPOINT \
  --region us-east-1

git add data/simple_test_file.txt
git commit -m "add-url update simple_test_file.txt"

updated_add_url_oid=$(git lfs ls-files -l | awk -v path="data/simple_test_file.txt" '$0 ~ (" " path "$") {print $1; exit}')
if [ -z "$updated_add_url_oid" ]; then
  err "unable to find updated LFS OID for data/simple_test_file.txt"
  exit 1
fi
if [ "$original_add_url_oid" = "$updated_add_url_oid" ]; then
  err "expected OID to change after add-url content update"
  exit 1
fi

git lfs pull origin main
cat data/simple_test_file.txt | grep "$updated_test_string"

# use the add-url command to add the file to project
# we are providing the sha256, so git-drs must trust it
git drs add-url s3://$PREFIX$BUCKET/simple_test_file2.txt data/simple_test_file2.txt  \
  --aws-access-key-id  $ACCESS_KEY \
  --aws-secret-access-key  $SECRET_KEY  \
  --endpoint-url $ENDPOINT \
  --sha256 $sha2562 \
  --region us-east-1

git lfs track data/simple_test_file2.txt
git add .gitattributes data/simple_test_file2.txt
git commit -m "add-url simple_test_file2.txt to git lfs"

echo "verify the sha256 matches for simple_test_file2.txt"
simple_test_file2_oid=$(git lfs ls-files -l | awk -v path="data/simple_test_file2.txt" '$0 ~ (" " path "$") {print $1; exit}')
if [ -z "$simple_test_file2_oid" ]; then
  echo "unable to find LFS OID for data/simple_test_file2.txt"
  exit 1
fi

git mv data/simple_test_file2.txt data/renamed_simple_test_file2.txt
git commit -m "rename simple_test_file2.txt path"

renamed_simple_test_file2_oid=$(git lfs ls-files -l | awk -v path="data/renamed_simple_test_file2.txt" '$0 ~ (" " path "$") {print $1; exit}')
if [ -z "$renamed_simple_test_file2_oid" ]; then
  echo "unable to find LFS OID for data/renamed_simple_test_file2.txt"
  exit 1
fi
if [ "$simple_test_file2_oid" != "$renamed_simple_test_file2_oid" ]; then
  echo "expected OID to stay the same after path change"
  exit 1
fi
if git lfs ls-files -l | grep -Fq " data/simple_test_file2.txt"; then
  echo "expected old path data/simple_test_file2.txt to be absent after rename"
  exit 1
fi

echo "Passed checks for rename of simple_test_file2.txt"

#
#
#
popd >/dev/null

echo "Listing bucket objects by sha256 via \`./list-indexd-sha256.sh $POD <POSTGRES_PASSWORD> $RESOURCE | ./list-s3-by-sha256.sh $MINIO_ALIAS $PREFIX$BUCKET\`" >&2
if ! $UTIL_DIR/list-indexd-sha256.sh "$POD" "$POSTGRES_PASSWORD" "$RESOURCE" | $UTIL_DIR/list-s3-by-sha256.sh "$MINIO_ALIAS" "$PREFIX$BUCKET"; then
  echo "command failed: ./list-indexd-sha256.sh \"$POD\" \"$POSTGRES_PASSWORD\" \"$RESOURCE\" | ./list-s3-by-sha256.sh \"$MINIO_ALIAS\" \"$PREFIX$BUCKET\""
  exit 1
fi


echo "coverage-test.sh: all steps completed successfully." >&2
go tool covdata textfmt -i="${INTEGRATION_COV_DIR}" -o "${INTEGRATION_PROFILE}"

echo "Integration coverage profile saved to ${INTEGRATION_PROFILE}"


echo "Combining coverage profiles..."
tests/scripts/coverage/combine-coverage.sh

