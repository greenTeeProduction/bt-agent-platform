package domains

import (
	"fmt"
	"reflect"
	"testing"
)

// arc42SectionSpec pins the shape of one Arc42Trees() entry as it exists
// today: the Sec{N}_Main root name, the Condition names gating PreGate, and
// how many strategies StrategyRouter selects between.
type arc42SectionSpec struct {
	key            string
	mainName       string
	preGateConds   []string
	routerChildren int
}

var arc42Sections = []arc42SectionSpec{
	{"arc42:section1", "Sec1_Main", []string{"GraphIsFresh"}, 2},
	{"arc42:section2", "Sec2_Main", []string{"GraphIsFresh"}, 1},
	{"arc42:section3", "Sec3_Main", []string{"GraphIsFresh"}, 1},
	{"arc42:section4", "Sec4_Main", []string{"Section1Done"}, 1},
	{"arc42:section5", "Sec5_Main", []string{"Section1Done", "Section4Done"}, 5},
	{"arc42:section6", "Sec6_Main", []string{"Section5Done"}, 1},
	{"arc42:section7", "Sec7_Main", []string{"Section5Done"}, 1},
	{"arc42:section8", "Sec8_Main", []string{"Section5Done"}, 1},
	{"arc42:section9", "Sec9_Main", []string{"Section4Done"}, 1},
	{"arc42:section10", "Sec10_Main", []string{"Section1Done"}, 1},
	{"arc42:section11", "Sec11_Main", []string{"Section1Done"}, 1},
	{"arc42:section12", "Sec12_Main", []string{"Section1Done"}, 1},
}

// TestArc42TreesReturnsAllSections pins what Arc42Trees() actually contains
// today: 12 per-section trees, one per arc42Sections spec. The monolith
// assembly tree was retired in 1fb9c70 ("retire arc42 monolith assembly
// machinery"), so this locks down the map's contents and will fail loudly if
// that count ever drifts again in either direction.
func TestArc42TreesReturnsAllSections(t *testing.T) {
	got := Arc42Trees()
	if len(got) != 12 {
		t.Errorf("len(Arc42Trees()) = %d, want 12 (one per arc42 section; the monolith assembly tree was retired in 1fb9c70)", len(got))
	}
	for _, spec := range arc42Sections {
		tree, ok := got[spec.key]
		if !ok {
			t.Errorf("Arc42Trees() missing key %q", spec.key)
			continue
		}
		if tree == nil {
			t.Errorf("Arc42Trees()[%q] is nil", spec.key)
		}
	}
}

// TestArc42TreesRootShape pins the top-level Sequence shape shared by every
// section tree: PreGate (Sequence) -> StrategyRouter (Selector) ->
// ValidateSection -> SaveSection -> MarkSectionDone (all Actions).
func TestArc42TreesRootShape(t *testing.T) {
	trees := Arc42Trees()
	wantChildNames := []string{"PreGate", "StrategyRouter", "ValidateSection", "SaveSection", "MarkSectionDone"}
	wantChildTypes := []string{"Sequence", "Selector", "Action", "Action", "Action"}

	for _, spec := range arc42Sections {
		t.Run(spec.key, func(t *testing.T) {
			root, ok := trees[spec.key]
			if !ok || root == nil {
				t.Fatalf("Arc42Trees()[%q] missing", spec.key)
			}
			if root.Type != "Sequence" {
				t.Errorf("root.Type = %q, want Sequence", root.Type)
			}
			if root.Name != spec.mainName {
				t.Errorf("root.Name = %q, want %q", root.Name, spec.mainName)
			}
			if len(root.Children) != len(wantChildNames) {
				t.Fatalf("len(root.Children) = %d, want %d", len(root.Children), len(wantChildNames))
			}
			for i, child := range root.Children {
				if child.Name != wantChildNames[i] {
					t.Errorf("child %d name = %q, want %q", i, child.Name, wantChildNames[i])
				}
				if child.Type != wantChildTypes[i] {
					t.Errorf("child %d type = %q, want %q", i, child.Type, wantChildTypes[i])
				}
			}
		})
	}
}

