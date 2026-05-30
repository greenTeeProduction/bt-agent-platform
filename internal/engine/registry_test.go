package engine

import (
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
	"github.com/nico/go-bt-evolve/internal/reflection"
)

// ─── Engine Tests ────────────────────────────────────────────────────────────

func TestNewEngine(t *testing.T) {
	e := NewEngine()
	if e == nil {
		t.Fatal("NewEngine returned nil")
	}
	if e.Actions == nil {
		t.Error("Actions map should not be nil")
	}
	if e.Conditions == nil {
		t.Error("Conditions map should not be nil")
	}
	// The engine should have at least the actions registered in init()
	if len(e.Actions) < 5 {
		t.Errorf("expected at least 5 actions, got %d", len(e.Actions))
	}
	// Should have conditions too
	if len(e.Conditions) < 3 {
		t.Errorf("expected at least 3 conditions, got %d", len(e.Conditions))
	}
}

func TestEngine_GetAction(t *testing.T) {
	e := NewEngine()

	// Known action (registered in init())
	fn := e.GetAction("ValidateInput")
	if fn == nil {
		t.Error("GetAction(ValidateInput) should return a function")
	}

	// Unknown action
	fn = e.GetAction("NonExistentActionXYZ")
	if fn != nil {
		t.Error("GetAction(NonExistentActionXYZ) should return nil")
	}
}

func TestEngine_GetCondition(t *testing.T) {
	e := NewEngine()

	// Known condition (registered in init())
	fn := e.GetCondition("HasClearTask")
	if fn == nil {
		t.Error("GetCondition(HasClearTask) should return a function")
	}

	// Unknown condition
	fn = e.GetCondition("NonExistentConditionXYZ")
	if fn != nil {
		t.Error("GetCondition(NonExistentConditionXYZ) should return nil")
	}
}

// ─── RegisterProviders Tests ─────────────────────────────────────────────

type testActionProvider struct {
	called bool
}

func (p *testActionProvider) RegisterActions() {
	p.called = true
}

type testConditionProvider struct {
	called bool
}

func (p *testConditionProvider) RegisterConditions() {
	p.called = true
}

type testBothProvider struct {
	testActionProvider
	testConditionProvider
}

func TestRegisterProviders_Actions(t *testing.T) {
	ap := &testActionProvider{}
	RegisterProviders(ap)
	if !ap.called {
		t.Error("ActionProvider.RegisterActions should have been called")
	}
}

func TestRegisterProviders_Conditions(t *testing.T) {
	cp := &testConditionProvider{}
	RegisterProviders(cp)
	if !cp.called {
		t.Error("ConditionProvider.RegisterConditions should have been called")
	}
}

func TestRegisterProviders_Both(t *testing.T) {
	bp := &testBothProvider{}
	RegisterProviders(bp)
	if !bp.testActionProvider.called {
		t.Error("BothProvider.RegisterActions should have been called")
	}
	if !bp.testConditionProvider.called {
		t.Error("BothProvider.RegisterConditions should have been called")
	}
}

func TestRegisterProviders_Empty(t *testing.T) {
	// Should not panic with no providers
	RegisterProviders()
}

func TestRegisterProviders_Nil(t *testing.T) {
	// Should not panic with nil interface
	RegisterProviders(nil)
}

// ─── Action Implementation Tests ────────────────────────────────────────

