package engine

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/nico/go-bt-evolve/internal/research"
)

func ResearchProgramStatus(user string) (map[string]any, error) {
	if user != "" {
		return nil, fmt.Errorf("shared framework programs require the framework operator scope")
	}
	ps, err := research.OpenPrograms(currentGoapProgramsPath())
	if err != nil {
		return nil, err
	}
	// Read-only status corrects unsupported labels in memory. The next update
	// preserves original bytes before committing the correction.
	ps.ReviewLegacyRedCompletions()
	return ps.Summary(), nil
}

func ReviewResearchPrograms(user string) (map[string]any, error) {
	if user != "" {
		return nil, fmt.Errorf("shared framework programs require the framework operator scope")
	}
	if err := research.UpdatePrograms(currentGoapProgramsPath(), func(*research.ProgramStore) error { return nil }); err != nil {
		return nil, err
	}
	return ResearchProgramStatus(user)
}

func ReviseResearchProgramMilestone(user, program string, index int, expected, revised string) error {
	if user != "" {
		return fmt.Errorf("shared framework programs require the framework operator scope")
	}
	return research.UpdatePrograms(currentGoapProgramsPath(), func(ps *research.ProgramStore) error {
		return ps.ReviseMilestone(program, index, expected, revised)
	})
}

func captureProgramMilestones(bb *Blackboard) ([]research.MilestoneRef, error) {
	blob, _ := bb.ChainState["goap_fusion_program_milestone"].(string)
	if strings.TrimSpace(blob) == "" {
		return nil, nil
	}
	if bb.User != "" {
		return nil, fmt.Errorf("personal run cannot claim shared framework milestones")
	}
	ps, err := research.OpenPrograms(currentGoapProgramsPath())
	if err != nil {
		return nil, err
	}
	var refs []research.MilestoneRef
	for token := range strings.SplitSeq(blob, ",") {
		id, index, ok := strings.Cut(strings.TrimSpace(token), ":")
		idx, parseErr := strconv.Atoi(index)
		if !ok || parseErr != nil {
			return nil, fmt.Errorf("invalid program milestone reference")
		}
		var ref *research.MilestoneRef
		for _, program := range ps.Programs {
			if program.ID == id && idx >= 0 && idx < len(program.Milestones) {
				for _, prior := range program.Milestones[:idx] {
					if prior.Status == "needs_review" {
						return nil, fmt.Errorf("program milestone depends on work held for review")
					}
				}
				m := program.Milestones[idx]
				if m.Status != "pending" {
					return nil, fmt.Errorf("program milestone is no longer pending")
				}
				ref = &research.MilestoneRef{ProgramID: id, Index: idx, Goal: m.Goal}
			}
		}
		if ref == nil {
			return nil, fmt.Errorf("program milestone reference is unavailable")
		}
		if !slices.Contains(refs, *ref) {
			refs = append(refs, *ref)
		}
	}
	return refs, nil
}

// Reconciliation reads Git and updates metadata only. The enclosing durable
// research-delivery journal holds new planning until this transaction succeeds.
func reconcileProgramDelivery(ctx context.Context, run *SuperpowersRun) error {
	if len(run.ProgramMilestones) == 0 {
		return nil
	}
	if run.User != "" {
		return fmt.Errorf("personal run cannot complete shared framework milestones")
	}
	delivery, err := inspectResearchDelivery(ctx, run)
	if err != nil {
		return err
	}
	return research.UpdateProgramsWithContext(ctx, currentGoapProgramsPath(), func(ps *research.ProgramStore) error {
		for _, ref := range run.ProgramMilestones {
			var milestone *research.Milestone
			for _, program := range ps.Programs {
				if program.ID == ref.ProgramID && ref.Index >= 0 && ref.Index < len(program.Milestones) {
					milestone = &program.Milestones[ref.Index]
				}
			}
			if milestone == nil || milestone.Goal != ref.Goal {
				return fmt.Errorf("program milestone changed after planning: %s:%d", ref.ProgramID, ref.Index)
			}
			if milestone.Status == "done" {
				if milestone.Delivery == nil || milestone.Delivery.RunID != run.ID || milestone.Delivery.Commit != delivery.Commit {
					return fmt.Errorf("program milestone has conflicting completion evidence")
				}
				continue
			}
			anchors := extractGoFilePaths(ref.Goal)
			for _, task := range run.Tasks {
				if task.Status != "done" && task.Status != "completed" {
					continue
				}
				if len(anchors) == 0 && researchGoalTraceID(task.Objective) != researchGoalTraceID(ref.Goal) {
					continue
				}
				scope := append(slices.Clone(task.Files), task.Tests...)
				var changed []string
				for _, file := range delivery.Files {
					if slices.Contains(scope, file) && (len(anchors) == 0 || slices.Contains(anchors, file)) {
						changed = append(changed, file)
					}
				}
				if len(changed) == 0 {
					continue
				}
				receipt := delivery
				receipt.TaskIndex, receipt.Files = task.Index, changed
				if !ps.MarkDelivered(ref.ProgramID, ref.Index, receipt) {
					return fmt.Errorf("program delivery was not recorded")
				}
				// The completed program claim may belong to the planning cycle
				// rather than this resumed execution; its work has now landed.
				ps.ClearClaim(ref.ProgramID)
				break
			}
		}
		return nil
	})
}

func completeGoapProgramMilestone(bb *Blackboard, run *SuperpowersRun) {
	if err := reconcileProgramDelivery(context.Background(), run); err != nil {
		setGoapState(bb, "program_delivery_error", err.Error())
		return
	}
	ps, err := research.OpenPrograms(currentGoapProgramsPath())
	if err != nil {
		setGoapState(bb, "program_delivery_error", err.Error())
		return
	}
	var done []string
	for _, ref := range run.ProgramMilestones {
		for _, p := range ps.Programs {
			if p.ID == ref.ProgramID && ref.Index >= 0 && ref.Index < len(p.Milestones) {
				m := p.Milestones[ref.Index]
				if m.Delivery != nil && m.Delivery.RunID == run.ID {
					done = append(done, fmt.Sprintf("%s:%d", p.ID, ref.Index))
				}
			}
		}
	}
	if len(done) > 0 {
		setGoapState(bb, "program_milestone_done", strings.Join(done, ","))
	}
}
