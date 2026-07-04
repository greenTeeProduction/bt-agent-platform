package engine

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

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
