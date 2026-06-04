#!/usr/bin/env bash
# Install pinned dev/CI tools into $(go env GOPATH)/bin.
# Versions align with .github/workflows/ci.yml and Makefile.

set -euo pipefail

GO="${GO:-go}"
GOPATH_BIN="$("${GO}" env GOPATH)/bin"
export PATH="${GOPATH_BIN}:${PATH}"

# Match golangci/golangci-lint-action@v6 with version: v1.64.8 (config schema v1).
GOLANGCI_VERSION="${GOLANGCI_VERSION:-v1.64.8}"
GOSEC_PKG="${GOSEC_PKG:-github.com/securego/gosec/v2/cmd/gosec@v2.27.1}"
GOVULN_PKG="${GOVULN_PKG:-golang.org/x/vuln/cmd/govulncheck@latest}"

echo "→ Installing dev tools to ${GOPATH_BIN}"
echo "  golangci-lint ${GOLANGCI_VERSION}"
"${GO}" install "github.com/golangci/golangci-lint/cmd/golangci-lint@${GOLANGCI_VERSION}"
echo "  gosec (${GOSEC_PKG})"
"${GO}" install "${GOSEC_PKG}"
echo "  govulncheck"
"${GO}" install "${GOVULN_PKG}"
echo "✓ Done. Ensure ${GOPATH_BIN} is on your PATH."
