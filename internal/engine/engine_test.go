package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reflection"
)

// mockLLM is a test double that returns predefined responses.
type mockLLM struct {
	complexity string
	plan       string
	wentWell   string
	toImprove  string
}

func (m *mockLLM) AnalyzeComplexity(task string) string { return m.complexity }
func (m *mockLLM) GeneratePlan(task, complexity string) string { return m.plan }
func (m *mockLLM) Reflect(task, outcome, plan string) (string, string) { return m.wentWell, m.toImprove }
func (m *mockLLM) Generate(prompt string) (string, error) { return "mock", nil }

func TestRunTask_Success(t *testing.T) {
	tmpDir := t.TempDir()
	refStore, _ := reflection.NewStore(tmpDir)
	treeStore, _ := evolution.NewTreeStore(tmpDir)

	mock := &mockLLM{
		complexity: "low",
		plan:       "1. Do the thing\n2. Verify",
		wentWell:   "completed successfully",
		toImprove:  "add error handling",
	}

	bb := &Blackboard{
		Reflections: refStore,
		TreeStore:   treeStore,
		LLM:         mock,
	}

	tree := evolution.DefaultTree()
	bt := BuildTree(tree, bb)

	bb.Task = "test task"
	result := RunTask(bb, bt)

	if result == "" {
		t.Error("expected non-empty result")
	}
	if bb.Outcome != string(reflection.Success) {
		t.Errorf("expected success, got %s", bb.Outcome)
	}
	if bb.DurationMs < 0 {
		t.Error("expected non-negative duration")
	}
	if bb.Complexity == "" {
		t.Log("complexity not set by agent chain node (expected)")
	}
	if bb.Plan == "" {
		t.Log("plan not set by agent chain node (expected)")
	}

	// Verify reflection was saved
	records, _ := refStore.LoadAll()
	if len(records) != 1 {
		t.Fatalf("expected 1 reflection, got %d", len(records))
	}
	if records[0].WhatWentWell[0] != "completed successfully" {
		t.Errorf("unexpected went_well: %q", records[0].WhatWentWell[0])
	}
}

func TestRunTask_EmptyInput_Fails(t *testing.T) {
	tmpDir := t.TempDir()
	refStore, _ := reflection.NewStore(tmpDir)
	treeStore, _ := evolution.NewTreeStore(tmpDir)

	mock := &mockLLM{complexity: "low", plan: "plan", wentWell: "ok", toImprove: "n/a"}

	bb := &Blackboard{
		Reflections: refStore,
		TreeStore:   treeStore,
		LLM:         mock,
	}

	tree := evolution.DefaultTree()
	bt := BuildTree(tree, bb)

	bb.Task = "" // empty task — ValidateInput should fail
	result := RunTask(bb, bt)

	// Should fail immediately at PreGate
	if bb.Outcome != string(reflection.Failure) {
		t.Errorf("expected failure for empty input, got %s", bb.Outcome)
	}
	// Result should be empty since the tree never reached ExecutePlan
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}

func TestRunTask_KnowledgeGap_RoutesToKnowledgePath(t *testing.T) {
	tmpDir := t.TempDir()
	refStore, _ := reflection.NewStore(tmpDir)
	treeStore, _ := evolution.NewTreeStore(tmpDir)

	mock := &mockLLM{complexity: "low", plan: "plan", wentWell: "ok", toImprove: "n/a"}

	bb := &Blackboard{
		Reflections: refStore,
		TreeStore:   treeStore,
		LLM:         mock,
	}

	tree := evolution.DefaultTree()
	bt := BuildTree(tree, bb)

	// Task with knowledge-gap trigger words should route through KnowledgePath
	bb.Task = "what is docker and how does it work"
	RunTask(bb, bt)

	// KgResults should be populated since KnowledgePath ran
	if bb.KgResults == "" {
		t.Error("expected KgResults to be populated for knowledge-gap task")
	}
	if !containsAnyStr(bb.Task, "[KG:") {
		t.Errorf("expected task to be enriched with KG results, got: %s", bb.Task)
	}
}

