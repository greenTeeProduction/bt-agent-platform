package a2a

import (
	"context"
	"testing"

	a2atypes "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestReviewA2ACallerCancellationReachesTree(t *testing.T) {
	e := executorForAgent(t, "review-agent", "review-tree")
	called := false
	actionName := "review_a2a_cancel" + t.TempDir()
	engine.RegisterAction(actionName, func(ctx *btcore.BTContext[engine.Blackboard]) int { called = true; return 1 })
	e.TreeMap = map[string]*evolution.SerializableNode{"review-agent": {Type: "Action", Name: actionName}}
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), agentNameKey{}, "review-agent"))
	cancel()
	ec := &a2asrv.ExecutorContext{Message: a2atypes.NewMessage(a2atypes.MessageRoleUser, a2atypes.NewTextPart("task"))}
	for _, err := range e.Execute(ctx, ec) {
		if err != nil {
			break
		}
	}
	if called {
		t.Fatal("cancelled A2A caller executed tree side effects")
	}
}
