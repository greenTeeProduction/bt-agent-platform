# Stable arc42 checkpoint — 2026-10-01

Persistent scheduled/manual admissions and sprint claims retain successful or
uncertain work when result recording fails. Separate execution and recovery
processes prove exactly one action across two restarts, including repaired
storage, registry reconciliation and ordinary reapproval/scheduling. The
[restart report](../2026-10-01-restart/README.md) records the fault matrix,
pre-fix failures and limitations. In-process repair alone is not this evidence.

The accumulated changes are preserved in coherent runtime, dashboard,
architecture/evidence, restart-recovery and scanner-correction commits. The
original shared checkout's later unfinished Sol-policy edits remain untouched;
this checkpoint uses an isolated worktree. No merge is implied.

## Exact snapshot and checks

[Source manifest](source-snapshot.json): base
`78d973da85d3e85fa7063735be858a62be903461`, canonical payload SHA-256
`121c9dca911fd891bca33bb9dad472d84b3bc1d8e4f800d6977a3c917cf92ae9`.
Each changed source/doc file is fingerprinted; evidence and derived graph files
are excluded. Subsequent operational evidence/doc commits do not change this
qualified Go implementation. Original runtime/dashboard work is included in
that base's history, with its own preceding verification reports.

| Command | Outcome / evidence |
|---|---|
| `go test -short -count=1 -race -coverprofile=<private>/settled-coverage.out -covermode=atomic ./...`, `GOFLAGS=-p=2` | Passed; 77.7% total statement coverage; [race.log](race.log). |
| `make build BIN_DIR=<private>/checked-bin` | Vet, format, tidy, zero-issue lint, high-severity security gate, thirteen binaries and AST-only graph passed; [build.log](build.log), [checked binary hashes](checked-binaries.json). |
| `go test -short -count=1 -race ./internal/reliability ./internal/config ./internal/engine -run 'Test(FileLock\|SaveFile_Private\|ProcessCheck)'` | Escaping lock sidecar, private config replacement and literal process query regressions pass; [security-regressions.log](security-regressions.log). |
| `go test -short -count=1 -race ./internal/gardener ./internal/config` | Corrected atomic-write fault fixtures and configuration tests pass; [metrics-config-race.log](metrics-config-race.log). |
| `python3 scripts/test_check_arc42.py`; `bash scripts/check-doc-drift.sh` | Eleven fixtures and structural/traceability drift checks pass; [docs.log](docs.log). These do not prove runtime behavior. |
| `gosec -no-fail -fmt sarif -out <private>/settled-results.sarif -severity medium ./...` | The 57 reported PR findings are removed locally; 256 baseline findings remain. [SARIF summary](sarif-summary.json) explicitly records them. `-no-fail` exit zero is not a clean security result. |
| Codex conventions review | Two private-artifact/exception-description findings corrected; read-only follow-up reports no remaining findings in scope. |

Go 1.26.5/Linux ARM64; fixture gates use `BT_SKIP_LLM_TESTS=1`, isolated
`BT_AGENT_HOME`, `GOTMPDIR=/tmp` and a separate build cache. Failed intermediate
checks exposed a legacy fixed-temp/missing-parent assumption and Go 1.26 lint
requirements; they were corrected before the settled run. No failed check is
counted as passing. Sanitized logs replace host/fixture paths; full coverage,
SARIF, immutable binaries and private operational artifacts survive under
`/mnt/ssd/bt-checkpoints/20261001-arc42`, rather than only `/tmp`.

## GitHub acceptance

[PR #83](https://github.com/greenTeeProduction/bt-agent-platform/pull/83) has
separate scanning checks. At `78d973da`, test, lint, build and Actions Security
Scan passed, while gosec failed with 57 alerts and CodeQL with five (three high,
two medium). [Check identities](github-before.json) preserve this distinction.
The original line-112 G301 correction did not close those later failures.
Rooted lock I/O, private atomic artifacts/config, rooted owner reads, fixed
`ps` arguments and status-only logs address their actual annotations. Narrow
G204/G404 exceptions document intentional authorized shell execution and
non-security search/retry randomness; no category/severity gate is disabled.
Updated GitHub outcomes require their own recorded head identity.

## Operational evidence and its limits

[Controlled Codex result](codex-execution.json), [retained probe log](codex.log)
and [overlay provenance](codex-overlay-provenance.json) qualify an actual host
account/CLI execution through BT's real Codex adapter. The initial `gpt-6.1-sol`
attempt failed because that account rejected the model; the single private
`gpt-5.5` probe passed and produced the expected artifact. Shared/global model
settings were unchanged; Claude was never invoked. GPT-5.5 availability for
ChatGPT sign-in at the observation date was checked against the
[official model documentation](https://learn.chatgpt.com/docs/models).
This verifies adapter delivery, not an entire deployed GOAP coding workflow.

[Backup/restore result](backup-restore.json) qualifies offline actual deployment
state in evolve/reflections/gardener roots: 238,472 files/links, 2,117,685,081 file
bytes, stable manifests during backup, every restored hash/link matching, and
zero restored links escaping the isolated root. The archive and detailed
manifests/configuration copies are private. Actual provider reauthentication,
external vault/worktrees, scheduled retention and numeric RPO/RTO remain open.
Deployed clean-build identity and restored-service read acceptance are recorded
separately after qualification; archive equality does not imply them.

Implemented, fixture-tested and operationally verified scopes stay separate.
Single-owner process recovery does not establish power-loss synchronization,
volume-loss recovery, stale fleet writers, other transport journals or an
authenticated operator reconciliation endpoint. Async dashboard ownership must
be included before enabling automatic drift restart; it remains disabled.

## Goal status and next work

Complete: C01, C02, C03, C04, C05, C07. Partial: C06, C08, C09, C10, C11, C12.
Blocked: none. C03's completion is the single-owner scheduler/history and
process-restart contract; C04 is the Codex-only policy, not universal provider
readiness. The [goal table and prioritized backlog](../../plans/2026-09-30-arc42-cleanup.md#stable-checkpoint-c01c12-status--2026-10-01)
retain remaining acceptance and all additional discoveries. Whole-project cleanup
remains active; this report establishes an increment checkpoint.
