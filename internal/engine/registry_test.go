package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/goap"
	"github.com/nico/go-bt-evolve/internal/reflection"
	btcore "github.com/rvitorper/go-bt/core"
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

// ============================================================================
// Alert Router condition tests (registerAlertRouterNodes — 33.3% → target 100%)
// ============================================================================

func TestCondition_IsCritical(t *testing.T) {
	fn := GetCondition("IsCritical")
	if fn == nil {
		t.Fatal("IsCritical condition not registered")
	}
	if !fn(&Blackboard{Task: "critical disk error on sda1"}) {
		t.Error("should match 'critical'")
	}
	if !fn(&Blackboard{Task: "EMERGENCY: system down"}) {
		t.Error("should match 'emergency'")
	}
	if !fn(&Blackboard{Task: "urgent security patch needed"}) {
		t.Error("should match 'urgent'")
	}
	if !fn(&Blackboard{Task: "severe memory leak detected"}) {
		t.Error("should match 'severe'")
	}
	if fn(&Blackboard{Task: "normal system check"}) {
		t.Error("should NOT match normal task")
	}
	if fn(&Blackboard{Task: ""}) {
		t.Error("should NOT match empty task")
	}
}

func TestCondition_IsSecurity(t *testing.T) {
	fn := GetCondition("IsSecurity")
	if fn == nil {
		t.Fatal("IsSecurity condition not registered")
	}
	if !fn(&Blackboard{Task: "security breach detected"}) {
		t.Error("should match 'security'")
	}
	if !fn(&Blackboard{Task: "brute force SSH attack"}) {
		t.Error("should match 'brute' and 'ssh'")
	}
	if !fn(&Blackboard{Task: "intrusion attempt blocked"}) {
		t.Error("should match 'intrusion'")
	}
	if !fn(&Blackboard{Task: "unauthorized access detected"}) {
		t.Error("should match 'unauthorized'")
	}
	if fn(&Blackboard{Task: "normal system update"}) {
		t.Error("should NOT match normal task")
	}
}

func TestCondition_IsTrading(t *testing.T) {
	fn := GetCondition("IsTrading")
	if fn == nil {
		t.Fatal("IsTrading condition not registered")
	}
	if !fn(&Blackboard{Task: "btc trading signal detected"}) {
		t.Error("should match 'btc' and 'trading'")
	}
	if !fn(&Blackboard{Task: "market price alert"}) {
		t.Error("should match 'price' and 'market'")
	}
	if !fn(&Blackboard{Task: "high volume signal"}) {
		t.Error("should match 'volume' and 'signal'")
	}
	if fn(&Blackboard{Task: "system health check"}) {
		t.Error("should NOT match health task")
	}
}

func TestCondition_IsDiskAlert(t *testing.T) {
	fn := GetCondition("IsDiskAlert")
	if fn == nil {
		t.Fatal("IsDiskAlert condition not registered")
	}
	if !fn(&Blackboard{Task: "disk sda 95% full"}) {
		t.Error("should match 'disk' and 'sda'")
	}
	if !fn(&Blackboard{Task: "nvme storage capacity warning"}) {
		t.Error("should match 'storage' and 'nvme'")
	}
	if !fn(&Blackboard{Task: "filesystem space low"}) {
		t.Error("should match 'filesystem' and 'space'")
	}
	if fn(&Blackboard{Task: "cpu utilization high"}) {
		t.Error("should NOT match CPU task")
	}
}

func TestCondition_IsHealthAlert(t *testing.T) {
	fn := GetCondition("IsHealthAlert")
	if fn == nil {
		t.Fatal("IsHealthAlert condition not registered")
	}
	if !fn(&Blackboard{Task: "health check failed"}) {
		t.Error("should match 'health'")
	}
	if !fn(&Blackboard{Task: "monitor service down"}) {
		t.Error("should match 'monitor' and 'down'")
	}
	if !fn(&Blackboard{Task: "critical failure in pipeline"}) {
		t.Error("should match 'failure' (and 'critical')")
	}
	if !fn(&Blackboard{Task: "crash detected in agent"}) {
		t.Error("should match 'crash'")
	}
	if !fn(&Blackboard{Task: "endpoint unreachable"}) {
		t.Error("should match 'unreachable'")
	}
	if fn(&Blackboard{Task: "system operating normally"}) {
		t.Error("should NOT match normal task")
	}
}