func TestGeneratePlanAction_NilLLM(t *testing.T) {
	bb := &Blackboard{Task: "test task", Plan: "old plan"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := generatePlanAction(ctx)
	if result != 1 {
		t.Errorf("generatePlanAction with nil LLM should return 1, got %d", result)
	}
	// Plan should not be overwritten when LLM is nil
	if bb.Plan != "old plan" {
		t.Errorf("Plan should be unchanged with nil LLM, got %q", bb.Plan)
	}
}

func TestGeneratePlanAction_WithMockLLM(t *testing.T) {
	bb := &Blackboard{
		Task:       "test task",
		Complexity: "medium",
		LLM:        &mockLLM{plan: "generated plan"},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := generatePlanAction(ctx)
	if result != 1 {
		t.Errorf("generatePlanAction with mock LLM should return 1, got %d", result)
	}
	if bb.Plan != "generated plan" {
		t.Errorf("Plan should be set by LLM, got %q", bb.Plan)
	}
}

func TestExecLLMCallAction_NilLLM(t *testing.T) {
	bb := &Blackboard{Task: "test task"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := execLLMCallAction(ctx)
	if result != -1 {
		t.Errorf("execLLMCallAction with nil LLM should return -1, got %d", result)
	}
}

func TestExecLLMCallAction_WithMockLLM(t *testing.T) {
	bb := &Blackboard{Task: "test task", LLM: &mockLLM{}}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := execLLMCallAction(ctx)
	if result != 1 {
		t.Errorf("execLLMCallAction with mock LLM should return 1, got %d", result)
	}
	if bb.Result == "" {
		t.Error("Result should be set by LLM")
	}
}

func TestExecRefineAction_NilLLM(t *testing.T) {
	bb := &Blackboard{Result: "some output"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := execRefineAction(ctx)
	if result != -1 {
		t.Errorf("execRefineAction with nil LLM should return -1, got %d", result)
	}
}

func TestExecRefineAction_EmptyResult(t *testing.T) {
	bb := &Blackboard{Result: "", LLM: &mockLLM{}}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := execRefineAction(ctx)
	if result != -1 {
		t.Errorf("execRefineAction with empty Result should return -1, got %d", result)
	}
}

func TestExecRefineAction_WithMockLLM(t *testing.T) {
	bb := &Blackboard{Result: "original output", LLM: &mockLLM{}}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := execRefineAction(ctx)
	if result != 1 {
		t.Errorf("execRefineAction with mock LLM should return 1, got %d", result)
	}
	// Result should be replaced by mock output
	if bb.Result == "original output" {
		t.Error("Result should be overwritten by LLM refine")
	}
}

// ─── Knowledge + Cache Flow Tests ────────────────────────────────────────

func TestKnowledgeQueryAction(t *testing.T) {
	bb := &Blackboard{Task: "find relevant papers"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := knowledgeQueryAction(ctx)
	if result != 1 {
		t.Errorf("knowledgeQueryAction should return 1, got %d", result)
	}
	if bb.KgResults == "" {
		t.Error("KgResults should be set")
	}
	if !strContains(bb.KgResults, "KG:") {
		t.Errorf("KgResults should contain 'KG:' prefix, got %q", bb.KgResults)
	}
}

func TestCacheCheckAction_WithCachedData(t *testing.T) {
	bb := &Blackboard{KgResults: "cached research data"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cacheCheckAction(ctx)
	if result != 1 {
		t.Errorf("cacheCheckAction with cached data should return 1, got %d", result)
	}
	if bb.CachedResult != "cached research data" {
		t.Errorf("CachedResult should be set from KgResults, got %q", bb.CachedResult)
	}
}

func TestCacheCheckAction_NoCachedData(t *testing.T) {
	bb := &Blackboard{KgResults: "KG: find papers — no cached results"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cacheCheckAction(ctx)
	if result != -1 {
		t.Errorf("cacheCheckAction with 'no cached' flag should return -1, got %d", result)
	}
}

func TestCacheCheckAction_Empty(t *testing.T) {
	bb := &Blackboard{KgResults: ""}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cacheCheckAction(ctx)
	if result != -1 {
		t.Errorf("cacheCheckAction with empty KgResults should return -1, got %d", result)
	}
}

func TestCacheResultAction_WithResult(t *testing.T) {
	bb := &Blackboard{Result: "computed output"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cacheResultAction(ctx)
	if result != 1 {
		t.Errorf("cacheResultAction should return 1, got %d", result)
	}
	if bb.CachedResult != "computed output" {
		t.Errorf("CachedResult should be set from Result, got %q", bb.CachedResult)
	}
}

func TestCacheResultAction_EmptyResult(t *testing.T) {
	bb := &Blackboard{Result: "", CachedResult: "previous"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cacheResultAction(ctx)
	if result != 1 {
		t.Errorf("cacheResultAction should return 1, got %d", result)
	}
	// CachedResult should not be overwritten with empty string
	if bb.CachedResult != "previous" {
		t.Errorf("CachedResult should be unchanged, got %q", bb.CachedResult)
	}
}

// ─── Cache Flow Integration Tests ───────────────────────────────────────

func TestCacheFlow_KnowledgeToCache(t *testing.T) {
	bb := &Blackboard{Task: "search"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}

	// Step 1: Query knowledge
	if knowledgeQueryAction(ctx) != 1 {
		t.Fatal("knowledge query failed")
	}

	// Step 2: Check cache — should not hit (has "no cached" flag)
	if cacheCheckAction(ctx) != -1 {
		t.Error("cache check should miss after knowledge query")
	}

	// Step 3: Set result and cache it
	bb.Result = "final answer"
	if cacheResultAction(ctx) != 1 {
		t.Error("cache result failed")
	}
	if bb.CachedResult != "final answer" {
		t.Errorf("expected 'final answer' in cache, got %q", bb.CachedResult)
	}
}

func TestCacheFlow_PreCachedData(t *testing.T) {
	bb := &Blackboard{
		Task:      "search",
		KgResults: "pre-loaded research data from knowledge graph",
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}

	// Cache check should succeed — data is already loaded
	if cacheCheckAction(ctx) != 1 {
		t.Error("cache check should hit on pre-loaded data")
	}
	if bb.CachedResult != "pre-loaded research data from knowledge graph" {
		t.Errorf("CachedResult mismatch: %q", bb.CachedResult)
	}
}

// ─── Tool Stub Tests ─────────────────────────────────────────────────────

func TestToolStub_Name(t *testing.T) {
	ts := toolStub{name: "web_search", desc: "search the web"}
	if ts.Name() != "web_search" {
		t.Errorf("Name() = %q, want %q", ts.Name(), "web_search")
	}
}

func TestToolStub_Description(t *testing.T) {
	ts := toolStub{name: "web_search", desc: "search the web"}
	if ts.Description() != "search the web" {
		t.Errorf("Description() = %q, want %q", ts.Description(), "search the web")
	}
}

func TestToolStub_Call(t *testing.T) {
	ts := toolStub{name: "web_search", desc: "search the web"}
	result := ts.Call("test query")
	if result != "" {
		t.Errorf("Call() = %q, want empty string", result)
	}
}

// ─── Package-Level Accessors ────────────────────────────────────────────

func TestGetAction_PackageLevel(t *testing.T) {
	fn := GetAction("ValidateInput")
	if fn == nil {
		t.Error("GetAction(ValidateInput) should return a function")
	}

	fn = GetAction("NonExistentActionXYZ")
	if fn != nil {
		t.Error("GetAction(NonExistentActionXYZ) should return nil")
	}
}

func TestGetCondition_PackageLevel(t *testing.T) {
	fn := GetCondition("HasClearTask")
	if fn == nil {
		t.Error("GetCondition(HasClearTask) should return a function")
	}

	fn = GetCondition("NonExistentConditionXYZ")
	if fn != nil {
		t.Error("GetCondition(NonExistentConditionXYZ) should return nil")
	}
}

// ─── Engine Registry Independence ───────────────────────────────────────

func TestEngine_Independence(t *testing.T) {
	// Two engines should have independent copies of the registry
	e1 := NewEngine()
	e2 := NewEngine()

	// They should have the same initial state
	if len(e1.Actions) != len(e2.Actions) {
		t.Errorf("engine action counts differ: %d vs %d", len(e1.Actions), len(e2.Actions))
	}

	// Modifying one engine should not affect the other
	e1.Actions["custom_test_action"] = func(ctx *btcore.BTContext[Blackboard]) int { return 1 }
	if _, exists := e2.Actions["custom_test_action"]; exists {
		t.Error("modifying e1 should not affect e2")
	}
}

// ─── Reflection + Outcome Integration ────────────────────────────────────

func TestReflectOnOutcomeAction_NilLLM(t *testing.T) {
	tmpDir := t.TempDir()
	refStore, _ := reflection.NewStore(tmpDir)
	bb := &Blackboard{
		Task:        "test task",
		Outcome:     "success",
		Plan:        "test plan",
		Result:      "valid output with enough content to pass quality validation",
		Reflections: refStore,
		DurationMs:  1234,
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := reflectOnOutcomeAction(ctx)
	// With nil LLM, should still return 1 (no-op)
	if result != 1 {
		t.Errorf("reflectOnOutcomeAction with nil LLM should return 1, got %d", result)
	}
}

func TestReflectOnOutcomeAction_WithMockLLM(t *testing.T) {
	tmpDir := t.TempDir()
	refStore, _ := reflection.NewStore(tmpDir)
	mock := &mockLLM{wentWell: "good analysis", toImprove: "add more detail"}
	bb := &Blackboard{
		Task:        "test task",
		Outcome:     "success",
		Plan:        "test plan",
		Result:      "valid output with enough content to pass quality validation checks",
		LLM:         mock,
		Reflections: refStore,
		DurationMs:  2500,
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := reflectOnOutcomeAction(ctx)
	if result != 1 {
		t.Errorf("reflectOnOutcomeAction with mock LLM should return 1, got %d", result)
	}
	// A reflection record should have been saved
	records, err := refStore.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(records) == 0 {
		t.Error("reflectOnOutcomeAction should save a reflection record")
	}
}

// ─── UpdateBehaviorTreeAction ────────────────────────────────────────────

func TestUpdateBehaviorTreeAction(t *testing.T) {
	bb := &Blackboard{}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := updateBehaviorTreeAction(ctx)
	if result != 1 {
		t.Errorf("updateBehaviorTreeAction should return 1, got %d", result)
	}
}

// ─── Edge case: empty task for knowledge query ───────────────────────────

func TestKnowledgeQueryAction_EmptyTask(t *testing.T) {
	bb := &Blackboard{Task: ""}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := knowledgeQueryAction(ctx)
	if result != 1 {
		t.Errorf("knowledgeQueryAction should return 1 even with empty task, got %d", result)
	}
	// Should still set KgResults with empty task in string
	if bb.KgResults == "" {
		t.Error("KgResults should be set even with empty task")
	}
}
