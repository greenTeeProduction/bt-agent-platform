package benchmark

import (
	"fmt"
	"os"
	"testing"

	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestTauBench_Airline(t *testing.T) {
	entries := BuiltinTauBenchAirline()
	if len(entries) != 5 {
		t.Fatalf("expected 5 airline entries, got %d", len(entries))
	}

	// Verify entries are well-formed
	for _, e := range entries {
		if e.ID == "" || e.Scenario == "" || e.Domain != "airline" {
			t.Errorf("airline entry %s malformed", e.ID)
		}
		if len(e.Tools) == 0 {
			t.Errorf("airline entry %s has no tools", e.ID)
		}
	}

	// Evaluate against GoDev tree (general-purpose)
	tree := evolution.GoDeveloperTree()
	mock := DefaultLLM()

	metrics := EvaluateTauBench(tree, entries, mock)

	fmt.Printf("\nτ-bench Airline: %d/%d goals achieved, %.0f%% action accuracy\n",
		metrics.GoalAchieved, metrics.TotalScenarios, metrics.ActionAccuracy*100)

	for _, r := range metrics.Results {
		status := "✓"
		if !r.GoalAchieved {
			status = "✗"
		}
		fmt.Printf("  %s %s: %d/%d actions matched\n",
			status, r.EntryID, r.ActionsMatched, r.ActionsExpected)
	}

	if metrics.TotalScenarios != 5 {
		t.Errorf("expected 5 scenarios processed, got %d", metrics.TotalScenarios)
	}

	// With mock LLM, goal achievement is by design modest.
	// The key metric is action accuracy — the tree should at least reference some tools.
	// A 0.0 would indicate the tree never outputs tool names at all.
	t.Logf("Goal achievement: %.0f%%, Action accuracy: %.0f%%",
		float64(metrics.GoalAchieved)/float64(metrics.TotalScenarios)*100,
		metrics.ActionAccuracy*100)

	if metrics.GoalAchieved == 0 {
		t.Log("Note: 0 goal achievements with mock LLM — expected for complex scenarios")
	}
}

func TestTauBench_Retail(t *testing.T) {
	entries := BuiltinTauBenchRetail()
	if len(entries) != 5 {
		t.Fatalf("expected 5 retail entries, got %d", len(entries))
	}

	// Verify entries are well-formed
	for _, e := range entries {
		if e.ID == "" || e.Scenario == "" || e.Domain != "retail" {
			t.Errorf("retail entry %s malformed", e.ID)
		}
		if len(e.Tools) == 0 {
			t.Errorf("retail entry %s has no tools", e.ID)
		}
	}

	// Use GoDev tree
	tree := evolution.GoDeveloperTree()
	mock := DefaultLLM()

	metrics := EvaluateTauBench(tree, entries, mock)

	fmt.Printf("\nτ-bench Retail: %d/%d goals achieved, %.0f%% action accuracy\n",
		metrics.GoalAchieved, metrics.TotalScenarios, metrics.ActionAccuracy*100)

	for _, r := range metrics.Results {
		status := "✓"
		if !r.GoalAchieved {
			status = "✗"
		}
		fmt.Printf("  %s %s: %d/%d actions matched\n",
			status, r.EntryID, r.ActionsMatched, r.ActionsExpected)
	}

	if metrics.TotalScenarios != 5 {
		t.Errorf("expected 5 scenarios processed, got %d", metrics.TotalScenarios)
	}

	t.Logf("Goal achievement: %.0f%%, Action accuracy: %.0f%%",
		float64(metrics.GoalAchieved)/float64(metrics.TotalScenarios)*100,
		metrics.ActionAccuracy*100)
}

func TestTauBench_MultiDomain(t *testing.T) {
	allEntries := DefaultTauBenchEntries()
	if len(allEntries) != 10 {
		t.Fatalf("expected 10 total entries (5 airline + 5 retail), got %d", len(allEntries))
	}

	domainCounts := map[string]int{}
	for _, e := range allEntries {
		domainCounts[e.Domain]++
	}
	if domainCounts["airline"] != 5 {
		t.Errorf("expected 5 airline entries, got %d", domainCounts["airline"])
	}
	if domainCounts["retail"] != 5 {
		t.Errorf("expected 5 retail entries, got %d", domainCounts["retail"])
	}

	// Run each domain through different tree types
	airlineEntries := BuiltinTauBenchAirline()
	retailEntries := BuiltinTauBenchRetail()
	mock := DefaultLLM()

	trees := map[string]*evolution.SerializableNode{
		"godev":       evolution.GoDeveloperTree(),
		"code_review": domains.CodeReviewTree(),
	}

	for treeName, tree := range trees {
		// Airline
		airMetrics := EvaluateTauBench(tree, airlineEntries, mock)
		fmt.Printf("\nτ-bench Airline via %s: %d/%d goals, %.0f%% actions (%.1fs avg)\n",
			treeName, airMetrics.GoalAchieved, airMetrics.TotalScenarios,
			airMetrics.ActionAccuracy*100, airMetrics.AvgTurns)

		// Retail
		retailMetrics := EvaluateTauBench(tree, retailEntries, mock)
		fmt.Printf("τ-bench Retail via %s: %d/%d goals, %.0f%% actions (%.1fs avg)\n",
			treeName, retailMetrics.GoalAchieved, retailMetrics.TotalScenarios,
			retailMetrics.ActionAccuracy*100, retailMetrics.AvgTurns)

		// Each tree type should produce metrics for all scenarios
		if airMetrics.TotalScenarios != 5 {
			t.Errorf("%s: airline got %d scenarios, expected 5", treeName, airMetrics.TotalScenarios)
		}
		if retailMetrics.TotalScenarios != 5 {
			t.Errorf("%s: retail got %d scenarios, expected 5", treeName, retailMetrics.TotalScenarios)
		}
	}
}

func TestTauBench_TaskCreation(t *testing.T) {
	// Test that buildTauBenchTask creates non-empty, well-structured prompts
	entries := BuiltinTauBenchAirline()
	if len(entries) == 0 {
		t.Fatal("no airline entries")
	}

	task := buildTauBenchTask(entries[0])

	if task == "" {
		t.Error("buildTauBenchTask returned empty string")
	}
	if len(task) < 50 {
		t.Errorf("task too short: %d chars", len(task))
	}

	// Should contain domain info and tool names
	if !containsStr(task, "airline") {
		t.Error("task should mention airline domain")
	}
	if !containsStr(task, "book_reservation") {
		t.Error("task should include book_reservation tool")
	}

	t.Logf("Built task (%d chars):\n%s\n", len(task), task[:minLen(200, len(task))])
}

func TestTauBench_ActionMatching(t *testing.T) {
	expected := []TauBenchAction{
		{Name: "get_user_details"},
		{Name: "search_direct_flight"},
		{Name: "book_reservation"},
	}

	// Output that mentions all actions
	output := "I called get_user_details for the customer, then used search_direct_flight to find available flights, and finally executed book_reservation."
	matched, missed := matchActions(output, expected)

	if len(matched) != 3 {
		t.Errorf("expected 3 matches, got %d: %v", len(matched), matched)
	}
	if len(missed) != 0 {
		t.Errorf("expected 0 missed, got %d: %v", len(missed), missed)
	}

	// Output that mentions none
	output2 := "Hello, how can I help you today?"
	matched2, missed2 := matchActions(output2, expected)
	if len(matched2) != 0 {
		t.Errorf("expected 0 matches, got %d: %v", len(matched2), matched2)
	}
	if len(missed2) != 3 {
		t.Errorf("expected 3 missed, got %d: %v", len(missed2), missed2)
	}

	// Case insensitive
	output3 := "I used GET_USER_DETAILS and Search_Direct_Flight."
	matched3, _ := matchActions(output3, expected)
	if len(matched3) != 2 {
		t.Errorf("expected 2 case-insensitive matches, got %d: %v", len(matched3), matched3)
	}
}

func TestTauBench_ToolDefinitions(t *testing.T) {
	// Verify both tool sets are well-formed
	airTools := airlineTools()
	if len(airTools) < 10 {
		t.Errorf("expected at least 10 airline tools, got %d", len(airTools))
	}

	retailTools := retailTools()
	if len(retailTools) < 10 {
		t.Errorf("expected at least 10 retail tools, got %d", len(retailTools))
	}

	// Verify no duplicate tool names within each domain
	checkDuplicates := func(tools []TauBenchTool, domain string) {
		seen := map[string]bool{}
		for _, tool := range tools {
			if tool.Name == "" {
				t.Errorf("%s: tool with empty name", domain)
			}
			if seen[tool.Name] {
				t.Errorf("%s: duplicate tool name %q", domain, tool.Name)
			}
			seen[tool.Name] = true
		}
	}
	checkDuplicates(airTools, "airline")
	checkDuplicates(retailTools, "retail")
}

func TestTauBench_TaskLoading(t *testing.T) {
	// Test loading from the real τ-bench repo if available
	repoPath := DefaultTauBenchRepoPath
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		t.Skipf("τ-bench repo not found at %s — skipping load test", repoPath)
	}

	// Test airline
	entries, err := LoadTauBenchTasks("airline")
	if err != nil {
		t.Fatalf("LoadTauBenchTasks(airline): %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected non-empty airline tasks")
	}
	t.Logf("Loaded %d airline tasks from τ-bench repo", len(entries))

	// Verify first entry structure
	if len(entries) > 0 {
		e := entries[0]
		if e.ID == "" {
			t.Error("first airline entry has empty ID")
		}
		if e.Domain != "airline" {
			t.Errorf("expected domain=airline, got %s", e.Domain)
		}
		if e.Scenario == "" {
			t.Error("first airline entry has empty scenario")
		}
		if len(e.Tools) == 0 {
			t.Error("first airline entry has no tools")
		}
	}

	// Test retail
	retailEntries, err := LoadTauBenchTasks("retail")
	if err != nil {
		t.Fatalf("LoadTauBenchTasks(retail): %v", err)
	}
	if len(retailEntries) == 0 {
		t.Error("expected non-empty retail tasks")
	}
	t.Logf("Loaded %d retail tasks from τ-bench repo", len(retailEntries))

	// Test invalid domain
	_, err = LoadTauBenchTasks("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent domain")
	}
}

func TestTauBench_EmptyEntries(t *testing.T) {
	// Evaluate with empty entries should not panic
	tree := evolution.GoDeveloperTree()
	mock := DefaultLLM()
	metrics := EvaluateTauBench(tree, nil, mock)

	if metrics.TotalScenarios != 0 {
		t.Errorf("expected 0 scenarios, got %d", metrics.TotalScenarios)
	}
	if metrics.GoalAchieved != 0 {
		t.Errorf("expected 0 goals, got %d", metrics.GoalAchieved)
	}
}

func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}
