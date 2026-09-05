# Superpowers Production Implementation Plan

> **For Hermes:** Use `verified-patching` and `test-driven-development` to implement this plan. Do **not** solve reliability by bypassing phases or deleting explicit implementation functionality. This plan replaces Superpowers stubs with production runtime components.

**Goal:** Replace the current `superpowers_pipeline` scaffold/stubs with a production-ready implementation that actually brainstorms, writes design/plan artifacts, gates with HITL, executes plan tasks with Claude Code using TDD, verifies, records evidence, and integrates cleanly with `goap_fusion`.

**Architecture:** Keep the monolithic 7-phase Superpowers behavior tree, but replace placeholder actions with a typed runtime layer: `SuperpowersRun`, artifact store, worktree manager, Claude runner, plan parser, HITL integration, task executor, verification runner, and finish reporter. GOAP fusion explicit-apply mode should delegate to this production Superpowers runtime instead of owning a separate partial implementation.

**Tech Stack:** Go, go-bt-evolve behavior trees, `btcore.BTContext[Blackboard]`, existing `HumanApprovalGate`, Claude Code CLI, git worktrees, Graphify, focused Go tests, Obsidian/Markdown artifacts.

---

## Current Stub Surface To Replace

Grounded in current source inspection:

| File | Current issue |
|---|---|
| `internal/domains/superpowers_pipeline.go` | Contains `ChainAgent` placeholders, `SkipBrainstorm`, `SkipWorktree`, manual HITL side-file flow, and nominal TDD nodes. |
| `internal/engine/actions_superpowers.go` | Most actions only mutate `ChainState` / append result strings; code-writing actions do not write code. |
| `internal/engine/actions_superpowers.go:240` | `SendHITLRequest` writes a sidecar JSON file instead of using platform `HumanApprovalGate`. |
| `internal/engine/actions_superpowers.go:319-355` | `WriteFailingTest` / `WriteMinimalCode` only set TDD state; they do not invoke Claude or modify files. |
| `internal/engine/actions_superpowers.go:372` | `RefactorIfNeeded` always succeeds. |
| `internal/engine/actions_superpowers.go:423-459` | `RequestCodeReview`, `GeneratePR`, `CleanupWorktree` are report-ish or unsafe external side-effect stubs. |
| `internal/engine/actions_domain.go:520` | Shared `ApplyFix` is still fake text (`+3 lines`) and should not be used in prod Superpowers verification retry. |
| `internal/engine/actions_goap_fusion.go` | Has a bespoke Superpowers plan + Claude bridge; should call shared production Superpowers runtime once implemented. |

---

## Production Acceptance Criteria

1. `domain:superpowers_pipeline` has no `ChainAgent` placeholder nodes in production path.
2. No phase is silently skipped unless the blackboard already has a verified artifact for that phase.
3. All pre-HITL artifact writers are idempotent across BT re-ticks.
4. HITL approval uses `HumanApprovalGate`, not custom sidecar request files.
5. A run creates a durable artifact directory:
   ```text
   docs/superpowers/runs/<run-id>/
   ├── run.json
   ├── design.md
   ├── plan.md
   ├── tasks/<NN>-<slug>/prompt.md
   ├── tasks/<NN>-<slug>/claude-output.md
   ├── tasks/<NN>-<slug>/red.txt
   ├── tasks/<NN>-<slug>/green.txt
   ├── verification/build.txt
   ├── verification/tests.txt
   ├── verification/lint.txt
   └── finish.md
   ```
6. Each implementation task executes through TDD:
   - write failing test
   - verify RED
   - implement minimal code
   - verify GREEN
   - optional refactor
   - focused verification
7. Claude Code is called with exact plan/task context, repo path, Superpowers directives, and expected final schema.
8. Worktree operations are guarded: only under `/tmp/worktrees/superpowers-*`; cleanup refuses unsafe paths.
9. Verification runs deterministic focused commands first, then broader package/build checks.
10. `goap_fusion` explicit apply path writes a plan then delegates to the production Superpowers runtime after HITL approval.
11. Scheduled/default `goap_fusion` path remains deterministic/no-HITL.
12. Binary strings verify new production runtime symbols after clean build.

---

## Task 1: Lock current stub behavior with failing tests

**Objective:** Add tests that fail against the current stub implementation and define the production contract.

**Files:**
- Create: `internal/domains/superpowers_prod_contract_test.go`
- Create: `internal/engine/superpowers_runtime_contract_test.go`
- Modify: `internal/domains/goap_fusion_direct_test.go`

