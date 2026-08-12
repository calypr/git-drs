#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "$SCRIPT_DIR/common.sh"

fixture_sha() { sha256_file "$TEST_ROOT/fixtures/$1"; }
fixture_size() { wc -c <"$TEST_ROOT/fixtures/$1" | tr -d ' '; }

{
  printf 'path\tsize\tsha256\n'
  printf 'a.bin\t%s\t%s\n' "$(fixture_size release/a.bin)" "$(fixture_sha release/a.bin)"
  printf 'nested/b.bin\t%s\t%s\n' "$(fixture_size release/nested/b.bin)" "$(fixture_sha release/nested/b.bin)"
  printf 'nested/c.bin\t%s\t%s\n' "$(fixture_size release/nested/c.bin)" "$(fixture_sha release/nested/c.bin)"
} >"$TEST_ROOT/fixtures/release.tsv"

cat >"$TEST_ROOT/fixtures/checksums.env" <<EOF
GLOBUS_ONLY_SHA256=$(fixture_sha standalone/globus-only.bin)
HTTPS_GLOBUS_SHA256=$(fixture_sha standalone/https-globus.bin)
HTTPS_ONLY_SHA256=$(fixture_sha standalone/https-only.bin)
BROKEN_GLOBUS_SHA256=$(fixture_sha standalone/broken-globus.bin)
RELEASE_A_SHA256=$(fixture_sha release/a.bin)
RELEASE_B_SHA256=$(fixture_sha release/nested/b.bin)
RELEASE_C_SHA256=$(fixture_sha release/nested/c.bin)
EOF

echo "wrote fixtures/release.tsv and fixtures/checksums.env"
