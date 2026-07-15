package engine

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// TestRunTask_BackstopsEmptyResultOnNonSuccess locks in the "no run output" /
// "(last: unknown)" black hole fix: today, any leaf that terminates the tree
// without success and without ever writing to bb.Result leaves RunTask's
// return value (and bb.Result) blank. Every downstream consumer — DLQ
// records, OutcomeErrorDetail, dashboards — is then left undiagnosable about
// which task failed and how. RunTask must backstop bb.Result with a message
// naming the task and the terminal outcome whenever the tree didn't succeed
// and bb.Result is still empty.
func TestRunTask_BackstopsEmptyResultOnNonSuccess(t *testing.T) {
	cases := []struct {
		name       string
		actionName string
		code       int // terminal code the stub action returns every tick
	}{
		// Immediate failure (-1): a leaf/condition that fails without ever
		// narrating why — the common case for silent condition failures.
		{"failure", "RunTaskBackstopFailureAction", -1},
		// Perpetually "running" (0): the 1000-tick safety limit in RunTask
		// trips and the terminal switch falls into its default (Partial)
		// branch — also currently leaves bb.Result blank.
		{"partial", "RunTaskBackstopPartialAction", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			RegisterAction(tc.actionName, func(_ *btcore.BTContext[Blackboard]) int {
				return tc.code
			})

			bb := &Blackboard{Task: "diagnose the " + tc.name + " case"}
			tree := &evolution.SerializableNode{Type: "Action", Name: tc.actionName}
			bt := BuildTree(tree, bb)

			result := RunTask(bb, bt)

			if bb.Outcome == string(evolution.Success) {
				t.Fatalf("test stub must not report success, got outcome=%q", bb.Outcome)
			}
			if result == "" {
				t.Fatal("RunTask must not return an empty result when the tree does not succeed")
			}
			if bb.Result == "" {
				t.Fatal("RunTask must backstop bb.Result when it is left empty on a non-success terminal outcome")
			}
			if !strings.Contains(bb.Result, bb.Task) {
				t.Errorf("backstop message should name the task for diagnosability, got: %q", bb.Result)
			}
			if !strings.Contains(bb.Result, bb.Outcome) {
				t.Errorf("backstop message should name the terminal outcome for diagnosability, got: %q", bb.Result)
			}
		})
	}
}

// TestRunTask_PreservesPendingApprovalOutcome locks in pending_approval as a
// real, non-terminal outcome. Today RunTask's tick loop treats every code==0
// return identically: it keeps ticking up to the 1000-tick safety limit and
// then the terminal switch's default branch stamps bb.Outcome = "partial",
// clobbering whatever the tree already recorded. A HITL gate that sets
// bb.Outcome = "pending_approval" and returns 0 (RUNNING, awaiting a human)
// must survive RunTask instead of being silently downgraded to "partial" —
// and RunTask must recognize it immediately rather than busy-spinning the
// tree 1000 times waiting for a human who cannot respond within a single
// synchronous call.
func TestRunTask_PreservesPendingApprovalOutcome(t *testing.T) {
	var calls int
	RegisterAction("RunTaskPendingApprovalAction", func(ctx *btcore.BTContext[Blackboard]) int {
		calls++
		ctx.Blackboard.Outcome = "pending_approval"
		ctx.Blackboard.Result = "Awaiting human approval (id=req-1): confirm"
		return 0
	})

	bb := &Blackboard{Task: "needs a human"}
	tree := &evolution.SerializableNode{Type: "Action", Name: "RunTaskPendingApprovalAction"}
	bt := BuildTree(tree, bb)

	RunTask(bb, bt)

	if bb.Outcome != "pending_approval" {
		t.Fatalf("RunTask must preserve pending_approval as a non-terminal outcome, got %q", bb.Outcome)
	}
	if calls > 1 {
		t.Fatalf("RunTask must stop ticking once pending_approval is reached instead of spinning to the tick limit, got %d ticks", calls)
	}
}