**Step 1: Add tree contract test**

Create `internal/domains/superpowers_prod_contract_test.go`:

```go
package domains

import (
    "testing"

    "github.com/nico/go-bt-evolve/internal/evolution"
)

func TestSuperpowersPipeline_ProductionContract_NoPlaceholderPath(t *testing.T) {
    tree := SuperpowersPipelineTree()
    forbiddenTypes := []string{"ChainAgent"}
    forbiddenNames := []string{"SkipBrainstorm", "SkipWorktree", "ManualIntervention"}

    for _, typ := range forbiddenTypes {
        if containsNodeType(*tree, typ) {
            t.Fatalf("production Superpowers tree must not contain placeholder node type %q", typ)
        }
    }
    for _, name := range forbiddenNames {
        if findNode(*tree, name) != nil {
            t.Fatalf("production Superpowers tree must not contain placeholder node %q", name)
        }
    }

    required := []string{
        "InitSuperpowersRun",
        "GenerateDesignArtifact",
        "GenerateImplementationPlan",
        "ApproveSuperpowersPlan",
        "ExecuteSuperpowersTaskBatch",
        "VerifySuperpowersRun",
        "WriteSuperpowersFinishReport",
    }
    for _, name := range required {
        if findNode(*tree, name) == nil {
            t.Fatalf("production Superpowers tree missing %q", name)
        }
    }
}

func containsNodeType(node evolution.SerializableNode, nodeType string) bool {
    if node.Type == nodeType { return true }
    for _, c := range node.Children {
        if containsNodeType(c, nodeType) { return true }
    }
    return false
}
```

**Step 2: Add runtime registration test**

Create `internal/engine/superpowers_runtime_contract_test.go`:

```go
package engine

import "testing"

func TestSuperpowersRuntime_ActionsRegistered(t *testing.T) {
    actions := []string{
        "InitSuperpowersRun",
        "GenerateDesignArtifact",
        "GenerateImplementationPlan",
        "ValidateImplementationPlanStrict",
        "ExecuteSuperpowersTaskBatch",
        "VerifySuperpowersRun",
        "WriteSuperpowersFinishReport",
    }
    for _, name := range actions {
        if GetAction(name) == nil {
            t.Fatalf("missing production Superpowers action %q", name)
        }
    }
}
```

**Step 3: Run RED**

Run:

```bash
/usr/local/go/bin/go test ./internal/domains ./internal/engine -count=1 -run 'TestSuperpowersPipeline_ProductionContract|TestSuperpowersRuntime_ActionsRegistered' -timeout 120s
```

Expected: FAIL because the production nodes/actions do not exist yet.

---

## Task 2: Add typed production runtime model

**Objective:** Define typed run/task/verification structures and blackboard helpers so actions stop passing opaque strings.

**Files:**
- Create: `internal/engine/superpowers_runtime_types.go`
- Create: `internal/engine/superpowers_runtime_types_test.go`

**Step 1: Add types**

Create `internal/engine/superpowers_runtime_types.go`:

```go
package engine

import "time"

type SuperpowersPhase string

const (
    SuperpowersPhaseDesign         SuperpowersPhase = "design"
    SuperpowersPhasePlan           SuperpowersPhase = "plan"
    SuperpowersPhaseHITL           SuperpowersPhase = "hitl"
    SuperpowersPhaseImplementation SuperpowersPhase = "implementation"
    SuperpowersPhaseVerification   SuperpowersPhase = "verification"
    SuperpowersPhaseFinish         SuperpowersPhase = "finish"
)

type SuperpowersRun struct {
    ID             string              `json:"id"`
    Task           string              `json:"task"`
    Phase          SuperpowersPhase    `json:"phase"`
    RepoDir        string              `json:"repo_dir"`
    WorktreePath   string              `json:"worktree_path"`
    WorktreeBranch string              `json:"worktree_branch"`
    ArtifactDir    string              `json:"artifact_dir"`
    DesignPath     string              `json:"design_path"`
    PlanPath       string              `json:"plan_path"`
    Tasks          []SuperpowersTask   `json:"tasks"`
    Verification   []VerificationCheck `json:"verification"`
    StartedAt      time.Time           `json:"started_at"`
    UpdatedAt      time.Time           `json:"updated_at"`
}

type SuperpowersTask struct {
    Index       int      `json:"index"`
    Title       string   `json:"title"`
    Objective   string   `json:"objective"`
    Files       []string `json:"files"`
    Tests       []string `json:"tests"`
    Risk        string   `json:"risk"`
    Body        string   `json:"body"`
    ArtifactDir string   `json:"artifact_dir"`
    Status      string   `json:"status"`
}

type VerificationCheck struct {
    Name     string `json:"name"`
    Command  string `json:"command"`
    Passed   bool   `json:"passed"`
    Output   string `json:"output"`
    Duration string `json:"duration"`
}

const chainKeySuperpowersRun = "superpowers_run"

func setSuperpowersRun(bb *Blackboard, run *SuperpowersRun) {
    if bb.ChainState == nil { bb.ChainState = map[string]any{} }
    bb.ChainState[chainKeySuperpowersRun] = run
}

func getSuperpowersRun(bb *Blackboard) (*SuperpowersRun, bool) {
    if bb == nil || bb.ChainState == nil { return nil, false }
    run, ok := bb.ChainState[chainKeySuperpowersRun].(*SuperpowersRun)
    return run, ok && run != nil
}
```

