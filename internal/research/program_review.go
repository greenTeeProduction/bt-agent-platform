package research

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/util"
)

func validatePrograms(ps *ProgramStore) error {
	seen := map[string]bool{}
	for _, p := range ps.Programs {
		if p == nil || p.ID == "" || seen[p.ID] {
			return fmt.Errorf("invalid or duplicate program identity")
		}
		seen[p.ID] = true
		for _, m := range p.Milestones {
			if strings.TrimSpace(m.Goal) == "" {
				return fmt.Errorf("program milestone has no goal")
			}
			switch m.Status {
			case "pending", "blocked", "done", "needs_review":
			default:
				return fmt.Errorf("unknown program milestone state")
			}
			if m.Delivery != nil && (m.Delivery.Validate() != nil || m.Status != "done" || m.CompletedRun != m.Delivery.RunID) {
				return fmt.Errorf("invalid milestone delivery evidence")
			}
		}
	}
	return nil
}

func preserveProgramReviewBackup(path string) error {
	data, err := util.ReadPersistenceFile(path)
	if err != nil {
		return err
	}
	backup := fmt.Sprintf("%s.before-red-review-%x.json", path, sha256.Sum256(data))
	old, err := util.ReadPersistenceFile(backup)
	if err == nil {
		if !bytes.Equal(old, data) {
			return fmt.Errorf("program review backup conflicts with original bytes")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return util.SavePersistenceFileMode(backup, data, 0600, 0750)
}

// ReviseMilestone explicitly resolves a review hold with a changed goal/test.
// It preserves previous evidence and uses the expected goal as a CAS condition.
func (ps *ProgramStore) ReviseMilestone(id string, index int, expectedGoal, revisedGoal string) error {
	revisedGoal = strings.TrimSpace(revisedGoal)
	if revisedGoal == "" || revisedGoal == strings.TrimSpace(expectedGoal) {
		return fmt.Errorf("review requires a revised goal or test requirement")
	}
	for _, p := range ps.Programs {
		if p.ID != id || index < 0 || index >= len(p.Milestones) {
			continue
		}
		m := &p.Milestones[index]
		if m.Status != "needs_review" || m.Goal != expectedGoal || m.Review == nil {
			return fmt.Errorf("milestone review changed or is unavailable")
		}
		m.ReviewHistory = append(m.ReviewHistory, *m.Review)
		m.Goal, m.Status, m.Review = revisedGoal, "pending", nil
		m.RedPassStreak, m.LastRedCmd, m.Attempts, m.BlockedAt = 0, "", 0, time.Time{}
		p.Updated = time.Now().UTC()
		return nil
	}
	return fmt.Errorf("program milestone not found")
}

// Summary keeps historical labels distinct from delivered code and review holds.
func (ps *ProgramStore) Summary() map[string]any {
	counts := map[string]int{}
	verified := 0
	for _, p := range ps.Programs {
		for _, m := range p.Milestones {
			counts[m.Status]++
			if m.Status == "done" && m.Delivery != nil && m.Delivery.Validate() == nil {
				verified++
			}
		}
	}
	return map[string]any{"states": counts, "verified_code_deliveries": verified, "legacy_unverified_done": counts["done"] - verified, "programs": ps.Programs}
}
