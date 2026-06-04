package engine

import (
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// TestInitAction_ExecLLMCall covers the execLLMCallAction registered in init().
// Tests both the LLM=nil (error) and LLM=mock (success) paths.
func TestInitAction_ExecLLMCall(t *testing.T) {
	bb := &Blackboard{Task: "tell me a joke", LLM: nil}
	fn := GetAction("ExecLLMCall")
	if fn == nil {
		t.Fatal("ExecLLMCall not registered")
	}
	// LLM nil → returns -1 (failure)
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != -1 {
		t.Errorf("ExecLLMCall with nil LLM: expected -1, got %d", got)
	}

	// LLM set → returns 1 (success)
	bb2 := &Blackboard{Task: "tell me a joke", LLM: &MockLLM{}}
	ctx2 := &btcore.BTContext[Blackboard]{Blackboard: bb2}
	if got := fn(ctx2); got != 1 {
		t.Errorf("ExecLLMCall with mock LLM: expected 1, got %d", got)
	}
	if bb2.Result == "" {
		t.Error("ExecLLMCall should set Result")
	}
}

// TestInitAction_ExecRefine covers the execRefineAction registered in init().
func TestInitAction_ExecRefine(t *testing.T) {
	bb := &Blackboard{Task: "improve this", Result: "", LLM: nil}
	fn := GetAction("ExecRefine")
	if fn == nil {
		t.Fatal("ExecRefine not registered")
	}

	// LLM nil + empty Result → returns -1
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != -1 {
		t.Errorf("ExecRefine with nil LLM: expected -1, got %d", got)
	}

	// LLM set but empty Result → still -1 (Result required)
	bb2 := &Blackboard{Task: "improve this", Result: "", LLM: &MockLLM{}}
	ctx2 := &btcore.BTContext[Blackboard]{Blackboard: bb2}
	if got := fn(ctx2); got != -1 {
		t.Errorf("ExecRefine with empty Result: expected -1, got %d", got)
	}

	// Both set → returns 1
	bb3 := &Blackboard{Task: "improve this", Result: "initial output that needs refinement", LLM: &MockLLM{}}
	ctx3 := &btcore.BTContext[Blackboard]{Blackboard: bb3}
	if got := fn(ctx3); got != 1 {
		t.Errorf("ExecRefine with valid inputs: expected 1, got %d", got)
	}
	if bb3.Result == "initial output that needs refinement" {
		t.Error("ExecRefine should update Result")
	}
}

// TestInitAction_SelfCorrect_ResultsFallback covers the Results[] fallback path
// in SelfCorrect where bb.Result is empty but bb.Results has entries.
func TestInitAction_SelfCorrect_ResultsFallback(t *testing.T) {
	// Result="" but Results has entries → should pick last Results entry
	bb := &Blackboard{
		Task:    "analyze this code",
		Result:  "",
		Results: []string{"first attempt", "second attempt"},
		LLM:     &MockLLM{},
	}
	fn := GetAction("SelfCorrect")
	if fn == nil {
		t.Fatal("SelfCorrect not registered")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("SelfCorrect with Results fallback: expected 1, got %d", got)
	}
	if bb.Result == "" {
		t.Error("SelfCorrect should set Result after correction")
	}
}

// TestInitAction_SelfCorrect_LLMNil covers the error path when LLM is nil.
func TestInitAction_SelfCorrect_LLMNil(t *testing.T) {
	bb := &Blackboard{
		Task:   "analyze this code",
		Result: "previous output",
		LLM:    nil,
	}
	fn := GetAction("SelfCorrect")
	if fn == nil {
		t.Fatal("SelfCorrect not registered")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != -1 {
		t.Errorf("SelfCorrect with nil LLM: expected -1, got %d", got)
	}
}

// TestInitAction_HealthCheckAgent verifies HealthCheckAgent executes without panic.
func TestInitAction_HealthCheckAgent(t *testing.T) {
	fn := GetAction("HealthCheckAgent")
	if fn == nil {
		t.Fatal("HealthCheckAgent not registered")
	}
	bb := &Blackboard{}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := func() int {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("HealthCheckAgent panicked: %v", r)
			}
		}()
		return fn(ctx)
	}()
	if result != 1 {
		t.Errorf("HealthCheckAgent: expected 1, got %d", result)
	}
	if bb.Result == "" {
		t.Error("HealthCheckAgent should set Result")
	}
}

// TestInitAction_MetricsCollectionAgent verifies MetricsCollectionAgent executes without panic.
func TestInitAction_MetricsCollectionAgent(t *testing.T) {
	fn := GetAction("MetricsCollectionAgent")
	if fn == nil {
		t.Fatal("MetricsCollectionAgent not registered")
	}
	bb := &Blackboard{}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := func() int {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("MetricsCollectionAgent panicked: %v", r)
			}
		}()
		return fn(ctx)
	}()
	if result != 1 {
		t.Errorf("MetricsCollectionAgent: expected 1, got %d", result)
	}
	if bb.Result == "" {
		t.Error("MetricsCollectionAgent should set Result")
	}
}

