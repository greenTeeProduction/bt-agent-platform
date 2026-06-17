package engine_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

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

func TestRouting_Monitoring(t *testing.T) {
	bb := &engine.Blackboard{Task: "check system health status", LLM: &engine.MockLLM{}}
	tree := engine.BuildTree(domains.AgentMonitorTree(), bb)
	outcome := engine.RunTask(bb, tree)
	if outcome == "" {
		t.Error("monitoring task should produce outcome")
	}
}

func TestIntegration_AllTreesExecute(t *testing.T) {
	tests := []struct {
		name string
		tree *evolution.SerializableNode
		task string
	}{
		{"default", evolution.DefaultTree(), "analyze this task and provide a report"},
		{"godev_code_review", evolution.GoDeveloperTree(), "review this go code for bugs"},
		{"godev_build", evolution.GoDeveloperTree(), "go build the project"},
		{"godev_test", evolution.GoDeveloperTree(), "run go test for coverage"},
		{"godev_knowledge", evolution.GoDeveloperTree(), "what is the best practice for error handling"},
		{"deep_research", evolution.DeepResearchTree(), "research quantum computing advances"},
		{"quick_research", evolution.QuickResearchTree(), "quick summary of Kubernetes"},
		{"pitch_agent", evolution.PitchAgentTree(), "build a DCF model for valuation"},
		{"earnings_reviewer", evolution.EarningsReviewerTree(), "analyze earnings call transcript"},
		{"market_researcher", evolution.MarketResearcherTree(), "research competitive landscape"},
		{"model_builder", evolution.ModelBuilderTree(), "build LBO model for acquisition"},
		{"meeting_prep", evolution.MeetingPrepTree(), "prepare client meeting briefing"},
		{"valuation_reviewer", evolution.ValuationReviewerTree(), "review GP valuation package"},
		{"gl_reconciler", evolution.GLReconcilerTree(), "reconcile general ledger breaks"},
		{"month_end_closer", evolution.MonthEndCloserTree(), "close month-end with accruals"},
		{"statement_auditor", evolution.StatementAuditorTree(), "audit LP statement for accuracy"},
		{"kyc_screener", evolution.KYCScreenerTree(), "screen KYC documents for sanctions"},
		{"code_review", domains.CodeReviewTree(), "review code for bugs and security issues"},
		{"devops_ci", domains.DevOpsCITree(), "deploy the application with CI/CD pipeline"},
		{"agent_monitor", domains.AgentMonitorTree(), "check system health status"},
		{"refactoring", domains.RefactoringTree(), "refactor the legacy module"},
		{"security_audit", domains.SecurityAuditTree(), "audit security vulnerabilities"},
		{"data_pipeline", domains.DataPipelineTree(), "extract transform load the dataset"},
		{"meeting_notes", domains.MeetingNotesTree(), "summarize the meeting transcript"},
		{"crash_investigator", domains.CrashInvestigatorTree(), "investigate the crash dump"},
		{"game_ai", domains.GameAITree(), "design NPC behavior tree for game"},
		{"trading_signal", domains.TradingSignalTree(), "generate trading signal from market data"},
		{"hermes_evolve", domains.HermesSelfEvolutionTree(), "periodic self-improvement check"},
		{"stockfish", evolution.StockfishEvolutionTree(), "evolve the behavior tree with stockfish"},
		{"stockfish_loop", evolution.StockfishEvolutionLoop(), "run continuous evolution cycle"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bb := &engine.Blackboard{Task: tt.task, LLM: &engine.MockLLM{}}
			bt := engine.BuildTree(tt.tree, bb)
			_ = engine.RunTask(bb, bt)
		})
	}
}

