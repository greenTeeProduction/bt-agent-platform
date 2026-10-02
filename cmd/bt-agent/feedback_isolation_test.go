package main

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestFeedbackForSameTreeIDStaysWithItsOwner(t *testing.T) {
	deps := newFeedbackDeps(t)
	const id = "goal:shared_local_id"
	for range 2 {
		recordUserFeedback(deps, "alice", id, "negative", "incorrect result")
	}
	// Neither another owner's feedback nor an unattributed legacy signal may
	// count against Bob or trigger an automation review on his behalf.
	if err := deps.refStore.Save(&evolution.Record{TaskID: "legacy-feedback", TreeName: id, UserFeedback: evolution.FeedbackNegative}); err != nil {
		t.Fatal(err)
	}
	got := recordUserFeedback(deps, "bob", id, "positive", "correct result")
	if got["positives"] != 1 || got["negatives"] != 0 || got["satisfaction"] != 1.0 || got["flagged_for_review"] == true {
		t.Fatalf("Bob inherited someone else's feedback: %v", got)
	}
}

func TestCompileSeedsForSameTreeIDDoNotOverwriteAnotherOwner(t *testing.T) {
	deps := newFeedbackDeps(t)
	const id = "goal:shared_local_id"
	seedCompileReflection(deps, "alice", id, "Alice's goal", []string{"first"})
	seedCompileReflection(deps, "bob", id, "Bob's goal", []string{"second"})
	seedCompileReflection(deps, "alice", id, "Alice's revised goal", []string{"third"})
	records, err := deps.refStore.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("want one seed per owner, got %d: %+v", len(records), records)
	}
	for _, r := range records {
		want := map[string]string{"alice": "third", "bob": "second"}[r.User]
		if want == "" || r.Plan != want {
			t.Fatalf("seed owner or replacement lost: %+v", r)
		}
	}
}
