#!/bin/bash
# notebooklm-auth-rotate.sh
# Called by auth guardian cron when tokens are stale/expired.
set -euo pipefail

export PATH="$HOME/.local/bin:$PATH"
STATE_FILE="/mnt/ssd/clawd/wiki/bt-research/state/nlm-auth-state.txt"
mkdir -p "$(dirname "$STATE_FILE")"

echo "[$(date -Iseconds)] Starting rotation..."

# Step 1: Try standard login (uses local Chrome)
if nlm login 2>&1 | tee /tmp/nlm-rotate.log | grep -q "Authentication valid"; then
    echo "ROTATED:standard $(date -Iseconds)" > "$STATE_FILE"
    echo "AUTH_ROTATED: standard login succeeded"
    exit 0
fi

# Step 2: Check if it's a headless issue
if grep -q "cannot open display\|DISPLAY\|chrome.*not found" /tmp/nlm-rotate.log 2>/dev/null; then
    echo "FAILED:headless $(date -Iseconds)" > "$STATE_FILE"
    echo "AUTH_FAILED: headless environment — manual intervention required. Run 'nlm login' from a desktop terminal."
    exit 1
fi

# Step 3: Unknown failure
echo "FAILED:unknown $(date -Iseconds)" > "$STATE_FILE"
echo "AUTH_FAILED: unknown error. Check /tmp/nlm-rotate.log"
cat /tmp/nlm-rotate.log
exit 1