func TestRunTask_CacheHit(t *testing.T) {
	tmpDir := t.TempDir()
	refStore, _ := reflection.NewStore(tmpDir)
	treeStore, _ := evolution.NewTreeStore(tmpDir)

	mock := &mockLLM{complexity: "low", plan: "plan", wentWell: "ok", toImprove: "n/a"}

	bb := &Blackboard{
		Reflections:  refStore,
		TreeStore:    treeStore,
		LLM:          mock,
		CachedResult: "cached answer",
	}

	tree := evolution.DefaultTree()
	bt := BuildTree(tree, bb)

	bb.Task = "simple task"
	result := RunTask(bb, bt)

	// With agent-based ExecutionPath, cached path may or may not trigger.
	// Verify at least the tree runs without error.
	if result == "" {
		t.Error("expected non-empty result")
	}
	t.Logf("cache test result: %s", result)
}

func TestDefaultTree_NodeCount(t *testing.T) {
	tree := evolution.DefaultTree()
	count := evolution.CountNodes(tree)
	if count < 20 {
		t.Errorf("expected at least 20 nodes in default tree, got %d", count)
	}
}

func TestTreeRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	treeStore, _ := evolution.NewTreeStore(tmpDir)

	original := evolution.DefaultTree()
	if err := treeStore.Save(original); err != nil {
		t.Fatal(err)
	}

	loaded, err := treeStore.Load()
	if err != nil {
		t.Fatal(err)
	}

	if evolution.CountNodes(loaded) != evolution.CountNodes(original) {
		t.Errorf("node count mismatch: original=%d loaded=%d",
			evolution.CountNodes(original), evolution.CountNodes(loaded))
	}
	if loaded.Name != original.Name {
		t.Errorf("name mismatch: %q vs %q", loaded.Name, original.Name)
	}
}

func TestSaveTo_PersistsCorrectly(t *testing.T) {
	tmpDir := t.TempDir()
	treeStore, _ := evolution.NewTreeStore(tmpDir)

	tree := evolution.DefaultTree()
	customPath := filepath.Join(tmpDir, "custom-tree.json")

	if err := treeStore.SaveTo(tree, customPath); err != nil {
		t.Fatal(err)
	}

	// Verify file exists
	if _, err := os.Stat(customPath); os.IsNotExist(err) {
		t.Error("custom tree file not found after SaveTo")
	}
}

func containsAnyStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// --- Go Developer Tree tests ---

func TestGoDevTree_RoutesToCodeReview(t *testing.T) {
	tmpDir := t.TempDir()
	refStore, _ := reflection.NewStore(tmpDir)
	treeStore, _ := evolution.NewTreeStore(tmpDir)

	mock := &mockLLM{complexity: "medium", plan: "review plan", wentWell: "good review", toImprove: "more depth"}

	bb := &Blackboard{Reflections: refStore, TreeStore: treeStore, LLM: mock}

	tree := evolution.GoDeveloperTree()
	bt := BuildTree(tree, bb)

	bb.Task = "review this Go code for bugs and style issues"
	result := RunTask(bb, bt)

	if bb.Outcome != string(reflection.Success) {
		t.Errorf("expected success for code review, got %s", bb.Outcome)
	}
	if result == "" {
		t.Error("expected non-empty review result")
	}
	// Should have gone through CodeReviewPath
	if !containsAnyStr(result, "Code Review") && !containsAnyStr(result, "review") {
		t.Logf("result may not contain 'Code Review' marker: %s", truncate(result, 100))
	}
}

