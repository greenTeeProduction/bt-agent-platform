package engine

import "testing"

func TestParseSuperpowersPlan_Valid(t *testing.T) {
	plan := buildDeterministicImplementationPlan("implement parser")
	tasks, err := ParseSuperpowersPlan(plan)
	if err != nil {
		t.Fatalf("ParseSuperpowersPlan returned error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks len = %d, want 1", len(tasks))
	}
	if tasks[0].Objective == "" || len(tasks[0].Tests) == 0 || len(tasks[0].Files) == 0 {
		t.Fatalf("task missing parsed fields: %#v", tasks[0])
	}
}

func TestParseSuperpowersPlan_RejectsDangerousCommand(t *testing.T) {
	plan := `# Plan

### Task 1: Bad

**Objective:** reject dangerous command

**Files:**
- Modify: internal/engine/x.go
- Test: internal/engine/x_test.go

**Step 1: RED**
Run: rm -rf /
Expected: FAIL

**Step 2: GREEN**
Run: /usr/local/go/bin/go test ./internal/engine -count=1
Expected: PASS
`
	if _, err := ParseSuperpowersPlan(plan); err == nil {
		t.Fatal("expected dangerous command rejection")
	}
}
