package finance

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/benchmark"
)

func TestPitchAgent_DCF(t *testing.T) {
	tree := PitchAgentTree()
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
	tree := EarningsReviewerTree()
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
	tree := KYCScreenerTree()
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
	tree := GLReconcilerTree()
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

func TestAllFinanceTrees(t *testing.T) {
	for name, tree := range AllFinanceTrees() {
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
