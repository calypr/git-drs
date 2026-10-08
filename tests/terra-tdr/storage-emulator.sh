#!/usr/bin/env bash

start_storage_emulator() {
  local seed_dir=$1
  local requested_port
  requested_port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')
  STORAGE_CONTAINER="git-drs-storage-${PPID}-$$"
  STORAGE_EMULATOR_HOST="127.0.0.1:$requested_port"
  STORAGE_EMULATOR_HTTP_ENDPOINT="http://$STORAGE_EMULATOR_HOST"
  export STORAGE_EMULATOR_HOST
  export STORAGE_EMULATOR_HTTP_ENDPOINT

  mkdir -p "$seed_dir/fixture-bucket"
  printf 'git-drs Terra DRS HTTP integration fixture\n' \
    >"$seed_dir/fixture-bucket/1614321.merge_output.gvcf.gz"

  docker run --detach --rm \
    --name "$STORAGE_CONTAINER" \
    --publish "127.0.0.1:$requested_port:8000" \
    --volume "$seed_dir:/seed:ro" \
    fsouza/fake-gcs-server@sha256:797ce226d62f947c009dc40246b30cfb456b8473d8241407f9d6f2c04e4d69ef \
    -backend filesystem \
    -data /seed \
    -filesystem-root /storage \
    -scheme http \
    -port 8000 \
    -public-host "$STORAGE_EMULATOR_HOST" >/dev/null

  for ((attempt = 0; attempt < 60; attempt++)); do
    if curl --fail --silent "$STORAGE_EMULATOR_HTTP_ENDPOINT/_internal/healthcheck" >/dev/null; then
      return 0
    fi
    local running
    running=$(docker inspect --format '{{.State.Running}}' "$STORAGE_CONTAINER" 2>/dev/null || true)
    if [[ "$running" != true ]]; then
      docker logs "$STORAGE_CONTAINER" >&2 || true
      stop_storage_emulator
      return 1
    fi
    sleep 1
  done

  echo "GCS emulator did not start on $STORAGE_EMULATOR_HTTP_ENDPOINT" >&2
  docker logs "$STORAGE_CONTAINER" >&2 || true
  stop_storage_emulator
  return 1
}

stop_storage_emulator() {
  if [[ -n "${STORAGE_CONTAINER:-}" ]]; then
    docker rm --force "$STORAGE_CONTAINER" >/dev/null 2>&1 || true
  fi
}

storage_object_download_count() {
  docker logs "$STORAGE_CONTAINER" 2>&1 |
    python3 -c 'import sys; marker = "GET /fixture-bucket/1614321.merge_output.gvcf.gz?X-Goog-Algorithm="; print(sum(marker in line for line in sys.stdin))'
}
