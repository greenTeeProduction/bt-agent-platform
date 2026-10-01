package agent

import (
	"context"
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestRunEvidenceIncludesOuterQualityFailureAndOwner(t *testing.T) {
	store, err := evolution.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = registry.Create(Definition{Name: "owned", Tree: "goal:report", Metadata: map[string]string{"user": "alice"}, Quality: &QualitySpec{MinLength: 1000}})
	if err != nil {
		t.Fatal(err)
	}
	engine.RegisterAction("EvidenceOuterWork", func(ctx *btcore.BTContext[engine.Blackboard]) int {
		ctx.Blackboard.Result = `{"completed":true,"message":"real deterministic output too short for the owner contract"}`
		return 1
	})
	tree := &evolution.SerializableNode{Type: "Action", Name: "EvidenceOuterWork"}
	deps := &RunDeps{Registry: registry, RefStore: store, ResolveTree: func(_ string) *evolution.SerializableNode { return tree }}
	result, err := deps.RunOnce(context.Background(), "owned", "produce report", RunOptions{EnforceQuality: true, DisableBlackboard: true})
	if err == nil || result.Outcome != "failure" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	records, err := store.LoadAll()
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	r := records[0]
	if r.Outcome != evolution.Failure || r.User != "alice" || r.TreeName != "goal:report" || r.TreeVersion == "" || r.Result != result.Output || r.Error == "" {
		t.Fatalf("premature or unattributed record: %+v", r)
	}
}
