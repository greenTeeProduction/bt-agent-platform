# GOAP Fusion Superpowers Bridge Implementation Plan

> **For Hermes:** Use verified-patching, writing-plans, and executing-plans. Do not amputate the Claude implementation path; preserve it behind explicit apply/HITL.

**Goal:** Make `goap-fusion-runner` write a Superpowers-format implementation plan and implement it with Claude Code for explicit apply tasks, while keeping scheduled runs deterministic and non-HITL.

**Architecture:** `domain:goap_fusion` remains dual-mode. Scheduled/default descriptions route to `ScheduledAnalysisPath`; explicit apply/fix/implement tasks route to `ClaudeSuperpowersPath`. The explicit path writes a plan under `docs/superpowers/plans/`, requests HITL approval for that plan, then invokes Claude Code with Superpowers SDLC directives: skill check, brainstorm/design context, worktree/single-branch adaptation, TDD, verification, code review/finish evidence.

**Tech Stack:** Go behavior trees, `internal/engine` action registry, Claude Code CLI, Superpowers markdown plan conventions, Jetson verified-patching workflow.

---

## Task 1: Restore and adapt dual-mode GOAP fusion tree

**Objective:** Replace old `ImplementationRouter` + ChainAgent fallback with `ExecutionRouter` containing `ClaudeSuperpowersPath` and `ScheduledAnalysisPath`.

**Files:**
- Modify: `internal/domains/goap_fusion.go`
- Test: `internal/domains/goap_fusion_direct_test.go`

**Steps:**
1. Add deterministic shared analysis steps: vault research, graphify load/generate, gap analysis, goal prioritization.
2. Add `ClaudeSuperpowersPath` gated by `IsApplyRequest`.
3. In explicit path: run `WriteSuperpowersImplementationPlan`, then `HumanApprovalGate`, then `RunSuperpowersClaudeImplementation`, `VerifyGoapBuild`, `ReportSuperpowersImplementation`.
4. Add `ScheduledAnalysisPath`: `WriteFusionAnalysis`, `VerifyGoapBuild`, `ReportFusionCycle`.
5. Remove `ChainAgent` fallback entirely.

**Verification:**
- Structural test finds `ClaudeSuperpowersPath`, `ScheduledAnalysisPath`, `WriteSuperpowersImplementationPlan`, `RunSuperpowersClaudeImplementation`.
- Structural test rejects any `ChainAgent` node.

---

## Task 2: Add Superpowers plan-writing action

**Objective:** Write a concrete, executable Superpowers implementation plan before code execution.

**Files:**
- Modify: `internal/engine/actions_goap_fusion.go`
- Creates at runtime: `docs/superpowers/plans/goap-fusion-<timestamp>.md`

**Steps:**
1. Register `WriteSuperpowersImplementationPlan`.
2. Read `goap_fusion_*` chain state: vault research, graphify report, improvement gaps, goal queue.
3. Select the top priority goal.
4. Write markdown plan with required `writing-plans` header and bite-sized tasks.
5. Include Superpowers directives: use skills, TDD RED/GREEN/REFACTOR, exact files, verification commands, finish evidence.
6. Store `goap_fusion_superpowers_plan_path` and `goap_fusion_active_plan` in ChainState.

**Verification:**
- Unit/structural test confirms action is registered.
- Live explicit apply smoke should create a plan file before returning HITL pending.

---

## Task 3: Add Claude implementation action using the Superpowers plan

**Objective:** Invoke Claude Code with the written plan and Superpowers SDLC instructions.

**Files:**
- Modify: `internal/engine/actions_goap_fusion.go`

**Steps:**
1. Register `RunSuperpowersClaudeImplementation`.
2. Read the plan path and plan content.
3. Build a prompt that explicitly instructs Claude to:
   - Load/use relevant Superpowers skills.
   - Execute the written plan.
   - Preserve existing functionality.
   - Use TDD.
   - Run gofmt/tests/build.
   - Record finish evidence.
4. Run Claude Code with `runClaudeCode`.
5. Capture result, changed files, elapsed time, and store in ChainState.
6. Do not silently reset useful changes unless verification fails.

**Verification:**
- Binary strings include `RunSuperpowersClaudeImplementation` and `Superpowers SDLC`.
- Explicit apply smoke with `BT_HITL_AUTO_APPROVE=false` stops at HITL after plan write; it does not start Claude before approval.

---

## Task 4: Harden shell and verification behavior

**Objective:** Avoid Jetson shell/cache footguns and broad hanging tests.

**Files:**
- Modify: `internal/engine/actions_goap_fusion.go`

**Steps:**
1. Change `runGoapShell` from `bash -lc` to `bash -c`.
2. Add `graphifyBin` const and `RunGraphifyUpdate` action if absent.
3. Make `VerifyGoapBuild` run a production-safe verification set:
   - `/usr/local/go/bin/go build ./cmd/bt-agent ./cmd/bt-agent-cli`
   - Focused tests for GOAP/Superpowers regressions.
4. Preserve targeted-package verification inside Claude implementation for actual changed packages.

**Verification:**
- Focused tests complete under 120s.
- Scheduled GOAP live run completes under 10s.

---

## Task 5: Add regression tests

**Objective:** Catch future reversions.

**Files:**
- Modify: `internal/domains/goap_fusion_direct_test.go`
- Optional: add engine test if needed.

**Steps:**
1. Test tree has dual-mode nodes.
2. Test explicit path includes `HumanApprovalGate` and Superpowers actions.
3. Test scheduled path exists.
4. Test no `ChainAgent` fallback.
5. Test required actions/conditions are registered.

**Verification:**
```bash
/usr/local/go/bin/go test ./internal/domains ./internal/engine -count=1 -run 'TestGoapFusion_Structure|TestSuperpowersPipelineSimulation|TestValidateOutputQuality_IncompleteStepLimitFails' -timeout 120s
```

---

## Task 6: Verified deployment

**Objective:** Build and deploy with no stale binary.

**Steps:**
1. `gofmt` changed Go files.
2. Run focused tests.
3. `rm -rf ~/.cache/go-build && go clean -cache -testcache`.
4. Build both binaries.
5. `strings /tmp/bt-agent-cli` verify new symbols.
6. Stop daemon, copy binary, restart daemon.
7. Run scheduled path live.
8. Run explicit apply path with `BT_HITL_AUTO_APPROVE=false` and verify plan file + HITL pending.
9. `graphify update .`.

**Expected final evidence:**
- Scheduled path: success, no HITL, <10s.
- Explicit path: plan file written, HITL pending before Claude.
- Tests/build: pass.
- Binary strings: new Superpowers bridge symbols present.
