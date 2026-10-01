# Sprint admission checkpoint — 2026-10-01

The seventeenth increment passes local checks. Shared capacity is reserved
before task claims; accepted batches own a five-minute cooperative budget and
release capacity after execution and cleanup return. This is implemented and
fixture-tested behavior. Production execution and restore remain unverified.

## Exact snapshot

Base commit: `db2c116f7cce1ab921eda65cc81c3fbdba53359f`, with accumulated work
preserved. [source-snapshot.json](source-snapshot.json) identifies 204 modified
or added files by SHA-256, size and mode. Its canonical payload digest is
`11fb1a628fecc16d22b56e87284f61b4a82983aeaf1db5fe7bb4c6dbc42af434`.
Unchanged files come from the base commit. Derived graph files and evidence
are excluded. This report and the checkpoint entry in the plan were added
after those checks; they do not change executable behavior.

[binary-sha256.json](binary-sha256.json) identifies the thirteen freshly built
binaries. The dashboard reports the base revision with `dirty=true`; its SHA
is the qualified artifact identity, not a claim that the base commit contains
the changes. Toolchain: Go 1.26.5, Linux ARM64.

## Commands and outcomes

Commands ran from the repository with Go and developer tools on PATH,
`BT_SKIP_LLM_TESTS=1`, isolated `BT_AGENT_HOME`, `GOTMPDIR=/tmp`, and a separate
Go build cache. Temporary roots and credentials are omitted from retained data.

| Command | Outcome / retained evidence |
|---|---|
| `go test -short -count=1 ./internal/dashboard ./cmd/bt-dashboard ./internal/api` | Passed; [focused.log](focused.log). |
| `go test -p 2 -short -count=1 ./...` | Passed; [full-short.log](full-short.log). |
| `go test -p 2 -short -count=1 -race ./internal/dashboard ./cmd/bt-dashboard ./internal/api` | Passed; [race.log](race.log). This is changed-package race coverage, not the full GitHub race/coverage command. |
| `make build BIN_DIR=<isolated-build>/bin` | Passed vet, format, tidy, lint configuration, zero-issue lint, high-severity gosec, thirteen binaries and AST-only graph refresh; [build.log](build.log). |
| `python3 scripts/test_check_arc42.py` | Eleven fixtures passed; [docs-fixtures.log](docs-fixtures.log). |
| `bash scripts/check-doc-drift.sh` | Passed; old walkthrough age remains advisory. |
| `node --test tests/unit/*.test.js` | Fifteen tests passed. |
| `git diff --check` | Passed. |
| Isolated final-binary loopback probes using loaded JSON and `.env` paths | Both passed; [JSON](http-json.json), [.env](http-dotenv.json). |

Network probes cover authenticated reads, rejection before dispatch, loaded
state owners, pending-task restart and stopped-writer fixture backup/restore.
They invoked zero coding processes and started no host BT units. A pending
task surviving restart does **not** prove safety after completed execution
whose result commit failed. In-process result repair does not prove that either.

## GitHub and operational limits

[PR #83](https://github.com/greenTeeProduction/bt-agent-platform/pull/83) at the
base SHA has failing `Test (short)` and separate `gosec` checks. The Actions
Security Scan job passed. [github-checks.json](github-checks.json) preserves
check identities and the actual findings: webhook panic-recovery test expected
three successful posts but got one; gosec flags directory permissions at
`internal/engine/superpowers_apply.go:112`. Local high-severity success does
not establish that GitHub's medium-severity SARIF/code-scanning check passes.

Agent, dashboard and gardener host units are inactive. Deployed build identity,
a controlled real Codex-backed execution and production backup/restore have
not been qualified. No RPO/RTO, fleet ownership or arbitrary-I/O preemption
claim follows from this checkpoint.

## Goal status and next work

The [cleanup plan](../../plans/2026-09-30-arc42-cleanup.md) records explicit
C01–C12 status and a prioritized backlog. Next priority is separate-process
crash recovery: successful or uncertain execution followed by failed recording
must remain held after restart, rather than being automatically dispatched.
