# 8. Crosscutting Concepts

Global [Sol policy](../sol-model-policy.md) defaults enabled and pins ordinary inference/coding to Sol. Owner-approved NotebookLM, embedding/indexing and memory-extraction integrations retain their own configuration. NotebookLM CLI/MCP share a profile; renewal uses a cross-process lock, bounded checks, account validation and 0.14 storage-mode-aware atomic writes under the upstream profile lock. Historical coding-only opt-outs cannot override the global policy.

These are current shared mechanisms and their boundaries. Historical
rationales remain in [§9](09-decisions.md); runtime examples are in
[§6](06-runtime-view.md), acceptance evidence in [§10](10-quality.md).
The shared tree/blackboard model is described in
[§5.2](05-building-blocks.md#52-core-engine); canonical terms are in
[§12](12-glossary.md).

## 8.1 Behavior Tree Execution Model

[`engine/tree.go`](../../internal/engine/tree.go) builds and runs the
serializable definition. Sequence/Selector/action/decorator semantics
compose workflows; PreGate → StrategyRouter → OutcomeSelector is a common
scaffold. It is not required of every valid tree.

`RunTask` initializes chain state, derives a timeout, limits reticks and
preserves approval/quota carryovers. Cancellation is cooperative; a
background-rooted subprocess action can have a different lifetime. Persisted
resume cursors and JSON-decoded state are untrusted input. Node-specific
range checks are tested in QS26/R22. PersistentMemSequence and ForEachTask
restart malformed, negative or high cursors from zero; the sequence permits
its exact completion cursor. Fractional/nonfinite/host-int overflow JSON
values are invalid. ForEachTask reports failure if its child removes the run.

Dashboard execution admission uses `WorkerPool.SubmitWithContext` and
`ConcurrencyLimiter.AcquireWithContext`. Rejected work releases its reservation;
accepted work releases it after execution/evidence cleanup. A buffered result
handoff lets a canceled HTTP waiter leave without blocking the worker. The
legacy Submit/Acquire wrappers retain background-context behavior. Pool shutdown
closes its wakeup signal before waiting for admission locks, so a full queue
cannot deadlock shutdown; previously accepted callbacks still drain.

The lower-layer [command cancellation helper](../../internal/reliability/command_cancel.go)
is shared by engine tool commands and the dashboard Hermes fallback. It creates
a process group, kills that group on cancellation and bounds inherited-pipe
cleanup. Descendants that create their own sessions need separate supervision.
See [ADR-266](09-decisions.md#adr-266) and the runtime limits in §6.1.

## 8.2 ChainAction Nodes

[`engine/chains.go`](../../internal/engine/chains.go) interprets chain
configuration and invokes model/tool collaborators. Build-time validation
checks `IsKnownChainKind`; runtime checks still handle dependency and
output failures. See the canonical kinds in [§5.5](05-building-blocks.md#55-chain-types).
A registered kind or a successful structural build does not prove model
availability or useful output (ADR-006, ADR-130).

## 8.3 MCP Protocol Layer

[`engine/mcp_server.go`](../../internal/engine/mcp_server.go) owns stdio
JSON-RPC dispatch. The three MCP mains enable sanitization, audit and rate
limits. When `BT_API_KEY` is nonempty, `tools/call` must include
`bt_api_key` in its JSON-RPC params; this is **not** HTTP Bearer auth.

Handlers sharing the server blackboard register through
`RegisterBlackboardTool`, which serializes the complete handler body.
Other independent handlers may run concurrently. `Server.Invoke` is an
in-process/test seam that bypasses transport wrapping and must not be
presented as a secured external request path (ADR-123/124).

## 8.4 File-Based Persistence

Core operational state uses JSON/YAML files; history/audit/interaction
streams also use JSONL. Sessions and some caches remain in memory.
Filesystem paths and owners are in [§7.2.2](07-deployment.md#722-storage-and-configuration).

Single-file writers use temporary-file replacement where their store
implements it. Shared read-modify-write operations need a sidecar lock
around the **entire** transaction: `research.UpdatePrograms` and the
agent circuit-breaker store are examples. Atomic rename by itself cannot
prevent a stale writer from losing another process's update.

[`reliability`](../../internal/reliability/reliability.go) offers
context-aware nonblocking-flock polling. Legacy acquisition waits without a
deadline; callers that require bounded waiting must pass a context and
handle acquisition failure. This does not bound arbitrary filesystem I/O.

Knowledge feedback stores retain recent run summaries as well as aggregates
(the historical ADR-105 reference; [`feedback_persist.go`](../../internal/knowledge/feedback_persist.go)).
Platform state uses the shared `util.PlatformHome` precedence: `BT_AGENT_HOME`,
legacy `BT_HOME`, configured definition-directory parent, then `~/.go-bt-evolve`.
The five engine-logging entrypoints validate JSON/.env/environment configuration
and publish immutable startup paths before opening state/log owners (ADR-265).
Explicit definition/history/log settings remain independent directory overrides;
engine logs otherwise use the shared root. Config loading and hot reload do not
migrate active stores. Management CLIs configure paths before registry access;
required agent history failure stops startup. Shared daemon/MCP and in-process reflection/tree/block
owners honor loaded reflection configuration; the legacy `~/.go-bt-reflections`
default remains separate. Dynamic generated-tree lookups use that installed
startup reflection root, including when configuration files are later removed.
Shared feedback commits sum each writer's pending run/evolution observations
once against the latest locked snapshot. Repeated saves do not replay them;
failed saves retain bounded pending history. Runtime fitness follows merged
run evidence, while winning structural fitness and its node count are
monotone and acknowledged in live state. Legacy untracked aggregate snapshots
cannot establish disjoint events and retain max-count reconciliation.
The [shared JSON transaction helper](../../internal/reliability/shared_json.go)
uses a 30-second default lock budget. Experience add/reuse/persist lock waits
also use a 30-second bound; the existing 500-entry cap remains in force. JobStore serializes replacement writes;
it does not merge stale whole-table snapshots. History constrains agent identifiers to local basenames and appends/reads through
the configured `os.Root`; traversal and outward symlinks cannot access another
owner. New history files/directories use 0600/0750; existing modes are not
changed. History caches records only
after successful JSONL append/close, propagates synchronous write errors and
computes all statistics under one read lock. A healthy completed run's
history failure is an `ExecutionPersistenceError`: it remains visible to
callers, does not trip execution breakers, and cannot trigger a retry of the
completed operation. Workflows propagate it through retry/parallel branches
and stop before subsequent steps.
Router and remote adapters retain the diagnostic through optional `error_kind`
metadata. ExecutionUncertainError stops automatic replay after a dispatched call
loses its outcome; explicit trusted non-admission is needed for safe fallback.
Uncertainty takes precedence in joined parallel errors. Workflows preserve
healthy terminal results across racing deadlines and abort uncertain branches
before retry/skip/downstream steps. This does not add durable request idempotency
or automatic evidence reconciliation ([ADR-267](09-decisions.md#adr-267)). Schedule/remove/pause APIs return job-store
write errors; registry YAML and job snapshots still are separate stores.
Neither append/close nor atomic
rename establishes cross-store ACID or power-loss durability.

Dashboard task mutations stage complete snapshots before replacement; failed
writes cannot publish status/output or admit claimed tasks. Approval/rejection
commits an `approval_audit_pending` marker before HITL synchronization. Audit
or marker-clear failure reports a partial-commit diagnostic and blocks admission;
idempotent retries resume synchronization after restart (ADR-263). Workflow
HTTP handlers also report failed task mirrors. Legacy unmarked tasks are not
retroactively reconciled.

HITL reload/update/retention operates on detached records under a 30-second
transaction lock budget; context-aware calls use the caller's shorter deadline
for mutex and sidecar contention. Approval waits return a single committed
status/payload snapshot and propagate expiry-write failures. HTTP/MCP reads
report storage errors rather than silently returning an empty inventory or 404.
Structural sandbox runs simulate approval without consulting or changing the
operational HITL store, as they simulate action effects. This is structural
benchmark evidence, not production approval or side-effect verification.
Gate reuse checks task/agent and node type/name/phase, with separate nested gate
request and post-child completion state. A missing/failed approval store fails
closed. These checks do not establish per-user transport authorization (R29).

Blackboard Set/Append/Delete stage entries, limit accounting and evictions,
replace the persistent file, then publish cache state. Failed mutations/commits
preserve the preceding cache/file; no successful acknowledgement is returned.
SetWithContext bounds in-process scope and sidecar waits by the shorter caller
deadline or ten-second default; compatibility methods use that default for
lock admission. Cancellation is checked before mutation/commit, but cannot
interrupt arbitrary filesystem I/O. Workflow input failure stops before agent
admission. Output-mirror failure retains completed or failed execution evidence
and prevents retry/skip. The two output mirrors remain separate transactions;
a committed first mirror survives failure of the second (ADR-271).

Cross-store workflow/task/approval records are not one ACID transaction.
Optional queue adapters do not turn the default file-based deployment into
a distributed database. Backup/restore and retention remain explicit
operational requirements (R12/R26).

The PR #83 corrections extend the configured-parent rooted I/O contract to
lock sidecars, artifact text, configuration, skills and feedback readers.
Sidecars use `0600`; reported new artifact directories use `0750`. Atomic
replacement also replaces a preexisting leaf symlink or permissive file rather
than following it or retaining its mode. Rooted reads reject escaping leaf
symlinks; operator-configured parent symlinks remain supported. Existing
unmodified directories are not retroactively hardened. Shared transactions
still need a lock; these changes do not add power-loss synchronization.

Private configuration saves intentionally preserve credentials for reload.
Use `Config.Sanitized` for presentation/sharing. Boolean merge behavior is
preserved; tracking presence in original JSON remains a separate compatibility
fix. Process lookup uses fixed `ps` arguments and literal Go matching. Explicit
shell tools and authorized command actions retain their execution contracts;
narrow G204 exceptions record that intent. G404 exceptions apply only to search
seeds and retry timing, never authentication tokens. Pipeline status logs omit
untrusted outcome/error text; private execution records retain those details.

## 8.5 Evolution Pipeline

The canonical runtime is [`gardener/evolve_v2.go`](../../internal/gardener/evolve_v2.go).
Per-tree reflection evidence, structural fitness and genuine runtime fitness
serve different purposes and must remain attributable to their owner.

Reflection selection is strict for shared and personal trees. Missing
tree attribution remains inspectable but is not assigned to an unrelated
tree. Personal records require an exact owner match; an empty owner denotes
shared evidence, not a wildcard. Catalog aliases reconcile `domain_name`
with `domain:name` (and the finance/research catalogs) without borrowing
other trees' outcomes. See [attribution regressions](../../internal/gardener/evidence_attribution_test.go).

### Governance fitness and live benchmark evidence

[Governance assessment](../../internal/evolution/governance.go) scores controls
on executable task paths: input guards (25%), result checks (25%), declared
JSON task contracts (10%), agent
instructions actually consumed at runtime (20%), execution bounds (15%) and
bounded recovery (5%). Coverage saturates per work node. Node count,
decorative depth, documentation-only descriptions and unconditional checks
such as the current `CheckConfidence` earn no control credit. A final check
covers the last result, not every intermediate worker. Recovery-only trees
receive zero governance credit.

The history-based composite assigns 10% to this structural governance signal;
it no longer rewards deleting nodes. Structural quick/Pareto/MCP scorers use
governance rather than guessed success/speed from tree size. Candidate
preservation rejects deleted task capabilities or stripped existing controls;
crossover retains the first parent's contract. Heuristic and MCTS generators
can propose executable input/result wrappers. Ordinary acceptance requires a
positive score change, followed by validation. None of these static scores
prove task impact or version-specific runtime success.

[Live benchmarks](../../internal/benchmark/live_model.go) use
`BT_BENCHMARK_BACKEND=ollama` and `BT_BENCHMARK_MODEL=qwen2.5:1.5b` by default.
`BT_BENCHMARK_OLLAMA_URL` scopes the endpoint; `BT_BENCHMARK_TIMEOUT` defaults
to 15 seconds per local call. Timeout/unavailability selects Sol 6.1 through
Codex login without changing the ordinary Sol-only policy. Paired comparisons
are repeated after a provider switch. Records retain model/call provenance,
actual output and declared JSON-field/length/quality/outcome contract results. Short tests
skip live-model integration; they do not replace it with synthetic inference.

Live runs use engine `NodeAdmission` to execute supported model actions and
fail explicitly when a task needs an unavailable isolated capability fixture.
They no longer make every action succeed through the structural sandbox.
A zero-model-call or unsupported run cannot qualify a promoted candidate.
The `bt-tree-integration` command uses the same provider and records per-tree
model evidence, warnings and task-contract rates; its report verifier rejects
missing inference, unsupported capabilities and insufficient contract results.
The [controlled live gate test](../../internal/benchmark/live_governance_test.go)
verifies bad-output rejection, validated recovery, empty-input rejection and
an independently checked arithmetic result. It is mechanism evidence, not a
fleet SLO or proof that domain tool workflows have been qualified.

A/B clones retain prompts, token limits, nested metadata and typed edges.
External adapters report model provenance and qualification warnings; missing
inference cannot earn correct-route or task-success credit. Their historical
output-matching metrics still need task-specific environment/postcondition
fixtures before they can establish real-world impact.

Population search offers governance mutations alongside block proposals;
Q-learning uses the same catalog. Pareto and MAP-Elites archives snapshot tree
and score together, so population sorting cannot corrupt their correspondence.
`Retry` bounds failed attempts and returns immediately on success; it no longer
uses a repeat decorator that re-executes successful work.

`QualityGate`, including typed quality edges, validates recovery output and
resumes running recovery without replaying primary work. `CheckpointVerifier`
requires one child and a nonempty boolean/string/number contract. Its `state_key`
is explicitly `world_state` (legacy default) or `goap_world_state`; the GOAP wrapper
selects the latter. String facts remain strings, numeric facts retain exact JSON
values through persistence, and missing facts never satisfy expected false.
Malformed declarations fail authoring validation and execution before the child.
Evolution preserves the selected map and every declared fact (ADR-286).

The checkpoint retains its attempt snapshot and retry budget across `Running`
ticks and caches terminal disposition within a run. Snapshots restore only the
selected in-memory state; a committed effect receipt followed by a failed gate
causes an uncertain execution stop and forbids replay. Both gate types preserve
typed execution stops. See [gate regressions](../../internal/engine/governance_gate_test.go)
and [typed checkpoint regressions](../../internal/engine/checkpoint_contract_test.go).
The facts still depend on their producer: a matching map alone does not prove an
external action, and the generic compiled/dynamic GOAP paths still need independent
effect observers before their assertions can establish task impact.

| Path | Current acceptance behavior | Remaining boundary |
|---|---|---|
| Ordinary heuristic/MCTS mutation competition | Shared scored proposals; detached definition/expansion and bounded benchmark checks, fitness/quality/meta-validation, configured predecessor snapshot and persist before live/experience publication | Accepted proposal source/score/reason and MCTS search settings are persisted. Complete cycle replay and default-on cost remain R20/R21. |
| Local parameter refinement | Re-score against target-tree records; whole settled-tree build/benchmark/meta/SLO checks before persistence | A useful parameter fit is not evidence of production task success. |
| Transposition/deep-search candidate | Replay the complete ordered winning proposal; re-score actual candidate, definition/benchmark/quality/meta/SLO checks, configured predecessor snapshot, then persist before live/experience publication | Cached scores include reflection evidence; a warm-cache winner remains replayable. Runs only when configured/enabled. |
| Island champion adoption | Evidence, improvement/bloat, validation, whole-tree definition/expansion preflight and quick benchmark, quality/meta-validation, configured predecessor snapshot, then persist before live assignment | Benchmark evaluation uses a real local Ollama model with Sol fallback; quick evidence is bounded. Snapshot/write failure preserves the live predecessor. R23 regression contracts apply. |
| Durable-archive MCP evolution tools | Tool-specific archive bounds and benchmark checks before persisting winners | Do not infer daemon wiring from an exported algorithm/tool. |

Snapshot revision allocation holds a per-tree index sidecar lock through
reload, unused-number selection, tree write and index commit, with a 30-second
budget or shorter explicit context. Existing unindexed revision files are
preserved and skipped; only committed index entries participate in latest/streak
restore. Names and index order are validated, reads remain within configured
roots and files/directories retain 0600/0700 permissions (ADR-264). TreeStore
metadata also uses private atomic replacement. Tree/index/metadata are separate
commits; interruptions can leave inspectable orphans and no retention cap or
power-loss durability is inferred.

Snapshots support recovery; archives/experience support future search.
Neither is interchangeable with an execution history. Failed snapshot/tree writes
do not acknowledge adoption or record successful mutation experience (ADR-262).
Experience retrieval returns detached proposal payloads. Diversity observations
now clone tree state before archiving; the historical live-pointer aliasing
bug is corrected. Attribution, metrics and production-shaped verification
remain review concerns (R24, ADR-246–253).

## 8.6 Error Resiliency

Restart exclusion is distinct from cancellation and durable recovery.
The shared [restart admission gate](../../internal/reliability/restart_admission.go)
retains process-local leases through actual cleanup. Dashboard owns HTTP and
detached callbacks; gardener owns cycles plus periodic analysis/tool/metadata
work (ADR-279). Its atomic idle seal rejects new admission during restart
handoff; proven rejection reopens it, accepted/uncertain handoff does not. `SafeGoWithCleanup` releases
pipeline ownership after panic handling, including a failing handler (ADR-278).
Worker/limiter/sprint/running-pipeline diagnostics conservatively supplement
leases. Persisted queue entries and waiting pipeline records do not establish
live ownership. These leases do not survive restart and cannot coordinate
another process's arbitrary systemd request; target-owned same-UID control now
covers the new dashboard/gardener sibling paths. Old controllers/manual systemd
calls and bt-agent self/transport ownership remain outside that contract.
Durable recovery claims remain separate.

Durable admission and result recording are separate transitions (ADR-277).
The scheduler commits an in-flight claim before scheduled or manual dispatch;
failed admission dispatches nothing. It retains that claim through history
recording and final state save. Restart converts interrupted claims to inactive
recovery holds, rather than inferring failure and repeating side effects.
Registry reconciliation preserves those holds and blocks clean duplicates.
Failed operator reconciliation leaves the hold intact. Ordinary scheduling
and removal are not recovery decisions. Persistent manual admissions never
become recurring jobs. Read-only scheduler stores cannot admit manual work.

This is single-owner process-restart safety with persisted state present.
Independent stale complete-snapshot writers, state-volume loss, power-loss
durability and transport-wide execution journals remain separate acceptance.
JSONL history is not an atomic multi-store result transaction, and transient
diagnostics may be lost even while the durable claim prevents replay.

[`reliability`](../../internal/reliability) owns panic handling, retry
policies, queues and DLQ primitives. `agent` owns per-agent breaker state and
scheduler outcome policy. `SafeGo` recovers/logs a goroutine panic; its
caller decides whether/how to retry, record a failure or queue work.

`internal/reliability` owns canonical healthy/wait/stop classification and
joined-diagnostic precedence; the `agent.IsHealthyOutcome` facade delegates
there. Engine consumers do not import the higher-level agent package. Use these
shared outcome helpers and the scoring helpers in `internal/reliability`. Expected rate-limit carryover, `no_change`
and `degraded` are not equivalent to an implemented change, even where
they count as breaker success. Retrying model/auth configuration errors
without changing configuration cannot repair them.

File-backed DLQ mutations read current disk membership under a three-second
sidecar lock budget, apply only their delta, atomically replace, then publish
cache state (ADR-280). Lock/write/read failure never authorizes an unlocked
write or successful acknowledgement. Read-only consumers reload with error
handling. Replay persists a random exact claim before dispatch; interrupted,
unrecorded or typed terminal execution is held indefinitely. Ordinary requeue,
scanner, purge and capacity eviction cannot clear it, and stale cooperating
writers cannot resurrect a removed entry. Unreadable/invalid state is preserved
in place and closes admission until explicit repair. The legacy void wrappers
are not acknowledgement APIs. In-memory queues have no restart guarantee;
mixed legacy writers, volume/power loss and operator transport remain open. Provider cooldown is separate from per-agent circuit breaking
(§8.19). Panic recovery is not automatic goroutine restart; code that
recovers outside a ticker loop can still lose that background activity.

## 8.7 Quality Gates

There are separate gates for definition validity, output quality, measured
tree fitness, implementation verification and safe application. Passing one
does not imply passing the rest. Structured/non-LLM results and sandboxed
evolution evaluations have intentional output-check exemptions.

[§10](10-quality.md) names evidence and acceptance criteria; [§11](11-risks-debt.md)
records unmet guarantees. In particular, neither all-green unit tests nor a
healthy analysis fallback proves an end-to-end autonomous change landed.

## 8.8 Tool Protocol

Actions invoke named tools or validated operator-configured executables.
Model/request data must remain arguments, not executable selection or shell
syntax. Coding runners validate absolute executable paths and separate the
prompt from CLI options. Their cancellation helper terminates subprocess
groups rather than only a wrapper process.

This is a command-construction contract, not a universal OS sandbox.
Provider-specific permission enforcement and the writable workspace differ
between implementation and read-only review (§8.19). Trusted operator
configuration still needs filesystem and credential protection.

## 8.9 Research Memory and Quota Economy

[`research`](../../internal/research) stores deduplicated findings and
multi-cycle programs. [`nlm_quota.go`](../../internal/engine/nlm_quota.go)
caches answers and maintains local query/research/import budgets using
Pacific-day accounting. Those caps are platform policy, not a guarantee of
a particular NotebookLM account entitlement.

Research may use NotebookLM, a read-only coding-provider review, or existing
vault context. Exhausted optional research can degrade without aborting the
entire cycle. NotebookLM authentication is separately diagnosed by
[`notebooklmauth`](../../internal/notebooklmauth). Program claims and
charge/refund state need lock-protected updates and explicit retirement.

Research delivery attribution (ADR-287) lives in a separate owner-scoped
`knowledge.json.trace-<owner SHA-256>.json` ledger. A source records the complete
answer digest, bounded excerpt and observation time. Goal identity preserves file
scope while removing transport prefixes and transient planning notes. Completed
tasks receive a receipt only when the actual Git commit belongs to the run, is
reachable from the target checkout (or bare repository master), changes declared
task files and has passing recorded verification. Receipts retain the full commit,
Git tree, changed files, commands/output digests, run/task identity and times.
Only sources observed before the run began receive links in its delivery receipt.

`bt_research_status` reads the current blackboard owner's ledger. It distinguishes
observed goals, deliveries, source-linked deliveries and goals needing review;
runtime adoption and measured impact explicitly remain `not_linked`. Legacy
`goap:implemented` labels, mere knowledge deduplication, dry runs, no-op applies and
repeated passing RED tests do not establish delivery. A research goal with repeated
passing RED commands is held for review rather than awarded completion credit.
The older program-milestone RED-precheck path still needs separate reconciliation.

The ledger uses rooted atomic JSON and a five-second transaction lock. Legacy
knowledge/budget and Superpowers journal saves now reject stale snapshots under the same bounded locking
convention instead of overwriting sibling evidence. New code-delivery attempts
journal pending attribution before applying code. Preflight repairs pending
receipts from run artifacts and Git observations, holding new planning on a repair
failure without re-executing the landed change. Unreadable or misidentified run
journals also hold planning. Legacy unmarked history is not
automatically upgraded. Source write failures remain visible diagnostics, not
fabricated provenance. See [delivery tests](../../internal/engine/research_delivery_test.go)
and [store transactions](../../internal/research/trace_test.go).

## 8.10 Autonomous Landing Pipeline

Superpowers records a run, creates an isolated worktree, executes tasks with
RED/GREEN evidence, verifies and reviews, then applies eligible changes.
The active repository must satisfy materialization and safe-sync
preconditions. Worktree isolation does not authorize bypassing failing
checks or replacing unrelated tracked edits.

Applying a candidate holds a repository-specific, context-cancellable file
lock in the shared run-artifact store through verification and commit. A
dirty shared checkout or an expired lock wait preserves the candidate as
`pending_patch`; the runner never resets staged or unstaged operator/sibling
work. Lint invocations wait on the linter's shared lock through
`run.allow-serial-runners` in [the lint configuration](../../.golangci.yml).
[Landing regressions](../../internal/engine/superpowers_main_preservation_test.go)
exercise real Git index/worktree preservation and cancellation under contention.

Run phase/evidence persistence allows diagnosis after a failed or interrupted
verification; some writes remain best-effort (D6). Per-task snapshots and
explicit apply state allow eligible partial landings/carryover. Use actual
commit/apply evidence before marking a program milestone complete.

Engine tests isolate `BT_SUPERPOWERS_*` settings inherited from a service;
individual tests declare provider/model/failover inputs with `t.Setenv`.
Explicitly enabled live-provider smoke tests retain operator configuration.
Verification failure classification uses the executor's diagnostic, excluding
subprocess logs: fixture messages about RED passes, quotas or pending patches
must not refund a failed implementation or mark unfinished work complete.
Full command output remains in the run evidence. These contracts are covered
by [engine test setup](../../internal/engine/goap_claude_backoff_test.go),
[failure classification](../../internal/engine/actions_goap_fusion_redpass_test.go)
and [runtime regression tests](../../internal/engine/superpowers_failover_runtime_independent_test.go).

Code landing, binary replacement, service restart and documentation sync
are different transitions. The [deployment view](07-deployment.md#73-release-recovery-and-operational-checks)
defines their operational verification. Auto-rebuild/restart are opt-in.

## 8.11 Observability

Use structured logs, traces, history, phase artifacts and process-local
Prometheus metrics together. Dashboard metrics are exposed at
`/api/metrics`, including `bt_build_info`. Several dashboard views merge
shared-file state; this does not make every metric a fleet aggregate.

Liveness, dependency readiness, scheduler breaker health and successful
implementation are different signals. Preserve run/phase identifiers,
selected provider and build identity when diagnosing failure. The
`/api/health` Go-version string reports the running Go runtime after D7;
use build identity separately for the source revision.

## 8.12 A2A Auction Task Allocation

[`a2a`](../../internal/a2a) discovers candidate cards, filters candidates,
collects bids, chooses a winner and delegates. Per-winner breaker entries
use `agent.NewWinnerCircuitBreakerStore` and a separate key prefix in the
shared breaker file, not another independent persistence implementation.
`AuctionDelegateWithContext` is wired through an engine injection hook; the
legacy context-free hook remains available to older embedders. Card lookup,
send and GetTask polling obey one client budget. Explicit same-origin rejection
before admission permits retry; ambiguous sends/reads/polls stop automatically.
Known non-completed task states are non-retryable at this transport/auction
boundary, rather than classified from status text.

Server-owned `bt_execution` metadata carries outcome, error kind and detail in
SDK status events, which merge into stored tasks. It replaces any client-supplied
extension on settled states. Completed history failure retains artifacts and
a typed persistence error. Failed/canceled history diagnostics remain visible
without turning them into successful work. Nested terminal diagnostics preserve
partial evidence and the failed surrounding task state; auction health uses that
state as well as the error type.

The engine retains terminal persistence/uncertainty/known-stop evidence in a synchronized shared
stop across branch blackboards. Built node wrappers stop later admission even
inside Retry/Selector/Sequence, and RunTask stops reticks. RunOnce retains typed
errors and quality diagnostics in history. Completed-child stops leave the
surrounding tree `aborted`; unknown child work leaves it `uncertain`. This does
not undo already admitted parallel work or add task persistence, request
idempotency, or automatic reconciliation (ADR-268/R30).

`ExecutionStoppedError` distinguishes known non-completed work from transient
transport failure. Wire metadata requires a recognized stopped outcome and a
matching task state; dashboard stopped results must not claim success. Legacy
A2A state-enum outcomes remain compatible; contradictory extensions become
uncertainty while retaining received evidence. A known wait (input/auth/approval
or quota carryover) is deferred, not delivered code, and its diagnostic remains
observable. Failed/aborted admitted siblings outrank waits; uncertainty outranks
all known dispositions. Parallel trees retain actual branch results before
shared-stop status conversion, and exclude unadmitted/stop-converted branches.
Workflow groups include ordinary failed/panicked sibling evidence when a typed
terminal branch exists, while normal skips retain their meaning (ADR-269).
A shared sequential workflow policy now covers every loop body step and nested
subworkflow. Approval waits/denials/escalations/errors cannot be skipped or
requested again by on_failure policy. Completed healthy child evidence converts
an ordinary failed surrounding workflow/container to a typed partial stop,
preventing automatic prefix replay; eligible fresh single-step retries and
ordinary explicit skips remain available. Nested results preserve approval IDs,
and creation uses caller-bounded HITL transactions. Pipeline status/browser
waiting is a settled invocation, not a durable resumable execution (ADR-270).

Agent-card HMAC-SHA256 checks depend on configured/persisted signing-key
trust. They do not authenticate arbitrary HTTP task requests; those require
the A2A request-auth policy. Remote execution and peer availability must be
verified before claiming multi-node resilience (ADR-223, ADR-257, R2/R19).

## 8.13 Leaf-Node Structural Validation

[`engine/validate.go`](../../internal/engine/validate.go) rejects invalid
node types, leaf children and unknown chains before activation. Persisted
or generated definitions require the same validation. Engine, evolution
and resolver coverage tests protect these boundaries; a diagram or prose
description cannot substitute for an executable definition.

## 8.14 Fleet-Wide Node Description Coverage

Domain registries define the coverable tree set;
[`DescriptionFor`](../../internal/domains/trees.go) supplies
[catalog consumers](05-building-blocks.md#catalog-and-mcp-interfaces) with
curated, non-registry, resolver-only and external-package descriptions plus
supported aliases. For `domain:<name>`, it strips exactly one prefix and
requires `<name>` in `AllDomainTrees`. This preserves qualified names such
as `domain:arc42:section1` without advertising unregistered domain IDs.

**Tested contract:**
[`TestDomainPrefixedTreesHaveSmokeDescriptionsAndConditionCoverage`](../../internal/domains/domains_test.go)
derives IDs from the registry and checks resolver identity, smoke-task
availability, isolated `BuildTree` construction, canonical-description
parity and descriptions on Condition nodes and guard edges. It also rejects
description lookup for unregistered domain IDs.
Registry/AST-derived tests catch newly reachable trees omitted from coverage
(ADR-250/251/255/258). Typed guard edges are machine data; node descriptions
are explanatory prose. Both matter, and one does not establish the other.

## 8.15 Validation-Gated Composition Activation

`bt_blocks_compose` rejects unknown strategy IDs and validates before
saving. Failed validation or persistence leaves the active tree unchanged.
HITL composition must retain the requested approval/tool blocks.

Implementation: [`blocks_tools.go`](../../cmd/bt-agent/blocks_tools.go).
The historical composition decision and the ExperienceBank-locking
decision both used ADR-024; §9 provides distinct anchors for them.

## 8.16 Self-Extending Claude Error Recovery

The historical node/API name remains `ClaudeErrorHandler`; the configured
delegation provider is Codex under the default policy (§8.19). The decorator first
tries eligible persisted recovery nodes. An unresolved signature may
request a bounded **read-only** proposal using registered vocabulary.

Proposals are validated against recovery policy: merely declaring success
is not a repair, and non-recoverable failure categories cannot be hidden
behind a success marker. Cooldowns, extension caps and disable thresholds
bound repeated attempts. `BT_CLAUDE_ERROR_HANDLER=off` disables this
mechanism; sandbox scoring does not use it. It cannot guarantee that every
new failure learns a usable fix.

Owners: [`error_handler_claude.go`](../../internal/engine/error_handler_claude.go),
[`error_handler_store.go`](../../internal/engine/error_handler_store.go) and
[`error_handler_node.go`](../../internal/engine/error_handler_node.go).

## 8.17 Composable Blocks and Expand-at-Build

`SubTreeRef` nodes resolve through the block registry before engine build.
This gives trees one reusable implementation of planning, tool setup,
approval and recovery stages. Block persistence, registry and interfaces
are in [§5.0](05-building-blocks.md#50-composable-blocks); activation follows
§8.15. Side-effect classifications must reach the actual approval gate,
not remain inert metadata on an enclosing Sequence (ADR-165, ADR-209).

## 8.18 Security and Trust Boundaries

Decision: [ADR-260](09-decisions.md#adr-260).

| Boundary | Implemented contract | Limit / evidence |
|---|---|---|
| Browser → dashboard | Protected routes accept a configured `X-API-Key` or valid `bt_session`; missing configured key fails closed. Login uses the platform key. | Shared operator credential, not individual account/RBAC support; [security route tests](../../cmd/bt-dashboard/security_test.go). |
| Pipeline request → YAML file | Catalog basenames only; shared rooted reads prevent escaping symlinks in both listing and execution. Configured directory symlinks remain an operator relocation mechanism. | [Actual authenticated HTTP/path/inventory tests](../../cmd/bt-dashboard/pipeline_path_regression_test.go); mounted filesystems and directory access remain host trust. ADR-272. |
| Cookie mutation | Double-submit `_csrf_token` cookie and `X-CSRF-Token`; valid API-key clients have a separate key-authenticated path. | Secure transport still depends on actual TLS/proxy configuration. |
| Session state | Cryptographic tokens, hashed server-side storage, expiry and detached validation snapshots. Dashboard cap is 100, TTL 24h; library defaults differ. Expired entries are reclaimed at capacity. | In-memory per dashboard process; restart invalidates sessions. QS27/QS28. |
| Network listener | Default loopback; explicit remote bind requires a configured key. | Does not create firewall rules or a VPN-only boundary; observed remote binds are R25. |
| Rate limiting | Bounded client buckets; at the 10,000-bucket cap, evict a cold bucket and admit a fresh client rather than globally locking out newcomers. | Process-local protection; [capacity regression test](../../internal/security/security_test.go). |
| MCP host → child | Host/OS process access, plus configured JSON-RPC key checks (§8.3). | HTTP headers/Bearer middleware do not describe this transport. |
| A2A peer → service | Task request credentials plus independent card signing/trust checks. | A signed card is not a tenant identity or universal peer trust. |
| Persona ID → stored learning data | User-scoped paths and automation-status gates. | Calling integration must authenticate/bind the user ID; this is not multi-tenant authorization. |
| Coding subprocess / model output | Isolated worktrees, phase-specific tools/sandbox, validation and apply checks. | Operator model/provider and permission overrides are trusted configuration; prompts/results can contain sensitive project data. |
| Logs / research / remote services | Audit and run evidence support diagnosis; provider/config data have distinct owners. | Retention, redaction and off-host data policy need operator review; do not embed secrets in architecture artifacts. |

Protected route definitions add standard JSON error schemas for 401/403
unless explicitly supplied. The response validator chooses an exact status or
an explicit default schema; a missing error schema cannot borrow a success
shape and convert authentication rejection to 500. Undocumented statuses remain
unvalidated, so this is not universal runtime schema coverage. [Shared auth and
response-policy regressions](../../internal/api/auth_response_regression_test.go)
and actual pipeline HTTP tests establish these contracts (ADR-272).

Required fields in the dashboard catalog name declared properties. A recursive
[catalog regression](../../internal/api/schema_catalog_regression_test.go) checks
response, request and parameter declarations to prevent descriptions becoming
required keys. Dynamic event-count, event-attribute and tree-category maps have
no invented required entry names. Actual authenticated audit responses preserve
optional attributes and emit an array, including an enabled empty buffer;
[diagnostic handler regressions](../../cmd/bt-dashboard/diagnostic_schema_regression_test.go)
cover disabled/empty/populated audit state and live metric category maps under
enforcement. This declaration check does not prove every handler payload,
constraint facet or undocumented error response.

Key-rotation/IP-filter primitives existing in `internal/security` are not
proof that every deployed entrypoint uses them. The actual main/middleware
wiring is authoritative.

## 8.19 Coding Provider Policy

Decision: [ADR-261](09-decisions.md#adr-261), extending [ADR-259](09-decisions.md#adr-259).

[`superpowers_provider.go`](../../internal/engine/superpowers_provider.go)
is the shared implementation/review/PR-repair selector for the
[code-improvement building blocks](05-building-blocks.md#57-research-and-code-improvement).
Unset provider means Codex; invalid values fail. Codex-only policy is on by
default, including unset/malformed policy configuration. It rejects Claude
selection/direct execution and disables cross-provider failover. The
current deployment pins that policy on; legacy compatibility requires an
explicit false setting. [Policy tests](../../internal/engine/codex_only_test.go)
cover injected providers and quota failure. Codex's source
default is an exact model pin, while explicit `auto`/`default`/`none` omits
its model flag.
Model entitlements must be checked with the account used by the service.

Read-only review pins restrictive tools or a read-only sandbox even if an
implementation override is permissive. Write-capable phases work in their
isolated repository. Provider-specific durable cooldown files prevent
Claude and Codex quotas from suppressing each other.

With explicit legacy policy opt-out,
`BT_SUPERPOWERS_RATE_LIMIT_FAILOVER=true` permits at most primary →
alternate under the same context, workspace and permission policy. It
does not switch on cancellation, missing executables, auth errors or
unsupported models. If both providers are cooling down, preserve the
earliest eligible retry time. Details and deployment precedence are in
[coding-delegation.md](../coding-delegation.md), and QS29–QS30 test the
contract.

[`delegationPreflightBackoff`](../../internal/engine/superpowers_failover.go)
enforces half-open eligibility: with failover enabled it examines both
providers before returning inactive. For each provider whose latest valid
deadline is at or before `now`, it calls
[`clearDelegationBackoffState`](../../internal/engine/goap_claude_backoff.go)
to remove the shared JSON file, legacy agent-scope blackboard entry and
run-local `ChainState` key. Future deadlines remain active. With failover
disabled, `delegationBackoffActive` applies the same cleanup to the primary.

**Tested contract:** `TestDelegationPreflightBackoff_ClearsExpiredState` in
the [runtime regression tests](../../internal/engine/actions_superpowers_prod_test.go)
covers both provider orderings, deadlines equal to `now`, a missing primary
and preservation of future state. `TestRunSuperpowersRuntime_ExpiredBackoffExecutes`
pins failover on with Claude as primary and verifies that an expired window
is cleared while execution proceeds.

## 8.20 Architecture Documentation Lifecycle

The twelve sections are the current architecture. ADRs retain decision
history, including explicit amendments. A dated deployment observation
does not replace a source default; a proposed quality target is not a
measured result.

Automated section sync receives section-specific guidance from
[GUIDELINES.md](GUIDELINES.md). Its passes are bounded and non-fatal;
structural drift validation remains a separate gate. Review meaningful
claim changes against code/tests, update linked goals/scenarios/risks and
refresh the graph after code changes. The [architecture index](README.md)
defines the maintenance and evidence convention.

## 8.21 Exact Inspection and Safe Tree Presentation

Execution resolution retains its legacy default and thinktank fallback policy.
Inspection disables those substitutions while reusing the same construction
branches. Qualified catalog IDs are not reduced to their last colon segment;
unknown prefixes cannot impersonate a registered suffix. Path separators, NUL
and standalone dot identifiers are rejected before generated-file lookup.
Bare catalog aliases keep historical catalog priority. Generated definitions
remain owned by the injected unscoped resolver; shared operator credentials do
not establish a user identity (R29).

The inspection HTTP success schema matches the bare IR, not an id/structure
wrapper. Root type/name and immediate child type/name are required; descendants
and metadata are preserved, without claiming recursively enforced IR validity.
The mind map uses internally derived structural paths rather than source names
or IDs for actions. Labels, tooltips and load failures are escaped, and unknown
node types use a fixed fallback palette (ADR-273).

## 8.22 Blackboard Initialization and Related Metadata Transactions

Default platform blackboards are persistent owners. NewPersistentManager fails
without publishing a manager when namespace creation fails. NewRunDeps selects
its loaded/configured home once; the standalone lazy path synchronizes one owner
or error through BoardManager. No initialized owner is silently replaced with a
memory-only fallback or retried against another environment root. Explicitly
injected managers can choose memory policy; injection/configuration occurs before
use. Replacing a failed runner after root repair is explicit recovery, not replay
of a completed operation.

SetEntriesWithContext stages a related entry group in one existing bounded
scope/sidecar transaction. It rejects invalid entries and self-evicted groups;
commit succeeds before cache publication. Entry metadata maps are copied at
storage/read boundaries. Successful-run promotion uses that group so output and
attribution cannot be partially replaced. A promotion failure after healthy
execution is ExecutionPersistenceError with retained healthy output/history,
not an instruction to execute the action again. History-write failure can join
the promotion diagnostic. This does not make all run telemetry/memory/other
artifact writes atomic, or make startup/arbitrary filesystem I/O preemptible
(ADR-274).

## 8.23 Sprint Task Results and Metadata Repair

Durably claimed sprint tasks remain in_progress until CommitExecution commits a
complete related result. Failed commit publishes neither a terminal task status
nor a workflow completion. Its output, raw outcome and task/agent/tree/run
attribution remain in process-local sprint diagnostics, alongside task_committed
and commit_error. A healthy execution's independent persistence diagnostic is
committed in task error/error_kind even when the task result itself commits.

New authenticated sprint requests repair retained result metadata against its
original task-store owner before admission. Repair checks in_progress status,
uses the existing bounded sidecar/atomic replacement transaction, and never
calls an executor. A changed owner or operator decision cannot be overwritten.
An existing idempotency key still observes its original job; it does not request
repair or a new execution. Interrupted/uncertain work requires operator
reconciliation, not automatic requeue. Other already-claimed tasks can complete
once even if an earlier task result cannot be persisted (ADR-275).

This is neither cross-store ACID nor durable sprint resumption. Diagnostics are
retained until another batch is admitted or the process ends. Operators must
copy unresolved evidence before restart. TaskStore serializes complete snapshots
but does not merge stale independent-process owners. TaskStore claim/commit context variants now bound mutex and sidecar waits under
the caller's budget; arbitrary filesystem I/O remains cooperative (ADR-276).

## 8.24 Sprint Capacity and Context Ownership

The admission serializer, shared limiter and worker queue observe the request
context, capped at 30 seconds by default. Pool admission precedes task claims;
the accepted callback waits for an explicit decision. Failed claim/no work drains
that callback as a no-op and releases its reservation once. No canceled capacity
waiter can claim or later execute a task. Status mutexes are not held across
limiter or queue capacity waits.

An accepted batch uses context.WithoutCancel for HTTP detachment and a separate
five-minute deadline starting at acceptance, including queue residence. Tasks
inherit that context and retain their existing task budget. Expiry cannot force
an uncooperative action to return; capacity remains owned until cleanup finishes.
Before another dispatch, an expired batch atomically returns only its never
started claims to approved/not_started within a separate 30-second record budget.
Started work is never inferred unexecuted from a timeout. Cleanup failure retains
uncommitted results for the metadata-only repair policy in §8.23 (ADR-276).

One reservation per sequential batch favors ownership simplicity over per-task
fairness. Nil pool/limiter injection retains the existing standalone fallback;
the dashboard startup wires shared controls. Process restart, generic filesystem
preemption, fleet-wide budgets and operational capacity/SLO qualification remain
separate risks; this does not create durable sprint resume.


### Terminal evidence and executable result contracts

`QualityGate.metadata.result_contract` declares `json_fields` (required values),
`required_keys` (required JSON fields), and/or `min_length`. Both primary and
recovery outputs must pass the declared constraints and ordinary output checks.
Malformed/unknown contract fields are validation errors. Contract numeric values
retain exact JSON precision across save/load; adjacent large integers cannot
collapse into the same expected value. A successful JSON contract exempts
only that exact output in that run from the generic prose-length minimum;
changed or unchecked output cannot reuse the exemption. Other output checks
still apply. Enforced JSON task
contracts earn additional governance credit; automatic evolution cannot remove
or rewrite an existing contract on the protected work to improve its score.
See [contract implementation](../../internal/evolution/result_contract.go),
[runtime gate tests](../../internal/engine/result_contract_test.go), and
[governance protection](../../internal/evolution/result_contract_test.go).

Run evidence is written once after terminal execution, rather than from an
intermediate `ReflectOnOutcome` node. Agent runs defer finalization until their
outer output/quality contracts have settled. Records retain owner, canonical
caller tree ID (root name only when unspecified), source-definition SHA-256,
expanded/executed versions, actual result, elapsed time, outcome, diagnostics,
and output-digest-linked gate verdicts. A live mutation records every version
that actually executes at a tick boundary. More than one version cannot count
as an unchanged-version sample. Gate-verdict storage is bounded to 1024 entries;
omission counts prevent a truncated record from qualifying as complete evidence.
Persistence failures are reported without replaying completed work.

Compilation and feedback records have explicit evidence kinds. Neither counts
as task success/latency or satisfies the gardener's execution-evidence gate.
Historical records remain inspectable; missing identity/version is never filled
in retrospectively. `FilterByTreeVersion` provides strict version selection;
full migration of gardener scoring/promotion from tree-level legacy history
remains open. Personal experience-store failures never fall back to the shared
bank. [Terminal writer](../../internal/engine/run_evidence.go),
[outer quality regression](../../internal/agent/run_evidence_test.go), and
[live result recording](../../internal/benchmark/live_evidence_test.go) cover
these contracts. These source changes do not establish deployed adoption.


### Runtime qualification and immutable versions

Gardener and MCP manual/genetic-family publication share
[paired qualification](../../internal/benchmark/runtime_publication.go) and a
[version store](../../internal/evolution/runtime_release.go) (ADR-281/282).
Factory response trees use their original fixed task and declared expected
JSON values. Other suites must provide independent result-value contracts and
isolated capability fixtures. Missing contracts, missing inference, changed
execution definitions or provider-mixed comparisons cannot publish a version.
At least three paired trials must show strictly more passing outcomes and no
regression; every candidate trial must pass. Rejected outputs are retained.

Definitions and accepted proofs are content addressed. A bounded sidecar lock
protects the compare-and-swap of `active.json`; prepared events alone do not
prove adoption. Rollback restores an exact retained ancestor without rerunning
work. Scoped runtime resolvers prefer personal authority, then shared authority,
and refuse corrupt managed definitions. Promoted versions skip unqualified
resolve-time reordering. Owner-bound engine commands reject another user.

Returned gardener improvement metrics use measured pass counts and definition
versions. Single-mutation experience can inherit that measured gain; a batch
cannot assign its entire gain to each individual operation. Legacy search
ranking still uses heuristic/history estimates and can miss useful candidates.
MCP proposals are retained separately from runnable definitions. Only committed
qualified publication credits shared discovery; personal manual evolution stays
out of the shared graph. GA heuristic mutations no longer record their estimated
gains as new experience. Legacy lineage-skip/archive estimates, unmanaged
resolve-time ordering, complete external-task corpora, legacy file concurrency
during first adoption and deployed rollout remain open.
Operator logs retain generated run IDs, registered route/method identity,
status, timestamps and counts. Runner/scheduler/response-validator diagnostic
fields do not copy raw request identifiers, schedules, validation fields or
error text into these log contexts. Protected results/history and encoded
client responses retain execution/validation detail; this is selective source
hardening, not universal log redaction or production logging qualification.

---

Workflow loading in `internal/agentexec.LoadPipeline` validates a local name and
reads through an `os.Root` opened on the configured workflow directory. Nested
local workflows and operator-relocated roots remain supported; traversal and
outward symlinks cannot read outside the opened root. Pipeline metric durations
are clamped nonnegative before conversion, matching the dashboard executor.

### Offline restoration is separate from qualified evolution

Recovery restores current authored definitions, without assigning task-success
credit. Exact-filename mapping prevents a corrupted root name from selecting
another tree's replacement. Plans are sealed in process, originals are checked
again before writes, and private backups precede changes. A bounded sidecar lock
serializes cooperating recovery commands. Legacy writers do not honor it, so
`--offline` is a required operator assertion, not a distributed exclusion proof.
Atomic per-file writes do not make the batch transactional or power-loss durable;
the manifest retains per-item completion. Unknown/personal trees and managed
versions keep their own authorities. Seven already implemented engine node types
are now recognized by schema validation; stateful review/task loops still require
isolated fixtures in live benchmarks (ADR-283).

### Automation approval persistence

`AutomationStore` uses rooted private atomic JSON replacement and a bounded
five-second sidecar wait around complete mutations. Records retain the raw owner;
a colliding sanitized workspace cannot adopt a different recorded owner. Cold
start and unowned legacy records remain distinct from corrupt storage. Reservation,
cap checks and resolution share the transaction. Agent creation independently
checks current disk state under a bounded per-definition lock; exact idempotence
compares all configuration after normalizing timestamps/default version.

The ledger and agent YAML are separate commits. A definition prepared before a
failed ledger commit remains gated; this is not a distributed transaction or a
power-loss durability guarantee. Legacy raw writers/updates do not all share the
creation lock. ADR-285 adds version consent and missing-ledger admission for marked autopilot
trees. Stale feedback-review identities and incomplete-reservation repair remain.

### File task effect evidence

`FileTask` uses a runtime-injected owner root; raw owner hashes isolate colliding
legacy sanitized paths. Only relative paths are accepted. Rooted operations prevent
escaping through nested symlinks, regular files are size-bounded, and atomic private
replacement avoids partial outputs. A five-second context-bounded lock serializes
cooperating writers per declared destination path. Readback checks both committed bytes and the
declared JSON contract; the unchanged input snapshot is checked before commit.
Terminal disposition is cached within the run. Failed post-write verification is
uncertain and cannot trigger automatic replay. The receipt is an observation at
completion, not continuous integrity or a distributed/power-loss guarantee.

Generic live benchmarks deny this effectful node unless a dedicated isolated
fixture supplies its real filesystem capability. Governance preservation prevents
removing or redirecting existing file contracts. Qualified publication does not yet
support general file-task corpora. Personal scheduler runs do not feed the shared
knowledge graph; their exact execution records retain owner/version/effects.

---

*Generated by bt-agent arc42 pipeline — section8Concepts tree*