**Step 2: Add helper tests**

Test that `setSuperpowersRun`/`getSuperpowersRun` preserves typed state.

**Step 3: Run GREEN for helpers**

```bash
/usr/local/go/bin/go test ./internal/engine -count=1 -run 'TestSuperpowersRunState' -timeout 120s
```

---

## Task 3: Add artifact store and idempotent phase writes

**Objective:** Make every production action durable and idempotent so BT re-ticks do not duplicate artifacts.

**Files:**
- Create: `internal/engine/superpowers_artifacts.go`
- Create: `internal/engine/superpowers_artifacts_test.go`

**Step 1: Implement artifact helpers**

Create:

```go
const superpowersRunsDir = "/home/nico/go-bt-evolve/docs/superpowers/runs"

func newSuperpowersRunID(task string, now time.Time) string
func ensureSuperpowersRunDirs(run *SuperpowersRun) error
func writeSuperpowersRunJSON(run *SuperpowersRun) error
func readSuperpowersRunJSON(path string) (*SuperpowersRun, error)
func writeArtifactOnce(path string, content []byte) (written bool, err error)
func safeSlug(s string) string
```

**Step 2: Guard idempotence**

`writeArtifactOnce` must:
- create parent dirs
- return `(false, nil)` if file already exists and has content
- never overwrite an approved plan unless a caller passes an explicit `force` variant

**Step 3: Test duplicate prevention**

```go
func TestWriteArtifactOnce_Idempotent(t *testing.T) {
    p := filepath.Join(t.TempDir(), "plan.md")
    written, err := writeArtifactOnce(p, []byte("one"))
    if err != nil || !written { t.Fatal(...) }
    written, err = writeArtifactOnce(p, []byte("two"))
    if err != nil || written { t.Fatal("second write should reuse") }
    got, _ := os.ReadFile(p)
    if string(got) != "one" { t.Fatalf("overwrote artifact") }
}
```

---

## Task 4: Extract a reusable command runner and Claude runner

**Objective:** Replace ad-hoc `exec.Command` calls and private GOAP-only `runClaudeCode` with injectable production runners.

**Files:**
- Create: `internal/engine/superpowers_runner.go`
- Create: `internal/engine/superpowers_runner_test.go`
- Modify: `internal/engine/actions_goap_fusion.go` later to reuse shared runner

**Step 1: Define interfaces**

```go
type CommandRunner interface {
    Run(ctx context.Context, dir string, name string, args ...string) CommandResult
}

type CommandResult struct {
    Command  string
    Dir      string
    Output   string
    Err      error
    Duration time.Duration
}

type ClaudeRunner interface {
    RunClaude(ctx context.Context, repoDir string, prompt string) CommandResult
}
```

**Step 2: Implement production runner**

Implementation details:
- use `/usr/local/go/bin/go` for Go commands
- use `bash -c`, never `bash -lc`
- context timeout per command
- stdout/stderr combined and capped only in reports, not in artifact files
- no shell string interpolation for dynamic args unless already shell-quoted

**Step 3: Implement Claude runner**

Production command:

```bash
/home/nico/.local/bin/claude --print --dangerously-skip-permissions '<prompt>'
```

Use direct `exec.CommandContext`, not shell, unless Claude CLI requires shell behavior.

**Step 4: Tests**

Use a fake runner in tests to verify:
- command dir is set
- timeout is honored
- output and errors are captured
- no `bash -lc` appears in production code path

---

## Task 5: Production worktree manager

