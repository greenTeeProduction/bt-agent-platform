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
