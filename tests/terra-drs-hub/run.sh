#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 /path/to/jade-data-repo /path/to/terra-drs-hub" >&2
  exit 2
fi

git_drs_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
tdr_root=$(cd "$1" && pwd)
hub_root=$(cd "$2" && pwd)
source "$git_drs_root/tests/terra-drs-hub/firestore-emulator.sh"
source "$git_drs_root/tests/terra-drs-hub/storage-emulator.sh"
coord_dir=$(mktemp -d)
port_file="$coord_dir/tdr-port"
stop_file="$coord_dir/tdr-stop"
hub_port_file="$coord_dir/hub-port"
hub_stop_file="$coord_dir/hub-stop"
tdr_log="$coord_dir/tdr.log"
hub_log="$coord_dir/hub.log"
proxy_log="$coord_dir/proxy.log"
hub_compile_log="$coord_dir/hub-compile.log"
certificate="$coord_dir/localhost.crt"
private_key="$coord_dir/localhost.key"
truststore="$coord_dir/truststore.p12"
signing_public_key_file="$coord_dir/tdr-signing-public-key.pem"

cp "$git_drs_root/tests/terra-drs-hub/TerraDrsHarnessTest.java" \
  "$tdr_root/src/test/java/bio/terra/service/filedata/TerraDrsHarnessTest.java"
cp "$git_drs_root/tests/terra-drs-hub/TerraDrsHubIntegrationTest.java" \
  "$hub_root/service/src/test/java/bio/terra/drshub/controllers/TerraDrsHubIntegrationTest.java"
cat >"$hub_root/service/src/test/resources/application-hub-ci.yaml" <<'YAML'
drshub:
  bardEventLoggingEnabled: false
  compactIdHosts:
    "drs.anv0": "localhost:${GIT_DRS_TDR_TLS_PORT}"
  drsProviders:
    terraDataRepo:
      hostRegex: "localhost:${GIT_DRS_TDR_TLS_PORT}"
      accessMethodConfigs:
        - type: gs
          auth: current_request
          fetchAccessUrl: true
YAML

openssl req -new -newkey rsa:2048 -nodes -keyout "$private_key" -x509 -days 1 \
  -out "$certificate" -subj "/CN=localhost" \
  -addext "subjectAltName=DNS:localhost,IP:127.0.0.1"

java_home=${JAVA_HOME:-$(java -XshowSettings:properties -version 2>&1 | sed -n 's/^[[:space:]]*java.home = //p' | head -n 1)}
if [[ -z "$java_home" ]]; then
  echo "Could not locate the active Java installation" >&2
  exit 1
fi
cp -L "$java_home/lib/security/cacerts" "$truststore"
chmod u+w "$truststore"
keytool -importcert -noprompt -alias git-drs-tdr-harness -file "$certificate" \
  -keystore "$truststore" -storepass changeit

cd "$hub_root"
if ! JAVA_TOOL_OPTIONS="${JAVA_TOOL_OPTIONS:-} -Djavax.net.ssl.trustStore=$truststore -Djavax.net.ssl.trustStorePassword=changeit" \
  ./gradlew :service:testClasses --no-daemon --console=plain >"$hub_compile_log" 2>&1; then
  cat "$hub_compile_log" >&2
  exit 1
fi

tls_port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')
FIRESTORE_CONTAINER=
start_firestore_emulator
STORAGE_CONTAINER=
start_storage_emulator "$coord_dir/gcs-seed" host.docker.internal

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

proxy_pid=
hub_pid=
stop_services() {
  touch "$hub_stop_file"
  touch "$stop_file"
  if [[ -n "$hub_pid" ]]; then
    wait "$hub_pid" 2>/dev/null || true
  fi
  if [[ -n "$proxy_pid" ]]; then
    kill "$proxy_pid" 2>/dev/null || true
    wait "$proxy_pid" 2>/dev/null || true
  fi
  wait "$tdr_pid" 2>/dev/null || true
  stop_firestore_emulator
  stop_storage_emulator
}
trap stop_services EXIT

for ((seconds = 0; seconds < 900; seconds++)); do
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
  echo "TDR DRS service did not start within 15 minutes" >&2
  cat "$tdr_log" >&2
  exit 1
fi

tdr_port=$(cat "$port_file")
python3 "$git_drs_root/tests/terra-drs-hub/tdr_https_proxy.py" \
  "http://127.0.0.1:$tdr_port" "$tls_port" "$certificate" "$private_key" "$proxy_log" \
  >"$coord_dir/proxy.out" 2>&1 &
