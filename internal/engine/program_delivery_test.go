package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/research"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestProgramDeliveryJournalRepairsMetadataWithoutReimplementation(t *testing.T) {
	isolateBtFusionKnowledge(t)
	seedGoalBudget(t)
	path := withGoapPrograms(t)
	ps, _ := research.OpenPrograms(path)
	p := ps.Add("delivery proof", "audit", []string{"Fix evidence in evidence.go"})
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}
	bb := &Blackboard{ChainState: map[string]any{"goap_fusion_program_milestone": p.ID + ":0"}}
	run := verifiedProgramRunForTest(t, bb, []SuperpowersTask{{Objective: p.Milestones[0].Goal, Files: []string{"evidence.go"}}})
	root := t.TempDir()
	run.ArtifactDir = filepath.Join(root, run.ID)
	run.ResearchDeliveryPending = true
	if err := writeSuperpowersRunJSON(run); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte("broken backlog"), 0600); err != nil {
		t.Fatal(err)
	}
	recordSuperpowersResearchDelivery(run)
	if !run.ResearchDeliveryPending || run.ResearchDeliveryError == "" {
		t.Fatal("failed milestone persistence lost its repair journal")
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := reconcileResearchDeliveries(context.Background(), root, ""); err != nil {
		t.Fatal(err)
	}
	saved, err := readSuperpowersRunJSON(filepath.Join(run.ArtifactDir, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	ps, err = research.OpenPrograms(path)
	if err != nil {
		t.Fatal(err)
	}
	m := ps.Programs[0].Milestones[0]
	if saved.ResearchDeliveryPending || saved.ResearchDeliveryError != "" || m.Status != "done" || m.Delivery == nil || m.Delivery.Commit != run.AppliedCommit {
		t.Fatalf("metadata repair did not retain real delivery: %+v", m)
	}
	current := (execCommandRunner{}).Run(context.Background(), run.RepoDir, "git", "rev-parse", "HEAD")
	if current.Err != nil || current.Output != run.AppliedCommit+"\n" {
		t.Fatal("repair changed the actual delivered commit")
	}
	if err := reconcileResearchDeliveries(context.Background(), root, ""); err != nil {
		t.Fatal(err)
	}
}

func TestProgramDeliveryRejectsChangedGoalAndPersonalOwner(t *testing.T) {
	path := withGoapPrograms(t)
	ps, _ := research.OpenPrograms(path)
	p := ps.Add("stable identity", "audit", []string{"Fix evidence in evidence.go"})
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}
	bb := &Blackboard{ChainState: map[string]any{"goap_fusion_program_milestone": p.ID + ":0"}}
	run := verifiedProgramRunForTest(t, bb, []SuperpowersTask{{Objective: p.Milestones[0].Goal, Files: []string{"evidence.go"}}})
	run.User = "personal-owner"
	if err := reconcileProgramDelivery(context.Background(), run); err == nil {
		t.Fatal("personal run changed shared program state")
	}
	run.User = ""
	if err := research.UpdatePrograms(path, func(ps *research.ProgramStore) error {
		ps.Programs[0].Milestones[0].Goal = "unrelated new goal in evidence.go"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := reconcileProgramDelivery(context.Background(), run); err == nil {
		t.Fatal("changed goal inherited completion for the old task")
	}
	ps, _ = research.OpenPrograms(path)
	if ps.Programs[0].Milestones[0].Status != "pending" {
		t.Fatal("rejected attribution altered the goal")
	}
}

func TestProgramPlanningStopsWhenBacklogCannotBeRead(t *testing.T) {
	path := withGoapPrograms(t)
	if err := os.WriteFile(path, []byte(`{"programs":[null]}`), 0600); err != nil {
		t.Fatal(err)
	}
	bb := &Blackboard{Task: "improve framework", ChainState: map[string]any{}}
	status := GetAction("PrioritizeGoapGoals")(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if status != -1 || bb.Outcome != "program_state_unavailable" {
		t.Fatalf("unreadable backlog permitted planning: %d %s", status, bb.Outcome)
	}
}

func TestProgramPlanningBatchStopsAtReviewHold(t *testing.T) {
	path := withGoapPrograms(t)
	ps, _ := research.OpenPrograms(path)
	p := ps.Add("held dependency", "audit", []string{"first task", "reviewed task", "dependent task"})
	ps.MarkNeedsReview(p.ID, 1, "prior-run", "test passed before implementation")
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}
	bb := &Blackboard{Task: "improve framework", ChainState: map[string]any{}}
	status := GetAction("PrioritizeGoapGoals")(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if status != 1 || bb.ChainState["goap_fusion_program_milestone"] != p.ID+":0" {
		t.Fatalf("batch crossed review hold: %d %+v", status, bb.ChainState)
	}
	bb.ChainState["goap_fusion_program_milestone"] = p.ID + ":2"
	if _, err := captureProgramMilestones(bb); err == nil {
		t.Fatal("stale selection crossed a newly placed review hold")
	}
}

func TestProgramPrecheckCannotReviewChangedGoal(t *testing.T) {
	id := seedPrecheckProgram(t)
	prior := goapRedPrecheckRunFn
	t.Cleanup(func() { goapRedPrecheckRunFn = prior })
	goapRedPrecheckRunFn = func(string) (string, error) {
		err := research.UpdatePrograms(currentGoapProgramsPath(), func(ps *research.ProgramStore) error {
			ps.Programs[0].Milestones[0].Goal = "new task with the same recorded command"
			return nil
		})
		return "passed", err
	}
	bb := &Blackboard{RunID: "precheck", ChainState: map[string]any{}}
	precheckGoapStaleMilestones(bb)
	ps, err := research.OpenPrograms(currentGoapProgramsPath())
	if err != nil {
		t.Fatal(err)
	}
	if ps.Programs[0].ID != id || ps.Programs[0].Milestones[0].Status != "pending" {
		t.Fatal("old test result put a changed goal into review")
	}
}
