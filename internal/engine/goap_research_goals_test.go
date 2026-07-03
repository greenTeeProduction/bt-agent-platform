package engine

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/research"

	btcore "github.com/rvitorper/go-bt/core"
)

func TestExtractGoapResearchGoalsParsesNumberedBlocks(t *testing.T) {
	answer := `Some preamble.
GOAL1: Add auction-based allocation to the A2A layer.
GAP1: No coordination primitive exists.
FILES1: internal/a2a/server.go, internal/engine/actions_a2a.go
GOAL2: Harden the gardener gate.
GAP2: Gate rescale left debt.
FILES2: internal/gardener/gardener.go
CITATIONS: [1][4]`
	goals := extractGoapResearchGoals(answer)
	if len(goals) != 2 {
		t.Fatalf("expected 2 goals, got %d: %+v", len(goals), goals)
	}
	if !strings.Contains(goals[0].Goal, "auction-based") || !strings.Contains(goals[0].Files, "internal/a2a/server.go") {
		t.Fatalf("goal 1 wrong: %+v", goals[0])
	}
	if goals[1].Gap != "Gate rescale left debt." {
		t.Fatalf("goal 2 gap wrong: %+v", goals[1])
	}
}

func TestExtractGoapResearchGoalsAcceptsLegacySingleFormat(t *testing.T) {
	answer := "GOAL: Fix the thing in `internal/engine/tree.go`.\nGAP: It is broken."
	goals := extractGoapResearchGoals(answer)
	if len(goals) != 1 || !strings.Contains(goals[0].Goal, "Fix the thing") {
		t.Fatalf("legacy GOAL:/GAP: must still parse, got %+v", goals)
	}
}

func TestGoalLineEmbedsFilesForPlanBuilder(t *testing.T) {
	g := goapResearchGoal{Goal: "Add auction allocation", Gap: "missing", Files: "internal/a2a/server.go"}
	line := g.Line()
	// The plan builder extracts file paths from the goal line text; FILES
	// must therefore be embedded when the goal itself names no .go paths.
	if !strings.Contains(line, "internal/a2a/server.go") {
		t.Fatalf("goal line must embed FILES paths: %q", line)
	}
	withPath := goapResearchGoal{Goal: "Fix `internal/engine/tree.go` routing", Files: "internal/engine/tree.go"}
	if strings.Count(withPath.Line(), "internal/engine/tree.go") != 1 {
		t.Fatalf("goal already naming the path must not duplicate it: %q", withPath.Line())
	}
}

func TestAppendGoapResearchGoalsAccumulatesAcrossSources(t *testing.T) {
	bb := &Blackboard{}
	appendGoapResearchGoals(bb, []goapResearchGoal{{Goal: "grill goal", Gap: "g1"}})
	appendGoapResearchGoals(bb, []goapResearchGoal{{Goal: "research goal", Gap: "g2"}, {Goal: "grill goal", Gap: "dup"}})
	lines := goapResearchGoalLines(bb)
	if len(lines) != 2 {
		t.Fatalf("goals must accumulate and dedupe across sources, got %v", lines)
	}
	if !strings.Contains(lines[0], "grill goal") || !strings.Contains(lines[1], "research goal") {
		t.Fatalf("order must be preserved: %v", lines)
	}
}

func TestExtractGoapProgramParsesMilestones(t *testing.T) {
	answer := `PROGRAM: Auction-based multi-agent task allocation
MILESTONE1: Define auction message types in internal/a2a/messages.go
MILESTONE2: Implement bid evaluation in internal/engine/actions_a2a.go
MILESTONE3: Wire auction routing into internal/domains/trees.go`
	spec := extractGoapProgram(answer)
	if spec == nil {
		t.Fatal("program block must parse")
	}
	if spec.Title != "Auction-based multi-agent task allocation" || len(spec.Milestones) != 3 {
		t.Fatalf("bad program: %+v", spec)
	}
	if extractGoapProgram("GOAL1: no program here") != nil {
		t.Fatal("answers without PROGRAM must yield nil")
	}
}

func TestRecentImplementedGoalsReadsStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knowledge.json")
	old := btFusionKnowledgePath
	btFusionKnowledgePath = path
	t.Cleanup(func() { btFusionKnowledgePath = old })

	store, _ := research.Open(path)
	store.Record("goap:implemented", "add -short to test commands", "add -short to test commands")
	store.Record("vault:note.md", "unrelated vault note", "vault content")
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	got := recentImplementedGoals(5)
	if len(got) != 1 || !strings.Contains(got[0], "-short") {
		t.Fatalf("must list only implemented goals, got %v", got)
	}
}

func TestScopeGoapGoalLineAppendsGrepMatches(t *testing.T) {
	oldFn := goapScopeGrepFn
	goapScopeGrepFn = func(keyword string) []string {
		if keyword == "auction" {
			return []string{"internal/a2a/server.go", "internal/engine/actions_a2a.go"}
		}
		return nil
	}
	t.Cleanup(func() { goapScopeGrepFn = oldFn })

	scoped := scopeGoapGoalLine("[P1] Implement auction-based task allocation for A2A agent coordination")
	if !strings.Contains(scoped, "internal/a2a/server.go") {
		t.Fatalf("fileless goal must gain grep-scoped files: %q", scoped)
	}
	already := "[P0] Fix internal/engine/tree.go now"
	if scopeGoapGoalLine(already) != already {
		t.Fatal("goals already naming files must pass through unchanged")
	}
}

func withGoapPrograms(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "programs.json")
	old := goapProgramsPath
	goapProgramsPath = path
	t.Cleanup(func() { goapProgramsPath = old })
	return path
}

func TestPrioritizeGoapGoalsQueuesActiveProgramMilestoneFirst(t *testing.T) {
	path := withGoapPrograms(t)
	ps, _ := research.OpenPrograms(path)
	ps.Add("Auction allocation", "test", []string{
		"Define auction messages in internal/a2a/messages.go",
		"Wire bid evaluation in internal/engine/actions_a2a.go",
	})
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}

	bb := &Blackboard{Task: "improve", ChainState: map[string]any{
		"goap_fusion_improvement_gaps": "NOTEBOOKLM_GOAL: some research goal in internal/engine/tree.go\nNOTEBOOKLM_GAP: because",
	}}
	fn := GetAction("PrioritizeGoapGoals")
	if fn == nil {
		t.Fatal("PrioritizeGoapGoals not registered")
	}
	if got := fn(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("status = %d; result: %s", got, bb.Result)
	}
	queue, _ := bb.ChainState["goap_fusion_goal_queue"].(string)
	lines := strings.Split(queue, "\n")
	if !strings.Contains(lines[0], "Program") || !strings.Contains(lines[0], "milestone 1/2") {
		t.Fatalf("active program milestone must head the queue, got: %s", queue)
	}
	if ref, _ := bb.ChainState["goap_fusion_program_milestone"].(string); !strings.HasSuffix(ref, ":0") {
		t.Fatalf("program milestone ref must be stamped for completion, got %q", ref)
	}
	if !strings.Contains(queue, "some research goal") {
		t.Fatalf("research goals must still follow the milestone: %s", queue)
	}
}

func TestCompleteGoapProgramMilestoneMarksDone(t *testing.T) {
	path := withGoapPrograms(t)
	ps, _ := research.OpenPrograms(path)
	p := ps.Add("Auction allocation", "test", []string{"m1", "m2"})
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}

	bb := &Blackboard{ChainState: map[string]any{
		"goap_fusion_program_milestone": p.ID + ":0",
	}}
	completeGoapProgramMilestone(bb, &SuperpowersRun{ID: "run-777"})

	re, _ := research.OpenPrograms(path)
	if re.Programs[0].Milestones[0].Status != "done" || re.Programs[0].Milestones[0].CompletedRun != "run-777" {
		t.Fatalf("milestone must be marked done by the applied run: %+v", re.Programs[0].Milestones[0])
	}
	if re.Programs[0].Milestones[1].Status != "pending" {
		t.Fatal("later milestones must stay pending")
	}
}