**Objective:** Replace `DetermineWorktreePath`, `CreateGitWorktree`, `SkipWorktree`, and cleanup stubs with safe worktree lifecycle code.

**Files:**
- Create: `internal/engine/superpowers_worktree.go`
- Create: `internal/engine/superpowers_worktree_test.go`
- Modify: `internal/engine/actions_superpowers.go`

**Step 1: Implement worktree manager**

Functions:

```go
func planSuperpowersWorktree(run *SuperpowersRun) (path string, branch string)
func validateSuperpowersWorktreePath(path string) error
func createSuperpowersWorktree(ctx context.Context, runner CommandRunner, run *SuperpowersRun) error
func cleanupSuperpowersWorktree(ctx context.Context, runner CommandRunner, run *SuperpowersRun) error
```

Safety rules:
- path must start with `/tmp/worktrees/superpowers-`
- path must not contain `..`
- cleanup refuses empty path, repo root, `/home/nico`, `/tmp`, `/`
- branch name must start with `superpowers/`

**Step 2: Verify baseline without broad hang**

Baseline should run:

```bash
/usr/local/go/bin/go test ./internal/domains ./internal/engine -count=1 -run 'TestSuperpowers|TestGoapFusion|TestValidateOutput' -timeout 120s
/usr/local/go/bin/go build ./cmd/bt-agent ./cmd/bt-agent-cli
```

Do not use `go test ./...` in scheduler-critical paths until known slow tests are isolated.

---

## Task 6: Replace brainstorm/design stubs with real design artifact generation

**Objective:** `GenerateDesignArtifact` must write a concrete design doc using Claude Code or reuse an existing approved design.

**Files:**
- Modify: `internal/domains/superpowers_pipeline.go`
- Modify: `internal/engine/actions_superpowers.go`
- Create: `internal/engine/superpowers_design_test.go`

**Step 1: Tree change**

Replace Phase 1 with:

```go
sel("Phase1_Design",
    seq("DesignAlreadyReady",
        cond("SuperpowersDesignReady", "design_path exists and verified"),
    ),
    seq("DesignGeneration",
        act("InitSuperpowersRun", "Create/load run state and artifact dir"),
        act("GenerateDesignArtifact", "Use Claude Code to write design.md with architecture, risks, acceptance criteria"),
        act("ValidateDesignArtifact", "Verify design.md exists and has required sections"),
    ),
)
```

No unconditional `SkipBrainstorm`.

**Step 2: Design prompt requirements**

Claude prompt must include:
- original task
- graphify report excerpt
- relevant current files, if named
- required sections: Goal, Architecture, Acceptance Criteria, Risks, Test Strategy, Non-goals
- explicit instruction: no code changes in design phase

**Step 3: Validate design**

Required headings:
- `## Goal`
- `## Architecture`
- `## Acceptance Criteria`
- `## Test Strategy`
- `## Risks`

**Step 4: Tests**

Use fake Claude runner returning a valid design and verify:
- file written once
- run JSON updated
- repeated action reuses design

---

## Task 7: Replace planning stubs with strict plan generation and parser

**Objective:** `GenerateImplementationPlan` writes `plan.md`; `ValidateImplementationPlanStrict` parses it into typed tasks.

**Files:**
- Create: `internal/engine/superpowers_plan_parser.go`
- Create: `internal/engine/superpowers_plan_parser_test.go`
- Modify: `internal/engine/actions_superpowers.go`

**Step 1: Plan format**

Require each task:

```markdown
### Task N: Title

**Objective:** ...

**Files:**
- Modify: `path`
- Test: `path`

**Step 1: Write failing test**
...

**Step 2: Run RED**
Run: `...`
Expected: FAIL ...

**Step 3: Implement**
...

**Step 4: Run GREEN**
Run: `...`
Expected: PASS
```

**Step 2: Parser behavior**

`ParseSuperpowersPlan(markdown string) ([]SuperpowersTask, error)` must reject:
- zero tasks
- missing Objective
- missing Files
- missing Test command
- task without RED/GREEN language
- dangerous shell commands (`rm -rf /`, `sudo`, writes outside repo/worktree)

**Step 3: Action behavior**

`GenerateImplementationPlan`:
- uses design.md and graphify report
- writes plan.md idempotently
- stores parsed tasks in run state
- sets `PlanPath` and phase `plan`

**Step 4: Tests**

Add table tests for valid and invalid plans.

---

## Task 8: Replace side-file HITL with native HumanApprovalGate

