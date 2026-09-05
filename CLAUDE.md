# BT Agent Platform

Go behavior-tree AI agent framework with MCP servers (`bt-agent`, `bt-evaluator`,
`bt-langagent`), A2A protocol support, and a web dashboard on port **9800**.

## Toolchain — read this first

Go 1.26 is at `/usr/local/go/bin/go` and **not on the non-interactive shell PATH**.
Prefix every go/make invocation:

```bash
PATH=/usr/local/go/bin:$PATH make check-quick
```

(`AGENTS.md` targets Cursor Cloud VMs — its symlink advice does not apply on this
machine.)

## Commands

| Goal | Command |
|------|---------|
| Pre-commit gate (mirrors CI Lint) | `make check-quick` — or `/check` |
| Full gate | `make check-full` |
| Fast tests (`-short -race`) | `make test` |
| Full tests (includes LLM-dependent) | `make test-full` |
| Lint only | `make lint` |
| Build all binaries into `bin/` | `make build` |
| Benchmark regression check | `make benchcmp-check` |

The git pre-commit hook runs gofmt → vet → golangci-lint → mod tidy → doc drift →
ci-doctor → short tests. A PostToolUse hook in `.claude/settings.json` gofmts every
edited `.go` file, so formatting failures there indicate something else.

## Known flake

`TestCMAESOptimizer_Convergence` (`internal/evolution/cmaes_test.go`) is stochastic
and unseeded; it occasionally fails in full runs. Retry it in isolation once before
treating a run as broken — but only if it is the sole failure.

## Conventions

Architecture and merge conventions (package consolidation mapping, engine
injection-hook pattern, ADR-003 persistence) live in
`.claude/skills/project-conventions/SKILL.md` and are enforced by the
`go-conventions-reviewer` agent — run it after merges from origin or changes
touching `internal/engine`.

## MCP

`.mcp.json` registers the project's own `bt-agent` server (stdio, from `bin/bt-agent` —
run `make build` first) and Playwright for dashboard verification. Note: launching
`bt-agent` also starts its A2A server on port 8686; a port conflict with an already
running instance is warned and ignored.
