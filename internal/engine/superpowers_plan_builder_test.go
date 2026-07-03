package engine

import (
	"strings"
	"testing"
)

// The goal-driven plan builder is what lets the self-improvement loop make
// BIGGER changes: instead of the legacy single-task template pinned to
// internal/engine/actions_superpowers.go with a single -run test filter and a
// "smallest safe change" objective, it derives tasks, file scope, and test
// packages from the cycle's own prioritized research goals.

const bigGoalTask = `improve the platform

GOAP goals:
[P0] Wire the loop: In ` + "`internal/domains/goap_fusion_loop.go`" + `, make GoapFusionLoopTree return the wired tree and add coverage in ` + "`internal/domains/goap_fusion_wire_seam_test.go`" + `.
[P0] Harden sync: extend internal/engine/superpowers_worktree.go ancestry handling and pin it in internal/engine/superpowers_sync_ahead_test.go.
[P2] Ensure all domain trees have smoke tests.

Gaps:
NOTEBOOKLM_GAP: production tree unwired.`

func TestGoalDrivenPlanBuildsOneTaskPerFileScopedGoal(t *testing.T) {
	plan := buildGoalDrivenImplementationPlan(bigGoalTask)

	tasks, err := ParseSuperpowersPlan(plan)
	if err != nil {
		t.Fatalf("generated plan must round-trip through ParseSuperpowersPlan: %v\n%s", err, plan)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected one task per file-scoped goal (2), got %d\n%s", len(tasks), plan)
	}
	if !containsStr(tasks[0].Files, "internal/domains/goap_fusion_loop.go") {
		t.Fatalf("task 1 files must come from its goal, got %v", tasks[0].Files)
	}
	if !containsStr(tasks[1].Files, "internal/engine/superpowers_worktree.go") {
		t.Fatalf("task 2 files must come from its goal, got %v", tasks[1].Files)
	}
	for _, task := range tasks {
		if strings.Contains(strings.ToLower(task.Objective), "smallest safe") {
			t.Fatalf("goal-driven tasks must not be clamped to smallest-safe: %q", task.Objective)
		}
	}
	if len(tasks[0].Tests) == 0 || !strings.Contains(tasks[0].Tests[0], "./internal/domains") {
		t.Fatalf("task 1 tests must target the goal's package, got %v", tasks[0].Tests)
	}
	if !strings.Contains(tasks[1].Tests[0], "./internal/engine") {
		t.Fatalf("task 2 tests must target the goal's package, got %v", tasks[1].Tests)
	}
}

func TestGoalDrivenPlanFallsBackToLegacyTemplateWithoutFileScopedGoals(t *testing.T) {
	task := "improve the platform\n\nGOAP goals:\n[P0] Make everything better somehow.\n\nGaps:\nnone"
	plan := buildGoalDrivenImplementationPlan(task)
	if plan != buildDeterministicImplementationPlan(task) {
		t.Fatalf("goals without concrete file scope must fall back to the legacy template:\n%s", plan)
	}
}

func TestGoalDrivenPlanCapsTaskCount(t *testing.T) {
	var b strings.Builder
	b.WriteString("improve\n\nGOAP goals:\n")
	for _, name := range []string{"alpha", "beta", "gamma", "delta", "epsilon"} {
		b.WriteString("[P0] Fix internal/engine/" + name + ".go now.\n")
	}
	plan := buildGoalDrivenImplementationPlan(b.String())
	tasks, err := ParseSuperpowersPlan(plan)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tasks) != maxGoalDrivenTasks {
		t.Fatalf("task count must cap at %d, got %d", maxGoalDrivenTasks, len(tasks))
	}
}

func TestGoalDrivenPlanEscalatesRiskForWideScope(t *testing.T) {
	task := "improve\n\nGOAP goals:\n[P0] Refactor internal/engine/a.go internal/domains/b.go internal/agentexec/c.go together.\n"
	plan := buildGoalDrivenImplementationPlan(task)
	tasks, err := ParseSuperpowersPlan(plan)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if tasks[0].Risk != "high" {
		t.Fatalf("a goal spanning >2 packages must carry high risk, got %q", tasks[0].Risk)
	}
}

func TestChangedPackagesTestCommandDerivesPackagesFromFiles(t *testing.T) {
	cmd := changedPackagesTestCommand([]string{
		"internal/engine/a.go",
		"internal/engine/a_test.go",
		"internal/domains/b.go",
		"graphify-out/graph.json", // non-Go artifacts must not become packages
	})
	for _, want := range []string{"./internal/engine", "./internal/domains", "go test"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("changed-packages command missing %q: %s", want, cmd)
		}
	}
	if strings.Contains(cmd, "graphify-out") {
		t.Fatalf("non-Go files must not produce test packages: %s", cmd)
	}
	if changedPackagesTestCommand([]string{"docs/readme.md"}) != "" {
		t.Fatal("no Go changes must yield an empty command (check skipped)")
	}
}

// The generated go test commands must carry -short so goal-driven RED/GREEN
// verification and the changed-packages check match the repo's make test
// convention and skip LLM-gated / flaky full-package tests.
func TestGoalDrivenPlanTestCommandsUseShort(t *testing.T) {
	plan := buildGoalDrivenImplementationPlan(bigGoalTask)
	tasks, err := ParseSuperpowersPlan(plan)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, plan)
	}
	for i, task := range tasks {
		if len(task.Tests) == 0 {
			t.Fatalf("task %d must have a test command", i+1)
		}
		if !strings.Contains(task.Tests[0], "-short") {
			t.Fatalf("task %d test command must include -short, got %q", i+1, task.Tests[0])
		}
	}
	if !strings.Contains(plan, "go test") || !strings.Contains(plan, "-short") {
		t.Fatalf("generated plan must run go test with -short:\n%s", plan)
	}
}

func TestChangedPackagesTestCommandUsesShort(t *testing.T) {
	cmd := changedPackagesTestCommand([]string{"internal/engine/a.go"})
	if !strings.Contains(cmd, "-short") {
		t.Fatalf("changed-packages command must include -short, got %q", cmd)
	}
}

func containsStr(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