**Objective:** Remove custom `BuildApprovalRequest` / `SendHITLRequest` sidecar behavior from production path.

**Files:**
- Modify: `internal/domains/superpowers_pipeline.go`
- Modify: `internal/engine/actions_superpowers.go`
- Create: `internal/domains/superpowers_hitl_test.go`

**Step 1: Tree change**

Use platform node:

```go
evolution.SerializableNode{
    Type:        "HumanApprovalGate",
    Name:        "ApproveSuperpowersPlan",
    Description: "Approve the written Superpowers implementation plan before Claude Code modifies files",
    Metadata: map[string]any{
        "phase": "pre",
        "side_effect_class": "external",
        "hitl_prompt": "Approve Superpowers plan execution with Claude Code?",
    },
    Children: []evolution.SerializableNode{
        act("ExecuteSuperpowersTaskBatch", "Execute approved plan tasks with Claude Code and TDD"),
    },
}
```

**Step 2: Remove sidecar actions from production path**

Keep legacy actions only if tests require backwards compatibility, but production tree must not call:
- `BuildApprovalRequest`
- `SendHITLRequest`
- `SetHITLApprovedFlag`
- `ProceedToImplementation`

**Step 3: Test**

Structural test asserts:
- `HumanApprovalGate` exists
- no `SendHITLRequest` node in `SuperpowersPipelineTree`
- plan-writing action is before gate and idempotent

---

## Task 9: Production task execution with Claude Code + TDD

**Objective:** Replace `WriteFailingTest`, `WriteMinimalCode`, and related state-only actions with one production task executor that drives Claude per task and captures RED/GREEN evidence.

**Files:**
- Create: `internal/engine/superpowers_task_executor.go`
- Create: `internal/engine/superpowers_task_executor_test.go`
- Modify: `internal/engine/actions_superpowers.go`

**Step 1: Executor contract**

```go
type SuperpowersTaskExecutor struct {
    Runner       CommandRunner
    Claude       ClaudeRunner
    RepoDir      string
    ArtifactRoot string
}

func (e *SuperpowersTaskExecutor) ExecuteTask(ctx context.Context, run *SuperpowersRun, task SuperpowersTask) (SuperpowersTask, error)
```

**Step 2: Per-task prompt**

Prompt must include:
- task body
- exact files/tests from parsed plan
- mandatory RED/GREEN/REFACTOR
- no unrelated edits
- preserve functionality
- final schema:
  ```text
  FILES_CHANGED:
  RED_COMMAND:
  RED_RESULT:
  GREEN_COMMAND:
  GREEN_RESULT:
  NOTES:
  ```

**Step 3: Verify RED/GREEN outside Claude**

After Claude output, the executor runs the exact commands from task spec:
- RED evidence if task declares separate RED command and it failed before implementation
- GREEN command after implementation must pass

If impossible to separate RED because Claude did both in one pass, mark task evidence incomplete and fail the task unless the plan explicitly allowed combined execution.

**Step 4: Changed-file guard**

Before each task:

```bash
git status --short --untracked-files=all
```

After each task:
- compute changed files excluding `graphify-out/`
- ensure every changed file is listed in task `Files`
- fail if unrelated files changed

**Step 5: Tests**

Use fake Claude/command runners:
- success path writes task artifacts
- failure path records output and returns error
- unrelated file change fails
- exact test command failure fails

---

## Task 10: Production verification runner

**Objective:** Replace broad/hanging verification with layered deterministic checks and artifact capture.

**Files:**
- Create: `internal/engine/superpowers_verifier.go`
- Create: `internal/engine/superpowers_verifier_test.go`
- Modify: `internal/engine/actions_superpowers.go`

**Step 1: Implement checks**

`VerifySuperpowersRun` should run:

```bash
/usr/local/go/bin/gofmt -w <changed-go-files>
/usr/local/go/bin/go test <changed-packages> -count=1 -timeout 120s
/usr/local/go/bin/go test ./internal/domains ./internal/engine -count=1 -run 'TestSuperpowers|TestGoapFusion|TestValidateOutput' -timeout 180s
/usr/local/go/bin/go build ./cmd/bt-agent ./cmd/bt-agent-cli
/usr/local/go/bin/go vet <changed-packages>
```

**Step 2: Save outputs**

Write:
- `verification/gofmt.txt`
- `verification/changed-package-tests.txt`
- `verification/focused-regressions.txt`
- `verification/build.txt`
- `verification/vet.txt`

**Step 3: Binary verification hook**

