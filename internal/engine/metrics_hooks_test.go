package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// Characterization tests for the metrics hooks (metrics_hooks.go).
//
// The file itself declares two injection points and no logic: internal/dashboard
// fills them in from its init() because engine cannot import dashboard (cycle via
// startup). So what there is to pin is the contract the engine's own call sites
// honor — a nil hook records nothing, and a wired hook sees exactly the arguments
// each var documents. The two call sites are observedCommand.Run
// (observability.go — every node tick of a built tree) and fitnessProbeAction
// (ops_actions.go — the FitnessProbe action).
//
// Shared helpers (newTestBlackboard, newTestBTContext) live in mem_nodes_test.go.

// nodeTick is one RecordNodeTickFn invocation, in argument order.
type nodeTick struct {
	nodeType   string
	nodeName   string
	parentName string
	blockID    string
	status     string
	durationMs int64
}

// captureNodeTicks swaps RecordNodeTickFn for a recorder and restores the
// previous value at test end. The previous value is nil in this test binary —
// internal/dashboard's init() is what wires a real one — so the swap doubles as
// proof the hook is settable at runtime.
func captureNodeTicks(t *testing.T) *[]nodeTick {
	t.Helper()
	var got []nodeTick
	prev := RecordNodeTickFn
	RecordNodeTickFn = func(nodeType, nodeName, parentName, blockID, status string, durationMs int64) {
		got = append(got, nodeTick{nodeType, nodeName, parentName, blockID, status, durationMs})
	}
	t.Cleanup(func() { RecordNodeTickFn = prev })
	return &got
}

// blockFitness is one RecordBlockFitnessFn invocation, in argument order.
type blockFitness struct {
	blockID string
	agent   string
	score   float64
}

// captureBlockFitness swaps RecordBlockFitnessFn for a recorder and restores the
// previous value at test end.
func captureBlockFitness(t *testing.T) *[]blockFitness {
	t.Helper()
	var got []blockFitness
	prev := RecordBlockFitnessFn
	RecordBlockFitnessFn = func(blockID, agent string, score float64) {
		got = append(got, blockFitness{blockID, agent, score})
	}
	t.Cleanup(func() { RecordBlockFitnessFn = prev })
	return &got
}

// constCommand is a leaf that always returns code.
func constCommand(code int) btcore.Command[Blackboard] {
	return btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return code })
}

func TestMetricsHooks_NilHooksRecordNothingAndCallSitesStillWork(t *testing.T) {
	// "Nil hooks mean don't record" — pinned by clearing both, because they are
	// NOT nil in this test binary: engine itself never imports internal/dashboard
	// (that is the cycle the file exists to break), but the engine *test* binary
	// reaches it transitively through internal/domains -> internal/startup, so
	// dashboard's init() has already wired the real recorders by the time any
	// test runs.
	prevTick, prevFitness := RecordNodeTickFn, RecordBlockFitnessFn
	RecordNodeTickFn, RecordBlockFitnessFn = nil, nil
	t.Cleanup(func() { RecordNodeTickFn, RecordBlockFitnessFn = prevTick, prevFitness })

	// Nil hooks must be skipped, not called: both call sites otherwise keep
	// their normal effects.
	t.Run("ObservedNodeTick", func(t *testing.T) {
		node := &evolution.SerializableNode{Type: "Action", Name: "MetricsHookCharNilTick"}
		cmd := observeNode(node, "Parent", constCommand(1))
		bb := newTestBlackboard()
		if got := cmd.Run(newTestBTContext(bb)); got != 1 {
			t.Fatalf("Run() = %d, want 1 (SUCCESS) with no hook wired", got)
		}
	})

	t.Run("FitnessProbeAction", func(t *testing.T) {
		bb := newTestBlackboard()
		bb.Outcome = "success"
		if got := fitnessProbeAction(newTestBTContext(bb)); got != 1 {
			t.Fatalf("fitnessProbeAction() = %d, want 1 (SUCCESS) with no hook wired", got)
		}
		if got, ok := bb.ChainState["block_fitness"]; !ok || got != float64(75) {
			t.Errorf("ChainState[block_fitness] = %v (present=%t), want 75 — the blackboard write is independent of the hook", got, ok)
		}
	})
}

