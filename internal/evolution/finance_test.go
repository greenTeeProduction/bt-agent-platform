package evolution_test

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/benchmark"
	evolution "github.com/nico/go-bt-evolve/internal/evolution"
)

func TestPitchAgent_DCF(t *testing.T) {
	tree := evolution.PitchAgentTree()
	mock := benchmark.DefaultMock()
	suite := benchmark.Suite{
		Name: "pitch_dcf",
		Tasks: []benchmark.TaskCase{
			{Task: "build a DCF model", ExpectedPath: "DCFPath", ShouldSucceed: true, MinResultLen: 10},
		},
	}
	metrics := benchmark.RunSuite(tree, suite, mock)
	if metrics.SuccessRate < 0.5 {
		t.Errorf("PitchAgent DCF success rate too low: %.2f", metrics.SuccessRate)
	}
}

func TestEarningsReviewer(t *testing.T) {
	tree := evolution.EarningsReviewerTree()
	mock := benchmark.DefaultMock()
	suite := benchmark.Suite{
		Name: "earnings_review",
		Tasks: []benchmark.TaskCase{
			{Task: "review Q3 earnings", ExpectedPath: "EarningsIngestPath", ShouldSucceed: true, MinResultLen: 10},
		},
	}
	metrics := benchmark.RunSuite(tree, suite, mock)
	if metrics.TotalTasks == 0 {
		t.Error("EarningsReviewer: no tasks run")
	}
	if metrics.SuccessRate < 0.5 {
		t.Errorf("EarningsReviewer success rate too low: %.2f", metrics.SuccessRate)
	}
}

func TestKYCScreener(t *testing.T) {
	tree := evolution.KYCScreenerTree()
	mock := benchmark.DefaultMock()
	suite := benchmark.Suite{
		Name: "kyc_screen",
		Tasks: []benchmark.TaskCase{
			{Task: "run KYC screening for new client", ExpectedPath: "KYCPath", ShouldSucceed: true, MinResultLen: 10},
		},
	}
	metrics := benchmark.RunSuite(tree, suite, mock)
	if metrics.TotalTasks == 0 {
		t.Error("KYCScreener: no tasks run")
	}
	if metrics.SuccessRate < 0.5 {
		t.Errorf("KYCScreener success rate too low: %.2f", metrics.SuccessRate)
	}
}

func TestGLReconciler(t *testing.T) {
	tree := evolution.GLReconcilerTree()
	mock := benchmark.DefaultMock()
	suite := benchmark.Suite{
		Name: "gl_recon",
		Tasks: []benchmark.TaskCase{
			{Task: "reconcile the general ledger", ExpectedPath: "ReconPath", ShouldSucceed: true, MinResultLen: 10},
		},
	}
	metrics := benchmark.RunSuite(tree, suite, mock)
	if metrics.TotalTasks == 0 {
		t.Error("GLReconciler: no tasks run")
	}
	if metrics.SuccessRate < 0.5 {
		t.Errorf("GLReconciler success rate too low: %.2f", metrics.SuccessRate)
	}
}

func TestAllFinanceTrees_FallbacksUseChainAction(t *testing.T) {
	for name, tree := range evolution.AllFinanceTrees() {
		assertFallbacksUseChainAction(t, name, *tree)
	}
}

func assertFallbacksUseChainAction(t *testing.T, treeName string, node evolution.SerializableNode) {
	t.Helper()
	if node.Name == "ExecutionPath" || node.Name == "FallbackExecution" {
		if len(node.Children) != 1 {
			t.Fatalf("%s/%s should contain exactly one ChainAction fallback, got %d children", treeName, node.Name, len(node.Children))
		}
		child := node.Children[0]
		if child.Type != "ChainAction" {
			t.Fatalf("%s/%s should use ChainAction fallback, got type=%s name=%s", treeName, node.Name, child.Type, child.Name)
		}
		if child.Name == "AnalyzeTask" || child.Name == "ExecutePlan" {
			t.Fatalf("%s/%s still uses stub action %q", treeName, node.Name, child.Name)
		}
	}
	for _, child := range node.Children {
		assertFallbacksUseChainAction(t, treeName, child)
	}
}

func TestAllFinanceTrees(t *testing.T) {
	for name, tree := range evolution.AllFinanceTrees() {
		mock := benchmark.DefaultMock()
		suite := benchmark.Suite{
			Name: "all_finance_" + name,
			Tasks: []benchmark.TaskCase{
				{Task: "process financial task", ShouldSucceed: true, MinResultLen: 10},
			},
		}
		metrics := benchmark.RunSuite(tree, suite, mock)
		if metrics.TotalTasks == 0 {
			t.Errorf("tree %s: no tasks run", name)
		}
		if metrics.SuccessRate < 0.5 {
			t.Errorf("tree %s: success rate too low: %.2f", name, metrics.SuccessRate)
		}
	}
}
