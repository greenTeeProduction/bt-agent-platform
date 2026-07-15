# ClaudeErrorHandler Node — Design

- Date: 2026-07-15
- Status: approved (design review with Nico, 2026-07-15)
- Scope decisions: data-plane decorator (approach A), wrapped around **all domain trees**, **auto-apply with guardrails**

## Problem

When a behavior tree fails, the platform's recovery today is static: `Retry`, `CircuitBreaker`, hand-wired fallback branches, and the goap loop's `ScheduledAnalysisPath`. There is no node that can look at a *novel* error and grow the tree a new handler for it. The fleet's failure notifications (degraded/failure outcomes) land on a human instead.

## Goal

A new decorator node type, `ClaudeErrorHandler`, that on subtree failure:

1. tries previously generated recovery nodes,
2. otherwise asks Claude Code (one guarded, read-only CLI call) to propose **one** recovery node composed of *registered* actions/conditions,
3. strict-validates the proposal, persists it, grafts it into the tree, ticks it immediately,
4. and passes the original failure through unchanged when nothing applies.

The tree is thereby *extended with a new node that can handle the error* — durable across runs and process restarts.

## Non-goals

- No LLM-authored Go code or script payloads (source-plane growth stays with the superpowers pipeline).
- No gardener integration, no dashboard panel, no vault-note escalation for unresolvable errors (possible follow-ups).
- No change to existing degraded/no_change honest-signal semantics — the handler only engages when a failure (−1) reaches it.

## Key constraint discovered during design

Scheduled runs rebuild trees fresh from the compiled catalog every run (`internal/agentexec/deps.go:56-61` → `domains.ResolveTreeID`); persisted gardener `tree-*.json` overrides never reach them. Therefore extensions must be **re-grafted at build time by the node itself** from its own store — no resolver or registry changes.

## Design

### 1. Node semantics

`ClaudeErrorHandler` is a decorator with children:

- Child 0: the protected subtree (set at wrap time).
- Children 1..n: generated recovery nodes, grafted at build time from the extension store (keyed by handler node name).

Tick:

1. Tick child 0. Status 1/0 → pass through.
2. On −1: tick recovery children in order (selector-style). By convention each proposed recovery node is a `Sequence` starting with a guard `Condition`, so non-matching errors fail fast and fall through.
3. If a recovery child returns 1 → return 1; stamp `ChainState["error_handler_recovered"] = <signature>`; append a `## Error Handler Recovery` note to `bb.Result`. Update the extension's success counter.
4. If none handled it: compute the error signature, consult the ledger (cooldown / cap / disabled / kill switch). If any guard blocks → return −1 (passthrough).
5. Otherwise invoke Claude once. `{"resolvable": false}` → stamp ledger, return −1. Valid proposal → append to extension store (atomic), build the node in-process via the existing node-build machinery, tick it once, return its status. 1 → recovered as in step 3. 0 (running) is treated as a failed recovery for this run (−1, ledger stamped) — recovery nodes are expected to be synchronous single-tick compositions; supporting long-running recoveries mid-graft is out of scope.

**Outcome semantics:** on recovery the node returns success and does **not** set `OutcomeRefinement` — the runner's `isHealthyOutcome` whitelist is `success/no_change/degraded` (`internal/agent/runner.go:349-359`), so a novel refinement like "recovered" would be renamed into the outcome and dead-lettered as an error. Recovery visibility comes from the `bb.Result` note and ChainState stamp only.

### 2. Error signature

`sha256(treeName | lastErrorNode | lastErrorCategory | digitStripped(first 200 chars of lastError-or-Result))`, first 12 hex chars. Inputs read from `ChainState["last_error"|"last_error_category"|"last_error_node"]` (populated by `reliability_decorators.go:44-67`) with `bb.Result` as fallback when the reliability layer didn't run.

### 3. Persistence (ADR-003)

Directory `~/.go-bt-evolve/error_handler/` (overridable for tests), JSON via tmp-file + rename:

- `extensions.json` — `map[handlerName][]Extension`; each Extension: the `SerializableNode`, error signature, created-at, success count, consecutive-failure count, disabled flag. Node `Metadata` carries `generated_by: claude_error_handler` + signature.
- `ledger.json` — `map[signature]LedgerEntry`: last attempt time, attempts, last verdict (`proposed`/`unresolvable`/`rejected`/`recovery_failed`).
- A `.bak` copy of `extensions.json` is written before each append (simple rollback; extensions are additive and individually disableable, so whole-tree `SnapshotTree` machinery is unnecessary).
- Concurrent writers (fleet agents share the store): a lock file guards read-modify-write; on contention the handler skips the Claude call this run and passes the failure through.

### 4. Claude invocation