func TestRecordNodeTickFn_ReceivesEveryTickField(t *testing.T) {
	tests := []struct {
		name     string
		node     *evolution.SerializableNode
		parent   string
		code     int
		wantTick nodeTick
	}{
		{
			name:     "AllFieldsPopulated",
			node:     &evolution.SerializableNode{Type: "Action", Name: "Probe", Metadata: map[string]any{"block_id": "core:pre_gate"}},
			parent:   "Root",
			code:     1,
			wantTick: nodeTick{nodeType: "Action", nodeName: "Probe", parentName: "Root", blockID: "core:pre_gate", status: "success"},
		},
		{
			name:     "FailureStatus",
			node:     &evolution.SerializableNode{Type: "Action", Name: "Probe"},
			parent:   "Root",
			code:     -1,
			wantTick: nodeTick{nodeType: "Action", nodeName: "Probe", parentName: "Root", status: "failure"},
		},
		{
			name:     "RunningStatus",
			node:     &evolution.SerializableNode{Type: "Action", Name: "Probe"},
			parent:   "Root",
			code:     0,
			wantTick: nodeTick{nodeType: "Action", nodeName: "Probe", parentName: "Root", status: "running"},
		},
		// Status is classified by sign, not by exact code (tickStatusLabel).
		{
			name:     "PositiveCodeAboveOneIsSuccess",
			node:     &evolution.SerializableNode{Type: "Sequence", Name: "Probe"},
			code:     7,
			wantTick: nodeTick{nodeType: "Sequence", nodeName: "Probe", status: "success"},
		},
		{
			name:     "NegativeCodeBelowMinusOneIsFailure",
			node:     &evolution.SerializableNode{Type: "Sequence", Name: "Probe"},
			code:     -9,
			wantTick: nodeTick{nodeType: "Sequence", nodeName: "Probe", status: "failure"},
		},
		// observeNode falls back to the node type when the node has no name, so
		// the metric label is never empty for a typed node.
		{
			name:     "UnnamedNodeReportsItsType",
			node:     &evolution.SerializableNode{Type: "Selector"},
			parent:   "Root",
			code:     1,
			wantTick: nodeTick{nodeType: "Selector", nodeName: "Selector", parentName: "Root", status: "success"},
		},
		{
			name:     "RootNodeHasEmptyParent",
			node:     &evolution.SerializableNode{Type: "Sequence", Name: "Root"},
			code:     1,
			wantTick: nodeTick{nodeType: "Sequence", nodeName: "Root", status: "success"},
		},
		// block_id is read out of node metadata with a string type assertion;
		// anything else leaves it empty, which the dashboard renders as "no
		// block label" rather than a bogus one.
		{
			name:     "NonStringBlockIDIsDropped",
			node:     &evolution.SerializableNode{Type: "Action", Name: "Probe", Metadata: map[string]any{"block_id": float64(7)}},
			code:     1,
			wantTick: nodeTick{nodeType: "Action", nodeName: "Probe", status: "success"},
		},
		{
			name:     "MetadataWithoutBlockIDKey",
			node:     &evolution.SerializableNode{Type: "Action", Name: "Probe", Metadata: map[string]any{"other": "x"}},
			code:     1,
			wantTick: nodeTick{nodeType: "Action", nodeName: "Probe", status: "success"},
		},
		// An untyped, unnamed node still records — with empty labels, verbatim.
		{
			name:     "UntypedUnnamedNode",
			node:     &evolution.SerializableNode{},
			code:     1,
			wantTick: nodeTick{status: "success"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ticks := captureNodeTicks(t)
			cmd := observeNode(tc.node, tc.parent, constCommand(tc.code))

			if got := cmd.Run(newTestBTContext(newTestBlackboard())); got != tc.code {
				t.Fatalf("Run() = %d, want %d passed through unchanged", got, tc.code)
			}
			if len(*ticks) != 1 {
				t.Fatalf("recorded %d ticks, want exactly 1: %+v", len(*ticks), *ticks)
			}
			got := (*ticks)[0]
			if got.durationMs < 0 {
				t.Errorf("durationMs = %d, want >= 0", got.durationMs)
			}
			got.durationMs = 0 // wall-clock, compared separately
			if got != tc.wantTick {
				t.Errorf("tick = %+v, want %+v", got, tc.wantTick)
			}
		})
	}
}

