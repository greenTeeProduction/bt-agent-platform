package agent

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestReviewRunScopeReleasedAfterConsumers(t *testing.T) {
	var handle *blackboard.Handle
	actionName := "review_scope" + t.TempDir()
	engine.RegisterAction(actionName, func(ctx *btcore.BTContext[engine.Blackboard]) int {
		handle = ctx.Blackboard.BB
		if err := handle.Set("payload", "value", "", "text"); err != nil {
			t.Error(err)
		}
		return 1
	})
	d := &RunDeps{Blackboards: blackboard.DefaultManager(), ResolveTree: func(string) *evolution.SerializableNode {
		return &evolution.SerializableNode{Type: "Action", Name: actionName}
	}}
	if _, err := d.RunOnce(t.Context(), "release", "task", RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if handle == nil {
		t.Fatal("no run scope")
	}
	if _, err := handle.Get("payload"); err == nil {
		t.Fatal("completed run retained payload")
	}
}
