#!/usr/bin/env bash
# Local checks aligned with BT Platform CI (Lint + Security + Test + Build).
# Usage: scripts/check.sh <mode>
#   quick          vet, fmt, mod-tidy, golangci-lint (fast pre-push)
#   full           quick + security-high + race tests + build (+ advisory extras)
#   build          build-quality (vet/fmt/tidy/golangci/gosec-high) + all Makefile binaries + graphify update
#   vet | fmt | mod-tidy | golangci | golangci-verify | graphify-update
#   security-high  gosec high severity (uses .gosec.json)
#   security-medium gosec medium severity (SARIF job parity)
#   test           short tests with race (BT_SKIP_LLM_TESTS=1)

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "${ROOT}"

if [[ -z "${GO:-}" && -x /usr/local/go/bin/go ]]; then
  GO="/usr/local/go/bin/go"
else
  GO="${GO:-go}"
fi

if [[ -z "${GOFMT:-}" && -x /usr/local/go/bin/gofmt ]]; then
  GOFMT="/usr/local/go/bin/gofmt"
else
  GOFMT="${GOFMT:-gofmt}"
fi

if [[ "${GO}" == */* ]]; then
  export PATH="$(dirname "${GO}"):${PATH}"
fi

if GOPATH_BIN="$("${GO}" env GOPATH 2>/dev/null)/bin"; then
  export PATH="${GOPATH_BIN}:${PATH}"
fi

GOLANGCI="${GOLANGCI:-golangci-lint}"
GOSEC="${GOSEC:-gosec}"
GRAPHIFY="${GRAPHIFY:-graphify}"
GRAPHIFY_ON_BUILD="${GRAPHIFY_ON_BUILD:-1}"
BUILD_CHECKS="${BUILD_CHECKS:-1}"
GOSEC_CONF="${GOSEC_CONF:-.gosec.json}"
GOSEC_EXCLUDE="${GOSEC_EXCLUDE:-G404,G304,G703,G704,G115}"
GOTOOLCHAIN="${GOTOOLCHAIN:-go1.26.3}"
export BT_SKIP_LLM_TESTS="${BT_SKIP_LLM_TESTS:-1}"
export GOTOOLCHAIN
TEST_TIMEOUT="${TEST_TIMEOUT:-300s}"

BIN_DIR="${BIN_DIR:-bin}"
BINARIES="${BINARIES:-bt-agent bt-evaluator bt-langagent bt-dashboard bt-gardener bt-agent-cli bt-security-probe bt-ci-doctor bt-tree-integration benchcmp bt-scalability-probe}"

step() { echo ""; echo "→ $*"; }
ok() { echo "  ✓ $*"; }
fail() { echo "  ✗ $*" >&2; exit 1; }

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "  ✗ missing command: $1 (run: make tools-install)" >&2
    exit 1
  fi
}

run_vet() {
  step "go vet"
  "${GO}" vet ./...
  ok "go vet"
}

run_fmt() {
  step "gofmt check"
  local unformatted
  unformatted="$("${GOFMT}" -l .)"
  if [[ -n "${unformatted}" ]]; then
    echo "${unformatted}"
    fail "gofmt: files need formatting (run: make fmt)"
  fi
  ok "gofmt"
}

run_mod_tidy() {
  step "go mod tidy check"
  "${GO}" mod tidy
  if ! git diff --exit-code go.mod go.sum >/dev/null 2>&1; then
    fail "go.mod or go.sum out of sync (commit after go mod tidy)"
  fi
  ok "go mod tidy"
}

run_golangci_verify() {
  require_cmd "${GOLANGCI}"
  step "golangci-lint config verify"
  "${GOLANGCI}" config verify
  ok "golangci-lint config verify"
}

run_golangci() {
  require_cmd "${GOLANGCI}"
  run_golangci_verify
  step "golangci-lint run"
  "${GOLANGCI}" run --timeout=5m ./...
  ok "golangci-lint run"
}

run_security_high() {
  require_cmd "${GOSEC}"
  step "gosec high (exclude=${GOSEC_EXCLUDE})"
  "${GOSEC}" -quiet -severity high -exclude="${GOSEC_EXCLUDE}" ./...
  ok "gosec high"
}

run_security_medium() {
  require_cmd "${GOSEC}"
  step "gosec medium (exclude=${GOSEC_EXCLUDE})"
  "${GOSEC}" -quiet -severity medium -exclude="${GOSEC_EXCLUDE}" ./...
  ok "gosec medium"
}

run_test() {
  step "go test -short -race (BT_SKIP_LLM_TESTS=${BT_SKIP_LLM_TESTS})"
  "${GO}" test -short -count=1 -race -timeout "${TEST_TIMEOUT}" ./...
  ok "tests"
}

run_build() {
  if [[ "${BUILD_CHECKS}" != "0" ]]; then
    run_build_quality
  fi
  step "build binaries"
  mkdir -p "${BIN_DIR}"
  local bin
  # VCS-stamp the binaries with the built revision. The main repo is bare, so
  # `go build` cannot resolve VCS info on its own; without this stamp the
  # deployed daemon's revision is "unknown" and DriftStatus is permanently inert.
  local stamp_rev stamp_ldflags=""
  stamp_rev="$(git rev-parse HEAD 2>/dev/null || echo "")"
  if [[ -n "${stamp_rev}" ]]; then
    stamp_ldflags="-X github.com/nico/go-bt-evolve/internal/dashboard.stampedRevision=${stamp_rev}"
  fi
  for bin in ${BINARIES}; do
    "${GO}" build -ldflags "${stamp_ldflags}" -o "${BIN_DIR}/${bin}" "./cmd/${bin}/"
  done
  ok "build"

  if [[ "${GRAPHIFY_ON_BUILD}" != "0" ]]; then
    run_graphify_update
  fi
}

run_build_quality() {
  echo "=== build-quality (runs before each local build) ==="
  run_vet
  run_fmt
  run_mod_tidy
  run_golangci
  run_security_high
}

run_graphify_update() {
  require_cmd "${GRAPHIFY}"
  step "graphify update"
  "${GRAPHIFY}" update .
  ok "graphify update"
}

run_quick() {
  echo "=== check-quick (Lint job parity) ==="
  run_vet
  run_fmt
  run_mod_tidy
  run_golangci
  echo ""
  echo "=== check-quick PASSED ==="
}

run_full() {
  echo "=== check-full (Lint + Security high + Test + Build) ==="
  run_quick
  run_security_high
  run_test
  BUILD_CHECKS=0 run_build
  step "govulncheck (advisory)"
  if command -v govulncheck >/dev/null 2>&1; then
    govulncheck ./... || echo "  ⚠ govulncheck reported issues (non-blocking locally)"
  else
    echo "  ⚠ govulncheck not installed (run: make tools-install)"
  fi
  ok "govulncheck (advisory)"
  echo ""
  echo "=== check-full PASSED ==="
}

MODE="${1:-}"
case "${MODE}" in
  vet) run_vet ;;
  fmt) run_fmt ;;
  mod-tidy) run_mod_tidy ;;
  golangci-verify) run_golangci_verify ;;
  golangci) run_golangci ;;
  build-quality) run_build_quality ;;
  graphify-update) run_graphify_update ;;
  security-high) run_security_high ;;
  security-medium) run_security_medium ;;
  test) run_test ;;
  build) run_build ;;
  quick) run_quick ;;
  full) run_full ;;
  *)
    echo "Usage: $0 {quick|full|vet|fmt|mod-tidy|golangci|golangci-verify|build-quality|graphify-update|security-high|security-medium|test|build}" >&2
    exit 2
    ;;
esac
