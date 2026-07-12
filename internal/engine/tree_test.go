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
