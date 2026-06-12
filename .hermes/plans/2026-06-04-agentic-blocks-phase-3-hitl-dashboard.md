# Phase 3 — HITL Extensions & Dashboard

> **Prerequisite:** Phase 0; Phase 1 recommended (quality gate before post-review)  
> **Parent:** [Master Roadmap](./2026-06-04-agentic-blocks-master-roadmap.md)  
> **Baseline:** `core:human_gate`, `internal/hitl`, MCP `bt_hitl_*` (pre-execution)

**Goal:** Tiered human oversight (pre + post), dashboard visibility, and full workflow approval—not only MCP.

---

## Block catalog (this phase)

| Block ID | Purpose |
|----------|---------|
| `core:human_review` | Post-execution approval before commit/side effects |
| `core:human_escalate` | On policy failure / repeated reject → operator queue |
| `core:hitl_tiered` | Selector: auto-approve `local`, gate `external`/`destroy` |

---

## Task 3.1 — Post-execution `core:human_review`

**Files:**
- Create: `internal/blocks/hitl_post.go`
- Modify: `internal/engine/hitl_gate.go` (metadata `phase: pre|post`)

**Implementation:**

1. Reuse `humanApprovalGateCmd` with metadata:
   - `phase: post` — create request **after** child runs, show `bb.Result` as `Proposed`.
2. Child runs once; gate stores pending review; second tick waits for approval before MarkSuccessful propagates.

**Alternative:** Sequence: `tool_execution` → `HumanApprovalGate(post)` → `MarkSuccessful`.

**Acceptance:**
- Test: child sets Result → pending → approve → outcome success.

---

## Task 3.2 — Tiered HITL by `side_effect_class`

**Files:**
- Create: `internal/blocks/hitl_tiered.go`
- Modify: `internal/engine/hitl_gate.go`, `verifier.go`

**Implementation:**

1. `hitl_tiered` Selector:
   - If metadata on upcoming action is `destroy|external` → `HumanApprovalGate`
   - Else → pass-through Sequence
2. Document verifier: ancestor gate still required for static validation; runtime tiered is additive.

**Acceptance:**
- Tree with only `local` effects skips gate when policy enabled.

---

## Task 3.3 — `core:human_escalate`

**Files:**
- Create: `internal/blocks/hitl_escalate.go`
- Modify: `internal/hitl/store.go` (status `escalated` optional)

**Triggers:**

- Rejected twice
- Expired approval
- `bb.FailureCount` > threshold

**Action:** `EscalateToOperator` + create high-priority HITL request.

**Acceptance:**
- Unit test on policy transitions.

---

## Task 3.4 — Dashboard REST API for HITL

**Files:**
- Create: `internal/dashboard/hitl_handlers.go`
- Modify: `cmd/bt-dashboard/main.go`

**Endpoints:**

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/hitl/pending` | List pending requests |
| GET | `/api/hitl/{id}` | Get request |
| POST | `/api/hitl/{id}/approve` | Body: reviewer, comment |
| POST | `/api/hitl/{id}/reject` | Body: reviewer, reason |

**Storage:** Read same `~/.go-bt-evolve/hitl/requests.json` via `hitl.InitStore` in dashboard startup.

**Acceptance:**
- curl integration test with temp dir.

---

## Task 3.5 — Dashboard UI (Tasks tab)

**Files:**
- Modify: `cmd/bt-dashboard/static/js/tabs/tasks.js`
- Modify: `cmd/bt-dashboard/static/js/lib/api.js`

**UI:**

- “Pending approvals” panel with approve/reject buttons
- Poll `/api/hitl/pending` every 10s (or SSE in Task 3.6)

**Acceptance:**
- Manual screenshot / `computerUse` optional; API test required.

---

## Task 3.6 — Optional SSE for HITL updates

**Files:**
- Modify: `cmd/bt-dashboard/main.go`

**Event:** `hitl_pending` when store changes (file watch or in-process hook).

**Acceptance:**
- Browser receives event on create/approve (stretch).

---

## Task 3.7 — Config struct fields (not only env)

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/hitl/policy.go`

**Fields:**

```go
HITL struct {
    Enabled      bool
    AutoApprove  bool
    TimeoutSecs  int
}
```

Env overrides file config.

**Acceptance:**
- `config.Load` test with YAML snippet.

---

## Task 3.8 — Workflow & task ID mapping

**Files:**
- Modify: `cmd/bt-agent/tools.go` (`bt_workflow_approve`)
- Modify: `internal/hitl/store.go`

**Implementation:**

1. Index requests by `task_id` in metadata (optional field on `Request`).
2. `bt_workflow_approve` with `task_id` looks up latest pending request for that task.
3. Legacy stub response when no mapping found (keep backward compat).

**Acceptance:**
- Approve by task_id integration test.

---

## Task 3.9 — Documentation

**Files:**
- Modify: `docs/API_REFERENCE.md`, `docs/TROUBLESHOOTING.md`, README

**Topics:**

- Pre vs post gates
- Env vars
- Dashboard approval flow
- CI: `BT_HITL_AUTO_APPROVE=true`

---

## Phase 3 exit criteria

- [ ] Post-review block + tests
- [ ] Dashboard approve/reject works against shared store
- [ ] `bt_workflow_approve` resolves task_id → hitl request