If changed files include Superpowers runtime, verify binary contains:

```bash
strings bin/bt-agent-cli | grep 'SuperpowersPipeline_Main'
strings bin/bt-agent-cli | grep 'ExecuteSuperpowersTaskBatch'
```

**Step 4: Tests**

Fake runner validates check order and stop-on-failure behavior.

---

## Task 11: Production finish/reporting and PR safety

**Objective:** Replace `RequestCodeReview`, `GeneratePR`, and `CleanupWorktree` stubs with safe local finish behavior; do not push/open PR without explicit approval.

**Files:**
- Create: `internal/engine/superpowers_finish.go`
- Create: `internal/engine/superpowers_finish_test.go`
- Modify: `internal/engine/actions_superpowers.go`

**Step 1: Finish report**

`WriteSuperpowersFinishReport` writes `finish.md` with:
- task
- run id
- branch/worktree
- changed files
- task statuses
- verification checks
- artifacts path
- unresolved risks

**Step 2: Code review packet**

Create `review.md` under run artifact dir:
- plan vs implementation checklist
- diff stat
- tests run
- known limitations

**Step 3: PR is explicit external side effect**

If PR creation remains desired, add separate path:

```text
ApproveSuperpowersPRPush (HumanApprovalGate, side_effect_class=external)
└── CreateSuperpowersPR
```

Default production finish must **not** push.

**Step 4: Cleanup safety**

Only cleanup if:
- verification passed
- finish report written
- path validates under `/tmp/worktrees/superpowers-`
- no uncommitted changes unless they are already copied/reported

---

## Task 12: Replace domain tree with production phases

**Objective:** Rewrite `SuperpowersPipelineTree` so it calls the production actions only.

**Files:**
- Modify: `internal/domains/superpowers_pipeline.go`
- Modify: `internal/domains/superpowers_sim_test.go`
- Modify: `internal/domains/trees.go` if descriptions need updating

**Target tree shape:**

```go
func SuperpowersPipelineTree() *evolution.SerializableNode {
    return &evolution.SerializableNode{
        Type: "Sequence",
        Name: "SuperpowersPipeline_Main",
        TimeoutMs: 3600000,
        Children: []evolution.SerializableNode{
            act("InitSuperpowersRun", "Create/load run and artifact directory"),
            act("LoadSuperpowersSkills", "Load required Superpowers directives"),
            act("GenerateDesignArtifact", "Write/reuse design.md"),
            act("ValidateDesignArtifact", "Strict design validation"),
            act("PrepareSuperpowersWorktree", "Create safe git worktree"),
            act("VerifySuperpowersBaseline", "Focused baseline tests/build"),
            act("GenerateImplementationPlan", "Write/reuse plan.md"),
            act("ValidateImplementationPlanStrict", "Parse and validate plan tasks"),
            evolution.SerializableNode{
                Type: "HumanApprovalGate",
                Name: "ApproveSuperpowersPlan",
                Metadata: map[string]any{
                    "phase": "pre",
                    "side_effect_class": "external",
                    "hitl_prompt": "Approve Claude Code execution of the written Superpowers plan?",
                },
                Children: []evolution.SerializableNode{
                    act("ExecuteSuperpowersTaskBatch", "Execute parsed tasks with Claude Code and TDD"),
                    act("VerifySuperpowersRun", "Run layered verification"),
                    act("WriteSuperpowersFinishReport", "Write final evidence report"),
                    act("UpdateBlackboard", "Mark complete"),
                },
            },
            outcome(),
            act("ReportPipelineComplete", "Return finish evidence"),
        },
    }
}
```

**Important:** `GenerateDesignArtifact` and `GenerateImplementationPlan` must be idempotent because they execute before a RUNNING HITL gate.

---

## Task 13: Integrate GOAP fusion with production Superpowers runtime

**Objective:** Replace GOAP fusion’s bespoke Superpowers bridge with a call into the shared production runtime.

**Files:**
- Modify: `internal/domains/goap_fusion.go`
- Modify: `internal/engine/actions_goap_fusion.go`
- Modify: `internal/domains/goap_fusion_direct_test.go`

**Step 1: Keep GOAP scheduled path unchanged**

Scheduled/default path must remain:

```text
ReadVaultResearch → ReadGraphifyReport → AnalyzeImprovementGaps → PrioritizeGoapGoals → WriteFusionAnalysis → VerifyGoapBuild → ReportFusionCycle
```

**Step 2: Explicit apply path should prepare context and invoke shared runtime**

