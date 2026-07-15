package dashboard

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
)

// TestPickTreeForTask_RoutesAuctionShapedTasksToAuctionDemo verifies that
// tasks whose text signals auction/delegation intent (mirroring
// engine.AuctionTaskKeywords, the same keyword set that gates the
// auction_demo tree's IsAuctionTask condition) are routed to the
// "auction_demo" tree so the sprint-execution path actually exercises
// internal/a2a's announce->bid->award auction machinery, instead of falling
// through to a generic tree that never touches auction code.
func TestPickTreeForTask_RoutesAuctionShapedTasksToAuctionDemo(t *testing.T) {
	if len(engine.AuctionTaskKeywords) == 0 {
		t.Fatal("engine.AuctionTaskKeywords must be non-empty and exported so dashboard can mirror it")
	}

	for _, kw := range engine.AuctionTaskKeywords {
		task := Task{
			Title:       "Please " + kw + " this piece of work",
			Description: "routed via keyword: " + kw,
		}
		got := PickTreeForTask(task)
		if got != "auction_demo" {
			t.Errorf("PickTreeForTask(keyword %q) = %q, want %q", kw, got, "auction_demo")
		}
	}
}

func TestPickTreeForTask_NonAuctionTasksUnaffected(t *testing.T) {
	task := Task{Title: "Fix a bug in the login flow", Description: "unrelated to auctions"}
	got := PickTreeForTask(task)
	if got != "domain:code_review" {
		t.Errorf("PickTreeForTask(bug task) = %q, want %q (regression: auction routing must not steal unrelated tasks)", got, "domain:code_review")
	}
}

// TestPickTreeForTask_ConsultsKnowledgeGraphOverStaticSwitch verifies that
// once a knowledge-graph discoverer is wired in (via DiscoverTreeFn, set from
// cmd/bt-dashboard to knowledge.KnowledgeGraph.Discover), PickTreeForTask
// prefers the graph's confident answer over the static 7-branch keyword
// switch — the knowledge graph is meant to replace the switch, not merely
// fill gaps it misses.
func TestPickTreeForTask_ConsultsKnowledgeGraphOverStaticSwitch(t *testing.T) {
	orig := DiscoverTreeFn
	t.Cleanup(func() { DiscoverTreeFn = orig })

	DiscoverTreeFn = func(task string) (string, float64) {
		return "domain:security_audit", 0.9
	}

	// The static switch would route this to domain:code_review (the "bug"
	// branch), but the wired knowledge graph should win.
	task := Task{Title: "Fix a bug in the login flow", Description: "unrelated to auctions"}
	got := PickTreeForTask(task)
	if got != "domain:security_audit" {
		t.Errorf("PickTreeForTask() = %q, want %q (knowledge graph must take priority over the static switch)", got, "domain:security_audit")
	}
}

// TestPickTreeForTask_FallsBackToStaticSwitchWhenGraphUnconfident verifies
// that when the wired knowledge graph reports no confident match (mirroring
// knowledge.KnowledgeGraph.Discover's own ("", 0) contract for an
// unconfident task), PickTreeForTask still falls back to the static keyword
// switch instead of routing to an empty tree ID.
func TestPickTreeForTask_FallsBackToStaticSwitchWhenGraphUnconfident(t *testing.T) {
	orig := DiscoverTreeFn
	t.Cleanup(func() { DiscoverTreeFn = orig })

	DiscoverTreeFn = func(task string) (string, float64) {
		return "", 0
	}

	task := Task{Title: "Fix a bug in the login flow", Description: "unrelated to auctions"}
	got := PickTreeForTask(task)
	if got != "domain:code_review" {
		t.Errorf("PickTreeForTask() = %q, want %q (must fall back to the static switch when the graph has no confident match)", got, "domain:code_review")
	}
}

// TestPickTreeForTask_AuctionRoutingBeatsKnowledgeGraph verifies that
// auction/delegation-shaped task text (ADR-073) still routes to
// "auction_demo" ahead of the knowledge graph, even when the graph is wired
// and confidently suggests a different tree — the auction pre-check is not
// part of the "static 7-branch switch" this goal replaces.
func TestPickTreeForTask_AuctionRoutingBeatsKnowledgeGraph(t *testing.T) {
	if len(engine.AuctionTaskKeywords) == 0 {
		t.Fatal("engine.AuctionTaskKeywords must be non-empty and exported so dashboard can mirror it")
	}
	orig := DiscoverTreeFn
	t.Cleanup(func() { DiscoverTreeFn = orig })

	DiscoverTreeFn = func(task string) (string, float64) {
		return "domain:code_review", 0.95
	}

	kw := engine.AuctionTaskKeywords[0]
	task := Task{Title: "Please " + kw + " this piece of work", Description: "routed via keyword: " + kw}
	got := PickTreeForTask(task)
	if got != "auction_demo" {
		t.Errorf("PickTreeForTask(keyword %q) = %q, want %q (auction routing must beat the knowledge graph)", kw, got, "auction_demo")
	}
}
