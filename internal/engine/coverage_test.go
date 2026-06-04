package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// ─── Output Quality Validation ───

func TestValidateOutput_Good(t *testing.T) {
	bb := &Blackboard{Result: "This is a comprehensive analysis with multiple sections and detailed findings."}
	if !validateOutputQuality(bb) {
		t.Errorf("good output should pass, score=%.2f", bb.QualityScore)
	}
}

func TestValidateOutput_Short(t *testing.T) {
	bb := &Blackboard{Result: "short"}
	if validateOutputQuality(bb) {
		t.Error("short output should fail")
	}
}

func TestValidateOutput_ErrorPattern(t *testing.T) {
	bb := &Blackboard{Result: "I cannot complete this task due to an error"}
	if validateOutputQuality(bb) {
		t.Error("error pattern should fail")
	}
}

func TestValidateOutput_QualityFailurePrefix(t *testing.T) {
	bb := &Blackboard{Result: "OUTPUT QUALITY FAILED (score=0.1): ## Fabricated Report\n\nThis output is long and structured, but it must remain a quality failure."}
	if validateOutputQuality(bb) {
		t.Fatalf("quality-failure wrapper should fail, score=%.2f", bb.QualityScore)
	}
	if bb.QualityScore > 0.1 {
		t.Fatalf("quality-failure wrapper should keep low quality score, got %.2f", bb.QualityScore)
	}
}

func TestValidateOutput_Structured(t *testing.T) {
	bb := &Blackboard{Result: "# Report\n\n## Findings\n- Finding 1\n- Finding 2\n\n## Code\n```\nexample\n```\n\nDetailed analysis with substantial content for quality validation."}
	if !validateOutputQuality(bb) {
		t.Errorf("structured output should pass, score=%.2f", bb.QualityScore)
	}
	if bb.QualityScore < 0.7 {
		t.Errorf("structured should score high, got %.2f", bb.QualityScore)
	}
}

func TestValidateOutput_Empty(t *testing.T) {
	bb := &Blackboard{Result: ""}
	if validateOutputQuality(bb) {
		t.Error("empty result should fail")
	}
}

func TestValidateOutput_FromResults(t *testing.T) {
	bb := &Blackboard{Result: "", Results: []string{"x", "This is a valid result from accumulated results that should pass quality checks."}}
	if !validateOutputQuality(bb) {
		t.Error("should fall back to Results when Result is empty")
	}
}

// ─── Tree Structure Tests ───

func TestTree_DefaultStructure(t *testing.T) {
	tree := evolution.DefaultTree()
	names := collectNames(tree)
	for _, name := range []string{"MainSequence", "PreGate", "StrategyRouter", "ReflectOnOutcome", "OutcomeSelector"} {
		if !contains(names, name) {
			t.Errorf("default tree missing: %s", name)
		}
	}
}

func TestTree_GoDevStructure(t *testing.T) {
	tree := evolution.GoDeveloperTree()
	names := collectNames(tree)
	for _, name := range []string{"GoDev_Main", "PreGate", "StrategyRouter", "OutcomeSelector"} {
		if !contains(names, name) {
			t.Errorf("GoDev tree missing: %s", name)
		}
	}
}

func TestTree_AllEvolution(t *testing.T) {
	fns := map[string]func() *evolution.SerializableNode{
		"hermes_evolve":  domains.HermesSelfEvolutionTree,
		"stockfish":      evolution.StockfishEvolutionTree,
		"stockfish_loop": evolution.StockfishEvolutionLoop,
	}
	for name, fn := range fns {
		tree := fn()
		if tree == nil || len(tree.Children) == 0 {
			t.Errorf("%s tree invalid", name)
		}
	}
}

func TestTree_AllFinance(t *testing.T) {
	fns := map[string]func() *evolution.SerializableNode{
		"pitch_agent":        evolution.PitchAgentTree,
		"earnings_reviewer":  evolution.EarningsReviewerTree,
		"market_researcher":  evolution.MarketResearcherTree,
		"model_builder":      evolution.ModelBuilderTree,
		"meeting_prep":       evolution.MeetingPrepTree,
		"valuation_reviewer": evolution.ValuationReviewerTree,
		"gl_reconciler":      evolution.GLReconcilerTree,
		"month_end_closer":   evolution.MonthEndCloserTree,
		"statement_auditor":  evolution.StatementAuditorTree,
		"kyc_screener":       evolution.KYCScreenerTree,
	}
	for name, fn := range fns {
		tree := fn()
		if tree == nil || len(tree.Children) == 0 {
			t.Errorf("%s tree invalid", name)
		}
	}
}

func TestTree_AllDomain(t *testing.T) {
	fns := map[string]func() *evolution.SerializableNode{
		"code_review":        domains.CodeReviewTree,
		"devops_ci":          domains.DevOpsCITree,
		"agent_monitor":      domains.AgentMonitorTree,
		"refactoring":        domains.RefactoringTree,
		"security_audit":     domains.SecurityAuditTree,
		"data_pipeline":      domains.DataPipelineTree,
		"meeting_notes":      domains.MeetingNotesTree,
		"crash_investigator": domains.CrashInvestigatorTree,
		"game_ai":            domains.GameAITree,
		"trading_signal":     domains.TradingSignalTree,
	}
	for name, fn := range fns {
		tree := fn()
		if tree == nil || len(tree.Children) == 0 {
			t.Errorf("%s tree invalid", name)
		}
	}
}

