package engine

import (
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// Test-only actions with deterministic tick codes, registered once so every
// test in this file can build SerializableNode children that route through
// buildNode -> bb.actionForName like production trees do.
const (
	decoratorsTestSuccess = "__decoratorsTestSuccess"
	decoratorsTestFail    = "__decoratorsTestFail"
	decoratorsTestRunning = "__decoratorsTestRunning"
	decoratorsTestSeq     = "__decoratorsTestSeq"
)

func init() {
	RegisterAction(decoratorsTestSuccess, func(_ *btcore.BTContext[Blackboard]) int { return 1 })
	RegisterAction(decoratorsTestFail, func(_ *btcore.BTContext[Blackboard]) int { return -1 })
	RegisterAction(decoratorsTestRunning, func(_ *btcore.BTContext[Blackboard]) int { return 0 })
	// decoratorsTestSeq pops the next code off bb.ChainState["__seq"] on each
	// tick, letting a test script an exact sequence of child tick codes.
	RegisterAction(decoratorsTestSeq, func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		seq, _ := bb.ChainState["__seq"].([]int)
		if len(seq) == 0 {
			return -1
		}
		code := seq[0]
		bb.ChainState["__seq"] = seq[1:]
		return code
	})
}

func decoratorsTestNode(nodeType, name string, children ...evolution.SerializableNode) *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: nodeType, Name: name, Children: children}
}

func actionChild(actionName string) evolution.SerializableNode {
	return evolution.SerializableNode{Type: "Action", Name: actionName}
}

func newDecoratorsBB() *Blackboard {
	return &Blackboard{ChainState: make(map[string]any)}
}

// --- BuildTimeout ---

func TestBuildTimeout_NoChildren(t *testing.T) {
	node := decoratorsTestNode("Timeout", "T")
	bb := newDecoratorsBB()
	cmd := BuildTimeout(node, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)
	if code := cmd.Run(ctx); code != -1 {
		t.Fatalf("no children: got %d, want -1", code)
	}
}

func TestBuildTimeout_PassesThroughChildResult(t *testing.T) {
	tests := []struct {
		name   string
		action string
		want   int
	}{
		{"success", decoratorsTestSuccess, 1},
		{"failure", decoratorsTestFail, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := decoratorsTestNode("Timeout", "T", actionChild(tt.action))
			node.TimeoutMs = 1000
			bb := newDecoratorsBB()
			cmd := BuildTimeout(node, bb)
			ctx := btcore.NewBTContext(t.Context(), bb)
			if code := cmd.Run(ctx); code != tt.want {
				t.Fatalf("got %d, want %d", code, tt.want)
			}
		})
	}
}

func TestBuildTimeout_UsesNodeTimeoutMsField(t *testing.T) {
	node := decoratorsTestNode("Timeout", "T", actionChild(decoratorsTestRunning))
	node.TimeoutMs = 100 // 100ms
	bb := newDecoratorsBB()
	cmd := BuildTimeout(node, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)
	now := time.Now()
	ctx.Now = func() time.Time { return now }

	if code := cmd.Run(ctx); code != 0 {
		t.Fatalf("first tick: got %d, want 0 (running)", code)
	}
	ctx.Now = func() time.Time { return now.Add(200 * time.Millisecond) }
	if code := cmd.Run(ctx); code != -1 {
		t.Fatalf("after deadline: got %d, want -1 (timed out)", code)
	}
}

func TestBuildTimeout_FallsBackToMetadataTimeoutMsWhenFieldUnset(t *testing.T) {
	node := decoratorsTestNode("Timeout", "T", actionChild(decoratorsTestRunning))
	node.Metadata = map[string]any{"timeout_ms": float64(100)}
	bb := newDecoratorsBB()
	cmd := BuildTimeout(node, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)
	now := time.Now()
	ctx.Now = func() time.Time { return now }

	if code := cmd.Run(ctx); code != 0 {
		t.Fatalf("first tick: got %d, want 0 (running)", code)
	}
	ctx.Now = func() time.Time { return now.Add(200 * time.Millisecond) }
	if code := cmd.Run(ctx); code != -1 {
		t.Fatalf("after deadline: got %d, want -1 (timed out)", code)
	}
}

