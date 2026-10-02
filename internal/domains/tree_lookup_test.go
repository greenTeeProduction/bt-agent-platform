package domains

import (
	"reflect"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestLookupTreeIDUsesCatalogDefinitionsWithoutFallback(t *testing.T) {
	old := DynamicResolveFn
	calls := 0
	DynamicResolveFn = func(string) *evolution.SerializableNode { calls++; return nil }
	t.Cleanup(func() { DynamicResolveFn = old })
	for name, want := range AllDomainTrees() {
		t.Run(name, func(t *testing.T) {
			for _, id := range []string{name, "domain:" + name} {
				got := LookupTreeID(id)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("catalog definition lost for %s: got=%+v want=%+v", id, got, want)
				}
			}
		})
	}
	if calls != 0 {
		t.Fatalf("builtin aliases consulted generated definitions %d times", calls)
	}
	for _, id := range []string{"missing", "domain:missing", "thinktank:missing", "wrong:code_review"} {
		if got := LookupTreeID(id); got != nil {
			t.Fatalf("unknown tree %s received substitute %s", id, got.Name)
		}
	}
	if got := ResolveTreeID("missing"); got == nil {
		t.Fatal("inspection changed execution's legacy default policy")
	}
	if got := ResolveTreeID("thinktank:missing"); got == nil {
		t.Fatal("inspection changed execution's legacy thinktank policy")
	}
}

func TestLookupTreeIDRetainsGeneratedDefinitionAndRejectsPathIdentifiers(t *testing.T) {
	old := DynamicResolveFn
	t.Cleanup(func() { DynamicResolveFn = old })
	generated := &evolution.SerializableNode{Type: "Sequence", Name: "OwnedGenerated", Children: []evolution.SerializableNode{{Type: "Action", Name: "OwnedChild", Metadata: map[string]any{"fixture": "retained"}}}}
	var calls []string
	DynamicResolveFn = func(id string) *evolution.SerializableNode {
		calls = append(calls, id)
		if id == "goal:generated" {
			return generated
		}
		return nil
	}
	if got := LookupTreeID("goal:generated"); got != generated || !reflect.DeepEqual(calls, []string{"goal:generated"}) {
		t.Fatalf("generated owner/identity changed: got=%+v calls=%v", got, calls)
	}
	calls = nil
	for _, id := range []string{"", ".", "..", "core:../../outside", "core:\\outside", "core:\x00outside"} {
		if got := LookupTreeID(id); got != nil {
			t.Fatalf("path-shaped tree id admitted: %q", id)
		}
	}
	if len(calls) != 0 {
		t.Fatalf("invalid ids reached filesystem resolver: %v", calls)
	}
	if got := LookupTreeID("default"); got == nil || got.Name != evolution.DefaultTree().Name || len(calls) != 0 {
		t.Fatal("explicit default must remain a compiled definition")
	}
}
