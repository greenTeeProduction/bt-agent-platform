package engine

import (
	"strings"
	"testing"
)

// A data-race regression test cannot fail without the race detector: on
// 2026-07-16 two mcpDeps race-guard milestones were closed on red-pass
// evidence at 02:07/03:00 while the guard did not yet exist — both plans'
// RED tests passed vacuously because their commands ran without -race.
// Tasks whose text names races/concurrency must be detected so their
// go-test commands run hardened.
func TestSuperpowersTaskMentionsRace(t *testing.T) {
	positives := []SuperpowersTask{
		{Title: "Guard mcpDeps against concurrent map writes"},
		{Objective: "add a sync.Mutex around the shared blackboard"},
		{Body: "fixes the data race in the stderr buffer"},
		{Objective: "the handler leaks a goroutine per request"},
		{Title: "RWMutex for the A2A CardCache"},
		{Objective: "reads and writes race under load"},
	}
	for _, task := range positives {
		if !superpowersTaskMentionsRace(task) {
			t.Errorf("task (%q / %q / %q) must be race-flavored", task.Title, task.Objective, task.Body)
		}
	}
	// "trace"/"tracing" are everywhere in this codebase (OTel) and contain
	// "race" as a substring — they must never trigger the hardening.
	negatives := []SuperpowersTask{
		{Title: "Wire OTel trace correlation into slog"},
		{Objective: "keep tracing spans joined across the A2A boundary"},
		{Body: "embrace the existing retry helper instead of a new one"},
		{Title: "Add braces to the lint config example"},
	}
	for _, task := range negatives {
		if superpowersTaskMentionsRace(task) {
			t.Errorf("task (%q / %q / %q) must NOT be race-flavored", task.Title, task.Objective, task.Body)
		}
	}
}

func TestRaceHardenedGoTestCommand(t *testing.T) {
	got := raceHardenedGoTestCommand("/usr/local/go/bin/go test ./internal/engine -short -count=1 -timeout 300s")
	want := "/usr/local/go/bin/go test -race ./internal/engine -short -count=1 -timeout 300s"
	if got != want {
		t.Fatalf("hardened command = %q, want %q", got, want)
	}
	already := "/usr/local/go/bin/go test -race ./internal/engine -count=1"
	if got := raceHardenedGoTestCommand(already); got != already {
		t.Fatalf("command with -race must stay unchanged, got %q", got)
	}
	script := "bash scripts/check-doc-drift.sh"
	if got := raceHardenedGoTestCommand(script); got != script {
		t.Fatalf("non-go-test command must stay unchanged, got %q", got)
	}
	build := "/usr/local/go/bin/go build ./cmd/bt-agent"
	if got := raceHardenedGoTestCommand(build); got != build {
		t.Fatalf("go build must stay unchanged, got %q", got)
	}
}

// End-to-end through the parser: the single choke point where plan tasks are
// materialized (fresh and resumed plans alike) hardens race-flavored tasks'
// test commands, so RED, GREEN, and review all run them identically.
func TestParseSuperpowersPlanHardensRaceFlavoredTaskTests(t *testing.T) {
	plan := `# Plan

### Task 1: Guard the shared blackboard against concurrent tool-call writes

**Objective:** add a mutex so concurrent MCP handlers stop racing on deps.bb

**Files:**
- Modify: cmd/bt-agent/tools.go
- Test: cmd/bt-agent/tools_test.go

**Step 1: RED**
Run: /usr/local/go/bin/go test ./cmd/bt-agent -run TestConcurrentBBWrites -count=1
Expected: FAIL

**Step 2: GREEN**
Run: /usr/local/go/bin/go test ./cmd/bt-agent -count=1
Expected: PASS

### Task 2: Wire OTel trace correlation into the webhook span

**Objective:** propagate the trace parent header into the delivery span

**Files:**
- Modify: internal/agent/webhook_publisher.go
- Test: internal/agent/webhook_publisher_test.go

**Step 1: RED**
Run: /usr/local/go/bin/go test ./internal/agent -run TestWebhookTrace -count=1
Expected: FAIL

**Step 2: GREEN**
Run: /usr/local/go/bin/go test ./internal/agent -count=1
Expected: PASS
`
	tasks, err := ParseSuperpowersPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(tasks))
	}
	if len(tasks[0].Tests) == 0 || len(tasks[1].Tests) == 0 {
		t.Fatal("fixture must yield test commands for both tasks")
	}
	for _, cmd := range tasks[0].Tests {
		if !strings.Contains(cmd, "go test -race ") {
			t.Fatalf("race-flavored task's test must run under the race detector: %q", cmd)
		}
	}
	for _, cmd := range tasks[1].Tests {
		if strings.Contains(cmd, "-race") {
			t.Fatalf("trace-correlation task must stay unhardened: %q", cmd)
		}
	}
}
