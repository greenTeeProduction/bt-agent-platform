#!/usr/bin/env bash
# Local checks aligned with BT Platform CI (Lint + Security + Test + Build).
# Usage: scripts/check.sh <mode>
#   quick          vet, fmt, mod-tidy, golangci-lint (fast pre-push)
#   full           quick + security-high + race tests + build (+ advisory extras)
#   vet | fmt | mod-tidy | golangci | golangci-verify
#   security-high  gosec high severity (uses .gosec.json)
#   security-medium gosec medium severity (SARIF job parity)
#   test           short tests with race (BT_SKIP_LLM_TESTS=1)
#   build          all Makefile binaries

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "${ROOT}"

GO="${GO:-go}"
GOFMT="${GOFMT:-gofmt}"
GOLANGCI="${GOLANGCI:-golangci-lint}"
GOSEC="${GOSEC:-gosec}"
GOSEC_CONF="${GOSEC_CONF:-.gosec.json}"
GOSEC_EXCLUDE="${GOSEC_EXCLUDE:-G404,G304,G703,G704,G115}"
export BT_SKIP_LLM_TESTS="${BT_SKIP_LLM_TESTS:-1}"
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
  step "build binaries"
  mkdir -p "${BIN_DIR}"
  local bin
  for bin in ${BINARIES}; do
    "${GO}" build -o "${BIN_DIR}/${bin}" "./cmd/${bin}/"
  done
  ok "build"
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
  run_build
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
  security-high) run_security_high ;;
  security-medium) run_security_medium ;;
  test) run_test ;;
  build) run_build ;;
  quick) run_quick ;;
  full) run_full ;;
  *)
    echo "Usage: $0 {quick|full|vet|fmt|mod-tidy|golangci|golangci-verify|security-high|security-medium|test|build}" >&2
    exit 2
    ;;
esac
