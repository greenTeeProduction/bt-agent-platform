package agentexec

import (
	"context"
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/dashboard"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// TestRunPipelineWithID_RecordsAgentMetrics pins milestone 2 of the Q3
// Reliability dashboard-metrics program: RunPipelineWithID drives
// agent.RunAgent directly (bypassing dashboard.AgentExecutor, whose
// RunTaskResult was already fixed in milestone 1 to call dashboard.RecordTask
// via recordTaskMetric), so today every YAML-workflow-driven pipeline run
// leaves dashboard.GetAgentMetrics() permanently empty for the agents it
// invokes — the /api/metrics and /api/alerts dashboard surfaces never see
// these runs at all.
func TestRunPipelineWithID_RecordsAgentMetrics(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())

	const agentName = "metrics-pipeline-agent"

	d := &agent.RunDeps{
		ResolveTree: func(_ string) *evolution.SerializableNode {
			return &evolution.SerializableNode{Type: "AlwaysSucceed"}
		},
	}

	pipeline := dashboard.Pipeline{
		Name: "metrics-pipeline-test",
		Steps: []dashboard.Step{
			{ID: "step1", Kind: dashboard.StepAgent, Agent: agentName, Input: "do the thing"},
		},
	}

	res, err := RunPipelineWithID(context.Background(), d, pipeline, "go", "")
	if err != nil {
		t.Fatalf("RunPipelineWithID: %v", err)
	}
	if res == nil || len(res.Steps) != 1 || res.Steps[0].Outcome != "success" {
		t.Fatalf("RunPipelineWithID result = %+v, want a single successful step (test setup must drive a succeeding outcome)", res)
	}

	var stats *dashboard.AgentStats
	for _, s := range dashboard.GetAgentMetrics() {
		s := s
		if s.Name == agentName {
			stats = &s
			break
		}
	}
	if stats == nil {
		t.Fatalf("dashboard.GetAgentMetrics() has no entry for %q — RunPipelineWithID must call dashboard.RecordTask on every agent.RunAgent call, like AgentExecutor.RunTaskResult already does", agentName)
	}
	if stats.TotalCount != 1 {
		t.Errorf("GetAgentMetrics()[%q].TotalCount = %d, want 1", agentName, stats.TotalCount)
	}
	if stats.SuccessCount != 1 {
		t.Errorf("GetAgentMetrics()[%q].SuccessCount = %d, want 1", agentName, stats.SuccessCount)
	}
}
