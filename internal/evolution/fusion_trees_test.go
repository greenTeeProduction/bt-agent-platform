package evolution

import "testing"

func TestFusionDeliberationTree_HasFusionAndDirectPaths(t *testing.T) {
	tree := FusionDeliberationTree()
	names := collectFusionNames(tree)
	for _, want := range []string{"FusionDeliberation", "PreGate", "StrategyRouter", "FusionPath", "DirectPath", "ShouldUseFusion", "fusion:{{.Task}}", "MarkFusionSkipped"} {
		if !names[want] {
			t.Fatalf("FusionDeliberationTree missing %q; names=%v", want, names)
		}
	}
}

func TestFusionDeliberationTree_UsesFusionChainAction(t *testing.T) {
	tree := FusionDeliberationTree()
	node := findFusionNode(tree, "fusion:{{.Task}}")
	if node == nil {
		t.Fatal("fusion chain node not found")
	}
	if node.Type != "ChainAction" {
		t.Fatalf("fusion node type=%q, want ChainAction", node.Type)
	}
	params, ok := node.Metadata["params"].(map[string]any)
	if !ok || params["max_tool_calls"] != "8" {
		t.Fatalf("fusion params missing max_tool_calls=8: %#v", node.Metadata)
	}
}

func collectFusionNames(n *SerializableNode) map[string]bool {
	out := map[string]bool{}
	var walk func(*SerializableNode)
	walk = func(cur *SerializableNode) {
		if cur == nil {
			return
		}
		out[cur.Name] = true
		for i := range cur.Children {
			walk(&cur.Children[i])
		}
	}
	walk(n)
	return out
}

func findFusionNode(n *SerializableNode, name string) *SerializableNode {
	if n == nil {
		return nil
	}
	if n.Name == name {
		return n
	}
	for i := range n.Children {
		if found := findFusionNode(&n.Children[i], name); found != nil {
			return found
		}
	}
	return nil
}
