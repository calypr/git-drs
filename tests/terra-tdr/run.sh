#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 /path/to/jade-data-repo" >&2
  exit 2
fi

git_drs_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
tdr_root=$(cd "$1" && pwd)
coord_dir=$(mktemp -d)
port_file="$coord_dir/port"
stop_file="$coord_dir/stop"
tdr_log="$coord_dir/tdr.log"
signing_public_key_file="$coord_dir/tdr-signing-public-key.pem"

source "$git_drs_root/tests/terra-tdr/firestore-emulator.sh"
source "$git_drs_root/tests/terra-tdr/storage-emulator.sh"
FIRESTORE_CONTAINER=
STORAGE_CONTAINER=
start_firestore_emulator
start_storage_emulator "$coord_dir/gcs-seed"

cp "$git_drs_root/tests/terra-tdr/TerraDrsHarnessTest.java" \
  "$tdr_root/src/test/java/bio/terra/service/filedata/TerraDrsHarnessTest.java"

(
  cd "$tdr_root"
  GIT_DRS_TDR_PORT_FILE="$port_file" GIT_DRS_TDR_STOP_FILE="$stop_file" \
    GIT_DRS_TDR_SIGNING_PUBLIC_KEY_FILE="$signing_public_key_file" \
    FIRESTORE_EMULATOR_HOST="$FIRESTORE_EMULATOR_HOST" \
    GIT_DRS_TDR_GCS_ENDPOINT="$STORAGE_EMULATOR_HTTP_ENDPOINT" \
    ./gradlew testUnit --tests bio.terra.service.filedata.TerraDrsHarnessTest \
      --no-daemon --console=plain --rerun-tasks
) >"$tdr_log" 2>&1 &
tdr_pid=$!

stop_services() {
  touch "$stop_file"
  if [[ -n "${tdr_pid:-}" ]]; then
    wait "$tdr_pid" || true
  fi
  stop_firestore_emulator
  stop_storage_emulator
}
trap stop_services EXIT

for ((seconds=0; seconds<900; seconds++)); do
  if [[ -s "$port_file" ]]; then
    break
  fi
  if ! kill -0 "$tdr_pid" 2>/dev/null; then
    cat "$tdr_log" >&2
    exit 1
  fi
  sleep 1
done

if [[ ! -s "$port_file" ]]; then
  echo "TDR controller did not start within 15 minutes" >&2
  cat "$tdr_log" >&2
  exit 1
fi

port=$(cat "$port_file")
cd "$git_drs_root"
if ! GIT_DRS_TDR_HTTP_ENDPOINT="http://127.0.0.1:$port" \
  GIT_DRS_TDR_GCS_ENDPOINT="$STORAGE_EMULATOR_HTTP_ENDPOINT" \
  GIT_DRS_TDR_SIGNING_PUBLIC_KEY_FILE="$signing_public_key_file" \
  go test -race -tags=integration -count=1 ./cmd/pull \
    -run '^TestIntegrationPullAgainstTDRController$' -v; then
  cat "$tdr_log" >&2
  exit 1
fi

downloads=$(storage_object_download_count)
if [[ "$downloads" != 1 ]]; then
  echo "GCS emulator served $downloads object downloads, want exactly one" >&2
  docker logs "$STORAGE_CONTAINER" >&2 || true
  exit 1
fi

touch "$stop_file"
if ! wait "$tdr_pid"; then
  cat "$tdr_log" >&2
  exit 1
fi
tdr_pid=
stop_firestore_emulator
stop_storage_emulator
trap - EXIT

tail -n 20 "$tdr_log"
