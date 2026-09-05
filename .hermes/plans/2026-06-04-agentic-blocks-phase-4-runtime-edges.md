# Phase 4 — Runtime Parity & Typed Edges

> **Prerequisite:** [Phase 0](./2026-06-04-agentic-blocks-phase-0-plumbing.md)  
> **Can parallel with:** Phase 1  
> **Parent:** [Master Roadmap](./2026-06-04-agentic-blocks-master-roadmap.md)

**Goal:** Every `KnownNodeTypes` entry either runs correctly in `buildNode` or is removed from the schema; typed edges (`guard`, `quality_gate`, …) affect execution where declared.

---

## Audit: node types vs buildNode

| Node type | Action |
|-----------|--------|
| `Parallel` | Implement `BuildParallel` using go-bt parallel composite |
| `Timeout`, `Retry`, `CircuitBreaker` | Already in buildNode — verify |
| `Budget` | Implement budget decorator (token/tick counter on blackboard) |
| `RateLimit` | Implement using `internal/reliability` or token bucket |
| `Inverter`, `Succeeder`, `Repeater`, `Runner` | Implement or deprecate |
| `Monitor` | Implement as side-effect logger node |
| `QualityGate` | Implement `BuildQualityGate` (BT node, not evolution fitness gate) |
| `SubTreeRef` | Build-time error if not expanded (Phase 0) |

---

## Task 4.1 — `Parallel` and `ReactiveParallel` parity

**Files:**
- Modify: `internal/engine/tree.go`
- Create: `internal/engine/parallel.go` (if not exists)

**Implementation:**

1. `Parallel` — standard go-bt parallel with success policy (metadata: `success_policy: all|one`).
2. Document difference from `ReactiveParallel`.

**Acceptance:**
- Test: two children, one fails → policy behavior.

---

## Task 4.2 — `Budget` decorator

**Files:**
- Create: `internal/engine/budget.go`
- Modify: `Blackboard` — `TokensUsed int`, `TickBudget int`

**Implementation:**

1. Child runs until budget exhausted → return Failure.
2. Metadata: `max_tokens`, `max_ticks`.

**Block:** `core:budget_gate` wraps agent execution (Phase 2 integration).

**Acceptance:**
- Test: max_ticks=2 stops deep tree.

---

## Task 4.3 — `RateLimit` decorator

**Files:**
- Create: `internal/engine/rate_limit.go`

**Implementation:**

1. Per-node or global limiter keyed by `node.Name`.
2. Returns Running (0) when throttled (align with HITL tick loop).

**Acceptance:**
- Test: 2 calls/sec limit delays second tick.

---

## Task 4.4 — `QualityGate` BT node

**Files:**
- Create: `internal/engine/quality_gate_node.go`
- Modify: `internal/blocks/quality.go` — optionally use node type instead of Selector

**Implementation:**

1. `BuildQualityGate(node, bb)` — child runs, then `validateOutputQuality` or `ValidateOutput` condition.
2. Failure → run recovery child or return Failure.

**Acceptance:**
- Align with `core:quality_gate` block behavior.

---

## Task 4.5 — Decorators: Inverter, Succeeder, Repeater, Runner

**Files:**
- Modify: `internal/engine/tree.go` or `decorators.go`

**Implementation:**

1. Map to go-bt decorators (`btdec` package).
2. If unused in 41 trees, mark deprecated in `KnownNodeTypes` comment.

**Acceptance:**
- Unit test per decorator.

---

## Task 4.6 — `Monitor` node

**Files:**
- Create: `internal/engine/monitor_node.go`

**Implementation:**

1. On child terminal: emit metric + log structured fields from blackboard.
2. Always return child status.

**Block:** `core:trace_checkpoint` (Phase 5) may wrap Monitor.

**Acceptance:**
- Prometheus counter increment test.

---

## Task 4.7 — Typed edge interpreter (minimal v1)

**Files:**
- Create: `internal/engine/typed_edges.go`
- Modify: `walkValidate` — keep schema validation

**Runtime v1 scope:**

| Edge type | Behavior |
|-----------|----------|
| `guard` | Skip child if condition false |
| `quality_gate` | After child, run condition; fail → recovery edge child if present |
| `approval` | Synonym for HumanApprovalGate placement hint (optional) |
| `interrupt` | Wire to `AbortOnEvent` where label matches |

**Defer:** full `dataflow` blackboard wiring (v2).

**Implementation:**

1. In `buildNode`, when building Sequence children, consult `node.Edges` for `ChildIndex` guards before appending child command.

**Acceptance:**
- Tree with guard edge skips child when condition false.
- Test in `typed_edges_test.go`.

---

## Task 4.8 — Verifier sync

**Files:**
- Modify: `internal/engine/verifier.go`, `internal/evolution/node_types.go`

**Implementation:**

1. Remove unknown types from `KnownNodeTypes` if not implemented.
2. Add `SubTreeRef` only as compile-time type with verifier warning if not expanded.

**Acceptance:**
- `go test ./internal/engine/... -run Verifier`.

---

## Task 4.9 — Migrate `core:parallel_fanout` to Parallel node

**Files:**
- Modify: `internal/blocks/fanout.go` (Phase 2)

**Acceptance:**
- Fanout block uses Parallel when Phase 4 complete.

---

## Phase 4 exit criteria

- [ ] Audit table 100% resolved (implement or remove)
- [ ] Typed edge guard + quality_gate tests
- [ ] No KnownNodeTypes entry maps to no-op default
