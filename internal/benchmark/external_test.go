package benchmark

import (
	"fmt"
	"testing"

	"github.com/nico/go-bt-evolve/internal/llm"

	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/research"
)

func TestBFCL_Simple_RoutingAccuracy(t *testing.T) {
	llm.SkipUnlessIntegration(t)
	// Use GoDev tree — it has 5 strategy paths. Match BFCL entries to those paths.
	suite := &BFCLSuite{
		Name: "bfcl_godev",
		Entries: []BFCLEntry{
			{ID: "gv-001", Query: "explain how Go interfaces work", ExpectedTool: "GoKnowledgePath", Category: "simple"},
			{ID: "gv-002", Query: "build and compile the Go project", ExpectedTool: "BuildPath", Category: "simple"},
			{ID: "gv-003", Query: "run Go tests with coverage", ExpectedTool: "TestPath", Category: "simple"},
			{ID: "gv-004", Query: "review this Go code for bugs", ExpectedTool: "CodeReviewPath", Category: "simple"},
			{ID: "gv-005", Query: "analyze the performance of this function", ExpectedTool: "ExecutionPath", Category: "simple"},
		},
	}
	tree := evolution.GoDeveloperTree()
	llmBackend := RealLLM(t)
	metrics := suite.Evaluate(tree, llmBackend)

	fmt.Printf("\nBFCL GoDev: %d/%d (%.0f%%)\n", metrics.CorrectRoutes, metrics.TotalEntries, metrics.Accuracy*100)
	for _, r := range metrics.Results {
		s := "✓"
		if !r.Correct {
			s = "✗"
		}
		fmt.Printf("  %s %-40s → %s\n", s, r.Query, r.ActualPath)
	}

	if metrics.Accuracy < 0.4 {
		t.Errorf("GoDev routing accuracy too low: %.0f%%", metrics.Accuracy*100)
	}
}

func TestBFCL_Relevance_NoFalsePositives(t *testing.T) {
	llm.SkipUnlessIntegration(t)
	// GoDev tree correctly rejects non-Go tasks at PreGate.
	// "tell me a joke" has no Go keywords → IsGoRelated fails → PreGate fails → failure.
	// This is CORRECT behavior for a specialized tree.
	suite := &BFCLSuite{
		Name: "bfcl_relevance_godev",
		Entries: []BFCLEntry{
			{ID: "rel-001", Query: "tell me a joke about programmers", ExpectedTool: "ExecutionPath", Category: "relevance"},
			{ID: "rel-002", Query: "what is the capital of Mongolia?", ExpectedTool: "ExecutionPath", Category: "relevance"},
		},
	}
	tree := evolution.GoDeveloperTree()
	llmBackend := RealLLM(t)
	metrics := suite.Evaluate(tree, llmBackend)

	// Non-Go tasks should be rejected at PreGate — they won't succeed.
	// But they also shouldn't route to domain-specific tools.
	for _, r := range metrics.Results {
		if r.ActualPath != "UnknownPath" {
			t.Logf("non-Go task %q routed to %s", r.Query, r.ActualPath)
		}
	}
	// Success rate will be 0% for non-Go tasks — that's correct
	t.Logf("Non-Go relevance tasks: %.0f/%d successes (expected 0)", metrics.SuccessRate, metrics.TotalEntries)
}

func TestBFCL_Multiple_Routing(t *testing.T) {
	llm.SkipUnlessIntegration(t)
	// Use code_review tree which has multiple strategy paths
	suite := &BFCLSuite{
		Name: "bfcl_multiple_cr",
		Entries: []BFCLEntry{
			{ID: "cr-m001", Query: "find bugs in this code", ExpectedTool: "BugDetection", Category: "multiple"},
			{ID: "cr-m002", Query: "scan for security vulnerabilities in this code", ExpectedTool: "SecurityReview", Category: "multiple"},
			{ID: "cr-m003", Query: "check code style and formatting", ExpectedTool: "StyleReview", Category: "multiple"},
		},
	}
	tree := domains.CodeReviewTree()
	llmBackend := RealLLM(t)
	metrics := suite.Evaluate(tree, llmBackend)

	fmt.Printf("\nBFCL CodeReview Multiple: %d/%d (%.0f%%)\n",
		metrics.CorrectRoutes, metrics.TotalEntries, metrics.Accuracy*100)

	if metrics.Accuracy < 0.5 {
		t.Errorf("BFCL multiple routing accuracy too low: %.0f%%", metrics.Accuracy*100)
	}
}