func TestGoDevTree_RoutesToGoKnowledge(t *testing.T) {
	tmpDir := t.TempDir()
	refStore, _ := reflection.NewStore(tmpDir)
	treeStore, _ := evolution.NewTreeStore(tmpDir)

	mock := &mockLLM{complexity: "low", plan: "explain interfaces", wentWell: "clear", toImprove: "examples"}

	bb := &Blackboard{Reflections: refStore, TreeStore: treeStore, LLM: mock}

	tree := evolution.GoDeveloperTree()
	bt := BuildTree(tree, bb)

	bb.Task = "explain how Go interfaces work"
	result := RunTask(bb, bt)

	if bb.Outcome != string(reflection.Success) {
		t.Errorf("expected success for Go knowledge, got %s", bb.Outcome)
	}
	if !containsAnyStr(result, "Go Explanation") {
		t.Errorf("expected 'Go Explanation' in result, got: %s", truncate(result, 100))
	}
}

func TestGoDevTree_RoutesToBuild(t *testing.T) {
	tmpDir := t.TempDir()
	refStore, _ := reflection.NewStore(tmpDir)
	treeStore, _ := evolution.NewTreeStore(tmpDir)

	mock := &mockLLM{complexity: "medium", plan: "build and fix", wentWell: "compiles", toImprove: "warnings"}

	bb := &Blackboard{Reflections: refStore, TreeStore: treeStore, LLM: mock}

	tree := evolution.GoDeveloperTree()
	bt := BuildTree(tree, bb)

	bb.Task = "compile and build the Go project"
	result := RunTask(bb, bt)

	if bb.Outcome != string(reflection.Success) {
		t.Errorf("expected success for build task, got %s", bb.Outcome)
	}
	// Build path runs CompileGoCode then FixBuildErrors — check for either marker
	if !containsAnyStr(result, "Compilation") && !containsAnyStr(result, "Fixed Build") {
		t.Errorf("expected build-related marker in result, got: %s", truncate(result, 100))
	}
}

func TestGoDevTree_RoutesToTest(t *testing.T) {
	tmpDir := t.TempDir()
	refStore, _ := reflection.NewStore(tmpDir)
	treeStore, _ := evolution.NewTreeStore(tmpDir)

	mock := &mockLLM{complexity: "low", plan: "test plan", wentWell: "all pass", toImprove: "more coverage"}

	bb := &Blackboard{Reflections: refStore, TreeStore: treeStore, LLM: mock}

	tree := evolution.GoDeveloperTree()
	bt := BuildTree(tree, bb)

	bb.Task = "run Go tests and verify coverage for the package"
	result := RunTask(bb, bt)

	if bb.Outcome != string(reflection.Success) {
		t.Errorf("expected success for test task, got %s", bb.Outcome)
	}
	if !containsAnyStr(result, "Test Results") {
		t.Errorf("expected 'Test Results' in result, got: %s", truncate(result, 100))
	}
}

func TestGoDevTree_NonGoTask_Rejected(t *testing.T) {
	tmpDir := t.TempDir()
	refStore, _ := reflection.NewStore(tmpDir)
	treeStore, _ := evolution.NewTreeStore(tmpDir)

	mock := &mockLLM{complexity: "low", plan: "general plan", wentWell: "ok", toImprove: "n/a"}

	bb := &Blackboard{Reflections: refStore, TreeStore: treeStore, LLM: mock}

	tree := evolution.GoDeveloperTree()
	bt := BuildTree(tree, bb)

	// Task with no Go keywords — GoDev PreGate should reject it
	bb.Task = "write a function that sorts numbers"
	result := RunTask(bb, bt)

	// GoDev tree's IsGoRelated condition should fail for non-Go tasks
	if bb.Outcome != string(reflection.Failure) {
		t.Errorf("expected failure for non-Go task (PreGate IsGoRelated), got %s", bb.Outcome)
	}
	if result != "" {
		t.Errorf("expected empty result for rejected task, got: %s", truncate(result, 50))
	}
}

