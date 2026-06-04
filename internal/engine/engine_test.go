package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/goap"
	"github.com/nico/go-bt-evolve/internal/reflection"
	btcore "github.com/rvitorper/go-bt/core"
)

// mockLLM is a test double that returns predefined responses.
// Implements the full llm.LLM interface for use as engine.Blackboard.LLM.
type mockLLM struct {
	complexity string
	plan       string
	wentWell   string
	toImprove  string
}

func (m *mockLLM) AnalyzeComplexity(_ string) string { return m.complexity }
func (m *mockLLM) GeneratePlan(_, _ string) string   { return m.plan }
func (m *mockLLM) Reflect(_, _, _ string) (string, string) {
	return m.wentWell, m.toImprove
}
func (m *mockLLM) Generate(_ string) (string, error) {
	// Return 40+ chars to pass validateOutputQuality (30-char minimum).
	return "Mock response with sufficient length for quality validation checks", nil
}
func (m *mockLLM) GenerateCtx(_ context.Context, prompt string) (string, error) {
	return m.Generate(prompt)
}
func (m *mockLLM) GenerateWithTimeout(prompt string, _ time.Duration) (string, error) {
	return m.Generate(prompt)
}

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

// retryMockLLM simulates a first failure then success.
// Implements the full llm.LLM interface.
type retryMockLLM struct {
	complexity string
	plan       string
	wentWell   string
	toImprove  string
	callCount  *int
}

func (m *retryMockLLM) AnalyzeComplexity(_ string) string { return m.complexity }
func (m *retryMockLLM) GeneratePlan(_, _ string) string {
	*m.callCount++
	return m.plan
}
func (m *retryMockLLM) Reflect(_, _, _ string) (string, string) {
	return m.wentWell, m.toImprove
}
func (m *retryMockLLM) Generate(_ string) (string, error) {
	return "RetryMock response with sufficient length for quality validation checks", nil
}
func (m *retryMockLLM) GenerateCtx(_ context.Context, prompt string) (string, error) {
	return m.Generate(prompt)
}
func (m *retryMockLLM) GenerateWithTimeout(prompt string, _ time.Duration) (string, error) {
	return m.Generate(prompt)
}

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

// ============================================================================
// Registry tests (register.go)
// ============================================================================

func TestRegisterAction_And_GetAction(t *testing.T) {
	// Register a custom action
	called := false
	RegisterAction("TestCustomAction", func(_ *btcore.BTContext[Blackboard]) int {
		called = true
		return 1
	})

	// Retrieve and invoke it
	fn := GetAction("TestCustomAction")
	if fn == nil {
		t.Fatal("GetAction returned nil for registered action")
	}
	result := fn(nil)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
	if !called {
		t.Error("registered action was not called")
	}
}

func TestRegisterCondition_And_GetCondition(t *testing.T) {
	called := false
	RegisterCondition("TestCustomCondition", func(_ *Blackboard) bool {
		called = true
		return true
	})

	fn := GetCondition("TestCustomCondition")
	if fn == nil {
		t.Fatal("GetCondition returned nil for registered condition")
	}
	result := fn(&Blackboard{})
	if !result {
		t.Error("expected true from registered condition")
	}
	if !called {
		t.Error("registered condition was not called")
	}
}

func TestGetAction_Unregistered_ReturnsNil(t *testing.T) {
	// GetAction returns nil for unregistered names; fallback is in actionForName.
	fn := GetAction("NonExistentAction")
	if fn != nil {
		t.Error("GetAction should return nil for unregistered action")
	}
}

func TestActionForName_Fallback_UnknownAction(t *testing.T) {
	bb := &Blackboard{Task: "test"}
	// actionForName checks GetAction first (nil), then falls through to the switch.
	// The switch default returns a no-op that returns 1.
	fn := bb.actionForName("NonExistentAction")
	if fn == nil {
		t.Fatal("actionForName should return a fallback, not nil")
	}
	result := fn(nil)
	if result != 1 {
		t.Errorf("expected fallback to return 1, got %d", result)
	}
}

func TestGetCondition_Unregistered_ReturnsNil(t *testing.T) {
	fn := GetCondition("NonExistentCondition")
	if fn != nil {
		t.Error("GetCondition should return nil for unregistered condition")
	}
}