func TestIntegration_AllKanbanTrees(t *testing.T) {
	trees := map[string]*evolution.SerializableNode{
		"task_creator": domains.KanbanTaskCreatorTree(),
		"refiner":      domains.KanbanRefinerTree(),
		"qa":           domains.KanbanQATree(),
		"monitor":      domains.KanbanBoardMonitorTree(),
		"workflow":     domains.KanbanWorkflowTree(),
		"autopilot":    domains.KanbanAutoPilotTree(),
	}
	for name, tree := range trees {
		t.Run(name, func(t *testing.T) {
			if tree == nil {
				t.Fatal("tree is nil")
			}
			bb := &engine.Blackboard{Task: "kanban " + name, LLM: &engine.MockLLM{}}
			bt := engine.BuildTree(tree, bb)
			outcome := engine.RunTask(bb, bt)
			if outcome == "" {
				t.Errorf("%s: no outcome", name)
			}
		})
	}
}

func TestDataPipelineTree_UsesObservedFileMetrics(t *testing.T) {
	tmp := t.TempDir()
	csvPath := filepath.Join(tmp, "input.csv")
	if err := os.WriteFile(csvPath, []byte("name,value\na,1\nb,2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	bb := &engine.Blackboard{Task: "extract data from " + csvPath, ChainState: map[string]any{}}
	bt := engine.BuildTree(domains.DataPipelineTree(), bb)
	engine.RunTask(bb, bt)

	if !strings.Contains(bb.Result, "csv_records_observed: 3") {
		t.Fatalf("expected real csv record count, got: %s", bb.Result)
	}
	if strings.Contains(bb.Result, "10,420") || strings.Contains(bb.Result, "10,418") {
		t.Fatalf("fabricated canned row count leaked: %s", bb.Result)
	}
	if !strings.Contains(bb.Result, "verification: passed anti-fabrication gate") {
		t.Fatalf("expected anti-fabrication verification, got: %s", bb.Result)
	}
}

func TestDataPipelineTree_NoSourceReportsBlockedNotFabricated(t *testing.T) {
	bb := &engine.Blackboard{Task: "run ETL workflow with no source path", ChainState: map[string]any{}}
	bt := engine.BuildTree(domains.DataPipelineTree(), bb)
	engine.RunTask(bb, bt)

	if !strings.Contains(bb.Result, "status: blocked") {
		t.Fatalf("expected blocked report, got: %s", bb.Result)
	}
	if strings.Contains(bb.Result, "10,420") || strings.Contains(bb.Result, "10,418") {
		t.Fatalf("fabricated canned row count leaked: %s", bb.Result)
	}
	if !strings.Contains(bb.Result, "available_tools:") {
		t.Fatalf("expected discovered tool list in report, got: %s", bb.Result)
	}
}

func TestNotebookLMTree_PreGateDiscoversRealToolset(t *testing.T) {
	tree := domains.NotebookLMTree()
	if len(tree.Children) == 0 || tree.Children[0].Name != "NotebookLM_PreGate" {
		t.Fatalf("expected NotebookLM_PreGate first child, got %#v", tree.Children)
	}
	preGate := tree.Children[0]
	seenSetup := false
	seenDiscover := false
	for _, child := range preGate.Children {
		if child.Name == "SetupNotebookLMTools" {
			seenSetup = true
		}
		if child.Name == "DiscoverAvailableTools" {
			seenDiscover = true
		}
	}
	if !seenSetup || !seenDiscover {
		t.Fatalf("NotebookLM pre-gate must setup and discover real tools before auth/use; setup=%v discover=%v", seenSetup, seenDiscover)
	}

	bb := &engine.Blackboard{ChainState: map[string]any{}}
	if fn := engine.GetAction("SetupNotebookLMTools"); fn == nil || fn(&btcore.BTContext[engine.Blackboard]{Blackboard: bb}) != 1 {
		t.Fatal("SetupNotebookLMTools action missing or failed")
	}
	if fn := engine.GetAction("DiscoverAvailableTools"); fn == nil || fn(&btcore.BTContext[engine.Blackboard]{Blackboard: bb}) != 1 {
		t.Fatal("DiscoverAvailableTools action missing or failed")
	}
	available, _ := bb.ChainState["available_tools"].(string)
	for _, name := range []string{"notebooklm_server_info", "notebooklm_list", "notebooklm_notebook_query", "file_write"} {
		if !strings.Contains(available, name) {
			t.Fatalf("NotebookLM available tools missing %q: %s", name, available)
		}
	}
}
