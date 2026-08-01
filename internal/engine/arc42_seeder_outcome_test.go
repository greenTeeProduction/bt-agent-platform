package engine

import (
	"path/filepath"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// 2026-08-01 21:09:14: the arc42 seeder recorded outcome=success, quality=0.8
// for a run whose own report reads "## arc42 Program Seeding Rejected Proposal
// — No usable proposal ... even after a feedback retry". It has seeded nothing
// since 2026-07-18 and every one of those failures was booked as a success.
//
// The existing comment on the program-active branch already states the intent —
// healthy no-ops refine to no_change so the throttle can suppress them, while
// "goals unavailable, store unreadable, proposal rejected ... are problems the
// operator must see". But leaving those UNREFINED does the opposite of making
// them seen: an unrefined tree success is recorded as plain "success". Refining
// them to "degraded" is what actually surfaces them — degraded is a healthy
// terminal outcome (no breaker trip, no dead-letter) that the routine
// notification throttle does NOT suppress, because it only throttles no_change.
func TestArc42Seeder_RejectedProposalIsNotRecordedAsSuccess(t *testing.T) {
	dir := t.TempDir()
	prevP := goapProgramsPath
	goapProgramsPath = filepath.Join(dir, "programs.json")
	t.Cleanup(func() { goapProgramsPath = prevP })

	// The proposal source is down — the live case since the nlm OAuth profile
	// corrupted and the Claude fallback started returning nothing.
	prevFetch := seedProgramFetchFn
	seedProgramFetchFn = func(string) string { return "" }
	t.Cleanup(func() { seedProgramFetchFn = prevFetch })

	seed := GetAction("SeedProgramFromArc42Goals")
	if seed == nil {
		t.Fatal("SeedProgramFromArc42Goals not registered")
	}
	bb := &Blackboard{ChainState: map[string]any{}}
	if got := seed(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("action status = %d, want 1 (a failed seed is not a tree failure)", got)
	}

	switch bb.Outcome {
	case "arc42_seeder_rejected_proposal", "arc42_goals_unavailable", "arc42_seeder_store_unreadable":
		if bb.OutcomeRefinement != "degraded" {
			t.Fatalf("outcome %q recorded with refinement %q — an unrefined tree success is booked as "+
				"plain \"success\", which is how 13 days of seeding nothing looked healthy. "+
				"Want \"degraded\": visible, throttle-proof, and still a healthy terminal state.",
				bb.Outcome, bb.OutcomeRefinement)
		}
	default:
		t.Skipf("environment produced outcome %q; this test targets the failure branches", bb.Outcome)
	}
}