func TestRecordNodeTickFn_NotRecordedWhenObserveNodeDeclinesToWrap(t *testing.T) {
	// observeNode passes the command straight through when it has nothing to
	// label, so those ticks never reach the hook.
	tests := []struct {
		name  string
		node  *evolution.SerializableNode
		child btcore.Command[Blackboard]
		// wantPassthrough: observeNode must return the child identity.
		wantPassthrough bool
	}{
		{name: "NilNode", node: nil, child: constCommand(1), wantPassthrough: true},
		{name: "NilChild", node: &evolution.SerializableNode{Type: "Action", Name: "Probe"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ticks := captureNodeTicks(t)
			cmd := observeNode(tc.node, "Root", tc.child)

			if !tc.wantPassthrough {
				if cmd != nil {
					t.Fatalf("observeNode() = %v, want the nil child returned unwrapped", cmd)
				}
				return
			}
			if cmd != tc.child {
				t.Fatalf("observeNode() returned a wrapper, want the child unwrapped")
			}
			if got := cmd.Run(newTestBTContext(newTestBlackboard())); got != 1 {
				t.Fatalf("Run() = %d, want 1", got)
			}
			if len(*ticks) != 0 {
				t.Errorf("recorded %d ticks, want 0 for an unwrapped command: %+v", len(*ticks), *ticks)
			}
		})
	}
}

func TestRecordNodeTickFn_RecordsEveryNodeOfABuiltTreeChildrenFirst(t *testing.T) {
	RegisterAction("MetricsHookCharTreeOK", func(_ *btcore.BTContext[Blackboard]) int { return 1 })
	RegisterAction("MetricsHookCharTreeFail", func(_ *btcore.BTContext[Blackboard]) int { return -1 })

	ticks := captureNodeTicks(t)
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "MetricsHookCharRoot",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MetricsHookCharTreeOK"},
			{Type: "Action", Name: "MetricsHookCharTreeFail"},
		},
	}
	bb := newTestBlackboard()
	cmd, err := BuildAndValidate(tree, bb)
	if err != nil {
		t.Fatalf("BuildAndValidate() error = %v", err)
	}
	if got := cmd.Run(newTestBTContext(bb)); got != -1 {
		t.Fatalf("Run() = %d, want -1 (the sequence fails on its second child)", got)
	}

	// Each node records once, and a parent records only after its children
	// return — its duration covers the whole subtree.
	want := []nodeTick{
		{nodeType: "Action", nodeName: "MetricsHookCharTreeOK", parentName: "MetricsHookCharRoot", status: "success"},
		{nodeType: "Action", nodeName: "MetricsHookCharTreeFail", parentName: "MetricsHookCharRoot", status: "failure"},
		{nodeType: "Sequence", nodeName: "MetricsHookCharRoot", status: "failure"},
	}
	if len(*ticks) != len(want) {
		t.Fatalf("recorded %d ticks, want %d: %+v", len(*ticks), len(want), *ticks)
	}
	for i, w := range want {
		got := (*ticks)[i]
		got.durationMs = 0
		if got != w {
			t.Errorf("tick %d = %+v, want %+v", i, got, w)
		}
	}
}

