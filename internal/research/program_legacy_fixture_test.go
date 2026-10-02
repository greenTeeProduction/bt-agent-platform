package research

import (
	"time"
)

// seedLegacyProgramDone creates historical state for protocol tests only.
func seedLegacyProgramDone(ps *ProgramStore, id string, idx int, run string) bool {
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
