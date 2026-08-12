#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "$SCRIPT_DIR/common.sh"
load_env
require_tools
print_git_drs_version
require_vars GIT_DRS_GLOBUS_CLIENT_ID

case "${1:-status}" in
  login) git drs auth globus login ;;
  status) git drs auth globus status ;;
  logout) git drs auth globus logout ;;
  *) echo "usage: $0 [login|status|logout]" >&2; exit 2 ;;
esac