func TestRecordNodeTickFn_UnwiringMidRunStopsRecording(t *testing.T) {
	ticks := captureNodeTicks(t)
	node := &evolution.SerializableNode{Type: "Action", Name: "MetricsHookCharUnwire"}
	cmd := observeNode(node, "Root", constCommand(1))
	ctx := newTestBTContext(newTestBlackboard())

	if got := cmd.Run(ctx); got != 1 {
		t.Fatalf("Run() = %d, want 1", got)
	}
	if len(*ticks) != 1 {
		t.Fatalf("recorded %d ticks after the first run, want 1", len(*ticks))
	}

	// Hooks are plain vars, so clearing one takes effect on the next tick of an
	// already-built tree — the wrapper reads the var per tick, not at build time.
	RecordNodeTickFn = nil
	if got := cmd.Run(ctx); got != 1 {
		t.Fatalf("Run() = %d after unwiring, want 1", got)
	}
	if len(*ticks) != 1 {
		t.Errorf("recorded %d ticks after unwiring, want the count frozen at 1", len(*ticks))
	}
}

func TestRecordBlockFitnessFn_ReceivesProbeArgs(t *testing.T) {
	tests := []struct {
		name      string
		outcome   string
		quality   float64
		agentName any // written to ChainState["agent_name"]; nil => key absent
		want      blockFitness
	}{
		// FitnessProbe is not block-scoped: every probe reports under the
		// literal block id "probe" (the dashboard drops empty ids entirely).
		{name: "SuccessWithoutQualityScore", outcome: "success", want: blockFitness{blockID: "probe", score: 75}},
		{name: "CompletedCountsAsSuccess", outcome: "completed", want: blockFitness{blockID: "probe", score: 75}},
		{name: "FailureWithoutQualityScore", outcome: "failure", want: blockFitness{blockID: "probe", score: 25}},
		{name: "EmptyOutcomeIsNotSuccess", want: blockFitness{blockID: "probe", score: 25}},
		// A positive quality score wins over the success/failure default, on a
		// 0-1 -> 0-100 scale, clamped at 100.
		{name: "QualityScoreScaledToPercent", outcome: "success", quality: 0.9, want: blockFitness{blockID: "probe", score: 90}},
		{name: "QualityScoreOutranksFailureDefault", outcome: "failure", quality: 0.4, want: blockFitness{blockID: "probe", score: 40}},
		{name: "QualityScoreClampedAtHundred", outcome: "success", quality: 1.5, want: blockFitness{blockID: "probe", score: 100}},
		{name: "NegativeQualityScoreFallsBackToDefault", outcome: "success", quality: -0.5, want: blockFitness{blockID: "probe", score: 75}},
		// The agent label comes from chain state; a missing or non-string value
		// is reported as empty and relabeled "default" downstream.
		{name: "AgentNameFromChainState", outcome: "success", agentName: "researcher", want: blockFitness{blockID: "probe", agent: "researcher", score: 75}},
		{name: "NonStringAgentNameIsDropped", outcome: "success", agentName: 42, want: blockFitness{blockID: "probe", score: 75}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fitness := captureBlockFitness(t)
			bb := newTestBlackboard()
			bb.Outcome = tc.outcome
			bb.QualityScore = tc.quality
			if tc.agentName != nil {
				bb.ChainState["agent_name"] = tc.agentName
			}

			if got := fitnessProbeAction(newTestBTContext(bb)); got != 1 {
				t.Fatalf("fitnessProbeAction() = %d, want 1 (SUCCESS — the probe never fails)", got)
			}
			if len(*fitness) != 1 {
				t.Fatalf("recorded %d fitness values, want exactly 1: %+v", len(*fitness), *fitness)
			}
			if got := (*fitness)[0]; got != tc.want {
				t.Errorf("fitness = %+v, want %+v", got, tc.want)
			}
			// The hook always mirrors what the probe wrote to the blackboard.
			if got, ok := bb.ChainState["block_fitness"]; !ok || got != tc.want.score {
				t.Errorf("ChainState[block_fitness] = %v (present=%t), want %v", got, ok, tc.want.score)
			}
		})
	}
}

