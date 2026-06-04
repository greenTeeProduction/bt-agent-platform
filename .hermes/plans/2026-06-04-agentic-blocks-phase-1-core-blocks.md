# Phase 1 — Core Agentic Blocks

> **Prerequisite:** [Phase 0](./2026-06-04-agentic-blocks-phase-0-plumbing.md) complete  
> **Parent:** [Master Roadmap](./2026-06-04-agentic-blocks-master-roadmap.md)

**Goal:** Add the blocks that complete the canonical agent loop—plan, perceive, clarify, validate—without waiting for new BT node types (Phase 4 handles `QualityGate` node).

---

## Block catalog (this phase)

| Block ID | Purpose | Primary actions/conditions |
|----------|---------|---------------------------|
| `core:plan` | Plan before act | `AssessComplexity`, `GeneratePlan` |
| `core:rag_gate` | KG/cache before expensive LLM | `QueryKG`, `UseCachedResult`, optional `ChainAction:rag_query` |
| `core:clarify_gate` | Ambiguity → ask user | `AskClarifyingQuestions`, `IsAmbiguousQuery`, telegram conditions |
| `core:quality_gate` | Output validation before success | `ValidateOutput` condition + `ReflectOnOutcome` selector |
| `core:strategy_router` | Reusable routing shell | `Selector` named `StrategyRouter` + doc for middle insert |

**Compose presets:**

- `DefaultTaskBlocksAgentic` = pre → plan → tool → quality → error
- `DefaultTaskBlocksWithHITL` (existing) = insert human_gate before tool
- `DefaultTaskBlocksFull` = pre → plan → rag → clarify → human → tool → quality → error

---

## Task 1.1 — `core:plan`

**Files:**
- Create: `internal/blocks/plan.go`
- Modify: `internal/blocks/builtin.go`
- Modify: `internal/blocks/reliability.go` (add `SpecPlan`)

**Subtree sketch:**

```
Sequence "Plan"
├── Condition ValidateInput
├── Action AssessComplexity
├── Timeout(60s) → Action GeneratePlan
└── Condition HasClearTask (or new HasPlan)
```

**Register condition `HasPlan`:** `bb.Plan != ""`.

**Acceptance:**
- Block expands; mock LLM sets `bb.Plan`.
- `bt_blocks_get` returns block JSON.

---

## Task 1.2 — `core:rag_gate`

**Files:**
- Create: `internal/blocks/rag.go`
- Modify: `internal/blocks/builtin.go`

**Subtree sketch:**

```
Selector "RAGGate"
├── Sequence "CacheHit"
│   ├── Condition UseCachedResult (or HasCachedFitness-style for KG)
│   └── Action MarkSuccessful (or set Result from cache)
└── Sequence "Retrieve"
    ├── Action QueryKG
    └── ChainAction rag_query (optional, metadata tools)
```

**Acceptance:**
- With `bb.KgResults` preset, selector takes cache path without LLM.
- Reliability: timeout 90s on rag chain.

---

## Task 1.3 — `core:clarify_gate`

**Files:**
- Create: `internal/blocks/clarify.go`
- Modify: `internal/blocks/builtin.go`
- Reference: `internal/evolution/telegram_clarify.go`, `cmd/bt-agent/main.go` init conditions

**Subtree sketch:**

```
Selector "ClarifyGate"
├── Sequence "ClearTask"
│   ├── Condition NOT IsAmbiguousQuery
│   └── Action ProceedDirectly (no-op success)
└── Sequence "NeedClarify"
    ├── Action AskClarifyingQuestions
    └── Action MarkSuccessful (outcome: awaiting_user)
```

**Policy:** metadata `require_clarify: true` for strict mode.

**Acceptance:**
- Ambiguous task (`"fix it"`) triggers clarify path.
- Document interaction with HITL (clarify vs approval).

---

## Task 1.4 — `core:quality_gate`

**Files:**
- Create: `internal/blocks/quality.go`
- Modify: `internal/blocks/builtin.go`

**Subtree sketch:**

```
Selector "QualityGate"
├── Sequence "Pass"
│   ├── Condition ValidateOutput
│   └── Action MarkSuccessful
└── Sequence "Fail"
    ├── Action ReflectOnOutcome
    └── Action SelfCorrect (or fail to parent error_handling)
```

**Note:** Align with `validateOutputQuality` in `RunTask`—either call same helper from condition or document double-check.

**Acceptance:**
- Short garbage `bb.Result` fails gate.
- Composed pipeline cannot MarkSuccessful without passing ValidateOutput.

---

## Task 1.5 — `core:strategy_router` (template block)

**Files:**
- Create: `internal/blocks/router.go`
- Modify: `internal/blocks/compose.go`

**Implementation:**

1. Export block whose root is `Selector` + name `StrategyRouter` + empty children placeholder.
2. `ComposeSpec.Middle` inserts user strategy **inside** router (document contract).
3. MCP `bt_blocks_compose` documents middle parameter.

**Acceptance:**
- `ComposeTaskTree` with strategy produces: pre_gate → StrategyRouter(strategy) → tool → error.

---

## Task 1.6 — Compose presets & MCP

**Files:**
- Modify: `internal/blocks/compose.go`, `hitl.go`
- Modify: `cmd/bt-agent/main.go` `resolveTree`
- Modify: `cmd/bt-agent/blocks_tools.go`

**New resolveTree IDs:**

- `composed:task:agentic`
- `composed:task:full` (all gates)

**MCP:**

- Extend `bt_blocks_compose` with `preset` enum: `default`, `agentic`, `hitl`, `full`.

**Acceptance:**
- Each preset resolves and runs under Phase 0 expand.

---

## Task 1.7 — Reliability specs for new blocks

**Files:**
- Modify: `internal/blocks/reliability.go`

**Specs:**

| Block | Timeout | Retry | Circuit |
|-------|---------|-------|---------|
| plan | 60s | 1 | light |
| rag_gate | 90s | 2 | kg |
| clarify_gate | 30s | 0 | none |
| quality_gate | 30s | 1 | none |

**Acceptance:**
- `TestBuiltinBlocksReliability` covers new IDs.

---

## Task 1.8 — Tests & documentation

**Files:**
- Modify: `internal/blocks/blocks_test.go`
- Modify: `docs/API_REFERENCE.md`, `docs/GETTING_STARTED.md`

**Tests per block:** expand, build, single tick (mock LLM).

**Docs:** Block catalog table, env vars, compose presets.

---

## Phase 1 exit criteria

- [ ] 5 new builtin blocks registered (total 10)
- [ ] `composed:task:agentic` integration test
- [ ] Expert archetype `Agent Pipeline` maps to block IDs in comment or `MustHave` update (Phase 5)
