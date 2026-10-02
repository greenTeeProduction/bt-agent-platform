package engine

import (
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/research"
)

// seedLegacyProgramDone creates historical state for protocol tests only.
func seedLegacyProgramDone(ps *research.ProgramStore, id string, idx int, run string) bool {
	for _, p := range ps.Programs {
		if p.ID == id && idx >= 0 && idx < len(p.Milestones) {
			p.Milestones[idx].Status = "done"
			p.Milestones[idx].CompletedRun = run
			p.Milestones[idx].CompletedAt = time.Now().UTC()
			return true
		}
	}
	return false
}

func verifiedProgramRunForTest(t *testing.T, bb *Blackboard, tasks []SuperpowersTask) *SuperpowersRun {
	t.Helper()
	refs, err := captureProgramMilestones(bb)
	if err != nil {
		t.Fatal(err)
	}
	for i := range tasks {
		if tasks[i].Title == "" {
			tasks[i].Title = tasks[i].Objective
		}
	}
	run := researchDeliveryFixture(t, tasks)
	run.ProgramMilestones = refs
	return run
}
