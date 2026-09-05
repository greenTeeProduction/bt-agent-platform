package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// SelfReviewTree is the proactive self-review agent (self-fixing fleet, spec
// docs/superpowers/specs/2026-07-17-self-fixing-fleet-design.md §3 Part B):
// on a schedule it gathers the autonomous commits landed since the last
// self-review, runs a READ-ONLY Claude Code review over their diffs, and
// seeds a self-fix:self-review:<sig> code-fix program per CONFIRMED defect
// via seedCodeFixProgram so the goap-fusion loop implements the fix.
//
// This is a SINGLE composite action (RunSelfReview), not the spec's literal
// four separate stages — deliberately, mirroring Arc42SeederTree
// (arc42_seeder.go), which does gather→propose→persist inside one action and
// reports success with an OutcomeRefinement on healthy skips. Two reasons:
//
//  1. Every act()/cond() name in a domain tree must be a REGISTERED
//     engine action/condition, or it silently resolves to the engine's
//     permissive unknown-action success fallback — see AuctionDemoTree's
//     comment and TestAuctionDemoTreeHasNoSilentNoOps in domains_test.go,
//     which caught exactly this: two of that tree's three "protocol" stages
//     were decorative names that were never actually registered.
//  2. A multi-action Sequence cannot short-circuit "no new commits" as a
//     HEALTHY success: an early child returning failure fails the whole
//     Sequence, which bubbles to the ClaudeErrorHandler wrapper every domain
//     tree gets from AllDomainTrees() (wrapWithErrorHandler in trees.go) and
//     would spuriously trigger a Claude-proposed "recovery" for a routine
//     no_change steady state.
//
// A single real action that internally handles every skip path (no new
// commits, rate-limited, review failed) and returns success avoids both
// traps, so this stays a plain sequence.
func SelfReviewTree() *evolution.SerializableNode {
	root := seq("SelfReview_Main",
		"Review autonomous commits since the last self-review and seed code-fix programs for confirmed defects",
		cond("TaskIsNotEmpty", "Scheduled task text must be present"),
		act("RunSelfReview",
			"Gather autonomous commits since last-reviewed SHA, run a read-only Claude Code review, seed a self-fix program per confirmed defect, advance the state SHA"),
		act("MarkSuccessful", "Mark the self-review run successful; the report carries the outcome"),
	)
	return &root
}