func TestBuildTimeout_DefaultsTo30SecondsWhenUnset(t *testing.T) {
	node := decoratorsTestNode("Timeout", "T", actionChild(decoratorsTestRunning))
	bb := newDecoratorsBB()
	cmd := BuildTimeout(node, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)
	now := time.Now()
	ctx.Now = func() time.Time { return now }

	if code := cmd.Run(ctx); code != 0 {
		t.Fatalf("first tick: got %d, want 0 (running)", code)
	}
	ctx.Now = func() time.Time { return now.Add(29 * time.Second) }
	if code := cmd.Run(ctx); code != 0 {
		t.Fatalf("before 30s default deadline: got %d, want 0 (still running)", code)
	}
	ctx.Now = func() time.Time { return now.Add(31 * time.Second) }
	if code := cmd.Run(ctx); code != -1 {
		t.Fatalf("after 30s default deadline: got %d, want -1 (timed out)", code)
	}
}

// --- BuildInverter ---

func TestBuildInverter_NoChildren(t *testing.T) {
	node := decoratorsTestNode("Inverter", "I")
	bb := newDecoratorsBB()
	cmd := BuildInverter(node, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)
	if code := cmd.Run(ctx); code != -1 {
		t.Fatalf("no children: got %d, want -1", code)
	}
}

func TestBuildInverter_InvertsChildResult(t *testing.T) {
	tests := []struct {
		name   string
		action string
		want   int
	}{
		{"success becomes failure", decoratorsTestSuccess, -1},
		{"failure becomes success", decoratorsTestFail, 1},
		{"running stays running", decoratorsTestRunning, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := decoratorsTestNode("Inverter", "I", actionChild(tt.action))
			bb := newDecoratorsBB()
			cmd := BuildInverter(node, bb)
			ctx := btcore.NewBTContext(t.Context(), bb)
			if code := cmd.Run(ctx); code != tt.want {
				t.Fatalf("got %d, want %d", code, tt.want)
			}
		})
	}
}

// --- BuildSucceeder ---

func TestBuildSucceeder_NoChildren(t *testing.T) {
	node := decoratorsTestNode("Succeeder", "S")
	bb := newDecoratorsBB()
	cmd := BuildSucceeder(node, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)
	if code := cmd.Run(ctx); code != 1 {
		t.Fatalf("no children: got %d, want 1", code)
	}
}

func TestBuildSucceeder_MapsResultToSuccessUnlessRunning(t *testing.T) {
	tests := []struct {
		name   string
		action string
		want   int
	}{
		{"success stays success", decoratorsTestSuccess, 1},
		{"failure becomes success", decoratorsTestFail, 1},
		{"running stays running", decoratorsTestRunning, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := decoratorsTestNode("Succeeder", "S", actionChild(tt.action))
			bb := newDecoratorsBB()
			cmd := BuildSucceeder(node, bb)
			ctx := btcore.NewBTContext(t.Context(), bb)
			if code := cmd.Run(ctx); code != tt.want {
				t.Fatalf("got %d, want %d", code, tt.want)
			}
		})
	}
}

// --- BuildRepeater ---

func TestBuildRepeater_NoChildren(t *testing.T) {
	node := decoratorsTestNode("Repeater", "R")
	bb := newDecoratorsBB()
	cmd := BuildRepeater(node, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)
	if code := cmd.Run(ctx); code != -1 {
		t.Fatalf("no children: got %d, want -1", code)
	}
}

func TestBuildRepeater_DefaultsToOneRepeatWhenMaxRetriesUnset(t *testing.T) {
	node := decoratorsTestNode("Repeater", "R", actionChild(decoratorsTestSuccess))
	// MaxRetries left at zero value.
	bb := newDecoratorsBB()
	cmd := BuildRepeater(node, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)
	if code := cmd.Run(ctx); code != 1 {
		t.Fatalf("single success tick with default times=1: got %d, want 1", code)
	}
}

func TestBuildRepeater_RepeatsChildUntilMaxRetriesThenSucceeds(t *testing.T) {
	node := decoratorsTestNode("Repeater", "R", actionChild(decoratorsTestSuccess))
	node.MaxRetries = 3
	bb := newDecoratorsBB()
	cmd := BuildRepeater(node, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)

	want := []int{0, 0, 1}
	for i, w := range want {
		if code := cmd.Run(ctx); code != w {
			t.Fatalf("tick %d: got %d, want %d", i, code, w)
		}
	}
}