Replace bespoke implementation children with:

```text
WriteSuperpowersImplementationPlanFromGoapContext
ApproveGoapFusionSuperpowersPlan
RunSuperpowersRuntimeFromExistingPlan
ReportSuperpowersImplementation
```

`RunSuperpowersRuntimeFromExistingPlan` should seed `SuperpowersRun` with:
- task
- graphify/vault context
- existing plan path
- run artifact dir

Then execute the same `ExecuteSuperpowersTaskBatch` / `VerifySuperpowersRun` code as standalone Superpowers pipeline.

**Step 3: Tests**

Update `TestGoapFusion_Structure` to assert:
- explicit path still has HITL
- no `ChainAgent`
- uses shared Superpowers runtime action, not bespoke Claude implementation action
- scheduled path still succeeds fast/no HITL

---

## Task 14: End-to-end dry-run mode

**Objective:** Add safe `dry_run` behavior so production runtime can be verified without modifying code.

**Files:**
- Modify: `internal/engine/superpowers_runtime_types.go`
- Modify: `internal/engine/superpowers_task_executor.go`
- Add tests in `internal/engine/superpowers_task_executor_test.go`

**Step 1: Add mode**

```go
type SuperpowersMode string
const (
    SuperpowersModeDryRun SuperpowersMode = "dry_run"
    SuperpowersModeApply  SuperpowersMode = "apply"
)
```

**Step 2: Behavior**

In dry-run:
- write design.md
- write plan.md
- parse tasks
- create HITL request if needed
- do not call Claude to modify files
- write `finish.md` with `DRY RUN` status

**Step 3: CLI smoke**

Run:

```bash
/tmp/bt-agent-cli run superpowers-prod-smoke --input "dry_run: add one focused regression test for superpowers parser"
```

Expected:
- success or pending HITL depending mode
- artifact dir created
- no repo source changes except artifacts

---

## Task 15: Agent registry and scheduler safety

**Objective:** Register a production Superpowers agent and prevent scheduled jobs from accidentally applying code.

**Files:**
- Create: `/home/nico/.go-bt-evolve/agents/superpowers-prod-runner.yaml`
- Modify if needed: `/home/nico/.go-bt-evolve/agents/goap-fusion-runner.yaml`

**Agent YAML:**

```yaml
name: superpowers-prod-runner
description: 'Production Superpowers SDLC runner: writes design and implementation plan, gates execution, then applies approved tasks with Claude Code and TDD. No scheduled automatic apply.'
version: 1.0.0
tree: domain:superpowers_pipeline
schedule: ''
```

Rules:
- no recurring schedule for apply-capable Superpowers runner
- scheduled `goap-fusion-runner` remains analysis/report-only text and avoids apply trigger verbs
- explicit apply requires user/direct invocation and HITL

---

## Task 16: Verified-patching deployment

**Objective:** Deploy with the known Jetson-safe pipeline.

**Files:**
- No new code; operational verification only.

**Commands:**

```bash
/usr/local/go/bin/gofmt -w \
  internal/domains/superpowers_pipeline.go \
  internal/domains/superpowers_prod_contract_test.go \
  internal/domains/superpowers_sim_test.go \
  internal/engine/actions_superpowers.go \
  internal/engine/superpowers_*.go \
  internal/domains/goap_fusion.go \
  internal/engine/actions_goap_fusion.go

/usr/local/go/bin/go test ./internal/domains ./internal/engine ./internal/hitl \
  -count=1 \
  -run 'TestSuperpowersPipeline_ProductionContract|TestSuperpowersRuntime|TestSuperpowersPlanParser|TestSuperpowersTaskExecutor|TestGoapFusion_Structure|TestValidateOutputQuality|TestStoreListAll' \
  -timeout 240s

rm -rf ~/.cache/go-build
/usr/local/go/bin/go clean -cache -testcache
/usr/local/go/bin/go build -o bin/bt-agent ./cmd/bt-agent
/usr/local/go/bin/go build -o bin/bt-agent-cli ./cmd/bt-agent-cli
cp bin/bt-agent-cli /tmp/bt-agent-cli

strings /tmp/bt-agent-cli | grep 'ExecuteSuperpowersTaskBatch'
strings /tmp/bt-agent-cli | grep 'WriteSuperpowersFinishReport'
strings /tmp/bt-agent-cli | grep 'ApproveSuperpowersPlan'
```

Deploy:

