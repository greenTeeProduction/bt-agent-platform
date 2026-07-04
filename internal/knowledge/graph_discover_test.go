package knowledge

import (
	"net/http"
	"net/http/httptest"
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

// =============================================================================
// Deterministic, rank-based tree discovery
//
// Go randomizes map iteration, so Discover must never let map order decide the
// winner. These tests hammer Discover many times and assert a single, stable
// answer for tasks that match multiple keywords / tie on score. They fail
// against the original implementation (Phase-1 returns the first arbitrary
// keyword hit at a flat 0.8; Phase-2 and discoverWithEmbeddings use strict `>`
// which keeps whichever tie-holder map iteration happened to visit first).
// =============================================================================

// discoverRuns runs Discover many times and returns the set of distinct
// (id, confidence) answers observed. A deterministic discover yields exactly
// one entry regardless of how many times it is called.
const discoverRuns = 200

func collectDiscoverIDs(kg *KnowledgeGraph, task string) (map[string]bool, map[float64]bool) {
	ids := map[string]bool{}
	confs := map[float64]bool{}
	for i := 0; i < discoverRuns; i++ {
		id, conf := kg.Discover(task)
		ids[id] = true
		confs[conf] = true
	}
	return ids, confs
}

// Phase-1: a task matching two keywords that map to different trees must route
// to the longest/most-specific matched keyword — deterministically.
func TestStringMatch_DeterministicLongestKeyword(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "domain:code_review",
		Name:     "Code Review",
		Category: "domain",
		Keywords: []string{"code"},
	})
	kg.Register(&TreeMeta{
		ID:       "finance:analysis",
		Name:     "Financial Analysis",
		Category: "finance",
		Keywords: []string{"financial"},
	})

	// "review the financial code" contains both "code" and "financial".
	ids, confs := collectDiscoverIDs(kg, "review the financial code")

	if len(ids) != 1 {
		t.Fatalf("multi-keyword discovery is non-deterministic: saw %d distinct trees over %d runs: %v",
			len(ids), discoverRuns, ids)
	}
	if !ids["finance:analysis"] {
		t.Errorf("expected longest keyword \"financial\" to win → finance:analysis, got %v", ids)
	}
	if len(confs) != 1 {
		t.Errorf("confidence must be stable across runs, saw %v", confs)
	}
}

// Phase-1: when two matched keywords are equally specific (same length) but map
// to different trees, the tie must break by sorted tree ID — deterministically.
func TestStringMatch_DeterministicTieBreakBySortedID(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "zzz:tree",
		Name:     "Zeta",
		Category: "test",
		Keywords: []string{"alpha"},
	})
	kg.Register(&TreeMeta{
		ID:       "aaa:tree",
		Name:     "Alpha",
		Category: "test",
		Keywords: []string{"gamma"},
	})

	// "alpha gamma workflow" matches both 5-char keywords equally.
	ids, _ := collectDiscoverIDs(kg, "alpha gamma workflow")

	if len(ids) != 1 {
		t.Fatalf("equal-length keyword tie is non-deterministic: saw %d distinct trees over %d runs: %v",
			len(ids), discoverRuns, ids)
	}
	if !ids["aaa:tree"] {
		t.Errorf("expected tie broken by sorted tree ID → aaa:tree, got %v", ids)
	}
}

// Phase-1: a task hitting two keywords on the SAME tree must accumulate a real
// score, not return the flat 0.8 that a single keyword yields.
func TestStringMatch_ScoreAccumulatesForMultipleKeywords(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "multi:tree",
		Name:     "Multi Keyword",
		Category: "test",
		Keywords: []string{"financial", "forecast"},
	})

	id, conf := kg.Discover("financial forecast report")
	if id != "multi:tree" {
		t.Fatalf("expected multi:tree, got %q", id)
	}
	if conf <= 0.8 {
		t.Errorf("two matched keywords should accumulate above the single-keyword 0.8 flat score, got %.4f", conf)
	}
}

// Phase-2: capability-overlap scoring must break exact-score ties by sorted
// tree ID rather than keeping the strict-`>` map-random winner.
func TestStringMatch_Phase2DeterministicTieBreak(t *testing.T) {
	kg := NewKnowledgeGraph()
	// Two trees with identical Phase-2 match scores (0.1 category + 3×0.1 domain
	// = 0.4) and no Phase-1 keyword/action hits for the task "planning session".
	kg.Register(&TreeMeta{
		ID:       "zzz:strategy",
		Name:     "Strategy",
		Category: "planning",
		Capabilities: []Capability{
			{Action: "aa", Domain: "planning", Strength: 1.0},
			{Action: "bb", Domain: "planning", Strength: 1.0},
			{Action: "cc", Domain: "planning", Strength: 1.0},
		},
	})
	kg.Register(&TreeMeta{
		ID:       "aaa:plan",
		Name:     "Plan",
		Category: "planning",
		Capabilities: []Capability{
			{Action: "dd", Domain: "planning", Strength: 1.0},
			{Action: "ee", Domain: "planning", Strength: 1.0},
			{Action: "ff", Domain: "planning", Strength: 1.0},
		},
	})

	ids, _ := collectDiscoverIDs(kg, "planning session")

	if len(ids) != 1 {
		t.Fatalf("Phase-2 score tie is non-deterministic: saw %d distinct trees over %d runs: %v",
			len(ids), discoverRuns, ids)
	}
	if !ids["aaa:plan"] {
		t.Errorf("expected Phase-2 tie broken by sorted tree ID → aaa:plan, got %v", ids)
	}
}

// discoverWithEmbeddings must break equal-similarity ties by sorted tree ID
// rather than keeping the strict-`>` map-random winner.
func TestDiscoverWithEmbeddings_DeterministicTieBreak(t *testing.T) {
	// Stub the embedding endpoint so Discover takes the embeddings path and every
	// task embeds to the same fixed vector.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":[1,0,0]}`))
	}))
	defer srv.Close()

	orig := defaultEmbeddingClient
	defer func() { defaultEmbeddingClient = orig }()
	defaultEmbeddingClient = &EmbeddingClient{BaseURL: srv.URL, Model: "test"}

	kg := NewKnowledgeGraph()
	// Identical embeddings + identical fitness ⇒ identical similarity ⇒ a tie.
	kg.Register(&TreeMeta{ID: "zzz:emb", Name: "Zeta", Category: "test", Fitness: 50, Embedding: Embedding{1, 0, 0}})
	kg.Register(&TreeMeta{ID: "aaa:emb", Name: "Alpha", Category: "test", Fitness: 50, Embedding: Embedding{1, 0, 0}})

	ids, _ := collectDiscoverIDs(kg, "some task")

	if len(ids) != 1 {
		t.Fatalf("embedding similarity tie is non-deterministic: saw %d distinct trees over %d runs: %v",
			len(ids), discoverRuns, ids)
	}
	if !ids["aaa:emb"] {
		t.Errorf("expected embedding tie broken by sorted tree ID → aaa:emb, got %v", ids)
	}
}