Reuses `ClaudeRunner` (`internal/engine/superpowers_runner.go:24`) with the read-only allowed-tools posture of the GOAP review fallback — Claude *proposes*, it never edits. The node uses a package-level `errorHandlerClaudeRunner ClaudeRunner` defaulting to `defaultSuperpowersClaudeRunner` (read-only tools override), swappable in tests. Model via `resolvedSuperpowersClaudeModel()`. Timeout 180 s.

Prompt contains: tree + failing node name, error category/text excerpt, `FailureCount`, the failing subtree serialized (truncated), the sorted lists of registered action and condition names (new exported `RegisteredActionNames()` / `RegisteredConditionNames()` in `registry.go`), the node-type allowlist, and the contract: reply with only `{"resolvable": true, "node": {…SerializableNode…}}` or `{"resolvable": false, "reason": "…"}`. Parsing extracts the first JSON object defensively.

### 5. Validation (strict, before any graft)

- `SerializableNode.Validate()` passes.
- Node-type allowlist: `Sequence, Selector, MemSequence, MemSelector, Retry, Timeout, Inverter, Succeeder, Action, Condition, AlwaysSucceed`.
- Every `Action`/`Condition` leaf name must resolve via `GetAction`/`GetCondition` — the engine's permissive unknown-name fallback (`tree.go:365-406`) is explicitly NOT acceptable for generated nodes.
- ≤ 10 nodes, depth ≤ 4, names unique within the handler's children.
- The proposal's first-ticked leaf must be a `Condition` (guard), so a generated node can never become an unguarded catch-all that fires on unrelated failures.
- No registry condition on error category exists today (verified), so this feature adds the parameterized condition `LastErrorCategoryIs:<category>` (mirroring the `ApplyGoapEffects:k=v` parameterized-action pattern) for proposals to guard on error class; `LastErrorNodeIs:<name>` is included for node-scoped guards.

### 6. Guardrails

- Per-signature cooldown, default 6 h (`BT_ERROR_HANDLER_COOLDOWN`, Go duration).
- Cap of 5 active extensions per handler (`BT_ERROR_HANDLER_MAX_NODES`).
- An extension with 3 consecutive failures is auto-disabled (matches the platform's 3-window conventions).
- Kill switch `BT_CLAUDE_ERROR_HANDLER=off` → pure passthrough decorator (no Claude calls, no grafting; existing recovery children still tick).
- One Claude call maximum per tick, ever — no retry loops inside the node.

### 7. Wiring

- `internal/evolution/node_types.go`: add `ClaudeErrorHandler` to `KnownNodeTypes`.
- `internal/engine/tree.go` build switch: construct the decorator; graft store extensions as children 1..n at build time.
- `internal/domains/trees.go`: `AllDomainTrees()` wraps every tree root in `SerializableNode{Type: "ClaudeErrorHandler", Name: "<treeName>_ErrorHandler", Children: [root]}` — pure data, no engine import (preserves the domains↛engine boundary; the injection-hook pattern is not needed because wrapping is data-only).
- Both resolve paths (`ResolveTreeID`, `ResolveTreeIDForUser`) inherit the wrap; `WireGoapFusionLoopTree`'s named-node descent is unaffected (it walks the whole tree).

### 8. Observability

Info log on graft (signature, node name), Warn on rejected/unresolvable proposals, ledger counters as the audit trail. `bb.Result` note makes recoveries visible in run outputs and vault notes.

### 9. Files

New: `internal/engine/error_handler_node.go`, `error_handler_store.go`, `error_handler_claude.go` + `_test.go` each.
Modified: `internal/engine/tree.go`, `internal/engine/registry.go`, `internal/evolution/node_types.go`, `internal/domains/trees.go`, `docs/arc42/go-bt-evolve-arc42.md` (building-block view: new node type).

### 10. Testing

Fake `ClaudeRunner` (interface exists); table-driven per project style:

- passthrough on child success/running; kill switch passthrough.
- child fails + valid proposal → grafted, persisted, ticked, success returned, `Result` note present, no `OutcomeRefinement` set.
- proposal with unknown action name / disallowed node type / oversize → rejected, ledger stamped, −1 returned.
- cooldown active / cap reached / signature disabled → no Claude call (assert zero runner invocations).
- existing recovery child handles a matching error with zero Claude calls; guard mismatch falls through to −1.
- extension store round-trip incl. rebuild-graft on a fresh build; consecutive-failure auto-disable; lock contention skip.
- domains: every `AllDomainTrees()` root is wrapped exactly once; goap fusion wire seam still finds its named nodes.
- node-type coverage tests updated (`KnownNodeTypes` ↔ build switch sync).

### 11. Known baseline caveat

Master's `-race` suite is red from a pre-existing data race in `internal/llm/acp.go:123` (introduced 2026-07-13, unrelated). Feature verification runs the touched packages (`engine`, `domains`, `evolution`) with `-race` individually; the acp fix is a separate follow-up branch.