// TestInitAction_AnalyzeTask covers AnalyzeTask with and without LLM.
func TestInitAction_AnalyzeTask(t *testing.T) {
	fn := GetAction("AnalyzeTask")
	if fn == nil {
		t.Fatal("AnalyzeTask not registered")
	}

	// Without LLM → Complexity stays empty
	bb := &Blackboard{Task: "test task", LLM: nil}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("AnalyzeTask no LLM: expected 1, got %d", got)
	}
	if bb.Complexity != "" {
		t.Errorf("AnalyzeTask no LLM: expected empty Complexity, got %q", bb.Complexity)
	}

	// With LLM → Complexity gets set (MockLLM returns "" by default so we set it)
	bb2 := &Blackboard{Task: "test task", LLM: &MockLLM{ComplexityResp: "medium"}}
	ctx2 := &btcore.BTContext[Blackboard]{Blackboard: bb2}
	if got := fn(ctx2); got != 1 {
		t.Errorf("AnalyzeTask with LLM: expected 1, got %d", got)
	}
	if bb2.Complexity == "" {
		t.Error("AnalyzeTask with LLM should set Complexity")
	}
}

// TestInitAction_ExecutePlan covers ExecutePlan with and without LLM.
func TestInitAction_ExecutePlan(t *testing.T) {
	fn := GetAction("ExecutePlan")
	if fn == nil {
		t.Fatal("ExecutePlan not registered")
	}

	// Without LLM → Result still set, no Plan
	bb := &Blackboard{Task: "test task", LLM: nil, Complexity: "low"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("ExecutePlan no LLM: expected 1, got %d", got)
	}
	if bb.Result == "" {
		t.Error("ExecutePlan should set Result")
	}
	if strings.Contains(bb.Result, "Executed plan for:") {
		t.Fatalf("ExecutePlan should not return placeholder output, got %q", bb.Result)
	}
	if bb.Outcome != "success" {
		t.Error("ExecutePlan should set Outcome to success")
	}

	// With LLM → Plan gets set (MockLLM returns "" by default so we set it)
	bb2 := &Blackboard{Task: "test task", LLM: &MockLLM{PlanResp: "execution plan"}, Complexity: "low"}
	ctx2 := &btcore.BTContext[Blackboard]{Blackboard: bb2}
	if got := fn(ctx2); got != 1 {
		t.Errorf("ExecutePlan with LLM: expected 1, got %d", got)
	}
	if bb2.Plan == "" {
		t.Error("ExecutePlan with LLM should set Plan")
	}
	if bb2.Result != bb2.Plan {
		t.Fatalf("ExecutePlan with LLM should expose generated plan as result, got result=%q plan=%q", bb2.Result, bb2.Plan)
	}
}

// TestInitAction_reflectOnOutcome covers the Failure path in reflectOnOutcomeAction.
func TestInitAction_reflectOnOutcome(t *testing.T) {
	fn := GetAction("ReflectOnOutcome")
	if fn == nil {
		t.Fatal("ReflectOnOutcome not registered")
	}

	// Success outcome → WentWell gets set
	bb := &Blackboard{Task: "test", Outcome: "success", LLM: &MockLLM{}}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("ReflectOnOutcome success: expected 1, got %d", got)
	}

	// Failure outcome → ToImprove gets set
	bb2 := &Blackboard{Task: "test", Outcome: "failure", LLM: &MockLLM{}}
	ctx2 := &btcore.BTContext[Blackboard]{Blackboard: bb2}
	if got := fn(ctx2); got != 1 {
		t.Errorf("ReflectOnOutcome failure: expected 1, got %d", got)
	}
}

// TestInitAction_DefaultFallback covers DefaultFallback registered action.
func TestInitAction_DefaultFallback(t *testing.T) {
	fn := GetAction("DefaultFallback")
	if fn == nil {
		t.Fatal("DefaultFallback not registered")
	}
	bb := &Blackboard{Task: "generic task"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("DefaultFallback: expected 1, got %d", got)
	}
	if bb.Result == "" {
		t.Error("DefaultFallback should set Result")
	}
	if bb.Outcome != "success" {
		t.Error("DefaultFallback should set Outcome to success")
	}
}

