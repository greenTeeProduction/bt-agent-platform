#!/usr/bin/env bash
# Install pinned dev/CI tools into $(go env GOPATH)/bin.
# Versions align with .github/workflows/ci.yml and Makefile.

set -euo pipefail

GO="${GO:-go}"
GOTOOLCHAIN="${GOTOOLCHAIN:-go1.26.3}"
export GOTOOLCHAIN
GOPATH_BIN="$("${GO}" env GOPATH)/bin"
export PATH="${GOPATH_BIN}:${PATH}"

# Match golangci/golangci-lint-action@v9 with version: v2.12.2 (config schema v2).
GOLANGCI_VERSION="${GOLANGCI_VERSION:-v2.12.2}"
GOSEC_PKG="${GOSEC_PKG:-github.com/securego/gosec/v2/cmd/gosec@v2.27.1}"
GOVULN_PKG="${GOVULN_PKG:-golang.org/x/vuln/cmd/govulncheck@latest}"

echo "→ Installing dev tools to ${GOPATH_BIN}"
echo "  golangci-lint ${GOLANGCI_VERSION}"
"${GO}" install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_VERSION}"
echo "  gosec (${GOSEC_PKG})"
"${GO}" install "${GOSEC_PKG}"
echo "  govulncheck"
"${GO}" install "${GOVULN_PKG}"
echo "✓ Done. Ensure ${GOPATH_BIN} is on your PATH."
