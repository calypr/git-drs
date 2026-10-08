#!/usr/bin/env bash

start_firestore_emulator() {
  local requested_port
  requested_port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')
  FIRESTORE_CONTAINER="git-drs-firestore-${PPID}-$$"
  FIRESTORE_EMULATOR_HOST="127.0.0.1:$requested_port"
  export FIRESTORE_EMULATOR_HOST

  docker run --detach --rm \
    --name "$FIRESTORE_CONTAINER" \
    --publish "127.0.0.1:$requested_port:8080" \
    --env CLOUDSDK_CORE_PROJECT=git-drs-ci-fixture \
    --entrypoint gcloud \
    gcr.io/google.com/cloudsdktool/google-cloud-cli@sha256:be2f0e582a94c9e06e7a384fe2b3b39d590eeb8da65a110eda991598c100fe80 \
    emulators firestore start --host-port=0.0.0.0:8080 >/dev/null

  for ((attempt = 0; attempt < 60; attempt++)); do
    if python3 - "$requested_port" <<'PY'
import socket
import sys

try:
    with socket.create_connection(("127.0.0.1", int(sys.argv[1])), timeout=1):
        pass
except OSError:
    raise SystemExit(1)
PY
    then
      return 0
    fi
    local container_running
    container_running=$(docker inspect --format '{{.State.Running}}' "$FIRESTORE_CONTAINER" 2>/dev/null || true)
    if [[ "$container_running" != true ]]; then
      docker logs "$FIRESTORE_CONTAINER" >&2 || true
      stop_firestore_emulator
      return 1
    fi
    sleep 1
  done

  echo "Firestore emulator did not start on $FIRESTORE_EMULATOR_HOST" >&2
  docker logs "$FIRESTORE_CONTAINER" >&2 || true
  stop_firestore_emulator
  return 1
}

stop_firestore_emulator() {
  if [[ -n "${FIRESTORE_CONTAINER:-}" ]]; then
    docker rm --force "$FIRESTORE_CONTAINER" >/dev/null 2>&1 || true
  fi
}
