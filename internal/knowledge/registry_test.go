package knowledge

import (
	"fmt"
	"testing"
)

func TestKnowledgeGraph(t *testing.T) {
	g := GlobalGraph

	if len(g.Trees) != 42 {
		t.Errorf("expected 42 trees, got %d", len(g.Trees))
	}

	expectedCats := map[string]int{
		"core":      2,
		"finance":   10,
		"research":  2,
		"domain":    14,
		"startup":   6,
		"thinktank": 5,
		"evolution": 3,
	}

	for cat, expected := range expectedCats {
		trees := g.ListByCategory(cat)
		if len(trees) != expected {
			t.Errorf("category %s: expected %d trees, got %d", cat, expected, len(trees))
		}
	}

	// Verify domain:doormate is registered and its name is "DoorMate Assistant"
	doormateTree, exists := g.Trees["domain:doormate"]
	if !exists {
		t.Error("expected domain:doormate tree to be registered")
	} else if doormateTree.Name != "DoorMate Assistant" {
		t.Errorf("expected domain:doormate name to be 'DoorMate Assistant', got '%s'", doormateTree.Name)
	}

	fmt.Println(g.Summary())

	// Test discovery
	id, conf := g.Discover("I need to review code for bugs")
	if id == "" || conf == 0 {
		t.Error("should discover code review for bug detection task")
	}
	t.Logf("discovered %s (%.2f) for 'review code for bugs'", id, conf)

	id, conf = g.Discover("analyze financials and build a DCF model")
	if id == "" || conf == 0 {
		t.Error("should discover a finance tree for DCF task")
	}
	t.Logf("discovered %s (%.2f) for 'analyze financials'", id, conf)

	id, conf = g.Discover("conduct deep research and synthesize findings")
	if id == "" || conf == 0 {
		t.Error("should discover research tree")
	}
	t.Logf("discovered %s (%.2f) for 'conduct deep research'", id, conf)

	// Test edges
	if len(g.Edges) == 0 {
		t.Error("should have edges/relationships")
	}
	t.Logf("edges: %d", len(g.Edges))
}
