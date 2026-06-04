# Security and linting — local checks vs CI

This document describes how to catch **high/critical** security findings and **lint** failures before push, using the same configs as GitHub Actions.

## Quick reference

| Goal | Command |
|------|---------|
| Install pinned tools | `make tools-install` |
| Fast pre-push (Lint job) | `make check-quick` |
| Full parity before PR | `make check-full` |
| gosec high only | `make security-high` |
| gosec medium (SARIF parity) | `make security-medium` |

## CI mapping

| CI job / step | Local command |
|---------------|---------------|
| Lint: `go vet` | `scripts/check.sh vet` |
| Lint: `gofmt` | `scripts/check.sh fmt` |
| Lint: `go mod tidy` | `scripts/check.sh mod-tidy` |
| Lint: `golangci-lint` | `scripts/check.sh golangci` |
| Security: gosec high | `scripts/check.sh security-high` |
| Security: gosec SARIF (medium) | `scripts/check.sh security-medium` |
| Test (short + race) | `BT_SKIP_LLM_TESTS=1 scripts/check.sh test` |
| Build | `scripts/check.sh build` |

## Tool versions

- **Go** — CI uses `actions/setup-go` with `go-version-file: go.mod` (currently **1.26.3**). golangci-lint must run with the same toolchain as `go build`; older runners (e.g. 1.23/1.24) fail typecheck with `package requires newer Go version go1.26`.

Pinned in `scripts/dev-tools.sh` / `make tools-install`:

- **golangci-lint v1.64.8** — matches CI (`golangci-lint-action@v6` with `version: v1.64.8`). Do not use golangci-lint v2 CLI with the current `.golangci.yml` without migrating config.
- **gosec v2.27.1** — same family as CI `go install github.com/securego/gosec/v2/cmd/gosec@latest`.

## Gosec excludes

High/medium local checks and CI `security-high` use `-exclude=G404,G304,G703,G704,G115` (`GOSEC_EXCLUDE` in `scripts/check.sh`). SARIF uses `-conf .gosec.json` for audit settings only.

| Rule | Rationale (summary) |
|------|---------------------|
| G404 | Intentional non-crypto `math/rand` in evolution/mutation paths |
| G304 | Variable file paths where containment is enforced (`os.OpenRoot`, validated dirs) |
| G703 | Path traversal taint on env-derived paths with validation |
| G704 | Dynamic HTTP URLs in tooling |
| G115 | Integer overflow conversions (noisy on large codebase) |

New code should **fix** issues when possible; add `#nosec` or update excludes only with review.

## Implementation checklist (branch `cursor/reusable-tree-blocks-c122`)

- [x] `scripts/check.sh` — `quick` / `full` and per-step modes
- [x] `scripts/dev-tools.sh` — pinned `tools-install`
- [x] Makefile — `check-quick`, `check-full`, `security-*`, `tools-install`; `ci` → `check-full` + `ci-doctor`
- [x] CI — golangci-lint `v1.64.8`, gosec high `v2.27.1` with `-exclude=G404,G304,G703,G704,G115`, tests set `BT_SKIP_LLM_TESTS=1`
- [ ] Pre-commit: optional `make check-quick` (install hook still uses vet-only fast path)
- [ ] Burn down excludes / add `scripts/security-changed.sh` for new-issues-only gosec

## Environment

- `BT_SKIP_LLM_TESTS=1` (default in `check.sh`) — skips live Ollama reachability in `config` runtime tests; matches CI test job.
- `GOLANGCI`, `GOSEC`, `GO` — override binaries if needed.
