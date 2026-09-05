package knowledge

import (
	"fmt"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// structuralParent builds a realistic parent tree with a distinctive PreGate
// action and router branches so a spliced child is attributable.
func structuralParent(gateAction, branchPrefix string) *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence", Name: "PreGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput"},
					{Type: "Action", Name: gateAction},
				},
			},
			{
				Type: "Selector", Name: "StrategyRouter",
				Children: []evolution.SerializableNode{
					{Type: "Sequence", Name: branchPrefix + "_Primary"},
					{Type: "Sequence", Name: branchPrefix + "_Fallback"},
				},
			},
			{Type: "Action", Name: "ReflectOnOutcome"},
		},
	}
}

func structuralTestFactory(t *testing.T) (*Factory, map[string]*evolution.SerializableNode) {
	t.Helper()
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "finance:alpha", Category: "finance", NodeCount: 8})
	kg.Register(&TreeMeta{ID: "research:beta", Category: "research", NodeCount: 8})

	trees := map[string]*evolution.SerializableNode{
		"finance:alpha": structuralParent("SetupFinanceTools", "Alpha"),
		"research:beta": structuralParent("SetupResearchTools", "Beta"),
	}
	f := NewFactory(kg)
	f.Resolve = func(id string) *evolution.SerializableNode { return trees[id] }
	return f, trees
}

func TestStructuralCrossover_SplicesRealSubtrees(t *testing.T) {
	f, _ := structuralTestFactory(t)

	child := f.Breed("analyze quarterly earnings", "finance", []string{"finance:alpha", "research:beta"})
	if child == nil {
		t.Fatal("Breed returned nil")
	}
	if child.Metadata["generated_by"] != "structural_crossover" {
		t.Fatalf("expected structural crossover, metadata = %v", child.Metadata)
	}
	if child.Metadata["parent_a"] != "finance:alpha" || child.Metadata["parent_b"] != "research:beta" {
		t.Fatalf("parent provenance = %v", child.Metadata)
	}

	// PreGate comes from parent A (real structure, not the synthetic clone).
	gate := extractSubtree(child, func(n *evolution.SerializableNode) bool { return n.Name == "SetupFinanceTools" })
	if gate == nil {
		t.Fatal("child missing parent A's PreGate action")
	}
	// Router branches come from parent B.
	branch := extractSubtree(child, func(n *evolution.SerializableNode) bool { return n.Name == "Beta_Primary" })
	if branch == nil {
		t.Fatal("child missing parent B's router branch")
	}
	// Standard evolvable tail is present.
	if extractSubtree(child, func(n *evolution.SerializableNode) bool { return n.Name == "OutcomeSelector" }) == nil {
		t.Fatal("child missing OutcomeSelector tail")
	}
}

func TestStructuralCrossover_ChildDoesNotAliasParent(t *testing.T) {
	f, trees := structuralTestFactory(t)
	child := f.Breed("task", "finance", []string{"finance:alpha", "research:beta"})

	// Mutate the child's spliced PreGate; the parent must stay untouched.
	gate := extractSubtree(child, func(n *evolution.SerializableNode) bool { return n.Name == "SetupFinanceTools" })
	gate.Name = "MutatedAction"
	if extractSubtree(trees["finance:alpha"], func(n *evolution.SerializableNode) bool { return n.Name == "MutatedAction" }) != nil {
		t.Fatal("mutating the child leaked into the parent tree")
	}
}

func TestStructuralCrossover_FallsBackWithoutStructure(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "finance:alpha", Category: "finance"})
	kg.Register(&TreeMeta{ID: "research:beta", Category: "research"})
	f := NewFactory(kg)

	// No Resolve hook → synthetic template path (previous behavior).
	child := f.Breed("task", "finance", []string{"finance:alpha", "research:beta"})
	if child == nil {
		t.Fatal("Breed returned nil")
	}
	if child.Metadata["generated_by"] == "structural_crossover" {
		t.Fatal("structural crossover must not run without a resolver")
	}

	// Resolve hook that misses → still synthetic.
	f.Resolve = func(string) *evolution.SerializableNode { return nil }
	child = f.Breed("task", "finance", []string{"finance:alpha", "research:beta"})
	if child.Metadata["generated_by"] == "structural_crossover" {
		t.Fatal("structural crossover must not run when parents have no stored structure")
	}
}

func TestStructuralCrossover_UnregisteredParentsSkipped(t *testing.T) {
	f, trees := structuralTestFactory(t)
	trees["ghost:tree"] = structuralParent("SetupGhostTools", "Ghost")

	// ghost:tree resolves but is not KG-registered → not eligible as parent.
	child := f.Breed("task", "finance", []string{"ghost:tree", "finance:alpha"})
	if child.Metadata != nil && child.Metadata["parent_a"] == "ghost:tree" {
		t.Fatal("unregistered parent must not participate in structural crossover")
	}
}

func TestStructuralCrossover_ValidationGate(t *testing.T) {
	f, _ := structuralTestFactory(t)
	f.Validate = func(*evolution.SerializableNode) error { return fmt.Errorf("rejected") }

	child := f.Breed("task", "finance", []string{"finance:alpha", "research:beta"})
	if child == nil {
		t.Fatal("Breed returned nil")
	}
	if child.Metadata["generated_by"] == "structural_crossover" {
		t.Fatal("validation-rejected splice must fall back to the synthetic path")
	}
}

func TestStructuralCrossover_CachesExtractedTemplates(t *testing.T) {
	f, _ := structuralTestFactory(t)
	_ = f.Breed("task", "finance", []string{"finance:alpha", "research:beta"})

	if tmpl := f.Templates["finance:alpha"]; tmpl == nil || tmpl.PreGate == nil {
		t.Fatal("parent A's extracted PreGate should be cached on its template")
	}
	if tmpl := f.Templates["research:beta"]; tmpl == nil || tmpl.StrategyRouter == nil {
		t.Fatal("parent B's extracted StrategyRouter should be cached on its template")
	}
}
