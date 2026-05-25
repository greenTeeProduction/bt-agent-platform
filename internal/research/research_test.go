package research

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/benchmark"
)

// TestDeepResearch tests that DeepResearchTree successfully routes and
// completes a comparison-style research query with acceptable success rate.
func TestDeepResearch(t *testing.T) {
	tree := DeepResearchTree()
	mock := benchmark.DefaultMock()
	suite := benchmark.Suite{
		Name: "deep_research_test",
		Tasks: []benchmark.TaskCase{
			{
				Task:          "research how X compares to Y",
				ShouldSucceed: true,
				MinResultLen:  20,
			},
		},
	}

	metrics := benchmark.RunSuite(tree, suite, mock)

	if metrics.SuccessRate < 0.5 {
		t.Errorf("deep research success rate too low: %.2f (want >= 0.5)", metrics.SuccessRate)
	}
	if metrics.TotalTasks != 1 {
		t.Errorf("expected 1 task, got %d", metrics.TotalTasks)
	}
	if len(metrics.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(metrics.Results))
	}

	t.Logf("deep research result: outcome=%s path=%s success=%v duration=%dms resultLen=%d",
		metrics.Results[0].Outcome, metrics.Results[0].Path,
		metrics.Results[0].Success, metrics.Results[0].DurationMs,
		metrics.Results[0].ResultLen)
}

// TestQuickResearch tests that QuickResearchTree successfully handles a
// simple fact-finding query (capital of France).
func TestQuickResearch(t *testing.T) {
	tree := QuickResearchTree()
	mock := benchmark.DefaultMock()
	suite := benchmark.Suite{
		Name: "quick_research_test",
		Tasks: []benchmark.TaskCase{
			{
				Task:          "what is the capital of France",
				ShouldSucceed: true,
				MinResultLen:  10,
			},
		},
	}

	metrics := benchmark.RunSuite(tree, suite, mock)

	if metrics.TotalTasks == 0 {
		t.Fatal("expected at least 1 task, got 0")
	}
	if len(metrics.Results) == 0 {
		t.Fatal("expected at least 1 result, got 0")
	}

	if !metrics.Results[0].Success {
		t.Errorf("quick research should succeed, got outcome=%q path=%q",
			metrics.Results[0].Outcome, metrics.Results[0].Path)
	}

	t.Logf("quick research result: outcome=%s path=%s success=%v duration=%dms resultLen=%d",
		metrics.Results[0].Outcome, metrics.Results[0].Path,
		metrics.Results[0].Success, metrics.Results[0].DurationMs,
		metrics.Results[0].ResultLen)
}

// TestAllResearchTrees runs every registered research tree variant through a
// 1-task suite to verify basic routing and execution work end-to-end.
func TestAllResearchTrees(t *testing.T) {
	trees := ResearchTrees()
	mock := benchmark.DefaultMock()

	if len(trees) == 0 {
		t.Fatal("ResearchTrees() returned no trees")
	}

	for name, tree := range trees {
		t.Run(name, func(t *testing.T) {
			suite := benchmark.Suite{
				Name: name,
				Tasks: []benchmark.TaskCase{
					{
						Task:          "research how " + name + " works",
						ShouldSucceed: true,
						MinResultLen:  10,
					},
				},
			}

			metrics := benchmark.RunSuite(tree, suite, mock)

			if metrics.TotalTasks == 0 {
				t.Errorf("tree %q: expected TotalTasks > 0", name)
			}
			if len(metrics.Results) == 0 {
				t.Errorf("tree %q: expected at least 1 result", name)
			}

			t.Logf("tree=%q successRate=%.2f successes=%d failures=%d avgDuration=%dms",
				name, metrics.SuccessRate, metrics.Successes,
				metrics.Failures, int64(metrics.AvgDurationMs))
		})
	}
}
