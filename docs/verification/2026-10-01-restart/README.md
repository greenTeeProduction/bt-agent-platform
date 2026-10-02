# Process restart recovery checkpoint

Persistent scheduler admissions and sprint claims now prevent automatic replay
after successful or uncertain actions whose recording failed. Separate execution
and restart processes verify exactly one durable action across two restarts.
This establishes implemented and fixture-tested process safety with state
present; production restore and power-loss durability remain separate.

## Snapshot and checks

Base: `0ae397f6`, plus the exact files in [source-snapshot.json](source-snapshot.json).
Canonical payload SHA-256:
`d46d7ae6a33175291de044e7191f2a5284e8bc736ebd49c11851dbf7c94b743c`.
Evidence and derived graph files are excluded. The report and final plan entry
were added after checks. [Binary identities](binary-sha256.json) qualify thirteen
freshly built artifacts, with dirty-worktree build metadata at this snapshot.

| Command / environment | Outcome |
|---|---|
| `go test -short -count=1 -race -coverprofile=<private>/coverage.out -covermode=atomic ./...` with `GOFLAGS=-p=2` | Full suite passed; [ci-race.log](ci-race.log). Matches CI race/coverage semantics with two local package workers. GitHub's runner outcome is still separate evidence. |
| `make build BIN_DIR=<isolated>/bin` | Passed vet/format/tidy/zero-issue lint/high-severity gosec/thirteen builds/AST graph; [build.log](build.log). |
| `python3 scripts/test_check_arc42.py` | Eleven fixtures passed; [docs-fixtures.log](docs-fixtures.log). |
| `bash scripts/check-doc-drift.sh` | Passed; old walkthrough age advisory. |
| Focused scheduler restart and sprint HTTP restart tests | Passed; [scheduler](restart-focused.log), [sprint](sprint-restart.log). |

Go 1.26.5/Linux ARM64; all these tests used `BT_SKIP_LLM_TESTS=1`, private
`BT_AGENT_HOME`, `GOTMPDIR=/tmp` and a separate build cache. No model/coding
provider was invoked. [The pre-fix failures](restart-red.log) preserve the
admission, unreadable-state and recovery regressions before the implementation.

## What crossed the process boundary

Seven scheduler cases cover scheduled successful/uncertain result-save failure,
history failure, exit immediately after action, and manual result/history/exit
cases. Result failures preserve the real admission file; restart uses a fresh
process and the real writable store. Each case starts two independent recovery
processes. Registry changes, ordinary scheduling and manual dispatch cannot
release the hold. Failed admission executes nothing; unreadable state is retained.

Two sprint cases exercise authenticated actual HTTP handlers and real local BT
actions: failed result recording after success, and exit immediately after the
action. Storage availability is repaired without rewriting admission bytes.
All transient diagnostics disappear at exit. Fresh processes retain `in_progress`
and reject ordinary reapproval; the action count stays one.

Trusted scheduler reconciliation commits before releasing a hold and advances
to a future slot without executing the interrupted work. Its Go API is not
an authenticated HTTP/MCP recovery endpoint. A completed history append followed
by failed job recording is conservatively held, not inferred safe to replay.

## Remaining limits

Uncommitted output can be lost. In-memory-only schedulers have no durable
admission. Independent stale whole-snapshot writers, state-volume loss,
power-loss synchronization, fleet ownership and other transport execution
journals remain open. Automatic dashboard restart must account for asynchronous
ownership. PR #83's separate medium-severity gosec alert is still pending here;
local high-severity success does not close it. Host services remain inactive
at this checkpoint; no production restore or Codex delivery claim follows.

Architecture: [ADR-277](../../arc42/09-decisions.md#adr-277).
Goal status and backlog: [cleanup plan](../../plans/2026-09-30-arc42-cleanup.md).
