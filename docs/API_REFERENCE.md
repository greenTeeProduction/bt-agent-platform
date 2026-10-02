# Go BT Platform — API Reference

This source-backed guide covers the internal Go module
`github.com/nico/go-bt-evolve`. These packages are internal implementation
interfaces, subject to Go's `internal` import restrictions; they are not an
external SDK. The [arc42 building-block view](arc42/05-building-blocks.md)
owns the architecture and package responsibilities.

To inspect current exported declarations from the repository root:

```bash
go doc ./internal/engine
go doc ./internal/agent RunDeps.RunOnce
go doc ./internal/evolution SerializableNode
```

## Packages at a glance

| Package | Responsibility |
|---|---|
| [`a2a`](#package-a2a) | Peer discovery, task transport, bidding/award and card trust |
| [`agent`](#package-agent) | Agent registry, scheduler, history, memory, events, breaker persistence and deploy drift |
| [`agentexec`](#package-agentexec) | Assemble run dependencies and scoped generated-tree resolution |
| [`api`](#package-api) | Dashboard route/schema descriptions and validation support |
| [`audit`](#package-audit) | Append-only task audit records |
| [`benchmark`](#package-benchmark) | Tree suites and measured acceptance evidence |
| [`blackboard`](#package-blackboard) | Scoped key/value state and persistence |
| [`blocks`](#package-blocks) | Reusable subtrees and composition |
| [`cicd`](#package-cicd) | CI/workflow diagnostics |
| [`config`](#package-config) | Load, default and validate platform settings |
| [`dashboard`](#package-dashboard) | HTTP-facing task/workflow services, execution adapters and metrics |
| [`domains`](#package-domains) | Built-in domain trees, descriptions and resolver hooks |
| [`doormate`](#package-doormate) | DoorMate-specific intent/profile features |
| [`engine`](#package-engine) | Build/tick trees, registered actions/conditions, chains, MCP transport and implementation workflow |
| [`evaluator`](#package-evaluator) | Score trees and order/search mutations |
| [`evolution`](#package-evolution) | Serializable tree model, mutation/search algorithms, fitness and archives |
| [`factory`](#package-factory) | Compile skill specifications into trees |
| [`fusion`](#package-fusion) | Multi-model panel, judging and synthesis |
| [`gardener`](#package-gardener) | Observe and evolve registered/global/personal trees |
| [`goap`](#package-goap) | World state, goals, canonical A* planning and plan compilation |
| [`hitl`](#package-hitl) | Approval request storage and decisions |
| [`knowledge`](#package-knowledge) | Tree capabilities, discovery, feedback, breeding and impact graph |
| [`llm`](#package-llm) | Configurable model adapters, fallback and health |
| [`notebooklmauth`](#package-notebooklmauth) | NotebookLM authentication diagnosis/recovery and browser integration |
| [`persona`](#package-persona) | User profiles, interactions, habits and tracked automations |
| [`reliability`](#package-reliability) | Panic/retry primitives, locks, DLQ, queues and routing |
| [`research`](#package-research) | Deduplicated knowledge, goals/programs and quota-related state |
| [`security`](#package-security) | HTTP/session/auth primitives, rate limits, CSRF, input/path checks and probes |
| [`startup`](#package-startup) | Company/sprint simulation and shared company state |
| [`thinktank`](#package-thinktank) | Multi-role analysis and recommendations |
| [`tracing`](#package-tracing) | Run/tool tracing and OpenTelemetry integration |
| [`util`](#package-util) | Small shared utilities |

## Execution and registration contracts

```go
func BuildTree(serTree *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard]
func RunTask(bb *Blackboard, tree btcore.Command[Blackboard]) string
func RegisterAction(name string, fn ActionFunc)
func RegisterCondition(name string, fn ConditionFunc)
func GetAction(name string) ActionFunc
func GetCondition(name string) ConditionFunc
func ValidateTree(tree *evolution.SerializableNode) []string
```

`GetAction` and `GetCondition` return a registered handler, or nil for an
unknown name. `ValidateTree` returns validation messages; an empty slice
means the tree passed those checks. Use the fuller verifier and activation
contracts described in [arc42 §8.15](arc42/08-crosscutting-concepts.md#815-validation-gated-composition-activation)
when changing the active tree. Node execution uses success `1`, running `0`
and failure `-1`; persisted resume state belongs to the run blackboard.

`RunDeps.BoardManager()` returns `(*blackboard.Manager, error)`. Default owner
initialization must succeed before execution; failed initialization returns no
memory substitute. An initialized owner/error remains fixed for that runner;
repair the root and construct a new runner to recover. Explicitly injected
managers retain their startup policy. HTTP pipeline startup returns 503 before
reserving work when its owner is unavailable; blackboard reads and MCP tools
also expose initialization failure. See [ADR-274](arc42/09-decisions.md#adr-274).

`RunDeps.RunOnce` resolves an agent, executes its tree and returns a
`*RunResult` plus an error. Inspect both: a completed operation can still
report a persistence failure. Successful-run
promotion groups output/task/run/session/time metadata in one transaction;
failed promotion retains healthy output and records a typed persistence
diagnostic in history. Inspect that diagnostic before replaying work. Agent definitions, registry and run options
are owned by `internal/agent`; see `go doc` for their complete fields.

## HTTP and MCP interfaces

The dashboard serves its current HTTP contract at `/api/openapi.json`.
Route definitions live in [internal/api/openapi.go](../internal/api/openapi.go).
Protected routes require `X-API-Key` or a valid browser session; a missing
configured key fails closed. Browser cookie mutations also require CSRF
validation. See [Getting Started](GETTING_STARTED.md) and
[arc42 security boundaries](arc42/08-crosscutting-concepts.md#818-security-and-trust-boundaries).

`GET /api/tree/structure?id=domain%3Aarc42%3Asection1` returns the bare
SerializableNode definition (type/name, children and available metadata).
Qualified IDs retain their namespace; historical bare catalog aliases remain
supported. Missing id returns 400; an unavailable definition returns 404 even
when catalog metadata exists. Inspection uses the operator's unscoped generated
resolver and does not authenticate a tenant. See [ADR-273](arc42/09-decisions.md#adr-273).

Both agent execution POST routes return 503 when their execution service is
unavailable and 408 on a canceled/expired admission or completion wait. Work
may already have started after a 408 or connection loss; inspect run history
before replaying side effects. Accepted work owns its concurrency reservation
until cleanup. A trusted `X-BT-Execution-Admitted: false` rejection permits
remote fallback; status alone does not. Successful response envelopes can still
carry terminal `error_kind` (`persistence`, `uncertain`, or `stopped`); inspect it alongside
raw outcome and error. A stopped result preserves a known failed/canceled/rejected
or waiting task; waiting is deferred rather than completed delivery. Terminal
diagnostics stop automatic retry/fallback. Remote POSTs do not follow redirects, and supplied tasks
must match their echoed result. See [runtime admission](arc42/06-runtime-view.md#61-task-execution-scenario).

MCP runs over the configured host's child-process/stdio boundary. Use that
server's `tools/list` to inspect registrations and schemas; tool availability
differs between `bt-agent`, `bt-evaluator` and `bt-langagent`. HTTP auth is
not a description of the stdio transport.

Pipeline selection accepts a workflow basename with optional .yaml suffix,
inside the configured root. Directory/traversal names return 400; unavailable
or escaping-symlink files return 404. Relative links inside the root work.
Inventory omits invalid/unreadable entries, returns [] for a missing directory
and 503 for other directory failures. Protected routes document standard
401/403 JSON error responses; validation uses exact-status or explicit default
schemas and leaves undocumented statuses unvalidated.

`GET /api/security/audit` returns actual audit counts and recent events to
an authenticated operator. `events` is always an array, including [] for a
disabled or enabled-empty buffer; event attributes are optional. Counts,
attributes and live-metric category maps contain dynamic keys, not mandatory
human-readable description fields. [Diagnostic handler tests](../cmd/bt-dashboard/diagnostic_schema_regression_test.go)
exercise those payloads with response enforcement enabled.

`GET /api/sprint/status` exposes running/job/progress/current_task, elapsed
seconds and task-store-wide tasks_completed/tasks_total. Before any sprint
starts, progress is idle, elapsed is zero and optional started_at is absent.
A completed sprint keeps its tracked progress/start evidence; these counters
are not restricted to tasks in that sprint. Progress can be failed with optional
error/error_kind and per-task diagnostics, including observed output/outcome,
agent/tree/run attribution, task_committed and commit_error. A new authenticated
sprint request repairs retained task metadata before claiming new work; repair
never runs the executor. Reusing an old idempotency key observes its original
job. Storage/conflicting owner/decision failures and execution uncertainty block
new admission with 503. Copy diagnostics before restart/new batch; this is
process-local reconciliation, not durable resumption (ADR-275). New admission
has a caller-bounded 30-second default, reserves shared capacity before claiming
tasks and returns 408/503 with non-dispatch evidence when rejected. Accepted
work has a detached five-minute budget exposed as deadline_at; it retains
capacity until actual cleanup. Expiry returns only never-started tasks as
approved/not_started and preserves started results. Cancellation is cooperative,
not forced wall-clock termination (ADR-276).

Pipeline status (`GET /api/pipelines/status?id=...`) distinguishes `running`,
`waiting`, `complete` and `failed`. Optional `error_kind` and nested `steps`
retain stopped diagnostics and approval task/request IDs. Waiting stops further
work; it does not automatically resume the same run. A completed prefix followed
by failure stops automatic group/outer replay. Approval cannot be bypassed by
`on_failure: skip` or `retry`. Loops execute all body steps; nested subworkflows
share sequential policy. Configured blackboard input-write failure prevents
agent admission. Output-mirror failures retain step evidence and stop retry/skip;
healthy completed work carries `error_kind: persistence`. Separate mirrors do
not form one transaction. See [workflow control](arc42/06-runtime-view.md#63-sprint-execution).

## Package owners

### Package: a2a

Peer discovery, task transport, bidding/award and card trust. Principal interface/consumers: `Server`, `BTAgentClient.SendTask`, `AuctionDelegateWithContext`; agent/dashboard wiring.

The legacy `AuctionDelegate` wrapper remains available. The context-aware hook
shares the caller budget; active SDK tasks are polled by ID. Completed or
unknown execution diagnostics retain received evidence and stop automatic
replay ([ADR-268](arc42/09-decisions.md#adr-268)).

[Source](../internal/a2a) · `go doc ./internal/a2a`

### Package: agent

Agent registry, scheduler, history, memory, events, breaker persistence and deploy drift. Principal interface/consumers: `RunDeps.RunOnce`, `Scheduler`, `AgentCircuitBreakerStore`; entrypoints.

[Source](../internal/agent) · `go doc ./internal/agent`

### Package: agentexec

Assemble run dependencies and scoped generated-tree resolution. Principal interface/consumers: `NewRunDeps`, `ResolveGeneratedTreeForUser`, `AutomationBlocked`.

[Source](../internal/agentexec) · `go doc ./internal/agentexec`

### Package: api

Dashboard route/schema descriptions and validation support. Principal interface/consumers: `DashboardRoutes`; OpenAPI and HTTP middleware.

[Source](../internal/api) · `go doc ./internal/api`

### Package: audit

Append-only task audit records. Principal interface/consumers: JSONL audit writer; agent execution.

[Source](../internal/audit) · `go doc ./internal/audit`

### Package: benchmark

Tree suites and measured acceptance evidence. Principal interface/consumers: `SuiteForTreeNamed`, `QuickValidate`; evolution and verification.

[Source](../internal/benchmark) · `go doc ./internal/benchmark`

### Package: blackboard

Scoped key/value state and persistence. Principal interface/consumers: `Manager`, scopes; agent/run/session state.
`SetWithContext(ctx, scope, key, value, summary, contentType) error` uses the
shorter caller or ten-second default scope/file-lock budget. `Set`, `Append` and
`Delete` stage mutation/eviction and publish cache state after successful file
replacement. These are single-scope transactions; filesystem I/O is cooperative.

[Source](../internal/blackboard) · `go doc ./internal/blackboard`

### Package: blocks

Reusable subtrees and composition. Principal interface/consumers: Block registry, `Expand`, presets; MCP and domain trees.

[Source](../internal/blocks) · `go doc ./internal/blocks`

### Package: cicd

CI/workflow diagnostics. Principal interface/consumers: CI-doctor checks and reporting.

[Source](../internal/cicd) · `go doc ./internal/cicd`

### Package: config

Load, default and validate platform settings. Principal interface/consumers: Shared configuration used by binaries.

[Source](../internal/config) · `go doc ./internal/config`

### Package: dashboard

HTTP-facing task/workflow services, execution adapters and metrics. Principal interface/consumers: Task stores, workflow objects, `AgentExecutor`; dashboard main.

[Source](../internal/dashboard) · `go doc ./internal/dashboard`

### Package: domains

Built-in domain trees, descriptions and resolver hooks. Principal interface/consumers: `AllDomainTrees`, `ResolveTreeIDForUser`, `DescriptionFor`.

[Source](../internal/domains) · `go doc ./internal/domains`

### Package: doormate

DoorMate-specific intent/profile features. Principal interface/consumers: HTTP handlers and persona integration.

[Source](../internal/doormate) · `go doc ./internal/doormate`

### Package: engine

Build/tick trees, registered actions/conditions, chains, MCP transport and implementation workflow. Principal interface/consumers: `BuildAndValidate`, `RunTask`, `Server`, action registry.

[Source](../internal/engine) · `go doc ./internal/engine`

### Package: evaluator

Score trees and order/search mutations. Principal interface/consumers: `EvaluateTree`, `OrderMutations`, transposition table.

[Source](../internal/evaluator) · `go doc ./internal/evaluator`

### Package: evolution

Serializable tree model, mutation/search algorithms, fitness and archives. Principal interface/consumers: `SerializableNode`, populations, gates, reflection/snapshot stores.

[Source](../internal/evolution) · `go doc ./internal/evolution`

### Package: factory

Compile skill specifications into trees. Principal interface/consumers: Skill-to-tree generator; distinct from `knowledge.Factory`.

[Source](../internal/factory) · `go doc ./internal/factory`

### Package: fusion

Multi-model panel, judging and synthesis. Principal interface/consumers: Fusion execution used by chain nodes.

[Source](../internal/fusion) · `go doc ./internal/fusion`

### Package: gardener

Observe and evolve registered/global/personal trees. Principal interface/consumers: `RunCycleV2`, registry, per-tree gates and snapshots.

[Source](../internal/gardener) · `go doc ./internal/gardener`

### Package: goap

World state, goals, canonical A* planning and plan compilation. Principal interface/consumers: `Planner`, `GoalQueue`, `CompilePlanToTree`.

[Source](../internal/goap) · `go doc ./internal/goap`

### Package: hitl

Approval request storage and decisions. Principal interface/consumers: Approval requests/gates; MCP and dashboard.

[Source](../internal/hitl) · `go doc ./internal/hitl`

### Package: knowledge

Tree capabilities, discovery, feedback, breeding and impact graph. Principal interface/consumers: `KnowledgeGraph`, `Factory`; runners and gardener.

[Source](../internal/knowledge) · `go doc ./internal/knowledge`

### Package: llm

Configurable model adapters, fallback and health. Principal interface/consumers: LLM interface; chains, fusion and evaluation.

[Source](../internal/llm) · `go doc ./internal/llm`

### Package: notebooklmauth

NotebookLM authentication diagnosis/recovery and browser integration. Principal interface/consumers: Auth helper used by `bt-notebooklm-auth` and research actions.

[Source](../internal/notebooklmauth) · `go doc ./internal/notebooklmauth`

### Package: persona

User profiles, interactions, habits and tracked automations. Principal interface/consumers: `Store`, automation finalization, feedback escalation.

[Source](../internal/persona) · `go doc ./internal/persona`

### Package: reliability

Panic/retry primitives, locks, DLQ, queues and routing. Principal interface/consumers: Shared reliability APIs; optional adapters are not necessarily deployed.

[Source](../internal/reliability) · `go doc ./internal/reliability`

### Package: research

Deduplicated knowledge, goals/programs and quota-related state. Principal interface/consumers: `KnowledgeStore`, `ProgramStore`, `UpdatePrograms`. `UpdateProgramsWithContext` bounds shared backlog transactions; `MarkDelivered` accepts a receipt only after the engine checks actual Git delivery.

Framework-scoped MCP controls: `bt_program_reconcile` backs up and corrects known unsupported RED-pass completion labels; `bt_program_review` requires `program_id`, zero-based `milestone_index`, `expected_goal` and a changed `revised_goal`. It reopens pending work without completion credit. `bt_research_status` reports program states separately from verified code deliveries. See [ADR-290](arc42/09-decisions.md#adr-290).

[Source](../internal/research) · `go doc ./internal/research`

### Package: security

HTTP/session/auth primitives, rate limits, CSRF, input/path checks and probes. Principal interface/consumers: Shared middleware and `SessionStore`; entrypoints choose wiring.

[Source](../internal/security) · `go doc ./internal/security`

### Package: startup

Company/sprint simulation and shared company state. Principal interface/consumers: `CompanyOrchestrator`, `CompanyState`; dashboard.

[Source](../internal/startup) · `go doc ./internal/startup`

### Package: thinktank

Multi-role analysis and recommendations. Principal interface/consumers: Analysis workflow used by dashboard/company flows.

[Source](../internal/thinktank) · `go doc ./internal/thinktank`

### Package: tracing

Run/tool tracing and OpenTelemetry integration. Principal interface/consumers: Spans and tracing wrappers.

[Source](../internal/tracing) · `go doc ./internal/tracing`

### Package: util

Small shared utilities. Principal interface/consumers: Reused formatting and data helpers.

[Source](../internal/util) · `go doc ./internal/util`