func TestBuildRepeater_FailsImmediatelyOnChildFailure(t *testing.T) {
	node := decoratorsTestNode("Repeater", "R", actionChild(decoratorsTestFail))
	node.MaxRetries = 5
	bb := newDecoratorsBB()
	cmd := BuildRepeater(node, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)
	if code := cmd.Run(ctx); code != -1 {
		t.Fatalf("got %d, want -1", code)
	}
}

// --- BuildRunner ---

func TestBuildRunner_NoChildren(t *testing.T) {
	node := decoratorsTestNode("Runner", "Rn")
	bb := newDecoratorsBB()
	cmd := BuildRunner(node, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)
	if code := cmd.Run(ctx); code != -1 {
		t.Fatalf("no children: got %d, want -1", code)
	}
}

func TestBuildRunner_PassesThroughChildResultUnchanged(t *testing.T) {
	tests := []struct {
		name   string
		action string
		want   int
	}{
		{"success", decoratorsTestSuccess, 1},
		{"failure", decoratorsTestFail, -1},
		{"running", decoratorsTestRunning, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := decoratorsTestNode("Runner", "Rn", actionChild(tt.action))
			bb := newDecoratorsBB()
			cmd := BuildRunner(node, bb)
			ctx := btcore.NewBTContext(t.Context(), bb)
			if code := cmd.Run(ctx); code != tt.want {
				t.Fatalf("got %d, want %d", code, tt.want)
			}
		})
	}
}

// --- BuildCircuitBreaker ---

func TestBuildCircuitBreaker_NoChildren(t *testing.T) {
	node := decoratorsTestNode("CircuitBreaker", "CB")
	bb := newDecoratorsBB()
	cmd := BuildCircuitBreaker(node, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)
	if code := cmd.Run(ctx); code != -1 {
		t.Fatalf("no children: got %d, want -1", code)
	}
}

func TestBuildCircuitBreaker_OpensAfterDefaultThreeConsecutiveFailures(t *testing.T) {
	node := decoratorsTestNode("CircuitBreaker", "CB", actionChild(decoratorsTestFail))
	bb := newDecoratorsBB()
	cmd := BuildCircuitBreaker(node, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)

	for i := range 2 {
		if code := cmd.Run(ctx); code != -1 {
			t.Fatalf("failure %d: got %d, want -1", i, code)
		}
	}
	if _, open := bb.ChainState["cb:CB_open"]; open {
		t.Fatal("circuit opened before reaching default threshold of 3")
	}
	// Third consecutive failure trips the breaker.
	if code := cmd.Run(ctx); code != -1 {
		t.Fatalf("tripping failure: got %d, want -1", code)
	}
	if _, open := bb.ChainState["cb:CB_open"]; !open {
		t.Fatal("expected circuit to be open after 3 consecutive failures")
	}
}

func TestBuildCircuitBreaker_ThresholdOverrideFromMetadata(t *testing.T) {
	node := decoratorsTestNode("CircuitBreaker", "CB", actionChild(decoratorsTestFail))
	node.Metadata = map[string]any{"threshold": float64(2)}
	bb := newDecoratorsBB()
	cmd := BuildCircuitBreaker(node, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)

	cmd.Run(ctx)
	if _, open := bb.ChainState["cb:CB_open"]; open {
		t.Fatal("circuit opened after only 1 failure with threshold=2")
	}
	cmd.Run(ctx)
	if _, open := bb.ChainState["cb:CB_open"]; !open {
		t.Fatal("expected circuit open after 2 failures with threshold=2")
	}
}

func TestBuildCircuitBreaker_OpenCircuitShortCircuitsAndSetsOutcome(t *testing.T) {
	node := decoratorsTestNode("CircuitBreaker", "CB", actionChild(decoratorsTestFail))
	node.Metadata = map[string]any{"threshold": float64(1)}
	bb := newDecoratorsBB()
	cmd := BuildCircuitBreaker(node, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)

	cmd.Run(ctx) // trips the breaker
	code := cmd.Run(ctx)
	if code != 0 {
		t.Fatalf("open circuit tick: got %d, want 0", code)
	}
	if bb.Outcome != "circuit_open" {
		t.Fatalf("Outcome = %q, want %q", bb.Outcome, "circuit_open")
	}
}