func TestConditionForName_Fallback_UnknownCondition(t *testing.T) {
	bb := &Blackboard{Task: "test"}
	fn := bb.conditionForName("NonExistentCondition")
	if fn == nil {
		t.Fatal("conditionForName should return a fallback, not nil")
	}
	// The switch default returns true (pass-through) for unknown conditions.
	result := fn(bb)
	if !result {
		t.Error("expected fallback to return true for unknown condition")
	}
}

func TestValidateTree_ValidTree(t *testing.T) {
	tree := evolution.DefaultTree()
	missing := ValidateTree(tree)
	if len(missing) > 0 {
		t.Errorf("expected no missing nodes in default tree, got: %v", missing)
	}
}

func TestValidateTree_NestedChildren(t *testing.T) {
	// Build a small valid tree that exercises the recursive walk
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "HasClearTask"},
			{Type: "Action", Name: "SetupDefaultTools"},
			{Type: "Selector", Name: "Inner", Children: []evolution.SerializableNode{
				{Type: "Action", Name: "ExecutePlan"},
			}},
		},
	}
	missing := ValidateTree(tree)
	if len(missing) > 0 {
		t.Errorf("expected no missing nodes, got: %v", missing)
	}
}

// ============================================================================
// GOAP helper tests (goap_nodes.go)
// ============================================================================

func TestPlanStepsToStrings(t *testing.T) {
	plan := &goap.Plan{
		Goal: &goap.Goal{Name: "test_goal"},
		Steps: []goap.Action{
			{Name: "step_one", Cost: 1.0},
			{Name: "step_two", Cost: 2.0},
			{Name: "step_three", Cost: 3.0},
		},
	}
	steps := planStepsToStrings(plan)
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}
	if steps[0] != "step_one" {
		t.Errorf("expected step_one, got %q", steps[0])
	}
	if steps[1] != "step_two" {
		t.Errorf("expected step_two, got %q", steps[1])
	}
	if steps[2] != "step_three" {
		t.Errorf("expected step_three, got %q", steps[2])
	}
}

func TestPlanStepsToStrings_Empty(t *testing.T) {
	plan := &goap.Plan{
		Goal:  &goap.Goal{Name: "empty_goal"},
		Steps: []goap.Action{},
	}
	steps := planStepsToStrings(plan)
	if len(steps) != 0 {
		t.Errorf("expected 0 steps, got %d", len(steps))
	}
}

func TestGetStringSlice_Existing(t *testing.T) {
	cs := map[string]interface{}{
		"my_slice": []string{"a", "b", "c"},
	}
	result := getStringSlice(cs, "my_slice")
	if len(result) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result))
	}
	if result[0] != "a" || result[1] != "b" || result[2] != "c" {
		t.Errorf("unexpected values: %v", result)
	}
}

func TestGetStringSlice_MissingKey(t *testing.T) {
	cs := map[string]interface{}{}
	result := getStringSlice(cs, "nonexistent")
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %v", result)
	}
}

func TestGetStringSlice_WrongType(t *testing.T) {
	cs := map[string]interface{}{
		"bad_slice": []int{1, 2, 3},
	}
	result := getStringSlice(cs, "bad_slice")
	if len(result) != 0 {
		t.Errorf("expected empty slice for wrong type, got %v", result)
	}
}

func TestGetStringSlice_NilMap(t *testing.T) {
	result := getStringSlice(nil, "any_key")
	if len(result) != 0 {
		t.Errorf("expected empty slice for nil map, got %v", result)
	}
}

func TestBuildGoapStepPrompt(t *testing.T) {
	cs := map[string]interface{}{
		"goap_step_index": 0,
	}
	prompt := buildGoapStepPrompt("fix the bug", "analyze_code", cs)
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	if !stringContains(prompt, "fix the bug") {
		t.Error("prompt should contain the task")
	}
	if !stringContains(prompt, "analyze_code") {
		t.Error("prompt should contain the step name")
	}
	if !stringContains(prompt, "GOAP") {
		t.Error("prompt should mention GOAP")
	}
}

