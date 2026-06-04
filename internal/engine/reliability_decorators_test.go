package engine

import (
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

func TestCircuitBreaker_OpensAfterFailures(t *testing.T) {
	ResetNodeCircuitBreakers()
	child := btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		ctx.Blackboard.Result = "fail"
		return -1
	})
	node := &evolution.SerializableNode{
		Type:       "CircuitBreaker",
		Name:       "TestCB",
		MaxRetries: 2,
		Metadata:   map[string]any{"cooldown_secs": float64(60)},
	}
	bb := &Blackboard{ChainState: make(map[string]any)}
	cmd := buildCircuitBreaker(node, child, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)

	for i := 0; i < 3; i++ {
		cmd.Run(ctx)
	}
	if cmd.Run(ctx) != -1 {
		t.Fatal("expected circuit open to reject")
	}
	if bb.ChainState["last_error_category"] == "" {
		t.Fatal("expected error category on blackboard")
	}
}

func TestTimeout_DecoratorBuilds(t *testing.T) {
	child := btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 1 })
	node := &evolution.SerializableNode{
		Type:      "Timeout",
		Name:      "TestTimeout",
		TimeoutMs: 1000,
	}
	cmd := buildTimeout(node, child)
	bb := &Blackboard{ChainState: make(map[string]any)}
	ctx := btcore.NewBTContext(t.Context(), bb)
	if cmd.Run(ctx) != 1 {
		t.Fatalf("expected success, got %d", cmd.Run(ctx))
	}
}

func TestBuildTree_WithTimeoutNode(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []evolution.SerializableNode{
			{
				Type:      "Timeout",
				Name:      "T",
				TimeoutMs: 5000,
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "MarkSuccessful"},
				},
			},
		},
	}
	bb := &Blackboard{ChainState: make(map[string]any)}
	cmd, err := BuildAndValidate(tree, bb)
	if err != nil {
		t.Fatal(err)
	}
	_ = cmd
	_ = time.Second
}