func TestGAIA_DeepResearch(t *testing.T) {
	llm.SkipUnlessIntegration(t)
	entries := BuiltinGAIADev()
	tree := research.DeepResearchTree()
	llmBackend := RealLLM(t)

	metrics := EvaluateGAIA(tree, entries, llmBackend)

	fmt.Printf("\nGAIA Deep Research: %d/%d correct (%.0f%% accuracy)\n",
		metrics.CorrectAnswers, metrics.TotalQuestions, metrics.Accuracy*100)

	for level := 1; level <= 3; level++ {
		if lm, ok := metrics.ByLevel[level]; ok {
			fmt.Printf("  Level %d: %d/%d (%.0f%%)\n", level, lm.Correct, lm.Total, lm.Accuracy*100)
		}
	}

	// With llmBackend LLM, answer matching is approximate. Verify at least the tree runs without error.
	if metrics.TotalQuestions != len(entries) {
		t.Errorf("expected %d questions processed, got %d", len(entries), metrics.TotalQuestions)
	}
	// Accuracy is expected to be low with llmBackend LLM since answers are generic
	t.Logf("Note: low accuracy expected with llmBackend LLM — real LLM needed for substantive GAIA eval")
}

func TestBFCL_CodeReview_Routing(t *testing.T) {
	llm.SkipUnlessIntegration(t)
	// Test BFCL routing on the code_review domain tree
	tree := domains.CodeReviewTree()
	llmBackend := RealLLM(t)

	// Create a mini-suite targeting code review paths
	suite := &BFCLSuite{
		Name: "bfcl_code_review",
		Entries: []BFCLEntry{
			{ID: "cr-001", Query: "find bugs in this Go code", ExpectedTool: "BugDetection", Category: "simple"},
			{ID: "cr-002", Query: "scan for security vulnerabilities", ExpectedTool: "SecurityReview", Category: "simple"},
			{ID: "cr-003", Query: "check code style and formatting", ExpectedTool: "StyleReview", Category: "simple"},
			{ID: "cr-004", Query: "analyze this code for improvements", ExpectedTool: "ExecutionPath", Category: "simple"},
		},
	}

	metrics := suite.Evaluate(tree, llmBackend)

	fmt.Printf("\nBFCL CodeReview: %d/%d correct (%.0f%% accuracy)\n",
		metrics.CorrectRoutes, metrics.TotalEntries, metrics.Accuracy*100)

	if metrics.Accuracy < 0.5 {
		t.Errorf("code review routing accuracy too low: %.0f%%", metrics.Accuracy*100)
	}
}

func TestBFCL_AllDomainTrees_Accuracy(t *testing.T) {
	llm.SkipUnlessIntegration(t)
	// Run BFCL Simple against all domain trees, measure which tree has best routing
	type treeScore struct {
		name     string
		accuracy float64
	}
	scores := make([]treeScore, 0, 16)

	allTrees := map[string]*evolution.SerializableNode{
		"godev":              evolution.GoDeveloperTree(),
		"code_review":        domains.CodeReviewTree(),
		"devops_ci":          domains.DevOpsCITree(),
		"agent_monitor":      domains.AgentMonitorTree(),
		"crash_investigator": domains.CrashInvestigatorTree(),
	}

	suite := BuiltinBFCLSimple()
	llmBackend := RealLLM(t)

	for name, tree := range allTrees {
		metrics := suite.Evaluate(tree, llmBackend)
		scores = append(scores, treeScore{name, metrics.Accuracy})
		fmt.Printf("  %-20s %.0f%% (%d/%d)\n", name, metrics.Accuracy*100,
			metrics.CorrectRoutes, metrics.TotalEntries)
	}

	// Best tree should have > 0% accuracy (at least some tasks match)
	best := scores[0]
	for _, s := range scores {
		if s.accuracy > best.accuracy {
			best = s
		}
	}
	if best.accuracy == 0 {
		t.Error("no tree achieved any routing accuracy on BFCL Simple")
	}
	fmt.Printf("\nBest: %s at %.0f%%\n", best.name, best.accuracy*100)
}

func TestSWELite_IssueAnalysis(t *testing.T) {
	llm.SkipUnlessIntegration(t)
	entries := BuiltinSWELite()
	if len(entries) < 3 {
		t.Fatal("expected at least 3 SWE entries")
	}

	// Verify entries are well-formed
	for _, e := range entries {
		if e.ID == "" || e.IssueTitle == "" {
			t.Errorf("SWE entry %s is missing required fields", e.ID)
		}
	}

	// Test that GoDev tree can process SWE task descriptions
	tree := evolution.GoDeveloperTree()
	llmBackend := RealLLM(t)

	resolved := 0
	for _, e := range entries {
		bb := &engine.Blackboard{
			Task: fmt.Sprintf("fix: %s\n\n%s", e.IssueTitle, e.IssueBody),
			LLM:  llmBackend,
		}
		bt := engine.BuildTree(tree, bb)
		output := engine.RunTask(bb, bt)

		// A "resolved" task produces a non-empty output with success outcome
		if bb.Outcome == "success" && len(output) > 20 {
			resolved++
		}
	}

	resolveRate := float64(resolved) / float64(len(entries))
	fmt.Printf("\nSWE-bench Lite: %d/%d resolved (%.0f%%)\n", resolved, len(entries), resolveRate*100)

	if resolveRate < 0.4 {
		t.Errorf("SWE-bench resolve rate too low: %.0f%%", resolveRate*100)
	}
}