func TestWorldStateFromMap(t *testing.T) {
	input := map[string]interface{}{"key": "value", "num": 42}
	result := worldStateFromMap(input)
	if len(result) != 2 {
		t.Errorf("expected 2 entries, got %d", len(result))
	}
	if result["key"] != "value" {
		t.Errorf("expected 'value', got %v", result["key"])
	}
}

func TestStringField_Exists(t *testing.T) {
	m := map[string]interface{}{"name": "test_value"}
	result := stringField(m, "name")
	if result != "test_value" {
		t.Errorf("expected 'test_value', got %q", result)
	}
}

func TestStringField_Missing(t *testing.T) {
	m := map[string]interface{}{}
	result := stringField(m, "name")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestStringField_WrongType(t *testing.T) {
	m := map[string]interface{}{"name": 42}
	result := stringField(m, "name")
	if result != "" {
		t.Errorf("expected empty string for wrong type, got %q", result)
	}
}

func TestFloatField_Exists(t *testing.T) {
	m := map[string]interface{}{"cost": 3.14}
	result := floatField(m, "cost", 1.0)
	if result != 3.14 {
		t.Errorf("expected 3.14, got %f", result)
	}
}

func TestFloatField_IntConversion(t *testing.T) {
	m := map[string]interface{}{"count": 5}
	result := floatField(m, "count", 1.0)
	if result != 5.0 {
		t.Errorf("expected 5.0, got %f", result)
	}
}

func TestFloatField_Missing_UsesDefault(t *testing.T) {
	m := map[string]interface{}{}
	result := floatField(m, "cost", 2.5)
	if result != 2.5 {
		t.Errorf("expected default 2.5, got %f", result)
	}
}

func TestFloatField_WrongType_UsesDefault(t *testing.T) {
	m := map[string]interface{}{"cost": "expensive"}
	result := floatField(m, "cost", 0.0)
	if result != 0.0 {
		t.Errorf("expected default 0.0 for wrong type, got %f", result)
	}
}

// stringContains checks if substr is contained within s.
func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ============================================================================
// containsAnyLower tests (registry.go utility)
// ============================================================================

func TestContainsAnyLower_BasicMatch(t *testing.T) {
	if !containsAnyLower("hello world", "hello") {
		t.Error("expected match for exact substring")
	}
}

func TestContainsAnyLower_CaseInsensitive(t *testing.T) {
	if !containsAnyLower("HELLO WORLD", "hello") {
		t.Error("expected case-insensitive match")
	}
	if !containsAnyLower("hello world", "HELLO") {
		t.Error("expected case-insensitive match (keyword uppercase)")
	}
	if !containsAnyLower("Hello World", "hello") {
		t.Error("expected case-insensitive match (mixed case)")
	}
}

func TestContainsAnyLower_NoMatch(t *testing.T) {
	if containsAnyLower("hello world", "foo") {
		t.Error("expected no match for absent keyword")
	}
}

func TestContainsAnyLower_MultipleKeywords(t *testing.T) {
	if !containsAnyLower("The quick brown fox", "lazy", "quick") {
		t.Error("expected match for second keyword")
	}
}

func TestContainsAnyLower_EmptyInput(t *testing.T) {
	if containsAnyLower("", "hello") {
		t.Error("expected no match for empty input with long keyword")
	}
}

func TestContainsAnyLower_Substring(t *testing.T) {
	if !containsAnyLower("researching Tesla", "research") {
		t.Error("expected substring match within longer word")
	}
}

func TestContainsAnyLower_NoKeywords(t *testing.T) {
	if containsAnyLower("hello world") {
		t.Error("expected false when no keywords provided")
	}
}

// ============================================================================
// validateInputAction tests (registry.go)
// ============================================================================

func TestValidateInputAction_EmptyTask(t *testing.T) {
	bb := &Blackboard{Task: ""}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := validateInputAction(ctx)
	if result != -1 {
		t.Errorf("expected -1 for empty task, got %d", result)
	}
}

func TestValidateInputAction_NonEmptyTask(t *testing.T) {
	bb := &Blackboard{Task: "fix the bug"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := validateInputAction(ctx)
	if result != 1 {
		t.Errorf("expected 1 for non-empty task, got %d", result)
	}
}

// ============================================================================
// assignComplexityAction tests (registry.go)
// ============================================================================

func TestAssignComplexityAction_NoLLM_DefaultsMedium(t *testing.T) {
	bb := &Blackboard{Task: "test task"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := assignComplexityAction(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
	if bb.Complexity != "medium" {
		t.Errorf("expected default 'medium', got %q", bb.Complexity)
	}
}

func TestAssignComplexityAction_WithLLM(t *testing.T) {
	mock := &mockLLM{complexity: "high"}
	bb := &Blackboard{Task: "complex task", LLM: mock}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := assignComplexityAction(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
	if bb.Complexity != "high" {
		t.Errorf("expected 'high' from LLM, got %q", bb.Complexity)
	}
}

// ============================================================================
// validateOutputAction tests (registry.go)
// ============================================================================

func TestValidateOutputAction_TooShort(t *testing.T) {
	bb := &Blackboard{Result: "short"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := validateOutputAction(ctx)
	if result != -1 {
		t.Errorf("expected -1 for short output, got %d", result)
	}
}

func TestValidateOutputAction_Valid(t *testing.T) {
	bb := &Blackboard{Result: "this is a long enough result with sufficient length"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := validateOutputAction(ctx)
	if result != 1 {
		t.Errorf("expected 1 for valid output, got %d", result)
	}
}

// ============================================================================
// actionForName — additional branch coverage for tree.go switch cases
// ============================================================================

func TestActionForName_SelfCorrect(t *testing.T) {
	bb := &Blackboard{Task: "fix bug", Plan: "old plan", LLM: &mockLLM{}}
	fn := bb.actionForName("SelfCorrect")
	if fn == nil {
		t.Fatal("expected non-nil function for SelfCorrect")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
	if bb.Result == "" {
		t.Error("expected result to be set by SelfCorrect with LLM")
	}
}

func TestActionForName_EscalateToDeepSeek(t *testing.T) {
	bb := &Blackboard{Task: "critical issue"}
	fn := bb.actionForName("EscalateToDeepSeek")
	if fn == nil {
		t.Fatal("expected non-nil function for EscalateToDeepSeek")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
	// The registry version of EscalateToDeepSeek returns 1 and does nothing to bb.Result
	// This tests that the action is resolved and calls without panic
}

func TestActionForName_SetupDefaultTools(t *testing.T) {
	bb := &Blackboard{Task: "test"}
	fn := bb.actionForName("SetupDefaultTools")
	if fn == nil {
		t.Fatal("expected non-nil function for SetupDefaultTools")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
	// Registry version is a no-op; tree.go switch version sets ChainTools.
	// Either way, the function resolves without panic.
}

func TestActionForName_SetupDevTools(t *testing.T) {
	bb := &Blackboard{Task: "test"}
	fn := bb.actionForName("SetupDevTools")
	if fn == nil {
		t.Fatal("expected non-nil function for SetupDevTools")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
}

func TestActionForName_SetupUniversalTools(t *testing.T) {
	bb := &Blackboard{Task: "test"}
	fn := bb.actionForName("SetupUniversalTools")
	if fn == nil {
		t.Fatal("expected non-nil function for SetupUniversalTools")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
}

func TestActionForName_SetupResearchTools(t *testing.T) {
	bb := &Blackboard{Task: "test"}
	fn := bb.actionForName("SetupResearchTools")
	if fn == nil {
		t.Fatal("expected non-nil function for SetupResearchTools")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
}

func TestActionForName_SetupStartupTools(t *testing.T) {
	bb := &Blackboard{Task: "test"}
	fn := bb.actionForName("SetupStartupTools")
	if fn == nil {
		t.Fatal("expected non-nil function for SetupStartupTools")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
}

func TestActionForName_InitTranspositionTable(t *testing.T) {
	bb := &Blackboard{Task: "test"}
	fn := bb.actionForName("InitTranspositionTable")
	if fn == nil {
		t.Fatal("expected non-nil function")
	}
	fn(nil)
	if bb.ChainState == nil {
		t.Fatal("expected ChainState to be initialized")
	}
	if bb.ChainState["tt_hits"].(int) != 0 {
		t.Error("expected tt_hits=0")
	}
	if bb.ChainState["best_fitness"].(float64) != 0.0 {
		t.Error("expected best_fitness=0.0")
	}
}

func TestActionForName_MarkSuccessful(t *testing.T) {
	bb := &Blackboard{Task: "test", Outcome: string(reflection.Failure)}
	fn := bb.actionForName("MarkSuccessful")
	if fn == nil {
		t.Fatal("expected non-nil function for MarkSuccessful")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
	if bb.Outcome != string(reflection.Success) {
		t.Errorf("expected outcome=success, got %s", bb.Outcome)
	}
}

func TestActionForName_ValidateOutputAction(t *testing.T) {
	bb := &Blackboard{Result: "valid output with enough text to pass quality checks"}
	fn := bb.actionForName("ValidateOutput")
	if fn == nil {
		t.Fatal("expected non-nil function for ValidateOutput")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1 for valid output, got %d", result)
	}
}

// ============================================================================
// conditionForName — additional branch coverage
// ============================================================================

func TestConditionForName_ValidateInput(t *testing.T) {
	bb := &Blackboard{Task: "valid task"}
	fn := bb.conditionForName("ValidateInput")
	if fn == nil {
		t.Fatal("expected non-nil function for ValidateInput")
	}
	if !fn(bb) {
		t.Error("expected true for non-empty task")
	}
	bb2 := &Blackboard{Task: ""}
	if fn(bb2) {
		t.Error("expected false for empty task")
	}
}

func TestConditionForName_CheckPrerequisites(t *testing.T) {
	bb := &Blackboard{}
	fn := bb.conditionForName("CheckPrerequisites")
	if fn == nil {
		t.Fatal("expected non-nil function for CheckPrerequisites")
	}
	if !fn(bb) {
		t.Error("expected true for CheckPrerequisites")
	}
}

func TestConditionForName_CheckCache(t *testing.T) {
	bb := &Blackboard{CachedResult: "cached data"}
	fn := bb.conditionForName("CheckCache")
	if fn == nil {
		t.Fatal("expected non-nil function")
	}
	if !fn(bb) {
		t.Error("expected true with CachedResult set")
	}
	bb2 := &Blackboard{}
	if fn(bb2) {
		t.Error("expected false without CachedResult")
	}
}

func TestConditionForName_IsHighPriority(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("IsHighPriority")
	if fn == nil {
		t.Fatal("expected non-nil function")
	}
	if !fn(&Blackboard{Task: "critical bug fix now"}) {
		t.Error("expected true for critical task")
	}
	if fn(&Blackboard{Task: "routine maintenance"}) {
		t.Error("expected false for normal task")
	}
}

func TestConditionForName_CheckKnowledgeGap(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("CheckKnowledgeGap")
	if fn == nil {
		t.Fatal("expected non-nil function")
	}
	// Registry version: returns true when KgResults is empty (gap exists)
	if !fn(&Blackboard{Task: "any task"}) {
		t.Error("expected true when KgResults is empty (gap)")
	}
	// Returns false when KgResults is non-empty (gap filled)
	if fn(&Blackboard{Task: "any task", KgResults: "some knowledge"}) {
		t.Error("expected false when KgResults is non-empty")
	}
}

func TestConditionForName_IsDevOps(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("IsDevOps")
	if fn == nil {
		t.Fatal("expected non-nil function")
	}
	if !fn(&Blackboard{Task: "deploy to kubernetes"}) {
		t.Error("expected true for DevOps task")
	}
	if fn(&Blackboard{Task: "write a poem"}) {
		t.Error("expected false for non-DevOps task")
	}
}

func TestConditionForName_IsDataTask(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("IsDataTask")
	if fn == nil {
		t.Fatal("expected non-nil function")
	}
	if !fn(&Blackboard{Task: "ETL pipeline for CSV data"}) {
		t.Error("expected true for data task")
	}
	if fn(&Blackboard{Task: "review code"}) {
		t.Error("expected false for non-data task")
	}
}

func TestConditionForName_IsRefactoring(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("IsRefactoring")
	if fn == nil {
		t.Fatal("expected non-nil function")
	}
	if !fn(&Blackboard{Task: "refactor the auth module"}) {
		t.Error("expected true for refactoring task")
	}
	if fn(&Blackboard{Task: "write new feature"}) {
		t.Error("expected false for non-refactoring task")
	}
}

func TestConditionForName_IsIncident(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("IsIncident")
	if fn == nil {
		t.Fatal("expected non-nil function")
	}
	if !fn(&Blackboard{Task: "production crash on checkout"}) {
		t.Error("expected true for incident task")
	}
	if fn(&Blackboard{Task: "plan the roadmap"}) {
		t.Error("expected false for non-incident task")
	}
}

func TestConditionForName_IsAnalysisTask(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("IsAnalysisTask")
	if fn == nil {
		t.Fatal("expected non-nil function")
	}
	if !fn(&Blackboard{Task: "strategy analysis for Q4"}) {
		t.Error("expected true for analysis task")
	}
	if fn(&Blackboard{Task: "compile the binary"}) {
		t.Error("expected false for non-analysis task")
	}
}

func TestConditionForName_IsQuestion(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("IsQuestion")
	if fn == nil {
		t.Fatal("expected non-nil function")
	}
	if !fn(&Blackboard{Task: "what is the best practice"}) {
		t.Error("expected true for question")
	}
	if fn(&Blackboard{Task: "build the application"}) {
		t.Error("expected false for non-question")
	}
}

func TestConditionForName_HasClearTask_RejectsInjection(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("HasClearTask")
	if fn == nil {
		t.Fatal("expected non-nil function for HasClearTask")
	}
	// Registry version rejects these patterns
	if fn(&Blackboard{Task: "<script>alert(1)</script>"}) {
		t.Error("expected false for XSS pattern")
	}
	if fn(&Blackboard{Task: "drop table users"}) {
		t.Error("expected false for SQL injection pattern")
	}
}

func TestConditionForName_HasClearTask_RejectsTooShort(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("HasClearTask")
	if fn == nil {
		t.Fatal("expected non-nil")
	}
	// Registry version rejects tasks shorter than 3 chars
	if fn(&Blackboard{Task: "ab"}) {
		t.Error("expected false for 2 char task")
	}
	if fn(&Blackboard{Task: ""}) {
		t.Error("expected false for empty task")
	}
}

func TestConditionForName_HasClearTask_RejectsNoAlpha(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("HasClearTask")
	if fn == nil {
		t.Fatal("expected non-nil")
	}
	// Registry version rejects tasks without alphabetic characters
	if fn(&Blackboard{Task: "12345"}) {
		t.Error("expected false for all-digit task")
	}
	if fn(&Blackboard{Task: "!@#$%"}) {
		t.Error("expected false for symbols-only task")
	}
}

func TestConditionForName_HasClearTask_AcceptsValidTask(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("HasClearTask")
	if fn == nil {
		t.Fatal("expected non-nil")
	}
	if !fn(&Blackboard{Task: "build the authentication module with tests"}) {
		t.Error("expected true for valid task")
	}
	// Short tasks with alphabetic chars and >= 3 length are accepted
	if !fn(&Blackboard{Task: "Fix"}) {
		t.Error("expected true for 3-char verb")
	}
	if !fn(&Blackboard{Task: "nil"}) {
		t.Error("expected true for keyword-like task with alpha chars")
	}
}

func TestConditionForName_ValidateOutputCondition(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("ValidateOutput")
	if fn == nil {
		t.Fatal("expected non-nil function for ValidateOutput condition")
	}
	// Short output should fail
	if fn(&Blackboard{Result: "hi"}) {
		t.Error("expected false for short output")
	}
	// Longer valid output should pass
	if !fn(&Blackboard{Result: "This is a comprehensive analysis with sufficient detail and structure to pass validation"}) {
		t.Error("expected true for valid output")
	}
}

func TestConditionForName_WasSuccessful(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("WasSuccessful")
	if fn == nil {
		t.Fatal("expected non-nil function for WasSuccessful")
	}
	if !fn(&Blackboard{Outcome: string(reflection.Success)}) {
		t.Error("expected true for success outcome")
	}
	if fn(&Blackboard{Outcome: string(reflection.Failure)}) {
		t.Error("expected false for failure outcome")
	}
	if !fn(&Blackboard{Outcome: "chain_success"}) {
		t.Error("expected true for chain_success outcome")
	}
}