func TestBuildCircuitBreaker_SuccessResetsFailureCounter(t *testing.T) {
	node := decoratorsTestNode("CircuitBreaker", "CB", actionChild(decoratorsTestSeq))
	node.Metadata = map[string]any{"threshold": float64(2)}
	bb := newDecoratorsBB()
	bb.ChainState["__seq"] = []int{-1, 1, -1, -1}
	cmd := BuildCircuitBreaker(node, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)

	cmd.Run(ctx) // fail 1/2
	cmd.Run(ctx) // success resets counter
	cmd.Run(ctx) // fail 1/2 again (counter was reset)
	if _, open := bb.ChainState["cb:CB_open"]; open {
		t.Fatal("circuit should not be open: success should have reset the failure streak")
	}
	cmd.Run(ctx) // fail 2/2
	if _, open := bb.ChainState["cb:CB_open"]; !open {
		t.Fatal("expected circuit open after 2 consecutive failures post-reset")
	}
}

// Characterizes intended circuit-breaker semantics: a child that is merely
// still running (tick code 0) has neither succeeded nor failed, so it must
// not clear an in-progress failure streak. This mirrors the sibling
// circuitBreakerCmd.Run in reliability_decorators.go, which only reacts to
// code == 1 (success) or code < 0 (failure) and leaves code == 0 untouched.
func TestBuildCircuitBreaker_RunningChildDoesNotResetFailureCount(t *testing.T) {
	node := decoratorsTestNode("CircuitBreaker", "CB", actionChild(decoratorsTestSeq))
	node.Metadata = map[string]any{"threshold": float64(2)}
	bb := newDecoratorsBB()
	bb.ChainState["__seq"] = []int{-1, 0, -1}
	cmd := BuildCircuitBreaker(node, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)

	cmd.Run(ctx) // fail 1/2
	cmd.Run(ctx) // running: must not clear the failure streak
	cmd.Run(ctx) // fail 2/2 -> should trip the breaker
	if _, open := bb.ChainState["cb:CB_open"]; !open {
		t.Fatal("expected circuit open: a running tick must not reset the failure streak")
	}
}

func TestBuildCircuitBreaker_CooldownOverrideFromMetadataReopensAfterExpiry(t *testing.T) {
	node := decoratorsTestNode("CircuitBreaker", "CB", actionChild(decoratorsTestSeq))
	node.Metadata = map[string]any{"threshold": float64(1), "cooldown_ms": float64(5)}
	bb := newDecoratorsBB()
	bb.ChainState["__seq"] = []int{-1, 1}
	cmd := BuildCircuitBreaker(node, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)

	cmd.Run(ctx) // trips the breaker, 5ms cooldown
	if code := cmd.Run(ctx); code != 0 {
		t.Fatalf("immediately after trip: got %d, want 0 (still open)", code)
	}
	time.Sleep(15 * time.Millisecond)
	if code := cmd.Run(ctx); code != 1 {
		t.Fatalf("after cooldown expiry: got %d, want 1 (child ran and succeeded)", code)
	}
}

func TestBuildCircuitBreaker_StateIsNamespacedPerNodeName(t *testing.T) {
	nodeA := decoratorsTestNode("CircuitBreaker", "A", actionChild(decoratorsTestFail))
	nodeA.Metadata = map[string]any{"threshold": float64(1)}
	nodeB := decoratorsTestNode("CircuitBreaker", "B", actionChild(decoratorsTestFail))
	nodeB.Metadata = map[string]any{"threshold": float64(1)}
	bb := newDecoratorsBB()
	cmdA := BuildCircuitBreaker(nodeA, bb)
	cmdB := BuildCircuitBreaker(nodeB, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)

	cmdA.Run(ctx)
	if _, open := bb.ChainState["cb:A_open"]; !open {
		t.Fatal("expected node A's circuit to be open")
	}
	if _, open := bb.ChainState["cb:B_open"]; open {
		t.Fatal("node B's circuit should be unaffected by node A's failures")
	}
	if code := cmdB.Run(ctx); code != -1 {
		t.Fatalf("node B first failure: got %d, want -1 (not yet open)", code)
	}
}