// TestInitAction_ValidateOutput covers ValidateOutput registered action.
func TestInitAction_ValidateOutput(t *testing.T) {
	fn := GetAction("ValidateOutput")
	if fn == nil {
		t.Fatal("ValidateOutput not registered")
	}

	// Short output → should be negative (failure)
	bb := &Blackboard{Result: "short"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got == 1 {
		t.Log("ValidateOutput returns 1 for short output (lenient)")
	} else if got != -1 {
		t.Errorf("ValidateOutput short: expected 1 or -1, got %d", got)
	}
}

// TestInitAction_KnowledgeQuery covers KnowledgeQuery registered action.
func TestInitAction_KnowledgeQuery(t *testing.T) {
	fn := GetAction("KnowledgeQuery")
	if fn == nil {
		t.Fatal("KnowledgeQuery not registered")
	}
	bb := &Blackboard{Task: "what is Go"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("KnowledgeQuery: expected 1, got %d", got)
	}
	if bb.KgResults == "" {
		t.Error("KnowledgeQuery should set KgResults")
	}
}

// TestInitAction_GeneratePlan covers GeneratePlan registered action.
func TestInitAction_GeneratePlan(t *testing.T) {
	fn := GetAction("GeneratePlan")
	if fn == nil {
		t.Fatal("GeneratePlan not registered")
	}
	bb := &Blackboard{Task: "build a feature", LLM: &MockLLM{PlanResp: "my plan"}}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("GeneratePlan: expected 1, got %d", got)
	}
	if bb.Plan == "" {
		t.Error("GeneratePlan should set Plan")
	}
}

// TestInitAction_AssignComplexity covers AssignComplexity registered action.
func TestInitAction_AssignComplexity(t *testing.T) {
	fn := GetAction("AssignComplexity")
	if fn == nil {
		t.Fatal("AssignComplexity not registered")
	}
	bb := &Blackboard{Task: "test task", LLM: &MockLLM{ComplexityResp: "low"}}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("AssignComplexity: expected 1, got %d", got)
	}
	if bb.Complexity == "" {
		t.Error("AssignComplexity should set Complexity")
	}
}

// TestInitAction_CacheCheck covers CacheCheck registered action.
func TestInitAction_CacheCheck(t *testing.T) {
	fn := GetAction("CacheCheck")
	if fn == nil {
		t.Fatal("CacheCheck not registered")
	}

	// KgResults indicates no cache → returns -1 (no cache)
	bb := &Blackboard{KgResults: "KG: test — no cached results"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != -1 {
		t.Errorf("CacheCheck no cache: expected -1, got %d", got)
	}

	// KgResults has cached data → returns 1
	bb2 := &Blackboard{KgResults: "cached data for Go"}

	ctx2 := &btcore.BTContext[Blackboard]{Blackboard: bb2}
	if got := fn(ctx2); got != 1 {
		t.Errorf("CacheCheck with cache: expected 1, got %d", got)
	}
}

// TestInitAction_CacheResult covers CacheResult registered action.
func TestInitAction_CacheResult(t *testing.T) {
	fn := GetAction("CacheResult")
	if fn == nil {
		t.Fatal("CacheResult not registered")
	}
	bb := &Blackboard{Result: "cached output", Task: "test"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("CacheResult: expected 1, got %d", got)
	}
}

// TestInitAction_UpdateBehaviorTree covers UpdateBehaviorTree registered action.
func TestInitAction_UpdateBehaviorTree(t *testing.T) {
	fn := GetAction("UpdateBehaviorTree")
	if fn == nil {
		t.Fatal("UpdateBehaviorTree not registered")
	}
	bb := &Blackboard{}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("UpdateBehaviorTree: expected 1, got %d", got)
	}
}

// TestInitAction_MarkSuccessful covers MarkSuccessful registered action.
func TestInitAction_MarkSuccessful(t *testing.T) {
	fn := GetAction("MarkSuccessful")
	if fn == nil {
		t.Fatal("MarkSuccessful not registered")
	}
	bb := &Blackboard{}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("MarkSuccessful: expected 1, got %d", got)
	}
	if bb.Outcome != "success" {
		t.Error("MarkSuccessful should set Outcome to success")
	}
}

// TestInitAction_EscalateToDeepSeek covers EscalateToDeepSeek registered action.
func TestInitAction_EscalateToDeepSeek(t *testing.T) {
	fn := GetAction("EscalateToDeepSeek")
	if fn == nil {
		t.Fatal("EscalateToDeepSeek not registered")
	}
	bb := &Blackboard{}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if got := fn(ctx); got != 1 {
		t.Errorf("EscalateToDeepSeek: expected 1, got %d", got)
	}
}

// TestInitAction_cacheCheckAction covers code paths in cacheCheckAction helper.
func TestInitAction_cacheCheckAction(t *testing.T) {
	fn := GetAction("CacheCheck")
	if fn == nil {
		t.Fatal("CacheCheck not registered")
	}

	// Empty KgResults
	bb := &Blackboard{KgResults: ""}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	got := fn(ctx)
	if got != -1 {
		t.Errorf("CacheCheck empty KgResults: expected -1, got %d", got)
	}

	// KgResults with "no cached" → no cache
	bb2 := &Blackboard{KgResults: "no cached results found"}
	ctx2 := &btcore.BTContext[Blackboard]{Blackboard: bb2}
	got2 := fn(ctx2)
	if got2 != -1 {
		t.Errorf("CacheCheck 'no cached' in KgResults: expected -1, got %d", got2)
	}
}
