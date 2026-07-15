package engine

// arc42 program seeder: a dedicated agent action that seeds
// ~/.go-bt-evolve/research/programs.json from the platform's LIVE arc42
// quality goals. It deliberately reads the original document
// (docs/arc42/go-bt-evolve-arc42.md via arc42GoalsDocPaths) on every run —
// never a snapshot — so edits to the architecture goals steer the very next
// seeded program. Each run targets ONE quality goal (deterministic daily
// rotation) and rejects any proposal that does not name that goal, on top
// of the loop seeder's existing grounding gates (file-scoped milestones
// that modify EXISTING production code).
//
// This is the seeding half of the goal-coverage feedback loop identified in
// the 2026-07-08 knowledge-graph trace: goals steer research questions and
// the knowledge store dedupes answers, but nothing seeded PROGRAMS from the
// goals directly until this agent.

import (
	"fmt"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/research"

	btcore "github.com/rvitorper/go-bt/core"
)

// arc42SeedTargetGoal picks the quality goal this run's program must
// advance: a deterministic daily rotation across the goals parsed from the
// live arc42 document. Nil when the document is unavailable or has no
// parseable §1.2 goals table.
func arc42SeedTargetGoal() *arc42Goal {
	goals := loadArc42QualityGoals()
	if len(goals) == 0 {
		return nil
	}
	g := goals[time.Now().YearDay()%len(goals)]
	return &g
}

// buildArc42SeedPrompt frames the standard seed prompt with a hard
// requirement to advance one named quality goal.
func buildArc42SeedPrompt(ps *research.ProgramStore, goal *arc42Goal) string {
	return buildSeedProgramPrompt(ps) + fmt.Sprintf(`

HARD REQUIREMENT for THIS proposal: the program must advance arc42 quality
goal %s (%s): %s
Name %q in the PROGRAM title. Proposals that serve a different goal will be
rejected this run.`, goal.ID, goal.Name, goal.Motivation, goal.ID)
}

// programNamesGoal reports whether the proposal names the targeted goal in
// its title or any milestone (by ID like "Q2" or by name like
// "Evolvability").
func programNamesGoal(spec *goapProgramSpec, goal *arc42Goal) bool {
	texts := append([]string{spec.Title}, spec.Milestones...)
	for _, t := range texts {
		if strings.Contains(t, goal.ID) || strings.Contains(strings.ToLower(t), strings.ToLower(goal.Name)) {
			return true
		}
	}
	return false
}

func init() {
	RegisterAction("SeedProgramFromArc42Goals", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		goal := arc42SeedTargetGoal()
		if goal == nil {
			bb.Outcome = "arc42_goals_unavailable"
			bb.Result = "## arc42 Program Seeding Skipped\n\nThe live arc42 document yielded no quality goals (docs/arc42/go-bt-evolve-arc42.md missing or its §1.2 table unparseable) — no program seeded. Fix the document; this agent never seeds from a static copy."
			return 1
		}

		ps, err := research.OpenPrograms(goapProgramsPath)
		if err != nil {
			bb.Outcome = "arc42_seeder_store_unreadable"
			bb.Result = "## arc42 Program Seeding Skipped\n\nProgram store unreadable: " + err.Error()
			return 1
		}
		if active := ps.Active(); active != nil {
			bb.Outcome = "arc42_seeder_program_active"
			bb.Result = fmt.Sprintf("## arc42 Program Seeding Skipped\n\nProgram %q is still active — nothing seeded (one program at a time; targeted quality goal this run would have been %s %s).", active.Title, goal.ID, goal.Name)
			// One-program-at-a-time is the expected steady state, so this skip
			// is a healthy no-op — refine to no_change so the notification
			// throttle can suppress the repeats. The other skip branches
			// (goals unavailable, store unreadable, proposal rejected) stay
			// unrefined: those are problems the operator must see.
			bb.OutcomeRefinement = "no_change"
			bb.QualityScore = 0.5
			bb.QualityAuthoritative = true
			return 1
		}

		att := fetchAcceptableGoapProgram(buildArc42SeedPrompt(ps, goal), func(spec *goapProgramSpec) string {
			if programNamesGoal(spec, goal) {
				return ""
			}
			return fmt.Sprintf("does not name the targeted quality goal %s (%s) in its title or milestones — seeded work must stay traceable to the arc42 goals", goal.ID, goal.Name)
		})
		if att.Spec == nil {
			bb.Outcome = "arc42_seeder_rejected_proposal"
			bb.Result = fmt.Sprintf("## arc42 Program Seeding Rejected Proposal\n\nNo usable proposal for quality goal %s %s even after a feedback retry: %s. Will retry on the next schedule.", goal.ID, goal.Name, truncateGoap(strings.Join(att.Rejections, " | "), 400))
			return 1
		}

		persistGoapProgram(bb, att.Spec, "arc42-seeder:"+goal.ID)
		bb.Outcome = "arc42_seeder_seeded"
		droppedNote := ""
		if n := len(att.Dropped); n > 0 {
			droppedNote = fmt.Sprintf("; tolerantly accepted, dropped %d invalid milestone(s)", n)
		}
		bb.Result = fmt.Sprintf("## arc42 Program Seeded\n\nNew program %q (%d file-scoped milestones%s) queued for the goap-fusion loop, advancing arc42 quality goal %s %s.", att.Spec.Title, len(att.Spec.Milestones), droppedNote, goal.ID, goal.Name)
		bb.Result += programContinueNote()
		return 1
	})
}
