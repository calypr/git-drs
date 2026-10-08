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

cp "$git_drs_root/tests/terra-tdr/TerraDrsHarnessTest.java" \
  "$tdr_root/src/test/java/bio/terra/service/filedata/TerraDrsHarnessTest.java"

(
  cd "$tdr_root"
  GIT_DRS_TDR_PORT_FILE="$port_file" GIT_DRS_TDR_STOP_FILE="$stop_file" \
    ./gradlew testUnit --tests bio.terra.service.filedata.TerraDrsHarnessTest \
      --no-daemon --console=plain
) >"$tdr_log" 2>&1 &
tdr_pid=$!

stop_tdr() {
  touch "$stop_file"
  wait "$tdr_pid" || true
}
trap stop_tdr EXIT

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
  go test -race -tags=integration -count=1 ./cmd/pull \
    -run '^TestIntegrationPullAgainstTDRController$' -v; then
  cat "$tdr_log" >&2
  exit 1
fi

touch "$stop_file"
trap - EXIT
if ! wait "$tdr_pid"; then
  cat "$tdr_log" >&2
  exit 1
fi

tail -n 20 "$tdr_log"