proxy_pid=$!

for ((attempt = 0; attempt < 30; attempt++)); do
  if python3 - "$tls_port" <<'PY'
import socket
import ssl
import sys

context = ssl.create_default_context()
context.check_hostname = False
context.verify_mode = ssl.CERT_NONE
try:
    connection = socket.create_connection(("127.0.0.1", int(sys.argv[1])), timeout=1)
    with context.wrap_socket(connection, server_hostname="localhost"):
        pass
except OSError:
    raise SystemExit(1)
PY
  then
    break
  fi
  if ! kill -0 "$proxy_pid" 2>/dev/null; then
    cat "$coord_dir/proxy.out" >&2
    exit 1
  fi
  sleep 1
done

(
  cd "$hub_root"
  GIT_DRS_HUB_PORT_FILE="$hub_port_file" GIT_DRS_HUB_STOP_FILE="$hub_stop_file" \
  GIT_DRS_TDR_TLS_PORT="$tls_port" \
  GIT_DRS_TDR_HTTP_PORT="$tdr_port" \
  GIT_DRS_TDR_PROXY_LOG="$proxy_log" \
  JAVA_TOOL_OPTIONS="${JAVA_TOOL_OPTIONS:-} -Djavax.net.ssl.trustStore=$truststore -Djavax.net.ssl.trustStorePassword=changeit" \
  ./gradlew :service:test --tests bio.terra.drshub.controllers.TerraDrsHubIntegrationTest \
    --no-daemon --console=plain --rerun-tasks -x :service:runMinnieKenny
) >"$hub_log" 2>&1 &
hub_pid=$!

for ((seconds = 0; seconds < 900; seconds++)); do
  if [[ -s "$hub_port_file" ]]; then
    break
  fi
  if ! kill -0 "$hub_pid" 2>/dev/null; then
    cat "$hub_log" >&2
    exit 1
  fi
  sleep 1
done
if [[ ! -s "$hub_port_file" ]]; then
  echo "DRS Hub did not start within 15 minutes" >&2
  cat "$hub_log" >&2
  exit 1
fi

hub_port=$(cat "$hub_port_file")
cd "$git_drs_root"
cli_binary="$coord_dir/git-drs-linux"
GOOS=linux GOARCH="$(go env GOARCH)" CGO_ENABLED=0 go build -o "$cli_binary" .
if ! GIT_DRS_BINARY="$cli_binary" \
  GIT_DRS_HUB_HTTP_ENDPOINT="http://127.0.0.1:$hub_port" \
  GIT_DRS_TDR_HTTP_ENDPOINT="http://127.0.0.1:$tdr_port" \
  GIT_DRS_TDR_PROXY_LOG="$proxy_log" \
  GIT_DRS_TDR_GCS_ENDPOINT="${STORAGE_EMULATOR_HTTP_ENDPOINT/127.0.0.1/host.docker.internal}" \
  GIT_DRS_TDR_SIGNING_PUBLIC_KEY_FILE="$signing_public_key_file" \
  go test -race -tags=integration -count=1 ./cmd/pull \
    -run '^TestIntegrationPullThroughTerraHubAndTDR$' -v; then
  cat "$hub_log" >&2
  cat "$tdr_log" >&2
  cat "$proxy_log" >&2 2>/dev/null || true
  cat "$coord_dir/proxy.out" >&2 2>/dev/null || true
  exit 1
fi

downloads=$(storage_object_download_count)
if [[ "$downloads" != 3 ]]; then
  echo "GCS emulator served $downloads object downloads, want exactly three" >&2
  docker logs "$STORAGE_CONTAINER" >&2 || true
  exit 1
fi

touch "$hub_stop_file"
if ! wait "$hub_pid"; then
  cat "$hub_log" >&2
  exit 1
fi
hub_pid=

touch "$stop_file"
if ! wait "$tdr_pid"; then
  cat "$tdr_log" >&2
  exit 1
fi
kill "$proxy_pid" 2>/dev/null || true
wait "$proxy_pid" 2>/dev/null || true
proxy_pid=
tdr_pid=
stop_firestore_emulator
stop_storage_emulator
trap - EXIT

cat "$hub_log"
printf '\nTDR proxy requests:\n'
cat "$proxy_log"
tail -n 20 "$tdr_log"