// TestArc42TreesPreGateConditions pins which Condition nodes gate each
// section's PreGate sequence today.
func TestArc42TreesPreGateConditions(t *testing.T) {
	trees := Arc42Trees()
	for _, spec := range arc42Sections {
		t.Run(spec.key, func(t *testing.T) {
			root := trees[spec.key]
			if root == nil || len(root.Children) == 0 {
				t.Fatalf("%s: root missing or has no children", spec.key)
			}
			preGate := root.Children[0]
			if preGate.Name != "PreGate" {
				t.Fatalf("%s: root.Children[0].Name = %q, want PreGate", spec.key, preGate.Name)
			}
			var gotConds []string
			for _, c := range preGate.Children {
				if c.Type == "Condition" {
					gotConds = append(gotConds, c.Name)
				}
			}
			if !reflect.DeepEqual(gotConds, spec.preGateConds) {
				t.Errorf("%s: PreGate conditions = %v, want %v", spec.key, gotConds, spec.preGateConds)
			}
		})
	}
}

// TestArc42TreesStrategyRouterChildCount pins how many strategies each
// section's StrategyRouter selects between today.
func TestArc42TreesStrategyRouterChildCount(t *testing.T) {
	trees := Arc42Trees()
	for _, spec := range arc42Sections {
		t.Run(spec.key, func(t *testing.T) {
			root := trees[spec.key]
			if root == nil || len(root.Children) < 2 {
				t.Fatalf("%s: root missing or has fewer than 2 children", spec.key)
			}
			router := root.Children[1]
			if router.Name != "StrategyRouter" {
				t.Fatalf("%s: root.Children[1].Name = %q, want StrategyRouter", spec.key, router.Name)
			}
			if len(router.Children) != spec.routerChildren {
				t.Errorf("%s: len(StrategyRouter.Children) = %d, want %d", spec.key, len(router.Children), spec.routerChildren)
			}
		})
	}
}

// TestArc42TreesChainHelperBuildsChainActionNode pins the shape the chain()
// helper produces: a ChainAction node whose Name is the raw prompt string and
// whose Metadata carries max_tokens as a float64 (mirrors JSON-decoded
// SerializableNode metadata elsewhere in the engine).
func TestArc42TreesChainHelperBuildsChainActionNode(t *testing.T) {
	node := chain("a description", "a prompt", 4096)

	if node.Type != "ChainAction" {
		t.Errorf("node.Type = %q, want ChainAction", node.Type)
	}
	if node.Name != "a prompt" {
		t.Errorf("node.Name = %q, want the raw prompt string", node.Name)
	}
	if node.Description != "a description" {
		t.Errorf("node.Description = %q, want %q", node.Description, "a description")
	}
	got, ok := node.Metadata["max_tokens"].(float64)
	if !ok {
		t.Fatalf("node.Metadata[%q] is %T, want float64", "max_tokens", node.Metadata["max_tokens"])
	}
	if got != 4096 {
		t.Errorf("node.Metadata[%q] = %v, want 4096", "max_tokens", got)
	}
}

// TestArc42TreesTreeHelperWrapsRoot pins tree()'s behavior: it takes a
// SerializableNode by value and returns a pointer to an equal copy, letting
// each sectionN function build its root inline as a value.
func TestArc42TreesTreeHelperWrapsRoot(t *testing.T) {
	root := seq("Root", "root description", cond("C1", "d1"))
	got := tree(root)
	if got == nil {
		t.Fatal("tree() returned nil")
	}
	if !reflect.DeepEqual(*got, root) {
		t.Errorf("*tree(root) = %+v, want %+v", *got, root)
	}
}

// arc42SectionKey is a guard against the key format silently changing (e.g.
// "arc42:sectionN" vs "arc42:section-N"), since bt-agent's switch_tree and the
// A2A resolver depend on the exact "domain:arc42:sectionN" ID shape.
func arc42SectionKey(n int) string {
	return fmt.Sprintf("arc42:section%d", n)
}

func TestArc42TreesSectionKeyFormat(t *testing.T) {
	trees := Arc42Trees()
	for n := 1; n <= 12; n++ {
		key := arc42SectionKey(n)
		if _, ok := trees[key]; !ok {
			t.Errorf("Arc42Trees() missing expected key %q", key)
		}
	}
}
