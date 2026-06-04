# Phase 5 — Evolution, Observability & Ops Blocks

> **Prerequisite:** Phases 0, 1; Phase 4 recommended for Monitor/QualityGate node  
> **Parent:** [Master Roadmap](./2026-06-04-agentic-blocks-master-roadmap.md)

**Goal:** Evolver-safe block promotion, block-level fitness, operational checkpoint blocks, and alignment with expert archetypes.

---

## Block catalog (this phase)

| Block ID | Purpose |
|----------|---------|
| `core:fitness_probe` | Run benchmark hook; write score to ChainState |
| `core:evolve_on_failure` | Conditional `UpdateBehaviorTree` with gates |
| `core:trace_checkpoint` | Span + blackboard snapshot |
| `core:audit_log` | Append-only JSONL audit entry |
| `core:dlq_escalate` | Push to DLQ on exhausted retry |

---

## Task 5.1 — Block-level fitness metrics

**Files:**
- Modify: `internal/metrics/bt_nodes.go`
- Create: `internal/blocks/fitness.go`
- Modify: `internal/gardener/` or `cmd/bt-gardener/main.go`

**Implementation:**

1. Prometheus: `bt_block_fitness_score{block_id, agent}` gauge.
2. After each task, if tree metadata contains `block_ids` from expand annotations, score per block (shared fitness or proportional).
3. Gardener reads metrics file for mutation ordering.

**Acceptance:**
- Metric appears after composed task run.

---

## Task 5.2 — Frozen / promoted blocks

**Files:**
- Modify: `internal/blocks/registry.go`
- Modify: `internal/evolution/diff.go` (`FilterMutations`)

**Implementation:**

1. `Block.Mutable == false` → evolution cannot mutate block content (already partial).
2. Add `Block.PromotedVersion int` — evolver creates `custom:<id>_v2` in CategoryCustom.
3. MCP `bt_blocks_promote` — copy custom → replace builtin (admin flag).

**Acceptance:**
- Mutation on `core:pre_gate` rejected; on custom allowed.

---

## Task 5.3 — `core:fitness_probe` block

**Files:**
- Create: `internal/blocks/fitness_probe.go`

**Implementation:**

1. Action calls `internal/benchmark` quick eval or reflection score.
2. Sets `bb.ChainState["block_fitness"]`.

**Use:** Evolution trees only (not default task pipeline).

**Acceptance:**
- `-short` test with mock benchmark.

---

## Task 5.4 — `core:evolve_on_failure`

**Files:**
- Create: `internal/blocks/evolve.go`

**Subtree:**

```
Sequence
├── Condition PersistentFailures
├── QualityGate (evolution gate — fitness regression)
└── Action UpdateBehaviorTree
```

**Integration:** `evolution.QualityGate` for mutation acceptance.

**Acceptance:**
- Failure count triggers action only when threshold met.

---

## Task 5.5 — Observability blocks

**Files:**
- Create: `internal/blocks/ops.go`
- Modify: `internal/engine/observability.go`

**`core:trace_checkpoint`:**

- Action records OpenTelemetry event + optional blackboard JSON (truncated).

**`core:audit_log`:**

- Append to `{base}/audit/task.jsonl` via new `internal/audit` package.

**Acceptance:**
- File created after run; trace span event count > 0.

---

## Task 5.6 — `core:dlq_escalate`

**Files:**
- Create: `internal/blocks/dlq.go`
- Use: `internal/reliability/dead_letter.go`

**Implementation:**

1. When parent Retry exhausted, invoke block to enqueue task payload.

**Acceptance:**
- DLQ file entry after forced failures in test.

---

## Task 5.7 — Expert archetype alignment

**Files:**
- Modify: `internal/evolution/expert.go`

**Implementation:**

1. Update `MustHave` / `ShouldHave` to reference block IDs:
   - `core:pre_gate` instead of `PreGate`
   - `core:quality_gate` instead of `QualityGate` string
2. Add `MatchBlockPattern(tree)` helper using `metadata.block_id` annotations.

**Acceptance:**
- `MatchPattern` tests for composed agent pipeline tree.

---

## Task 5.8 — Evolution mutation enhancements

**Files:**
- Modify: `internal/blocks/mutations.go`, `internal/evolution/learning.go`

**Implementation:**

1. Increase block mutation probability when fitness stagnates (configurable).
2. `compose_blocks` mutation validates all IDs exist before apply.
3. Log mutations to audit JSONL.

**Acceptance:**
- Learning test inserts block before target node.

---

## Task 5.9 — MCP tools

**Files:**
- Modify: `cmd/bt-agent/blocks_tools.go`

**New tools:**

- `bt_blocks_promote` — promote custom block version
- `bt_blocks_fitness` — return per-block scores
- `bt_blocks_freeze` — set Mutable=false

**Acceptance:**
- Documented in API_REFERENCE.

---

## Task 5.10 — ADR and arc42 update

**Files:**
- Create: `docs/adr/ADR-008-composable-blocks.md`
- Modify: `docs/arc42/05-building-blocks.md`, `08-crosscutting-concepts.md`

**ADR topics:**

- SubTreeRef + expand-at-build
- Block vs domain tree
- HITL + evolution safety

---

## Phase 5 exit criteria

- [ ] Block fitness metric in Prometheus
- [ ] Expert archetypes reference block IDs
- [ ] ADR-008 merged
- [ ] Ops blocks tested with audit file output

---

## Optional: Domain tree migration (ongoing)

Track separately in master roadmap “Optional track”:

- Per-domain PRs replacing inline PreGate with `composed:*`
- Gardener fitness comparison before merge
