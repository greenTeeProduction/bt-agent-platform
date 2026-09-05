#!/bin/bash
# Cron and daemon use the exact same policy, lock and cooldown state.
set -euo pipefail
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
AUTH_BIN="${BT_NOTEBOOKLM_AUTH_BIN:-$SCRIPT_DIR/../bin/bt-notebooklm-auth}"
if [[ ! -x "$AUTH_BIN" ]]; then
    echo "auth_error: bt-notebooklm-auth is unavailable; build/install the helper before enabling rotation" >&2
    exit 1
fi
exec "$AUTH_BIN"
