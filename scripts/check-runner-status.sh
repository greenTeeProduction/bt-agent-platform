#!/usr/bin/env bash
# Check if GitHub Actions self-hosted runner is installed and operational.
# Returns JSON evidence for the CI/CD maturity dimension.
set -euo pipefail

RUNNER_DIR="${RUNNER_DIR:-$HOME/actions-runner}"
echo '{'
echo '  "runner_dir": "'"$RUNNER_DIR"'",'
echo '  "installed": '$(if [ -f "$RUNNER_DIR/config.sh" ]; then echo 'true'; else echo 'false'; fi)','

if [ -f "$RUNNER_DIR/config.sh" ]; then
    echo '  "service_status": "'$(if command -v systemctl &>/dev/null; then
        systemctl is-active "actions.runner.*" 2>/dev/null || echo "inactive"
    elif [ -f "$RUNNER_DIR/svc.sh" ]; then
        "$RUNNER_DIR/svc.sh" status 2>&1 | head -1 || echo "unknown"
    else
        echo "unknown"
    fi)'",'
    echo '  "runner_name": "'$(grep -oP '(?<=name": ")[^"]+' "$RUNNER_DIR/.runner" 2>/dev/null || echo "unknown")'",'
    echo '  "labels": "'$(grep -oP '(?<=labels": ")[^"]+' "$RUNNER_DIR/.runner" 2>/dev/null || echo "unknown")'",'
    echo '  "work_dir": "'$(grep -oP '(?<=workFolder": ")[^"]+' "$RUNNER_DIR/.runner" 2>/dev/null || echo "unknown")'",'
    echo '  "repo": "'$(grep -oP '(?<=repository": ")[^"]+' "$RUNNER_DIR/.runner" 2>/dev/null || echo "unknown")'"'
else
    echo '  "service_status": "not_installed"'
fi

echo '}'
