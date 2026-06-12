# Phase 0 — Composition Plumbing

> **Prerequisite:** None  
> **Blocks Phase 1+:** Do not add new blocks until this phase passes acceptance tests.  
> **Parent:** [Master Roadmap](./2026-06-04-agentic-blocks-master-roadmap.md)

**Goal:** Every composed tree (`composed:*`, `SubTreeRef`, `bt_blocks_compose`) executes real block subtrees—not no-op pass-through actions.

---

## Problem statement

- `blocks.Compose(..., inline=false)` emits `SubTreeRef` children.
- `engine.buildNode` has no `SubTreeRef` case → default success leaf.
- `blocks.Expand` exists and is tested in `blocks_test.go` but is never called from production build paths.

---

## Task 0.1 — Wire Expand into BuildAndValidate

**Files:**
- Modify: `internal/engine/tree.go`
- Modify: `go.mod` (only if needed; prefer narrow import)

**Implementation:**

1. Add optional expand hook to avoid hard coupling (matches `block_hooks` pattern):

```go
// internal/engine/tree.go
var expandTreeFn func(*evolution.SerializableNode) (*evolution.SerializableNode, error)

func RegisterTreeExpander(fn func(*evolution.SerializableNode) (*evolution.SerializableNode, error)) {
    expandTreeFn = fn
}
```

2. In `internal/blocks/hooks.go` `init()`:

```go
engine.RegisterTreeExpander(func(tree *evolution.SerializableNode) (*evolution.SerializableNode, error) {
    return Expand(DefaultRegistry, tree)
})
```

3. In `BuildAndValidate`:

```go
func BuildAndValidate(serTree *evolution.SerializableNode, bb *Blackboard) (btcore.Command[Blackboard], error) {
    tree := serTree
    if expandTreeFn != nil && hasSubTreeRefs(tree) {
        expanded, err := expandTreeFn(tree)
        if err != nil {
            return nil, fmt.Errorf("expand: %w", err)
        }
        tree = expanded
    }
    info := ValidateTreeFull(tree)
    // ...
}
```

4. Add `hasSubTreeRefs` helper in `engine` or `blocks` (exported from blocks).

**Acceptance:**
- `TestComposeAndExpand` pattern replicated in `engine` integration test: compose → BuildAndValidate → first child is not `SubTreeRef`.
- `go test ./internal/engine/... -run Composed -count=1` passes.

---

## Task 0.2 — Expand in bt-agent tree load paths

**Files:**
- Modify: `cmd/bt-agent/main.go` (`resolveTree`, agent init)
- Modify: `cmd/bt-agent/tools.go` (`bt_get_tree`, `bt_run_task` if tree rebuilt)

**Implementation:**

1. Ensure `blocks.InitRegistry` runs before any `resolveTree("composed:...")`.
2. After `Compose` / `ComposeTaskTree` / `ComposeTaskTreeWithHITL`, optionally persist expanded tree for debugging (`metadata expanded_from`).
3. Document `composed:task`, `composed:task:hitl`, `composed:block1,block2` in `docs/API_REFERENCE.md`.

**Acceptance:**
- Manual: `bt_run_task` with agent using `composed:task` runs `ValidateInput` (fail on empty task).
- MCP `bt_blocks_compose` + `bt_run_task` uses expanded tree.

---

## Task 0.3 — Fail closed on unknown SubTreeRef at build

**Files:**
- Modify: `internal/engine/tree.go` (`buildNode` default case)
- Modify: `internal/engine/verifier.go`

**Implementation:**

1. If expand hook nil and node type `SubTreeRef` → build failing action with clear error (not silent success).
2. Verifier: error if `SubTreeRef` present and `expandTreeFn` not registered (test-only flag ok).

**Acceptance:**
- Unit test: build without `RegisterTreeExpander` + SubTreeRef tree → validation or build error.

---

## Task 0.4 — Annotate expanded nodes for observability

**Files:**
- Modify: `internal/blocks/expand.go` (may already have `annotateBlockSource`)

**Implementation:**

1. Ensure each inlined node has `metadata.block_id` and `metadata.block_source` for traces.
2. `observeNode` span attributes include `block_id` when present.

**Acceptance:**
- Span test or log assertion contains `block_id=core:pre_gate` after expand.

---

## Task 0.5 — Compose API: inline vs ref policy

**Files:**
- Modify: `internal/blocks/compose.go`
- Modify: `cmd/bt-agent/blocks_tools.go`

**Implementation:**

1. Add `ComposeSpec.Inline bool` or document when MCP `bt_blocks_compose` uses `inline=true` for persistence/evolution.
2. Default production compose: `inline=false` + expand-at-build.
3. Evolution mutations that insert blocks: always `SubTreeRef` (smaller diffs).

**Acceptance:**
- `blocks_test.go`: both paths produce identical runtime behavior after expand.

---

## Task 0.6 — Integration test: full composed task pipeline

**Files:**
- Create: `internal/engine/composed_pipeline_test.go`

**Test scenario:**

1. `blocks.InitRegistry` with temp dir.
2. `hitl.SetPolicy(AutoApprove: true)`.
3. Compose `DefaultTaskBlocks`, expand, build, run with mock LLM.
4. Assert: `bb.Outcome` success, `bb.Plan` non-empty, `ValidateInput` ran.

**Acceptance:**
- Test passes in CI `-short` mode.

---

## Phase 0 exit criteria

- [ ] No production path builds trees containing `SubTreeRef` without expand
- [ ] `composed:task` MCP integration test green
- [ ] Documented in README “Composed trees” subsection
