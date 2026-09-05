package domains

import "testing"

func TestResolveTreeID_FusionAliases(t *testing.T) {
	for _, id := range []string{"fusion", "fusion_deliberation"} {
		tree := ResolveTreeID(id)
		if tree == nil {
			t.Fatalf("ResolveTreeID(%q) returned nil", id)
		}
		if tree.Name != "FusionDeliberation" {
			t.Fatalf("ResolveTreeID(%q) name=%q, want FusionDeliberation", id, tree.Name)
		}
	}
}
