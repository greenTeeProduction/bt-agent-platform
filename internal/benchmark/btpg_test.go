package benchmark

import (
	"fmt"
	"testing"

	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestBTPG_QualityMetrics_StaticAnalysis(t *testing.T) {
	tree := evolution.GoDeveloperTree()

	metrics := BTPGQualityScore(tree)

	fmt.Printf("\nBTPG Static: GoDevTree → nodes=%d depth=%d branchFactor=%.2f\n",
		metrics.NodeCount, metrics.Depth, metrics.BranchingFactor)

	if metrics.NodeCount < 5 {
		t.Errorf("GoDev tree should have at least 5 nodes, got %d", metrics.NodeCount)
	}
	if metrics.Depth < 2 {
		t.Errorf("GoDev tree should have depth >= 2, got %d", metrics.Depth)
	}
	if metrics.BranchingFactor < 1.0 {
		t.Errorf("GoDev tree branching factor should be >= 1.0, got %.2f", metrics.BranchingFactor)
	}
}

func TestBTPG_QualityMetrics_CodeReviewTree(t *testing.T) {
	tree := domains.CodeReviewTree()

	metrics := BTPGQualityScore(tree)

	fmt.Printf("\nBTPG Static: CodeReviewTree → nodes=%d depth=%d branchFactor=%.2f\n",
		metrics.NodeCount, metrics.Depth, metrics.BranchingFactor)

	if metrics.NodeCount < 3 {
		t.Errorf("CodeReview tree should have at least 3 nodes, got %d", metrics.NodeCount)
	}
	if metrics.Depth < 1 {
		t.Errorf("CodeReview tree should have depth >= 1, got %d", metrics.Depth)
	}
}

func TestBTPG_QualityMetrics_AllDomainTrees(t *testing.T) {
	trees := map[string]*evolution.SerializableNode{
		"GoDev":           evolution.GoDeveloperTree(),
		"CodeReview":      domains.CodeReviewTree(),
		"DevOpsCI":        domains.DevOpsCITree(),
		"AgentMonitor":    domains.AgentMonitorTree(),
		"CrashInvestigator": domains.CrashInvestigatorTree(),
	}

	fmt.Println("\nBTPG Static Analysis:")
	for name, tree := range trees {
		m := BTPGQualityScore(tree)
		fmt.Printf("  %-18s nodes=%3d depth=%2d branchFactor=%.2f\n",
			name, m.NodeCount, m.Depth, m.BranchingFactor)

		if m.NodeCount == 0 {
			t.Errorf("%s tree has 0 nodes", name)
		}
	}
}

func TestBTPG_TaskExecution(t *testing.T) {
	if testing.Short() { t.Skip("skipping LLM-dependent BTPG test in short mode") }
	tasks := BuiltinBTPGTasks()
	if len(tasks) != 8 {
		t.Errorf("expected 8 BTPG tasks, got %d", len(tasks))
	}

	// Verify all tasks are non-empty
	for i, task := range tasks {
		if task == "" {
			t.Errorf("BTPG task %d is empty", i)
		}
		// Service robot tasks should be imperative/physical
		if len(task) < 10 {
			t.Errorf("BTPG task %d too short: %q", i, task)
		}
	}

	// Run with real LLM (falls back to mock if Ollama unavailable)
	tree := evolution.GoDeveloperTree()
	llm := DefaultLLM()
	result := EvaluateBTPG(tree, tasks, llm)

	fmt.Printf("\nBTPG Task Execution: success=%.0f%% efficiency=%.4f robustness=%.2f\n",
		result.Metrics.SuccessRate*100,
		result.Metrics.PlanningEfficiency,
		result.Metrics.RobustnessScore)

	for _, tr := range result.PerTask {
		mark := "✗"
		if tr.Success {
			mark = "✓"
		}
		fmt.Printf("  %s %-30s visited=%d path=%s\n", mark, tr.Task, tr.NodesVisited, tr.Path)
	}

	if len(result.PerTask) != 8 {
		t.Errorf("expected 8 per-task results, got %d", len(result.PerTask))
	}
	if result.Metrics.SuccessRate < 0 || result.Metrics.SuccessRate > 1.0 {
		t.Errorf("success rate out of range: %.2f", result.Metrics.SuccessRate)
	}
	if result.Metrics.PlanningEfficiency < 0 {
		t.Errorf("planning efficiency should be non-negative: %.4f", result.Metrics.PlanningEfficiency)
	}
}

func TestBTPG_TaskExecution_FiveTasks(t *testing.T) {
	if testing.Short() { t.Skip("skipping LLM-dependent BTPG test in short mode") }
	// Test with subset of 5 tasks as specified
	tasks := BuiltinBTPGTasks()[:5]
	tree := evolution.GoDeveloperTree()
	llm := DefaultLLM()

	result := EvaluateBTPG(tree, tasks, llm)

	fmt.Printf("\nBTPG 5-Task: success=%.0f%% robustness=%.2f\n",
		result.Metrics.SuccessRate*100, result.Metrics.RobustnessScore)

	if result.Metrics.SuccessRate < 0.3 {
		t.Logf("BTPG success rate low (%.0f%%), may be expected with generic tree on robot tasks",
			result.Metrics.SuccessRate*100)
	}
}

func TestBTPG_EmptyTasks(t *testing.T) {
	tree := evolution.GoDeveloperTree()
	llm := DefaultMock()

	result := EvaluateBTPG(tree, nil, llm)
	if len(result.PerTask) != 0 {
		t.Errorf("expected 0 results for empty tasks, got %d", len(result.PerTask))
	}
	if result.Metrics.NodeCount == 0 {
		t.Error("static metrics should still be computed for empty input")
	}
}

func TestBTPG_EdgeCaseRobustness(t *testing.T) {
	if testing.Short() { t.Skip("skipping LLM-dependent BTPG test in short mode") }
	// Tasks that are edge cases should test robustness
	edgeTasks := []string{
		"bring coffee",          // very short
		"??",                    // ambiguous
		"clean the entire house and do the laundry and wash dishes and mop floors!!", // very long
	}

	tree := evolution.GoDeveloperTree()
	llm := DefaultLLM()
	result := EvaluateBTPG(tree, edgeTasks, llm)

	fmt.Printf("\nBTPG Edge Cases: success=%.0f%% robustness=%.2f\n",
		result.Metrics.SuccessRate*100, result.Metrics.RobustnessScore)

	// Robustness should be computed even for edge cases
	if result.Metrics.RobustnessScore < 0 || result.Metrics.RobustnessScore > 1.0 {
		t.Errorf("robustness score out of range: %.2f", result.Metrics.RobustnessScore)
	}
}

func TestBTPG_NilTree(t *testing.T) {
	metrics := BTPGQualityScore(nil)
	if metrics.NodeCount != 0 {
		t.Errorf("nil tree should have 0 nodes, got %d", metrics.NodeCount)
	}
	if metrics.Depth != 0 {
		t.Errorf("nil tree should have depth 0, got %d", metrics.Depth)
	}
}

func TestBTPG_BuiltinTasks_Content(t *testing.T) {
	tasks := BuiltinBTPGTasks()

	// Verify all tasks are service-robot style (physical actions)
	actionVerbs := []string{"bring", "clean", "set", "find", "water", "take", "feed", "turn"}
	for i, verb := range actionVerbs {
		if !containsStr(tasks[i], verb) {
			t.Errorf("BTPG task %d (%q) missing expected verb %q", i, tasks[i], verb)
		}
	}
}
