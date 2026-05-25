package thinktank

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestDefaultFellows(t *testing.T) {
	fellows := DefaultFellows()
	if len(fellows) != 5 {
		t.Errorf("expected 5 fellows, got %d", len(fellows))
	}
	roles := map[string]bool{}
	for _, f := range fellows {
		if f.Name == "" || f.Role == "" || f.Persona == "" {
			t.Errorf("fellow %s missing fields", f.Name)
		}
		roles[f.Role] = true
	}
	expected := []string{"bull", "bear", "technical", "macro", "contrarian"}
	for _, r := range expected {
		if !roles[r] {
			t.Errorf("missing role: %s", r)
		}
	}
}

func TestNewThinkTank(t *testing.T) {
	tt := NewThinkTank("Test Tank", "Should we invest in AI?")
	if tt.Name != "Test Tank" {
		t.Errorf("wrong name: %s", tt.Name)
	}
	if tt.Topic != "Should we invest in AI?" {
		t.Errorf("wrong topic: %s", tt.Topic)
	}
	if len(tt.Fellows) != 5 {
		t.Errorf("expected 5 fellows, got %d", len(tt.Fellows))
	}
	if tt.DelphiRounds != 3 {
		t.Errorf("expected 3 delphi rounds, got %d", tt.DelphiRounds)
	}
}

func TestFellowResearchTree(t *testing.T) {
	fellow := Fellow{
		Name: "Test Fellow", Role: "bull",
		Persona: "You are a tester.",
	}
	tree := FellowResearchTree(fellow, "test topic")
	if tree == nil {
		t.Fatal("tree is nil")
	}
	if tree.Type != "Sequence" {
		t.Errorf("expected Sequence root, got %s", tree.Type)
	}
	if len(tree.Children) < 3 {
		t.Errorf("expected at least 3 children (PreGate + research nodes), got %d", len(tree.Children))
	}
	// Verify tree structure is non-trivial
	totalNodes := countNodes(tree)
	if totalNodes < 5 {
		t.Errorf("expected at least 5 nodes, got %d", totalNodes)
	}
	hasChain := hasChainAction(tree)
	if !hasChain {
		t.Log("no ChainAction nodes found — tree may use Action nodes")
	}
	t.Logf("Fellow tree: %d nodes", totalNodes)
}

func TestSynthesisTree(t *testing.T) {
	tree := SynthesisTree()
	if tree == nil {
		t.Fatal("tree is nil")
	}
	if len(tree.Children) < 5 {
		t.Errorf("expected at least 5 synthesis phases, got %d", len(tree.Children))
	}
}

func TestPeerReviewTree(t *testing.T) {
	tree := PeerReviewTree()
	if tree == nil {
		t.Fatal("tree is nil")
	}
}

func TestReportGenerationTree(t *testing.T) {
	tree := ReportGenerationTree()
	if tree == nil {
		t.Fatal("tree is nil")
	}
}

// helpers
func countNodes(n *evolution.SerializableNode) int {
	if n == nil {
		return 0
	}
	c := 1
	for i := range n.Children {
		c += countNodes(&n.Children[i])
	}
	return c
}

func hasChainAction(n *evolution.SerializableNode) bool {
	if n == nil {
		return false
	}
	if n.Type == "ChainAction" {
		return true
	}
	for i := range n.Children {
		if hasChainAction(&n.Children[i]) {
			return true
		}
	}
	return false
}
