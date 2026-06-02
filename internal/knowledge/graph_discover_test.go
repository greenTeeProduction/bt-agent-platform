package knowledge

import (
	"testing"
)

// =============================================================================
// DiscoverRelated
// =============================================================================

func TestDiscoverRelated_EmptyGraph(t *testing.T) {
	kg := NewKnowledgeGraph()
	results := kg.DiscoverRelated("nonexistent")
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty graph, got %d", len(results))
	}
}

func TestDiscoverRelated_ConnectedTo(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "a", Name: "A", Category: "test"})
	kg.Register(&TreeMeta{ID: "b", Name: "B", Category: "test"})
	kg.Register(&TreeMeta{ID: "c", Name: "C", Category: "test"})
	kg.Connect("a", "b", "depends_on")
	kg.Connect("a", "c", "depends_on")

	// Tree 'a' is connected TO b and c (edges from a)
	results := kg.DiscoverRelated("a")
	if len(results) != 2 {
		t.Fatalf("expected 2 related trees for 'a', got %d: %v", len(results), results)
	}
	seen := map[string]bool{}
	for _, r := range results {
		seen[r] = true
	}
	if !seen["b"] || !seen["c"] {
		t.Errorf("expected 'a' related to 'b' and 'c', got %v", results)
	}
}

func TestDiscoverRelated_ConnectedFrom(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "x", Name: "X", Category: "test"})
	kg.Register(&TreeMeta{ID: "y", Name: "Y", Category: "test"})
	kg.Connect("y", "x", "depends_on")

	// Tree 'x' is connected FROM y (edge to x)
	results := kg.DiscoverRelated("x")
	if len(results) != 1 {
		t.Fatalf("expected 1 related tree for 'x', got %d: %v", len(results), results)
	}
	if results[0] != "y" {
		t.Errorf("expected 'x' related to 'y', got %q", results[0])
	}
}

func TestDiscoverRelated_Bidirectional(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "center", Name: "Center", Category: "test"})
	kg.Register(&TreeMeta{ID: "left", Name: "Left", Category: "test"})
	kg.Register(&TreeMeta{ID: "right", Name: "Right", Category: "test"})
	kg.Connect("left", "center", "depends_on")
	kg.Connect("center", "right", "extends")

	// Center is connected TO right AND FROM left
	results := kg.DiscoverRelated("center")
	if len(results) != 2 {
		t.Fatalf("expected 2 related trees for 'center', got %d: %v", len(results), results)
	}
	seen := map[string]bool{}
	for _, r := range results {
		seen[r] = true
	}
	if !seen["left"] || !seen["right"] {
		t.Errorf("expected 'center' related to 'left' and 'right', got %v", results)
	}
}

func TestDiscoverRelated_Unconnected(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "isolated", Name: "Isolated", Category: "test"})

	results := kg.DiscoverRelated("isolated")
	if len(results) != 0 {
		t.Errorf("expected 0 related trees for isolated node, got %d", len(results))
	}
}

func TestDiscoverRelated_Deduplicates(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "a", Name: "A", Category: "test"})
	kg.Register(&TreeMeta{ID: "b", Name: "B", Category: "test"})
	// Bidirectional edges — same pair should only appear once
	kg.Connect("a", "b", "depends_on")
	kg.Connect("b", "a", "depends_on")

	results := kg.DiscoverRelated("a")
	if len(results) != 1 {
		t.Fatalf("expected 1 related (deduplicated) for 'a', got %d: %v", len(results), results)
	}
	if results[0] != "b" {
		t.Errorf("expected 'a' related to 'b', got %q", results[0])
	}
}

func TestDiscoverRelated_NonExistentTree(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "a", Name: "A", Category: "test"})
	results := kg.DiscoverRelated("nonexistent")
	if len(results) != 0 {
		t.Errorf("expected 0 results for nonexistent tree, got %d", len(results))
	}
}

func TestDiscoverRelated_SelfLoopEdge(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "self", Name: "Self", Category: "test"})
	kg.Connect("self", "self", "depends_on")

	results := kg.DiscoverRelated("self")
	if len(results) != 0 {
		t.Errorf("self-loop should not appear as a related tree, got %d: %v", len(results), results)
	}
}