func TestTree_Research(t *testing.T) {
	deep := evolution.DeepResearchTree()
	if deep == nil || len(deep.Children) == 0 {
		t.Error("DeepResearchTree invalid")
	}
	quick := evolution.QuickResearchTree()
	if quick == nil || len(quick.Children) == 0 {
		t.Error("QuickResearchTree invalid")
	}
}

// ─── Routing Path Tests ───

func TestRouting_CodeReview(t *testing.T) {
	bb := &Blackboard{Task: "review this Go code for bugs", LLM: &MockLLM{}}
	tree := BuildTree(evolution.GoDeveloperTree(), bb)
	outcome := RunTask(bb, tree)
	if outcome == "" {
		t.Error("code review task should produce outcome")
	}
}

func TestRouting_GoKnowledge(t *testing.T) {
	bb := &Blackboard{Task: "what is the best practice for Go error handling", LLM: &MockLLM{}}
	tree := BuildTree(evolution.GoDeveloperTree(), bb)
	outcome := RunTask(bb, tree)
	if outcome == "" {
		t.Error("knowledge task should produce outcome")
	}
}

func TestRouting_Finance(t *testing.T) {
	bb := &Blackboard{Task: "build a DCF model for valuation", LLM: &MockLLM{}}
	tree := BuildTree(evolution.PitchAgentTree(), bb)
	outcome := RunTask(bb, tree)
	if outcome == "" {
		t.Error("DCF task should produce outcome")
	}
}

func TestRouting_Research(t *testing.T) {
	bb := &Blackboard{Task: "research quantum computing advances", LLM: &MockLLM{}}
	tree := BuildTree(evolution.DeepResearchTree(), bb)
	outcome := RunTask(bb, tree)
	if outcome == "" {
		t.Error("research task should produce outcome")
	}
}

func TestRouting_Monitoring(t *testing.T) {
	bb := &Blackboard{Task: "check system health status", LLM: &MockLLM{}}
	tree := BuildTree(domains.AgentMonitorTree(), bb)
	outcome := RunTask(bb, tree)
	if outcome == "" {
		t.Error("monitoring task should produce outcome")
	}
}

// ─── Outcome Flow ───

func TestOutcome_Success(t *testing.T) {
	bb := &Blackboard{Task: "test task", Outcome: string(evolution.Success), LLM: &MockLLM{}, Result: "valid output with sufficient length to pass quality validation checks"}
	tree := evolution.DefaultTree()
	bt := BuildTree(tree, bb)
	_ = RunTask(bb, bt)
	if bb.Outcome != string(evolution.Success) {
		t.Errorf("expected success, got %s", bb.Outcome)
	}
}

func TestOutcome_EmptyTaskFails(t *testing.T) {
	bb := &Blackboard{Task: "", LLM: &MockLLM{}}
	tree := evolution.DefaultTree()
	bt := BuildTree(tree, bb)
	outcome := RunTask(bb, bt)
	if outcome == string(evolution.Success) {
		t.Error("empty task should not succeed")
	}
}

// ─── Builder Edge Cases ───

func TestBuildTree_Minimal(t *testing.T) {
	bb := &Blackboard{LLM: &MockLLM{}}
	node := &evolution.SerializableNode{Type: "Sequence", Name: "minimal"}
	bt := BuildTree(node, bb)
	if bt == nil {
		t.Error("BuildTree should work with minimal node")
	}
}

func TestBuildTree_UnknownType(t *testing.T) {
	bb := &Blackboard{LLM: &MockLLM{}}
	node := &evolution.SerializableNode{Type: "UnknownType", Name: "unknown"}
	bt := BuildTree(node, bb)
	if bt == nil {
		t.Error("BuildTree should handle unknown types")
	}
}

// ─── Blackboard ───

func TestBlackboard_AllFields(t *testing.T) {
	store, _ := evolution.NewTreeStore("/tmp/test-trees")
	refStore, _ := evolution.NewStore("/tmp/test-reflections")
	bb := &Blackboard{
		Task: "task", Complexity: "medium", Plan: "plan", Result: "result",
		Outcome: "success", KgResults: "kg", CachedResult: "cached",
		FailureCount: 1, Reflections: refStore, TreeStore: store,
		LLM: &MockLLM{}, ChainState: map[string]any{"k": "v"},
		Results: []string{"r1", "r2"}, QualityScore: 0.85,
	}
	if bb.QualityScore != 0.85 {
		t.Error("QualityScore")
	}
	if len(bb.Results) != 2 {
		t.Error("Results")
	}
}

// ─── Serialization ───

func TestTree_SerializeRoundtrip(t *testing.T) {
	original := evolution.DefaultTree()
	store, _ := evolution.NewTreeStore("/tmp/test-trees")
	store.Save(original)
	loaded, err := store.Load()
	if err != nil || loaded == nil || loaded.Name != original.Name {
		t.Error("serialize roundtrip failed")
	}
}

// ─── Helpers ───

func collectNames(node *evolution.SerializableNode) []string {
	var names []string
	names = append(names, node.Name)
	for i := range node.Children {
		names = append(names, collectNames(&node.Children[i])...)
	}
	return names
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
