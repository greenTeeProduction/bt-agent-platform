package knowledge

import "testing"

func TestFactory_ResolvesExplicitParentTreeIDs(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "finance:a", Name: "Finance A", Category: "finance", NodeCount: 10, Fitness: 80})
	kg.Register(&TreeMeta{ID: "research:b", Name: "Research B", Category: "research", NodeCount: 12, Fitness: 90})

	f := NewFactory(kg)
	if f.Templates["finance:a"] == nil {
		t.Fatal("expected template lookup by explicit tree ID finance:a")
	}
	if f.Templates["research:b"] == nil {
		t.Fatal("expected template lookup by explicit tree ID research:b")
	}

	tree, treeID := f.CreateFromParents("finance:a", "research:b", "hybrid financial research")
	if tree == nil {
		t.Fatal("expected generated tree")
	}
	meta := kg.Trees[treeID]
	if meta == nil {
		t.Fatalf("expected registered metadata for %s", treeID)
	}
	if meta.Category != "finance" {
		t.Fatalf("expected category inherited from first explicit parent, got %q", meta.Category)
	}
}

func TestFactory_PicksHighestFitnessTemplateForCategory(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "finance:low", Name: "Low", Category: "finance", NodeCount: 10, Fitness: 10})
	kg.Register(&TreeMeta{ID: "finance:high", Name: "High", Category: "finance", NodeCount: 12, Fitness: 95})
	kg.Register(&TreeMeta{ID: "finance:mid", Name: "Mid", Category: "finance", NodeCount: 11, Fitness: 50})

	f := NewFactory(kg)
	if got := f.Templates["finance"].SourceID; got != "finance:high" {
		t.Fatalf("expected category template to pick highest fitness finance:high, got %s", got)
	}
}

func TestNewFactory_NilGraphCreatesEmptyGraph(t *testing.T) {
	f := NewFactory(nil)
	if f == nil || f.Graph == nil {
		t.Fatal("NewFactory(nil) should create a usable empty graph")
	}
	if f.Templates == nil {
		t.Fatal("NewFactory(nil) should initialize templates")
	}
}