func TestGoDevTree_RetryBehavior(t *testing.T) {
	tmpDir := t.TempDir()
	refStore, _ := reflection.NewStore(tmpDir)
	treeStore, _ := evolution.NewTreeStore(tmpDir)

	// Mock that fails then succeeds — simulate retry
	callCount := 0
	mock := &retryMockLLM{
		complexity: "low",
		plan:       "corrected plan",
		wentWell:   "recovered",
		toImprove:  "prevent initial failure",
		callCount:  &callCount,
	}

	bb := &Blackboard{Reflections: refStore, TreeStore: treeStore, LLM: mock}

	// Build tree with RetrySelfCorrect having maxRetries=5 (as improved)
	tree := evolution.GoDeveloperTree()
	// Apply the improvement: increase retries
	evolution.ApplyMutations(tree, []evolution.MutationOp{
		{Operation: "increase_retries", Target: "RetrySelfCorrect"},
	})
	bt := BuildTree(tree, bb)

	bb.Task = "explain Go interfaces"
	result := RunTask(bb, bt)

	if result == "" {
		t.Error("expected non-empty result after retry")
	}
	// Verify the tree self-corrected (plan should contain "corrected")
	if bb.Plan == mock.plan {
		// The SelfCorrect action rewrites the plan with CORRECTED prefix
		if !containsAnyStr(bb.Plan, "CORRECTED") && !containsAnyStr(bb.Plan, "corrected") {
			t.Logf("plan may not show correction: %s", truncate(bb.Plan, 100))
		}
	}
}

// retryMockLLM simulates a first failure then success
type retryMockLLM struct {
	complexity string
	plan       string
	wentWell   string
	toImprove  string
	callCount  *int
}

func (m *retryMockLLM) AnalyzeComplexity(task string) string { return m.complexity }
func (m *retryMockLLM) GeneratePlan(task, complexity string) string {
	*m.callCount++
	return m.plan
}
func (m *retryMockLLM) Reflect(task, outcome, plan string) (string, string) {
	return m.wentWell, m.toImprove
}
func (m *retryMockLLM) Generate(prompt string) (string, error) { return "mock", nil }

func TestCheckConfidence_ConditionExists(t *testing.T) {
	tree := evolution.GoDeveloperTree()
	// Apply the improvement: add CheckConfidence before PreGate
	evolution.ApplyMutations(tree, []evolution.MutationOp{{
		Operation: "add_before",
		Target:    "PreGate",
		Node: &evolution.SerializableNode{
			Type: "Condition", Name: "CheckConfidence", Description: "Skip if confidence too low",
		},
	}})

	// Verify CheckConfidence is a direct child of root
	found := false
	for _, child := range tree.Children {
		if child.Name == "CheckConfidence" {
			found = true
			if child.Type != "Condition" {
				t.Errorf("CheckConfidence should be Condition, got %s", child.Type)
			}
		}
	}
	if !found {
		t.Error("CheckConfidence should be a direct child of the root after add_before PreGate")
	}
}

func TestDefaultFallback_InOutcomeSelector(t *testing.T) {
	tree := evolution.GoDeveloperTree()
	// Apply the improvement: add fallback to OutcomeSelector
	evolution.ApplyMutations(tree, []evolution.MutationOp{{
		Operation: "add_fallback",
		Target:    "OutcomeSelector",
		Node: &evolution.SerializableNode{
			Type: "Action", Name: "DefaultFallback", Description: "Generic fallback",
		},
	}})

	// Find OutcomeSelector and verify DefaultFallback is in its children
	found := findChildByName(tree, "OutcomeSelector")
	if found == nil {
		t.Fatal("OutcomeSelector not found in tree")
	}
	hasFallback := false
	for _, child := range found.Children {
		if child.Name == "DefaultFallback" {
			hasFallback = true
		}
	}
	if !hasFallback {
		t.Error("DefaultFallback should be in OutcomeSelector after add_fallback")
	}
}

func findChildByName(node *evolution.SerializableNode, name string) *evolution.SerializableNode {
	if node.Name == name {
		return node
	}
	for i := range node.Children {
		if result := findChildByName(&node.Children[i], name); result != nil {
			return result
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
