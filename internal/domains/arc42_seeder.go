package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// Arc42SeederTree is the dedicated arc42 program-seeder agent: it reads the
// LIVE arc42 quality goals (docs/arc42/01-introduction-goals.md — never a
// snapshot) and, when no multi-cycle program is active, seeds
// ~/.go-bt-evolve/research/programs.json with a goal-targeted, grounded
// program for the goap-fusion loop to build. All skip paths (goals
// unavailable, program active, proposal rejected) succeed with an
// explanatory report — the action itself is the gate, so the tree stays a
// plain sequence.
func Arc42SeederTree() *evolution.SerializableNode {
	root := seq("Arc42Seeder_Main",
		"Seed the next improvement program from the live arc42 quality goals",
		cond("TaskIsNotEmpty", "Scheduled task text must be present"),
		act("SeedProgramFromArc42Goals",
			"Read live arc42 §1.2 quality goals, target one goal (daily rotation), fetch a PROGRAM proposal, validate grounded goal-named milestones, persist to programs.json"),
		act("MarkSuccessful", "Mark the seeding run successful; the report carries the outcome"),
	)
	return &root
}
