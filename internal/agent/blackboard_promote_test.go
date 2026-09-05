package agent

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/engine"
)

func TestPromoteRunToAgentScope(t *testing.T) {
	dir := t.TempDir()
	mgr := blackboard.DefaultManager()
	if err := mgr.EnablePersistence(dir); err != nil {
		t.Fatal(err)
	}
	d := &RunDeps{Blackboards: mgr}
	bb := &engine.Blackboard{
		RunID: "run_promote_test",
		BB:    blackboard.NewHandle(mgr, "run_promote_test", "sess_1", "demo-agent"),
	}
	d.promoteRunToAgentScope("demo-agent", bb, "do something", "final output text")

	scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: "demo-agent"}
	e, err := mgr.Get(scope, "runs/latest/output")
	if err != nil || e.Value != "final output text" {
		t.Fatalf("promote output: %+v err=%v", e, err)
	}
	runID, err := mgr.Get(scope, "runs/latest/run_id")
	if err != nil || runID.Value != "run_promote_test" {
		t.Fatalf("promote run_id: %+v err=%v", runID, err)
	}
}
