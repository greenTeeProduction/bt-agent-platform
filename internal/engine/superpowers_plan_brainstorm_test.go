package engine

import (
	"strings"
	"testing"
)

const oneGoalTask = "improve\n\nGOAP goals:\n[P0] Add an auction retry/backoff layer in internal/a2a/auction.go with tests"

func withBrainstorm(t *testing.T, fn func(task string, goals []string, det string) string) {
	t.Helper()
	old := goalPlanBrainstormFn
	goalPlanBrainstormFn = fn
	t.Cleanup(func() { goalPlanBrainstormFn = old })
}

func TestBrainstormExpansionAcceptedWhenDeeper(t *testing.T) {
	expanded := `### Task 1: Add retry types
**Objective:** define retry config in internal/a2a/auction.go
**Files:**
- Modify: internal/a2a/auction.go
- Test: internal/a2a/auction_test.go

**Step 1: RED**
**Step 2: Run RED**
Run: /usr/local/go/bin/go test ./internal/a2a -short -count=1 -timeout 300s
**Step 3: GREEN**
**Step 4: Run GREEN**
Run: /usr/local/go/bin/go test ./internal/a2a -short -count=1 -timeout 300s

**Risk:** medium

### Task 2: Wire backoff loop
**Objective:** implement backoff in internal/a2a/auction.go
**Files:**
- Modify: internal/a2a/auction.go
- Test: internal/a2a/auction_test.go

**Step 1: RED**
**Step 2: Run RED**
Run: /usr/local/go/bin/go test ./internal/a2a -short -count=1 -timeout 300s
**Step 3: GREEN**
**Step 4: Run GREEN**
Run: /usr/local/go/bin/go test ./internal/a2a -short -count=1 -timeout 300s

**Risk:** medium
`
	withBrainstorm(t, func(_ string, _ []string, _ string) string { return expanded })
	plan := buildGoalDrivenImplementationPlan(oneGoalTask)
	tasks, err := ParseSuperpowersPlan(plan)
	if err != nil {
		t.Fatalf("expanded plan must parse: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("a 1-goal cycle must expand to the deeper 2-task plan, got %d", len(tasks))
	}
}

func TestBrainstormRejectedWhenNotDeeper(t *testing.T) {
	// Returns a single-task plan — no deeper than the deterministic one.
	withBrainstorm(t, func(_ string, _ []string, det string) string {
		return "### Task 1: trivial\n**Objective:** x in internal/a2a/auction.go\n**Files:**\n- Modify: internal/a2a/auction.go\n\nRED GREEN\nRun: go test ./internal/a2a\n**Risk:** low\n"
	})
	plan := buildGoalDrivenImplementationPlan(oneGoalTask)
	tasks, _ := ParseSuperpowersPlan(plan)
	if len(tasks) != 1 {
		t.Fatalf("a non-deeper expansion must be rejected, keeping the 1-task plan, got %d", len(tasks))
	}
	if !strings.Contains(plan, "complete, verified change") {
		t.Fatal("rejection must keep the deterministic plan")
	}
}

func TestBrainstormRejectedWhenMalformedOrProse(t *testing.T) {
	for _, bad := range []string{"", "here is a great plan, trust me", "### Task 1: no files\n**Objective:** vague\nRED GREEN\nRun: go test\n**Risk:** low\n"} {
		withBrainstorm(t, func(_ string, _ []string, _ string) string { return bad })
		plan := buildGoalDrivenImplementationPlan(oneGoalTask)
		if tasks, _ := ParseSuperpowersPlan(plan); len(tasks) != 1 {
			t.Fatalf("malformed/prose/fileless expansion %q must be rejected, got %d tasks", bad[:min(len(bad), 20)], len(tasks))
		}
	}
}

func TestBrainstormRejectedWhenRunOmitsShort(t *testing.T) {
	// A genuinely deeper 2-task expansion, but Task 2's Run commands omit
	// -short. Such commands run the full package including LLM-gated and
	// flaky tests, producing false RED passes / aborted GREEN cycles. The
	// gate must reject the whole expansion and keep the deterministic plan
	// rather than land a plan whose verification skips -short.
	expanded := `### Task 1: Add retry types
**Objective:** define retry config in internal/a2a/auction.go
**Files:**
- Modify: internal/a2a/auction.go
- Test: internal/a2a/auction_test.go

**Step 1: RED**
**Step 2: Run RED**
Run: /usr/local/go/bin/go test ./internal/a2a -short -count=1 -timeout 300s
**Step 3: GREEN**
**Step 4: Run GREEN**
Run: /usr/local/go/bin/go test ./internal/a2a -short -count=1 -timeout 300s

**Risk:** medium

### Task 2: Wire backoff loop
**Objective:** implement backoff in internal/a2a/auction.go
**Files:**
- Modify: internal/a2a/auction.go
- Test: internal/a2a/auction_test.go

**Step 1: RED**
**Step 2: Run RED**
Run: /usr/local/go/bin/go test ./internal/a2a -count=1 -timeout 300s
**Step 3: GREEN**
**Step 4: Run GREEN**
Run: /usr/local/go/bin/go test ./internal/a2a -count=1 -timeout 300s

**Risk:** medium
`
	withBrainstorm(t, func(_ string, _ []string, _ string) string { return expanded })
	plan := buildGoalDrivenImplementationPlan(oneGoalTask)
	tasks, _ := ParseSuperpowersPlan(plan)
	if len(tasks) != 1 {
		t.Fatalf("expansion with a -short-less Run command must be rejected, keeping the 1-task plan, got %d", len(tasks))
	}
	if !strings.Contains(plan, "complete, verified change") {
		t.Fatal("rejection must keep the deterministic plan")
	}
}

func TestBrainstormRejectedWhenFilesNotGoPath(t *testing.T) {
	// Deeper 2-task expansion, but Task 2 lists a Files entry that is not a
	// repo-relative Go path (a doc file). Non-empty is not enough — every
	// Files entry must match goFilePathRe, or the executor has no real Go
	// target to edit and test against.
	expanded := `### Task 1: Add retry types
**Objective:** define retry config in internal/a2a/auction.go
**Files:**
- Modify: internal/a2a/auction.go
- Test: internal/a2a/auction_test.go

**Step 1: RED**
**Step 2: Run RED**
Run: /usr/local/go/bin/go test ./internal/a2a -short -count=1 -timeout 300s
**Step 3: GREEN**
**Step 4: Run GREEN**
Run: /usr/local/go/bin/go test ./internal/a2a -short -count=1 -timeout 300s

**Risk:** medium

### Task 2: Document backoff
**Objective:** describe backoff design for internal/a2a/auction.go
**Files:**
- Modify: docs/backoff-design.md

**Step 1: RED**
**Step 2: Run RED**
Run: /usr/local/go/bin/go test ./internal/a2a -short -count=1 -timeout 300s
**Step 3: GREEN**
**Step 4: Run GREEN**
Run: /usr/local/go/bin/go test ./internal/a2a -short -count=1 -timeout 300s

**Risk:** medium
`
	withBrainstorm(t, func(_ string, _ []string, _ string) string { return expanded })
	plan := buildGoalDrivenImplementationPlan(oneGoalTask)
	tasks, _ := ParseSuperpowersPlan(plan)
	if len(tasks) != 1 {
		t.Fatalf("expansion with a non-Go-path Files entry must be rejected, keeping the 1-task plan, got %d", len(tasks))
	}
	if !strings.Contains(plan, "complete, verified change") {
		t.Fatal("rejection must keep the deterministic plan")
	}
}

func TestBrainstormNotCalledWhenPlanAlreadyFull(t *testing.T) {
	called := false
	withBrainstorm(t, func(_ string, _ []string, _ string) string { called = true; return "" })
	// Three file-scoped goals already fill the task budget.
	task := "improve\n\nGOAP goals:\n[P0] Fix internal/a2a/a.go\n[P0] Fix internal/engine/b.go\n[P0] Fix internal/domains/c.go"
	buildGoalDrivenImplementationPlan(task)
	if called {
		t.Fatal("brainstorm must not run when the deterministic plan already fills the task budget")
	}
}