// ============================================================================
// Alert Router action tests
// ============================================================================

func TestAction_RouteToAllChannels(t *testing.T) {
	fn := GetAction("RouteToAllChannels")
	if fn == nil {
		t.Fatal("RouteToAllChannels action not registered")
	}
	bb := &Blackboard{Task: "critical disk alert"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
	if bb.Result == "" {
		t.Error("Result should be set")
	}
	if !stringContains(bb.Result, "CRITICAL") {
		t.Error("result should contain CRITICAL")
	}
	if !stringContains(bb.Result, "ALL channels") {
		t.Error("result should mention ALL channels")
	}
}

func TestAction_RouteToSecurityChannel(t *testing.T) {
	fn := GetAction("RouteToSecurityChannel")
	if fn == nil {
		t.Fatal("RouteToSecurityChannel action not registered")
	}
	bb := &Blackboard{Task: "SSH brute force attack"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
	if !stringContains(bb.Result, "Security Alert") {
		t.Error("result should contain Security Alert")
	}
	if !stringContains(bb.Result, "Security team") {
		t.Error("result should mention Security team")
	}
	if !stringContains(bb.Result, "Delivered") {
		t.Error("result should mention Delivered")
	}
}

func TestAction_RouteToTradingChannel(t *testing.T) {
	fn := GetAction("RouteToTradingChannel")
	if fn == nil {
		t.Fatal("RouteToTradingChannel action not registered")
	}
	bb := &Blackboard{Task: "BTC price spike detected"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
	if !stringContains(bb.Result, "Trading Signal") {
		t.Error("result should contain Trading Signal")
	}
	if !stringContains(bb.Result, "Trading channels") {
		t.Error("result should mention Trading channels")
	}
}

func TestAction_RouteToDevOpsChannel(t *testing.T) {
	fn := GetAction("RouteToDevOpsChannel")
	if fn == nil {
		t.Fatal("RouteToDevOpsChannel action not registered")
	}
	bb := &Blackboard{Task: "disk sda1 at 95%"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
	if !stringContains(bb.Result, "Alert Routed") {
		t.Error("result should contain Alert Routed")
	}
	if !stringContains(bb.Result, "DevOps/Admin") {
		t.Error("result should mention DevOps/Admin")
	}
}

func TestAction_RouteToDefaultChannel(t *testing.T) {
	fn := GetAction("RouteToDefaultChannel")
	if fn == nil {
		t.Fatal("RouteToDefaultChannel action not registered")
	}
	bb := &Blackboard{Task: "general system notification"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
	if !stringContains(bb.Result, "Default channel") {
		t.Error("result should mention Default channel")
	}
}

// ============================================================================
// GOAP condition tests (registerGoapNodes — 37.5% → target higher)
// ============================================================================

func TestCondition_HasGoapGoal_NoChainState(t *testing.T) {
	fn := GetCondition("HasGoapGoal")
	if fn == nil {
		t.Fatal("HasGoapGoal condition not registered")
	}
	bb := &Blackboard{Task: "build and deploy a microservice"}
	// No ChainState = no goals
	if fn(bb) {
		t.Error("should return false when ChainState is nil")
	}
}

func TestCondition_HasGoapGoal_NoGoals(t *testing.T) {
	fn := GetCondition("HasGoapGoal")
	if fn == nil {
		t.Fatal("HasGoapGoal condition not registered")
	}
	bb := &Blackboard{
		Task:       "build a pipeline",
		ChainState: map[string]interface{}{},
	}
	if fn(bb) {
		t.Error("should return false when no goap_goals in ChainState")
	}
}

func TestCondition_HasGoapGoal_EmptyTask(t *testing.T) {
	fn := GetCondition("HasGoapGoal")
	if fn == nil {
		t.Fatal("HasGoapGoal condition not registered")
	}
	bb := &Blackboard{
		Task: "",
		ChainState: map[string]interface{}{
			"goap_goals": []*goap.Goal{goap.NewGoal("test", 1.0, goap.WorldState{})},
		},
	}
	if fn(bb) {
		t.Error("should return false when task is empty")
	}
}

func TestCondition_HasGoapGoal_PureQuestion(t *testing.T) {
	fn := GetCondition("HasGoapGoal")
	if fn == nil {
		t.Fatal("HasGoapGoal condition not registered")
	}
	bb := &Blackboard{
		Task: "what is a monad",
		ChainState: map[string]interface{}{
			"goap_goals": []*goap.Goal{goap.NewGoal("test", 1.0, goap.WorldState{})},
		},
	}
	if fn(bb) {
		t.Error("should reject pure knowledge questions (no action verb)")
	}
}

func TestCondition_HasGoapGoal_MultiStepTask(t *testing.T) {
	fn := GetCondition("HasGoapGoal")
	if fn == nil {
		t.Fatal("HasGoapGoal condition not registered")
	}
	bb := &Blackboard{
		Task: "first build the API, then deploy it to production",
		ChainState: map[string]interface{}{
			"goap_goals": []*goap.Goal{goap.NewGoal("task_completed", 1.0, goap.WorldState{"task_status": "completed"})},
		},
	}
	if !fn(bb) {
		t.Error("should accept multi-step task with action verb")
	}
	// Should have created goap_current_goal
	if _, ok := bb.ChainState["goap_current_goal"]; !ok {
		t.Error("should have set goap_current_goal")
	}
}

func TestCondition_HasGoapGoal_WithActionVerb(t *testing.T) {
	fn := GetCondition("HasGoapGoal")
	if fn == nil {
		t.Fatal("HasGoapGoal condition not registered")
	}
	bb := &Blackboard{
		Task: "build a real-time chat application",
		ChainState: map[string]interface{}{
			"goap_goals": []*goap.Goal{goap.NewGoal("task_completed", 1.0, goap.WorldState{"task_status": "completed"})},
		},
	}
	if !fn(bb) {
		t.Error("should accept task with action verb 'build'")
	}
}

func TestCondition_HasGoapGoal_ConfigureTask(t *testing.T) {
	fn := GetCondition("HasGoapGoal")
	if fn == nil {
		t.Fatal("HasGoapGoal condition not registered")
	}
	bb := &Blackboard{
		Task: "configure the Kubernetes cluster",
		ChainState: map[string]interface{}{
			"goap_goals": []*goap.Goal{goap.NewGoal("task_completed", 1.0, goap.WorldState{"task_status": "completed"})},
		},
	}
	if !fn(bb) {
		t.Error("should accept task with action verb 'configure'")
	}
}

func TestCondition_HasMoreGoapSteps_NoChainState(t *testing.T) {
	fn := GetCondition("HasMoreGoapSteps")
	if fn == nil {
		t.Fatal("HasMoreGoapSteps condition not registered")
	}
	bb := &Blackboard{Task: "test"}
	if fn(bb) {
		t.Error("should return false when ChainState is nil")
	}
}

func TestCondition_HasMoreGoapSteps_NoIndex(t *testing.T) {
	fn := GetCondition("HasMoreGoapSteps")
	if fn == nil {
		t.Fatal("HasMoreGoapSteps condition not registered")
	}
	bb := &Blackboard{
		Task:       "test",
		ChainState: map[string]interface{}{},
	}
	if fn(bb) {
		t.Error("should return false when goap_step_index is missing")
	}
}

func TestCondition_HasMoreGoapSteps_NoSteps(t *testing.T) {
	fn := GetCondition("HasMoreGoapSteps")
	if fn == nil {
		t.Fatal("HasMoreGoapSteps condition not registered")
	}
	bb := &Blackboard{
		Task: "test",
		ChainState: map[string]interface{}{
			"goap_step_index": 0,
		},
	}
	if fn(bb) {
		t.Error("should return false when goap_steps is missing")
	}
}

func TestCondition_HasMoreGoapSteps_HasRemaining(t *testing.T) {
	fn := GetCondition("HasMoreGoapSteps")
	if fn == nil {
		t.Fatal("HasMoreGoapSteps condition not registered")
	}
	bb := &Blackboard{
		Task: "test",
		ChainState: map[string]interface{}{
			"goap_step_index": 0,
			"goap_steps":      []string{"step1", "step2", "step3"},
		},
	}
	if !fn(bb) {
		t.Error("should return true when index < len(steps)")
	}
}

func TestCondition_HasMoreGoapSteps_AllDone(t *testing.T) {
	fn := GetCondition("HasMoreGoapSteps")
	if fn == nil {
		t.Fatal("HasMoreGoapSteps condition not registered")
	}
	bb := &Blackboard{
		Task: "test",
		ChainState: map[string]interface{}{
			"goap_step_index": 3,
			"goap_steps":      []string{"step1", "step2", "step3"},
		},
	}
	if fn(bb) {
		t.Error("should return false when index >= len(steps)")
	}
}

func TestCondition_HasMoreGoapSteps_WrongType(t *testing.T) {
	fn := GetCondition("HasMoreGoapSteps")
	if fn == nil {
		t.Fatal("HasMoreGoapSteps condition not registered")
	}
	// goap_steps is wrong type (int instead of []string)
	bb := &Blackboard{
		Task: "test",
		ChainState: map[string]interface{}{
			"goap_step_index": 0,
			"goap_steps":      42,
		},
	}
	if fn(bb) {
		t.Error("should return false when goap_steps is wrong type")
	}
}

func TestCondition_HasMoreGoapSteps_WrongIndexType(t *testing.T) {
	fn := GetCondition("HasMoreGoapSteps")
	if fn == nil {
		t.Fatal("HasMoreGoapSteps condition not registered")
	}
	bb := &Blackboard{
		Task: "test",
		ChainState: map[string]interface{}{
			"goap_step_index": "not_an_int",
			"goap_steps":      []string{"step1"},
		},
	}
	if fn(bb) {
		t.Error("should return false when goap_step_index is wrong type")
	}
}

// ============================================================================
// GOAP action tests
// ============================================================================

func TestAction_SetupGoapTools(t *testing.T) {
	fn := GetAction("SetupGoapTools")
	if fn == nil {
		t.Fatal("SetupGoapTools action not registered")
	}
	bb := &Blackboard{Task: "build a deployment pipeline"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
	if bb.ChainState == nil {
		t.Fatal("ChainState should be initialized")
	}
	if _, ok := bb.ChainState["goap_actions"]; !ok {
		t.Error("goap_actions should be set")
	}
	if _, ok := bb.ChainState["goap_goals"]; !ok {
		t.Error("goap_goals should be set")
	}
	if _, ok := bb.ChainState["goap_config"]; !ok {
		t.Error("goap_config should be set")
	}
}

func TestAction_SetupGoapTools_Idempotent(t *testing.T) {
	fn := GetAction("SetupGoapTools")
	if fn == nil {
		t.Fatal("SetupGoapTools action not registered")
	}
	bb := &Blackboard{
		Task: "build a pipeline",
		ChainState: map[string]interface{}{
			"goap_actions": "already_seeded",
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
	// Should NOT overwrite existing
	if bb.ChainState["goap_actions"] != "already_seeded" {
		t.Error("should preserve existing goap_actions (idempotent)")
	}
}

func TestAction_GoapFallback(t *testing.T) {
	fn := GetAction("GoapFallback")
	if fn == nil {
		t.Fatal("GoapFallback action not registered")
	}
	bb := &Blackboard{Task: "complex planning task"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
	if bb.Outcome != "partial" {
		t.Errorf("expected outcome 'partial', got %q", bb.Outcome)
	}
	if !stringContains(bb.Result, "falling back") {
		t.Error("result should mention falling back")
	}
}

func TestAction_ReflectGoapOutcome_Success(t *testing.T) {
	fn := GetAction("ReflectGoapOutcome")
	if fn == nil {
		t.Fatal("ReflectGoapOutcome action not registered")
	}
	bb := &Blackboard{
		Task:    "build a pipeline",
		Outcome: "success",
		ChainState: map[string]interface{}{
			"goap_plan_found": true,
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
	if bb.Outcome != "success" {
		t.Errorf("expected outcome 'success', got %q", bb.Outcome)
	}
}

func TestAction_ReflectGoapOutcome_NoPlan(t *testing.T) {
	fn := GetAction("ReflectGoapOutcome")
	if fn == nil {
		t.Fatal("ReflectGoapOutcome action not registered")
	}
	bb := &Blackboard{
		Task:       "simple task",
		Outcome:    "failure",
		ChainState: map[string]interface{}{},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
	// Should preserve original outcome when no plan found
	if bb.Outcome != "failure" {
		t.Errorf("expected outcome 'failure', got %q", bb.Outcome)
	}
}

func TestAction_ReflectGoapOutcome_NilChainState(t *testing.T) {
	fn := GetAction("ReflectGoapOutcome")
	if fn == nil {
		t.Fatal("ReflectGoapOutcome action not registered")
	}
	bb := &Blackboard{
		Task:    "task",
		Outcome: "running",
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
	// Should not panic with nil ChainState
	if bb.Outcome != "running" {
		t.Errorf("expected outcome 'running', got %q", bb.Outcome)
	}
}

// ============================================================================
// Action: ExecuteGoapStep edge cases
// ============================================================================

func TestAction_ExecuteGoapStep_NoChainState(t *testing.T) {
	fn := GetAction("ExecuteGoapStep")
	if fn == nil {
		t.Fatal("ExecuteGoapStep action not registered")
	}
	bb := &Blackboard{Task: "test"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != -1 {
		t.Errorf("expected -1 for nil ChainState, got %d", result)
	}
	if bb.Outcome != "failure" {
		t.Errorf("expected failure outcome, got %q", bb.Outcome)
	}
}

func TestAction_ExecuteGoapStep_NoIndex(t *testing.T) {
	fn := GetAction("ExecuteGoapStep")
	if fn == nil {
		t.Fatal("ExecuteGoapStep action not registered")
	}
	bb := &Blackboard{
		Task:       "test",
		ChainState: map[string]interface{}{},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != -1 {
		t.Errorf("expected -1 for missing index, got %d", result)
	}
}

func TestAction_ExecuteGoapStep_NoSteps(t *testing.T) {
	fn := GetAction("ExecuteGoapStep")
	if fn == nil {
		t.Fatal("ExecuteGoapStep action not registered")
	}
	bb := &Blackboard{
		Task: "test",
		ChainState: map[string]interface{}{
			"goap_step_index": 0,
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != -1 {
		t.Errorf("expected -1 for missing steps, got %d", result)
	}
}

func TestAction_ExecuteGoapStep_PastEnd(t *testing.T) {
	fn := GetAction("ExecuteGoapStep")
	if fn == nil {
		t.Fatal("ExecuteGoapStep action not registered")
	}
	bb := &Blackboard{
		Task: "test",
		ChainState: map[string]interface{}{
			"goap_step_index": 5,
			"goap_steps":      []string{"step1", "step2"},
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1 for past-end, got %d", result)
	}
	if bb.Outcome != "success" {
		t.Errorf("expected success outcome, got %q", bb.Outcome)
	}
	if !stringContains(bb.Result, "all GOAP steps completed") {
		t.Error("result should indicate completion")
	}
}

func TestAction_PlanGoapActions_NoActions(t *testing.T) {
	fn := GetAction("PlanGoapActions")
	if fn == nil {
		t.Fatal("PlanGoapActions action not registered")
	}
	bb := &Blackboard{
		Task:       "do something",
		ChainState: map[string]interface{}{},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != -1 {
		t.Errorf("expected -1 for no actions, got %d", result)
	}
	if bb.Outcome != "failure" {
		t.Errorf("expected failure outcome, got %q", bb.Outcome)
	}
}

func TestAction_PlanGoapActions_EmptyActions(t *testing.T) {
	fn := GetAction("PlanGoapActions")
	if fn == nil {
		t.Fatal("PlanGoapActions action not registered")
	}
	bb := &Blackboard{
		Task: "do something",
		ChainState: map[string]interface{}{
			"goap_actions": []goap.Action{},
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != -1 {
		t.Errorf("expected -1 for empty actions, got %d", result)
	}
	if !stringContains(bb.Result, "no valid actions") {
		t.Error("result should mention no valid actions")
	}
}

// ============================================================================
// containsAnyLower tests (line coverage for registerAlertRouterNodes helper)
// ============================================================================

func TestContainsAnyLower_Positive(t *testing.T) {
	if !containsAnyLower("critical disk error", "critical", "emergency") {
		t.Error("should match 'critical'")
	}
	if !containsAnyLower("EMERGENCY ALERT", "critical", "emergency") {
		t.Error("should match case-insensitive")
	}
	if !containsAnyLower("System Health OK", "health") {
		t.Error("should match 'health'")
	}
}

func TestContainsAnyLower_Negative(t *testing.T) {
	if containsAnyLower("normal system check", "critical", "emergency") {
		t.Error("should NOT match normal text")
	}
	if containsAnyLower("", "error") {
		t.Error("should NOT match empty string")
	}
	if containsAnyLower("short", "longer_word") {
		t.Error("should NOT match when keyword longer than string")
	}
}
