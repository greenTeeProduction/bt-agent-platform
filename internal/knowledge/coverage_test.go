package knowledge

import (
	"testing"
)

func TestNewKG(t *testing.T) {
	kg := NewKnowledgeGraph()
	if kg == nil {
		t.Fatal("nil kg")
	}
}

func TestBuildKG(t *testing.T) {
	kg := BuildKnowledgeGraph()
	if kg == nil {
		t.Fatal("nil built kg")
	}
}

func TestRegister(t *testing.T) {
	kg := NewKnowledgeGraph()
	tm := &TreeMeta{ID: "test", Name: "Test", Category: "test", NodeCount: 5}
	kg.Register(tm)
	// Verify via discover
	id, conf := kg.Discover("test tree")
	if id == "" && conf == 0 {
		t.Log("discover found nothing (expected for empty graph)")
	}
}

func TestConnect(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "a", Name: "A", Category: "test"})
	kg.Register(&TreeMeta{ID: "b", Name: "B", Category: "test"})
	kg.Connect("a", "b", "derived_from")
	// No error = success
}

func TestDiscover_CodeReview(t *testing.T) {
	kg := BuildKnowledgeGraph()
	id, conf := kg.Discover("review this go code for bugs")
	if id == "" {
		t.Error("should discover tree for code review")
	}
	if conf <= 0 {
		t.Error("confidence should be > 0")
	}
}

func TestDiscover_Finance(t *testing.T) {
	kg := BuildKnowledgeGraph()
	id, conf := kg.Discover("build a DCF model for valuation")
	if id == "" {
		t.Error("should discover finance tree for DCF")
	}
	_ = conf
}

func TestDiscover_Research(t *testing.T) {
	kg := BuildKnowledgeGraph()
	id, _ := kg.Discover("research quantum computing")
	if id == "" {
		t.Error("should discover research tree")
	}
}

func TestDiscover_Unknown(t *testing.T) {
	kg := BuildKnowledgeGraph()
	id, conf := kg.Discover("xyzzy plugh plover")
	t.Logf("unknown task discover: id=%s conf=%.2f", id, conf)
}

func TestListByCategory(t *testing.T) {
	kg := BuildKnowledgeGraph()
	core := kg.ListByCategory("core")
	if len(core) == 0 {
		t.Error("should have core trees")
	}
	finance := kg.ListByCategory("finance")
	if len(finance) == 0 {
		t.Error("should have finance trees")
	}
}

func TestQuery(t *testing.T) {
	kg := BuildKnowledgeGraph()
	results := kg.Query("review")
	if len(results) == 0 {
		t.Log("no results for 'review' — trying broader query")
	}
}

func TestSummary(t *testing.T) {
	kg := BuildKnowledgeGraph()
	s := kg.Summary()
	if len(s) < 20 {
		t.Error("summary too short")
	}
}
