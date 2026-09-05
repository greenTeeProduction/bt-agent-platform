package dashboard

import (
	"context"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/blackboard"
)

func TestRunner_PromotesStepOutputToSession(t *testing.T) {
	mgr := blackboard.DefaultManager()
	runID := "sess_test_run"
	r := &Runner{
		RunID:       runID,
		Blackboards: mgr,
		RunAgent: func(ctx context.Context, agentName, _, task string) (string, string, error) {
			return "success", "agent output for " + task, nil
		},
	}
	wf := Pipeline{
		Name: "test",
		Steps: []Step{
			{ID: "analyze", Kind: StepAgent, Agent: "demo", Input: "{{.input}}"},
		},
	}
	result, err := r.Run(context.Background(), wf, "hello pipeline")
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "success" {
		t.Fatalf("expected success, got %s", result.Outcome)
	}
	scope := blackboard.Scope{Kind: blackboard.ScopeSession, ID: runID}
	e, err := mgr.Get(scope, "steps/analyze/output")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.Value, "agent output") {
		t.Fatalf("unexpected step output: %q", e.Value)
	}
	in, err := mgr.Get(scope, "input")
	if err != nil || in.Value != "hello pipeline" {
		t.Fatalf("input seed: %+v err=%v", in, err)
	}
}