func TestRecordBlockFitnessFn_InitializesNilChainState(t *testing.T) {
	// Production Blackboards reach actions with a nil ChainState; the probe
	// allocates one rather than panicking, then still reports through the hook.
	fitness := captureBlockFitness(t)
	bb := &Blackboard{Outcome: "success"}

	if got := fitnessProbeAction(newTestBTContext(bb)); got != 1 {
		t.Fatalf("fitnessProbeAction() = %d, want 1", got)
	}
	if bb.ChainState == nil {
		t.Fatal("ChainState still nil, want an allocated map")
	}
	if len(*fitness) != 1 || (*fitness)[0] != (blockFitness{blockID: "probe", score: 75}) {
		t.Errorf("fitness = %+v, want one {probe, \"\", 75} record", *fitness)
	}
}

// TestRecordNodeTickFn_RecordsFailureWhenTheNodePanics pins the one behavior the
// current call site gets wrong. Panics are a live path here, not a theoretical
// one: RunTask recovers at the tree root (tree.go — "TREE PANIC"), chain nodes
// recover their own (chains.go), and tracedAction deliberately does NOT recover
// so "the caller's panic semantics stand" (registry.go). A node that panics
// therefore unwinds observedCommand.Run, which records its tick on the normal
// return path only — so the loudest possible failure is the one that produces no
// bt_node_ticks_total, no bt_node_errors_total and no duration sample at all,
// and a crash-looping node reads as idle on the dashboard instead of failing.
// The same function already treats the panic path as real for its other two
// observability side effects — the span (defer span.End()) and the ctx.Context
// restore (defer) both survive it; only the metrics hook does not. Recording
// from a defer with status "failure" closes that gap, and the panic must keep
// propagating so RunTask's recovery still marks the run failed.
func TestRecordNodeTickFn_RecordsFailureWhenTheNodePanics(t *testing.T) {
	tests := []struct {
		name       string
		node       *evolution.SerializableNode
		parent     string
		wantTick   nodeTick
		panicValue any
	}{
		{
			name:       "NamedActionWithBlockID",
			node:       &evolution.SerializableNode{Type: "Action", Name: "Boom", Metadata: map[string]any{"block_id": "core:pre_gate"}},
			parent:     "Root",
			wantTick:   nodeTick{nodeType: "Action", nodeName: "Boom", parentName: "Root", blockID: "core:pre_gate", status: "failure"},
			panicValue: "boom",
		},
		{
			name:       "UnnamedNodeReportsItsType",
			node:       &evolution.SerializableNode{Type: "Sequence"},
			wantTick:   nodeTick{nodeType: "Sequence", nodeName: "Sequence", status: "failure"},
			panicValue: "boom",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ticks := captureNodeTicks(t)
			child := btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { panic(tc.panicValue) })
			cmd := observeNode(tc.node, tc.parent, child)

			var recovered any
			func() {
				defer func() { recovered = recover() }()
				cmd.Run(newTestBTContext(newTestBlackboard()))
			}()
			if recovered != tc.panicValue {
				t.Fatalf("recovered %v, want the node's panic (%v) to keep propagating to RunTask's recovery", recovered, tc.panicValue)
			}

			if len(*ticks) != 1 {
				t.Fatalf("recorded %d ticks for a panicking node, want exactly 1 failure tick: %+v", len(*ticks), *ticks)
			}
			got := (*ticks)[0]
			if got.durationMs < 0 {
				t.Errorf("durationMs = %d, want >= 0", got.durationMs)
			}
			got.durationMs = 0
			if got != tc.wantTick {
				t.Errorf("tick = %+v, want %+v", got, tc.wantTick)
			}
		})
	}
}
