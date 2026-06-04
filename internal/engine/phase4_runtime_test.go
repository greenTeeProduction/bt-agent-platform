package engine

import (
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestBuildParallel_AllMustSucceed(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Parallel",
		Name: "P",
		Metadata: map[string]any{
			"success_policy": "all",
		},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MarkSuccessful"},
			{
				Type:     "Inverter",
				Children: []evolution.SerializableNode{{Type: "Action", Name: "MarkSuccessful"}},
			},
		},
	}
	bb := &Blackboard{ChainState: make(map[string]any)}
	cmd, err := BuildAndValidate(tree, bb)
	if err != nil {
		t.Fatal(err)
	}
	code := cmd.Run(btcore.NewBTContext(t.Context(), bb))
	if code != -1 {
		t.Fatalf("expected failure when one child fails, got %d", code)
	}
}

func TestBuildParallel_OneSuccess(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Parallel",
		Name: "P",
		Metadata: map[string]any{
			"success_policy": "one",
		},
		Children: []evolution.SerializableNode{
			{
				Type:     "Inverter",
				Children: []evolution.SerializableNode{{Type: "Action", Name: "MarkSuccessful"}},
			},
			{Type: "Action", Name: "MarkSuccessful"},
		},
	}
	bb := &Blackboard{ChainState: make(map[string]any)}
	cmd, err := BuildAndValidate(tree, bb)
	if err != nil {
		t.Fatal(err)
	}
	code := cmd.Run(btcore.NewBTContext(t.Context(), bb))
	if code != 1 {
		t.Fatalf("expected success with one child ok, got %d", code)
	}
}

func TestBuildBudget_MaxTicks(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type:     "Budget",
		Name:     "B",
		Metadata: map[string]any{"max_ticks": float64(2)},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MarkSuccessful"},
		},
	}
	bb := &Blackboard{ChainState: make(map[string]any)}
	cmd := BuildBudget(tree, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)
	if c := cmd.Run(ctx); c != 1 {
		t.Fatalf("run 1: got %d", c)
	}
	if c := cmd.Run(ctx); c != 1 {
		t.Fatalf("run 2: got %d", c)
	}
	if c := cmd.Run(ctx); c != -1 {
		t.Fatalf("run 3 should exhaust budget, got %d ticks=%d", c, bb.TreeTicks)
	}
}

func TestBuildRateLimit_ThrottlesSecondTick(t *testing.T) {
	ResetRateLimiters()
	tree := &evolution.SerializableNode{
		Type:     "RateLimit",
		Name:     "RLTest",
		Metadata: map[string]any{"interval_ms": float64(500)},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MarkSuccessful"},
		},
	}
	bb := &Blackboard{ChainState: make(map[string]any)}
	cmd := BuildRateLimit(tree, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)
	if c := cmd.Run(ctx); c != 1 {
		t.Fatalf("first run expected success, got %d", c)
	}
	if c := cmd.Run(ctx); c != 0 {
		t.Fatalf("second immediate run expected running/throttled, got %d", c)
	}
	time.Sleep(600 * time.Millisecond)
	if c := cmd.Run(ctx); c != 1 {
		t.Fatalf("after interval expected success, got %d", c)
	}
}

func TestBuildQualityGate_RejectsLowQuality(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "QualityGate",
		Name: "QG",
		Children: []evolution.SerializableNode{
			{
				Type: "Action",
				Name: "SetResult",
				// use MarkSuccessful but set bad result via custom - use action that sets short result
			},
		},
	}
	// Short result fails validateOutputQuality
	bb := &Blackboard{Task: "t", Result: "x", ChainState: make(map[string]any)}
	// Override: run tree with Action setting empty quality
	tree.Children[0] = evolution.SerializableNode{Type: "Action", Name: "MarkSuccessful"}
	bb.Result = ""
	cmd := BuildQualityGate(tree, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)
	code := cmd.Run(ctx)
	if code != -1 {
		t.Fatalf("expected quality failure without recovery, got %d", code)
	}
}

func TestBuildInverter(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type:     "Inverter",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "MarkSuccessful"}},
	}
	bb := &Blackboard{}
	cmd := BuildInverter(tree, bb)
	if code := cmd.Run(btcore.NewBTContext(t.Context(), bb)); code != -1 {
		t.Fatalf("inverter should flip success to failure, got %d", code)
	}
}

func TestBuildMonitor_IncrementsCounter(t *testing.T) {
	before := MonitorTerminalCount.Load()
	tree := &evolution.SerializableNode{
		Type:     "Monitor",
		Name:     "M",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "MarkSuccessful"}},
	}
	bb := &Blackboard{ChainState: make(map[string]any)}
	cmd := BuildMonitor(tree, bb)
	_ = cmd.Run(btcore.NewBTContext(t.Context(), bb))
	if MonitorTerminalCount.Load() != before+1 {
		t.Fatalf("expected monitor counter increment")
	}
}

func TestTypedEdges_GuardSkipsChild(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Seq",
		Edges: []evolution.TypedEdge{
			{Type: evolution.EdgeGuard, ChildIndex: 1, Condition: "allow_step"},
		},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MarkSuccessful"},
			{
				Type:     "Inverter",
				Children: []evolution.SerializableNode{{Type: "Action", Name: "MarkSuccessful"}},
			},
		},
	}
	bb := &Blackboard{ChainState: map[string]any{"allow_step": false}}
	cmd, err := BuildAndValidate(tree, bb)
	if err != nil {
		t.Fatal(err)
	}
	code := cmd.Run(btcore.NewBTContext(t.Context(), bb))
	if code != 1 {
		t.Fatalf("guard should skip failing child, got %d", code)
	}
}

func TestUnknownNodeType_FailsClosed(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "NotARealType", Name: "X"}
	bb := &Blackboard{}
	cmd := buildNode(tree, bb, "")
	code := cmd.Run(btcore.NewBTContext(t.Context(), bb))
	if code != -1 {
		t.Fatalf("unknown type should fail, got %d", code)
	}
}
