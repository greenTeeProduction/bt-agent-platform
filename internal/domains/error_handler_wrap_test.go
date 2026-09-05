package domains

import "testing"

func TestAllDomainTreesWrappedInClaudeErrorHandler(t *testing.T) {
	for name, tree := range AllDomainTrees() {
		if tree == nil {
			t.Errorf("%s: nil tree", name)
			continue
		}
		if tree.Type != "ClaudeErrorHandler" {
			t.Errorf("%s: root type = %q, want ClaudeErrorHandler", name, tree.Type)
			continue
		}
		if want := name + "_ErrorHandler"; tree.Name != want {
			t.Errorf("%s: root name = %q, want %q", name, tree.Name, want)
		}
		if len(tree.Children) != 1 {
			t.Errorf("%s: wrapper must have exactly 1 child, got %d", name, len(tree.Children))
		}
		if len(tree.Children) == 1 && tree.Children[0].Type == "ClaudeErrorHandler" {
			t.Errorf("%s: double-wrapped", name)
		}
	}
}

func TestResolveDomainTreeIsWrapped(t *testing.T) {
	tree := ResolveTreeID("domain:goap_fusion_loop")
	if tree == nil || tree.Type != "ClaudeErrorHandler" {
		t.Fatalf("domain:goap_fusion_loop root = %+v, want ClaudeErrorHandler wrapper", tree)
	}
}
