#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 /path/to/jade-data-repo /path/to/terra-drs-hub" >&2
  exit 2
fi

git_drs_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
tdr_root=$(cd "$1" && pwd)
hub_root=$(cd "$2" && pwd)
coord_dir=$(mktemp -d)
port_file="$coord_dir/tdr-port"
stop_file="$coord_dir/tdr-stop"
tdr_log="$coord_dir/tdr.log"
hub_log="$coord_dir/hub.log"
proxy_log="$coord_dir/proxy.log"
hub_compile_log="$coord_dir/hub-compile.log"
certificate="$coord_dir/localhost.crt"
private_key="$coord_dir/localhost.key"
truststore="$coord_dir/truststore.p12"

cp "$git_drs_root/tests/terra-tdr/TerraDrsHarnessTest.java" \
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
keytool -importcert -noprompt -alias git-drs-tdr-harness -file "$certificate" \
  -keystore "$truststore" -storepass changeit

cd "$hub_root"
if ! JAVA_TOOL_OPTIONS="${JAVA_TOOL_OPTIONS:-} -Djavax.net.ssl.trustStore=$truststore -Djavax.net.ssl.trustStorePassword=changeit" \
  ./gradlew :service:testClasses --no-daemon --console=plain >"$hub_compile_log" 2>&1; then
  cat "$hub_compile_log" >&2
  exit 1
fi

tls_port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')

(
  cd "$tdr_root"
  GIT_DRS_TDR_PORT_FILE="$port_file" GIT_DRS_TDR_STOP_FILE="$stop_file" \
    ./gradlew testUnit --tests bio.terra.service.filedata.TerraDrsHarnessTest \
      --no-daemon --console=plain
) >"$tdr_log" 2>&1 &
tdr_pid=$!

proxy_pid=
stop_services() {
  touch "$stop_file"
  if [[ -n "$proxy_pid" ]]; then
    kill "$proxy_pid" 2>/dev/null || true
    wait "$proxy_pid" 2>/dev/null || true
  fi
  wait "$tdr_pid" 2>/dev/null || true
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

cd "$hub_root"
if ! GIT_DRS_TDR_TLS_PORT="$tls_port" \
  GIT_DRS_TDR_HTTP_PORT="$tdr_port" \
  GIT_DRS_TDR_PROXY_LOG="$proxy_log" \
  JAVA_TOOL_OPTIONS="${JAVA_TOOL_OPTIONS:-} -Djavax.net.ssl.trustStore=$truststore -Djavax.net.ssl.trustStorePassword=changeit" \
  ./gradlew :service:test --tests bio.terra.drshub.controllers.TerraDrsHubIntegrationTest \
    --no-daemon --console=plain -x :service:runMinnieKenny >"$hub_log" 2>&1; then
  cat "$hub_log" >&2
  cat "$tdr_log" >&2
  cat "$proxy_log" >&2 2>/dev/null || true
  cat "$coord_dir/proxy.out" >&2 2>/dev/null || true
  exit 1
fi

touch "$stop_file"
if ! wait "$tdr_pid"; then
  cat "$tdr_log" >&2
  exit 1
fi
kill "$proxy_pid" 2>/dev/null || true
wait "$proxy_pid" 2>/dev/null || true
proxy_pid=
trap - EXIT

cat "$hub_log"
printf '\nTDR proxy requests:\n'
cat "$proxy_log"
tail -n 20 "$tdr_log"
