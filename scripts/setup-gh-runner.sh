#!/usr/bin/env bash
# Setup GitHub Actions self-hosted runner on Jetson ARM64
# Usage: ./scripts/setup-gh-runner.sh [--token TOKEN] [--repo nico/go-bt-evolve] [--name jetson-runner]
#
# Prerequisites:
#   - GitHub repo with Actions enabled
#   - A runner registration token (from Settings → Actions → Runners → New self-hosted runner)
#     Or pass --token on command line.
#   - systemd (for service installation)
#   - ARM64 Linux (Jetson AGX Orin or similar)
#
# This script:
#   1. Downloads the latest ARM64 runner binary from GitHub
#   2. Extracts and configures it for the target repo
#   3. Installs a systemd service for persistent execution
#   4. Starts the service

set -euo pipefail

REPO="${REPO:-nico/go-bt-evolve}"
RUNNER_NAME="${RUNNER_NAME:-jetson-runner-$(hostname)}"
RUNNER_DIR="${RUNNER_DIR:-$HOME/actions-runner}"
TOKEN=""

usage() {
    cat <<EOF
Usage: $0 [OPTIONS]

Options:
  --token TOKEN       GitHub runner registration token (required)
  --repo OWNER/REPO   Target repository (default: nico/go-bt-evolve)
  --name NAME         Runner name (default: jetson-runner-<hostname>)
  --dir PATH          Installation directory (default: ~/actions-runner)
  -h, --help          Show this help

Example:
  $0 --token AABBCDDEEFFGGHHIIJJKKLLMMNNOOPPQQRRSSTT

To get a token:
  1. Go to https://github.com/$REPO/settings/actions/runners/new
  2. Select "New self-hosted runner" → Linux → ARM64
  3. Copy the token from the configuration command shown
EOF
    exit 0
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --token) TOKEN="$2"; shift 2 ;;
        --repo) REPO="$2"; shift 2 ;;
        --name) RUNNER_NAME="$2"; shift 2 ;;
        --dir) RUNNER_DIR="$2"; shift 2 ;;
        -h|--help) usage ;;
        *) echo "Unknown option: $1"; usage ;;
    esac
done

if [[ -z "$TOKEN" ]]; then
    echo "Error: --token is required."
    echo "Get one at: https://github.com/${REPO}/settings/actions/runners/new"
    echo ""
    usage
fi

echo "=== GitHub Actions Runner Setup for Jetson ARM64 ==="
echo "  Repo:        $REPO"
echo "  Runner name: $RUNNER_NAME"
echo "  Install dir: $RUNNER_DIR"
echo ""

# --- Step 1: Download runner ---
RUNNER_VERSION="2.321.0"
RUNNER_TARBALL="actions-runner-linux-arm64-${RUNNER_VERSION}.tar.gz"
RUNNER_URL="https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/${RUNNER_TARBALL}"

echo "[1/5] Downloading runner v${RUNNER_VERSION} for ARM64..."
mkdir -p "$RUNNER_DIR"
cd "$RUNNER_DIR"

if [[ -f "./config.sh" ]]; then
    echo "  Runner already downloaded. Removing old config..."
    ./config.sh remove --token "$TOKEN" 2>/dev/null || true
fi

curl -o "$RUNNER_TARBALL" -L "$RUNNER_URL"
echo "  Extracting..."
tar xzf "$RUNNER_TARBALL"
rm "$RUNNER_TARBALL"

# --- Step 2: Install dependencies ---
echo "[2/5] Checking dependencies..."
if ! command -v curl &>/dev/null; then
    echo "  Installing curl..."
    sudo apt-get update -qq && sudo apt-get install -y -qq curl
fi

# Verify Ollama is available (needed for nightly tests)
if curl -s http://localhost:11434/api/tags >/dev/null 2>&1; then
    echo "  ✓ Ollama reachable at localhost:11434"
else
    echo "  ⚠ Ollama not reachable — nightly Ollama tests will skip"
fi

# --- Step 3: Configure runner ---
echo "[3/5] Configuring runner..."
./config.sh \
    --url "https://github.com/${REPO}" \
    --token "$TOKEN" \
    --name "$RUNNER_NAME" \
    --work "_work" \
    --labels "self-hosted,jetson,arm64,ollama" \
    --unattended \
    --replace

# --- Step 4: Install systemd service ---
echo "[4/5] Installing systemd service..."
sudo ./svc.sh install

# --- Step 5: Start service ---
echo "[5/5] Starting runner service..."
sudo ./svc.sh start

# --- Verify ---
echo ""
echo "=== Setup Complete ==="
echo ""
echo "Runner status:"
sudo ./svc.sh status 2>/dev/null || echo "  (check with: sudo ${RUNNER_DIR}/svc.sh status)"
echo ""
echo "Verify at: https://github.com/${REPO}/settings/actions/runners"
echo ""
echo "Useful commands:"
echo "  sudo ${RUNNER_DIR}/svc.sh status   — check runner status"
echo "  sudo ${RUNNER_DIR}/svc.sh stop     — stop runner"
echo "  sudo ${RUNNER_DIR}/svc.sh start    — start runner"
echo "  sudo ${RUNNER_DIR}/svc.sh uninstall — remove service"
echo ""
echo "Runner labels: self-hosted, jetson, arm64, ollama"
echo "These match the nightly.yml runs-on: [self-hosted, jetson, arm64] tag."