```bash
systemctl --user stop bt-agent
sleep 2
cp bin/bt-agent /home/nico/go-bt-evolve/bt-agent
systemctl --user reset-failed bt-agent
systemctl --user start bt-agent
sleep 3
systemctl --user status bt-agent --no-pager | head -8
```

Update graph:

```bash
graphify update .
```

---

## Task 17: Live verification matrix

**Objective:** Prove production behavior with real CLI runs.

**Files:**
- Runtime artifacts under `docs/superpowers/runs/`
- History under `/home/nico/.go-bt-evolve/history/*.jsonl`

**Smoke 1: Superpowers dry-run**

```bash
timeout 180 /tmp/bt-agent-cli run superpowers-prod-runner \
  --input "dry_run: add a focused parser regression test for Superpowers plan validation" \
  --json
```

Expected:
- artifact dir created
- design.md and plan.md exist
- no source changes outside artifact dir
- no Claude apply before HITL

**Smoke 2: Explicit apply stops at HITL**

```bash
BT_HITL_AUTO_APPROVE=false timeout 180 /tmp/bt-agent-cli run superpowers-prod-runner \
  --input "implement a tiny focused Superpowers parser improvement" \
  --json
```

Expected:
- outcome `partial` / pending approval
- exactly one plan file
- no duplicate artifacts across HITL ticks
- no Claude modification before approval

**Smoke 3: GOAP scheduled path remains safe**

```bash
timeout 120 /tmp/bt-agent-cli run goap-fusion-runner \
  --input "Scheduled GOAP fusion cycle: read vault research and graphify report, identify improvement gaps, prioritize goals, record vault analysis, and run health checks" \
  --json
```

Expected:
- `success`
- `<10s` typical runtime
- no HITL
- no Claude apply

**Smoke 4: GOAP explicit path writes plan then HITL**

```bash
BT_HITL_AUTO_APPROVE=false timeout 180 /tmp/bt-agent-cli run goap-fusion-runner \
  --input "Implement one explicit GOAP fusion improvement with Claude Code using the production Superpowers pipeline" \
  --json
```

Expected:
- plan under `docs/superpowers/runs/<run-id>/plan.md` or imported existing GOAP plan
- HITL pending before Claude
- no duplicate plans

---

## Migration / Cleanup Notes

After production path passes:

1. Keep legacy action names temporarily only if external trees reference them.
2. Mark deprecated actions with comments:
   ```go
   // Deprecated: use ExecuteSuperpowersTaskBatch.
   ```
3. Remove production tree references to:
   - `SkipBrainstorm`
   - `SkipWorktree`
   - `BuildApprovalRequest`
   - `SendHITLRequest`
   - `WriteFailingTest` as state-only stub
   - `WriteMinimalCode` as state-only stub
   - `ManualIntervention` as unconditional failure fallback
4. Do not delete legacy code until `AllDomainTrees()` smoke and existing benchmark suites pass.

---

## Rollback Plan

If production implementation breaks scheduled BT operation:

1. Revert only `superpowers_pipeline` registration and GOAP explicit apply integration.
2. Keep parser/types/artifact store if tests pass; they are inert unless tree calls them.
3. Restore daemon binary from previous build if needed:
   ```bash
   systemctl --user stop bt-agent
   cp /home/nico/go-bt-evolve/bin/bt-agent /home/nico/go-bt-evolve/bt-agent
   systemctl --user start bt-agent
   ```
4. Verify scheduled agents:
   ```bash
   python3 - <<'PY'
   import json
   for j in json.load(open('/home/nico/.go-bt-evolve/jobs/scheduler-jobs.json')):
       if j['agent_name'] in ('goap-fusion-runner','bt-fusion'):
           print(j['agent_name'], j['next_run'], j.get('in_flight'))
   PY
   ```

---

## Done Definition

This plan is complete only when all are true:

- [ ] `SuperpowersPipelineTree` production path has no `ChainAgent` placeholders.
- [ ] Production actions generate real design/plan/run artifacts.
- [ ] HITL uses `HumanApprovalGate`.
- [ ] Claude Code task execution modifies files only after approval.
- [ ] RED/GREEN evidence is captured per task.
- [ ] Verification artifacts exist and match commands run.
- [ ] GOAP explicit apply delegates to shared production Superpowers runtime.
- [ ] GOAP scheduled path remains no-HITL and fast.
- [ ] Focused tests pass.
- [ ] Clean build passes.
- [ ] Binary strings verify production runtime compiled.
- [ ] Daemon deployed and live smoke verified.
