# 8. Crosscutting Concepts

These are current shared mechanisms and their boundaries. Historical
rationales remain in [§9](09-decisions.md); runtime examples are in
[§6](06-runtime-view.md), acceptance evidence in [§10](10-quality.md).

## 8.1 Behavior Tree Execution Model

[`engine/tree.go`](../../internal/engine/tree.go) builds and runs the
serializable definition. Sequence/Selector/action/decorator semantics
compose workflows; PreGate → StrategyRouter → OutcomeSelector is a common
scaffold. It is not required of every valid tree.

`RunTask` initializes chain state, derives a timeout, limits reticks and
preserves approval/quota carryovers. Cancellation is cooperative; a
background-rooted subprocess action can have a different lifetime. Persisted
resume cursors and JSON-decoded state are untrusted input. Node-specific
range checks and their remaining coverage gap are QS26/R22.

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
Cross-store workflow/task/approval records are not one ACID transaction.
Optional queue adapters do not turn the default file-based deployment into
a distributed database. Backup/restore and retention remain explicit
operational requirements (R12/R26).

## 8.5 Evolution Pipeline

The canonical runtime is [`gardener/evolve_v2.go`](../../internal/gardener/evolve_v2.go).
Per-tree reflection evidence, structural fitness and genuine runtime fitness
serve different purposes and must remain attributable to their owner.

| Path | Current acceptance behavior | Remaining boundary |
|---|---|---|
| Ordinary heuristic/MCTS mutation competition | Merge scored proposals, benchmark validation, fitness/quality/meta-validation and tree persistence under configured gates | MCTS provenance/replay and default-on cost remain R20/R21. |
| Local parameter refinement | Re-score against target-tree records before downstream validation | A useful parameter fit is not evidence of production task success. |
| Transposition/deep-search candidate | Validation/meta-validation before commit, with baseline restoration on rejection | Runs only when its configuration and search conditions enable it. |
| Island champion adoption | Evidence, enabled quality gate, improvement/bloat checks and validation before overwrite | Still lacks the ordinary path's benchmark/meta-validation and a pre-migration snapshot: R23. |
| Durable-archive MCP evolution tools | Tool-specific archive bounds and benchmark checks before persisting winners | Do not infer daemon wiring from an exported algorithm/tool. |

Snapshots support recovery; archives/experience support future search.
Neither is interchangeable with an execution history. Diversity observations
now clone tree state before archiving; the historical live-pointer aliasing
bug is corrected. Attribution, metrics and production-shaped verification
remain review concerns (R24, ADR-246–253).

## 8.6 Error Resiliency

[`reliability`](../../internal/reliability) owns panic handling, retry
policies, queues and DLQ primitives. `agent` owns per-agent breaker state and
scheduler outcome policy. `SafeGo` recovers/logs a goroutine panic; its
caller decides whether/how to retry, record a failure or queue work.

Use the canonical outcome helpers in `internal/agent` and scoring helpers
in `internal/reliability`. Expected rate-limit carryover, `no_change`
and `degraded` are not equivalent to an implemented change, even where
they count as breaker success. Retrying model/auth configuration errors
without changing configuration cannot repair them.

File-backed DLQ replay reloads cross-process state before picking up replay
requests. Provider cooldown is separate from per-agent circuit breaking
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

## 8.10 Autonomous Landing Pipeline

Superpowers records a run, creates an isolated worktree, executes tasks with
RED/GREEN evidence, verifies and reviews, then applies eligible changes.
The active repository must satisfy materialization and safe-sync
preconditions. Worktree isolation does not authorize bypassing failing
checks or replacing unrelated tracked edits.

Run phase/evidence persistence allows diagnosis after a failed or interrupted
verification; some writes remain best-effort (D6). Per-task snapshots and
explicit apply state allow eligible partial landings/carryover. Use actual
commit/apply evidence before marking a program milestone complete.

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

Domain registries define the coverable tree set; `DescriptionFor` resolves
curated, non-registry and resolver-only descriptions plus supported aliases.
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
delegation provider can now be Claude Code or Codex. The decorator first
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
| Cookie mutation | Double-submit `_csrf_token` cookie and `X-CSRF-Token`; valid API-key clients have a separate key-authenticated path. | Secure transport still depends on actual TLS/proxy configuration. |
| Session state | Cryptographic tokens, hashed server-side storage, expiry and detached validation snapshots. Dashboard cap is 100, TTL 24h; library defaults differ. Expired entries are reclaimed at capacity. | In-memory per dashboard process; restart invalidates sessions. QS27/QS28. |
| Network listener | Default loopback; explicit remote bind requires a configured key. | Does not create firewall rules or a VPN-only boundary; observed remote binds are R25. |
| Rate limiting | Bounded client buckets; at the 10,000-bucket cap, evict a cold bucket and admit a fresh client rather than globally locking out newcomers. | Process-local protection; [capacity regression test](../../internal/security/security_test.go). |
| MCP host → child | Host/OS process access, plus configured JSON-RPC key checks (§8.3). | HTTP headers/Bearer middleware do not describe this transport. |
| A2A peer → service | Task request credentials plus independent card signing/trust checks. | A signed card is not a tenant identity or universal peer trust. |
| Persona ID → stored learning data | User-scoped paths and automation-status gates. | Calling integration must authenticate/bind the user ID; this is not multi-tenant authorization. |
| Coding subprocess / model output | Isolated worktrees, phase-specific tools/sandbox, validation and apply checks. | Operator model/provider and permission overrides are trusted configuration; prompts/results can contain sensitive project data. |
| Logs / research / remote services | Audit and run evidence support diagnosis; provider/config data have distinct owners. | Retention, redaction and off-host data policy need operator review; do not embed secrets in architecture artifacts. |

Key-rotation/IP-filter primitives existing in `internal/security` are not
proof that every deployed entrypoint uses them. The actual main/middleware
wiring is authoritative.

## 8.19 Coding Provider Policy

Decision: [ADR-259](09-decisions.md#adr-259).

[`superpowers_provider.go`](../../internal/engine/superpowers_provider.go)
is the shared implementation/review/PR-repair selector. Unset provider means
Claude; invalid provider values fail. Codex's source default is an exact
model pin, while explicit `auto`/`default`/`none` omits its model flag.
Model entitlements must be checked with the account used by the service.

Read-only review pins restrictive tools or a read-only sandbox even if an
implementation override is permissive. Write-capable phases work in their
isolated repository. Provider-specific durable cooldown files prevent
Claude and Codex quotas from suppressing each other.

`BT_SUPERPOWERS_RATE_LIMIT_FAILOVER=true` permits at most primary →
alternate under the same context, workspace and permission policy. It
does not switch on cancellation, missing executables, auth errors or
unsupported models. If both providers are cooling down, preserve the
earliest eligible retry time. Details and deployment precedence are in
[coding-delegation.md](../coding-delegation.md), and QS29–QS30 test the
contract.

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

---

*Generated by bt-agent arc42 pipeline — section8Concepts tree*