// TestRunTask_PreservesRateLimitCarryoverOutcome locks in
// "goap_fusion_rate_limited" as a real terminal outcome that must survive
// RunTask's terminal switch. A GOAP fusion leaf that hits an active Claude
// rate-limit backoff sets bb.Outcome = "goap_fusion_rate_limited" and returns
// -1 (the tree's generic failure code) so the deliberate graceful-degrade
// carryover can be distinguished from a genuine failure by the scheduler.
// Today RunTask's terminal switch unconditionally stamps
// `case code == -1: bb.Outcome = string(evolution.Failure)`, clobbering the
// sentinel before it ever reaches the scheduler — collapsing a designed
// pause into a generic failure that gets retried into the DLQ.
func TestRunTask_PreservesRateLimitCarryoverOutcome(t *testing.T) {
	RegisterAction("RunTaskRateLimitCarryoverAction", func(ctx *btcore.BTContext[Blackboard]) int {
		ctx.Blackboard.Outcome = "goap_fusion_rate_limited"
		ctx.Blackboard.Result = "Claude rate-limit backoff active until 2026-07-08T22:35:14Z; plan carried over to the next cycle."
		return -1
	})

	bb := &Blackboard{Task: "goap fusion cycle"}
	tree := &evolution.SerializableNode{Type: "Action", Name: "RunTaskRateLimitCarryoverAction"}
	bt := BuildTree(tree, bb)

	RunTask(bb, bt)

	if bb.Outcome != "goap_fusion_rate_limited" {
		t.Fatalf("RunTask must preserve goap_fusion_rate_limited as a terminal outcome instead of collapsing it to %q, got %q", evolution.Failure, bb.Outcome)
	}
}

// TestValidateTree_LeafTypesRejectChildren locks in the generalized
// leaf-with-children rule across BOTH validation entry points.
//
// engine.buildNode (tree.go) constructs "Action", "Condition", and
// "AlwaysSucceed" as childless leaves, silently discarding any declared
// node.Children. Declaring children on one of these is a construction error
// that must surface — not just from ValidateTreeFull (the structured path,
// already covered for AlwaysSucceed in Task 1), but also from the flat
// ValidateTree that preflight / BuildAndValidate callers consume as []string.
func TestValidateTree_LeafTypesRejectChildren(t *testing.T) {
	cases := []struct {
		nodeType string
		name     string
	}{
		{"Action", "NoopAgent"},
		{"Condition", "ValidateInput"},
		{"AlwaysSucceed", ""},
	}

	for _, tc := range cases {
		t.Run(tc.nodeType, func(t *testing.T) {
			withChildren := &evolution.SerializableNode{
				Type: tc.nodeType,
				Name: tc.name,
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "GeneratePlan"},
				},
			}

			// Flat ValidateTree path (preflight / BuildAndValidate consumers).
			msgs := ValidateTree(withChildren)
			if !containsLeafChildrenMsg(msgs, tc.nodeType) {
				t.Fatalf("ValidateTree(%s leaf with children) should flag the leaf-with-children discard, got: %v",
					tc.nodeType, msgs)
			}
		})
	}

	// Structured ValidateTreeFull path: extend Task 1's AlwaysSucceed coverage
	// to the newly generalized Action / Condition leaf types.
	for _, nodeType := range []string{"Action", "Condition"} {
		withChildren := &evolution.SerializableNode{
			Type: nodeType,
			Name: "leaf",
			Children: []evolution.SerializableNode{
				{Type: "Action", Name: "GeneratePlan"},
			},
		}
		info := ValidateTreeFull(withChildren)
		found := false
		for _, e := range info.Errors {
			if strings.Contains(e, nodeType) && strings.Contains(e, "must not declare children") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("ValidateTreeFull(%s leaf with children) should flag leaf-with-children, got: %v",
				nodeType, info.Errors)
		}
	}

	// Negative case: a legitimate Sequence with children stays clean on both
	// paths — the rule must not over-fire on real composite nodes.
	seq := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "GeneratePlan"},
		},
	}
	for _, m := range ValidateTree(seq) {
		if strings.Contains(m, "must not declare children") {
			t.Fatalf("Sequence with children must not trigger the leaf-children rule (ValidateTree), got: %v",
				ValidateTree(seq))
		}
	}
	if info := ValidateTreeFull(seq); !info.Valid() {
		t.Fatalf("Sequence with children should be valid (ValidateTreeFull), got: %v", info.Errors)
	}
}

// containsLeafChildrenMsg reports whether msgs holds a leaf-with-children
// message naming the given node type. The flat validate.go path appends
// "<name>: <type> leaf must not declare children".
func containsLeafChildrenMsg(msgs []string, nodeType string) bool {
	for _, m := range msgs {
		if strings.Contains(m, nodeType) && strings.Contains(m, "must not declare children") {
			return true
		}
	}
	return false
}
