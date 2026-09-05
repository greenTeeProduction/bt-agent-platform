# Phase 2 — Multi-Agent & Tool Profiles

> **Prerequisite:** [Phase 1](./2026-06-04-agentic-blocks-phase-1-core-blocks.md)  
> **Parent:** [Master Roadmap](./2026-06-04-agentic-blocks-master-roadmap.md)

**Goal:** Swappable tool setup blocks, delegation/subtree dispatch, parallel fan-out, and agent memory read/write as composable units.

---

## Block catalog (this phase)

| Block ID | Purpose |
|----------|---------|
| `core:tools_dev` | `SetupDevTools` |
| `core:tools_research` | `SetupResearchTools` |
| `core:tools_startup` | `SetupStartupTools` |
| `core:tools_universal` | `SetupUniversalTools` |
| `core:tools_default` | Extract from pre_gate (optional split) |
| `core:delegate` | Subtree dispatch via blackboard `delegate_tree_id` |
| `core:a2a_handoff` | `DelegateToA2A` with metadata URL |
| `core:parallel_fanout` | `map_reduce` chain or ReactiveParallel wrapper |
| `core:merge_results` | Merge `bb.Results` → `bb.Result` |
| `core:memory_load` | Read agent memory into `ChainState` |
| `core:memory_write` | Persist summary to agent memory store |

---

## Task 2.1 — Tool profile blocks

**Files:**
- Create: `internal/blocks/tools_profile.go`
- Modify: `internal/blocks/builtin.go`
- Modify: `core:pre_gate` — remove hardcoded `SetupDefaultTools` OR make pre_gate call `SubTreeRef` to `core:tools_default`

**Design decision (pick one):**

- **A (recommended):** `pre_gate` only validates; compose adds `core:tools_*` before `tool_execution`.
- **B:** `pre_gate` accepts metadata `tools_profile: dev|research|...`.

**Acceptance:**
- Agent YAML can set `tree: composed:task:agentic` + metadata tools=dev via MCP compose.
- `bt_run_task` has `file_read` when dev profile composed.

---

## Task 2.2 — `core:delegate`

**Files:**
- Create: `internal/blocks/delegate.go`
- Modify: `internal/engine/registry.go` (if needed: `DelegateToTree` action)

**Implementation:**

1. Action reads `bb.ChainState["delegate_tree_id"]` and `delegate_task`.
2. Calls same logic as MCP `bt_delegate_to_tree` (extract shared `runTreeByID(treeID, task)` in `cmd/bt-agent` or `internal/agent`).
3. Block wraps action in Timeout + Retry.

**Subtree:**

```
Sequence "Delegate"
├── Condition HasDelegateTarget
├── HumanApprovalGate (optional, metadata side_effect external)
└── Action DelegateToTree
```

**Acceptance:**
- Integration test: delegate to minimal tree that sets Result.

---

## Task 2.3 — `core:a2a_handoff`

**Files:**
- Create: `internal/blocks/a2a.go`
- Use: `internal/engine/a2a_nodes.go`

**Metadata:** `a2a_url`, `timeout_ms`.

**Acceptance:**
- Mock `DelegateToA2AFn` in test; block sets Result.

---

## Task 2.4 — `core:parallel_fanout` + `core:merge_results`

**Files:**
- Create: `internal/blocks/fanout.go`
- May depend: [Phase 4](./2026-06-04-agentic-blocks-phase-4-runtime-edges.md) `Parallel` node OR use `ChainAction:map_reduce` only

**Phase 2 minimum:** `map_reduce` chain with metadata `subtasks_from: plan`.

**Phase 4 upgrade:** Replace inner chain with `Parallel` node when runtime exists.

**Acceptance:**
- Plan with numbered steps → multiple results in `bb.Results` → merge sets `bb.Result`.

---

## Task 2.5 — Memory blocks

**Files:**
- Create: `internal/blocks/memory.go`
- Modify: `cmd/bt-agent/tools.go` (extract memory helpers)

**Implementation:**

1. `memory_load`: MCP `bt_agent_memory_read` logic as engine action `LoadAgentMemory`.
2. `memory_write`: `WriteAgentMemory` after quality gate.

**Blackboard:** `ChainState["agent_memory"]` slice.

**Acceptance:**
- Round-trip test with temp memory store.

---

## Task 2.6 — MCP & compose presets

**Files:**
- Modify: `cmd/bt-agent/blocks_tools.go`, `main.go`

**New tools:**

- `bt_blocks_compose_delegate` — preset with delegate block
- `bt_blocks_list_profiles` — list tool profile blocks

**Acceptance:**
- Document in API_REFERENCE.

---

## Phase 2 exit criteria

- [ ] 10 additional blocks (or 8 + 2 optional)
- [ ] Delegate integration test
- [ ] Tool profile switching demo in GETTING_STARTED
