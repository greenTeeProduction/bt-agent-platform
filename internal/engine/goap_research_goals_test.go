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
	appendGoapResearchGoals(bb, []goapResearchGoal{{Goal: "Add grill coverage in internal/engine/a.go", Gap: "g1"}})
	appendGoapResearchGoals(bb, []goapResearchGoal{{Goal: "Fix research routing in internal/engine/b.go", Gap: "g2"}, {Goal: "Add grill coverage in internal/engine/a.go", Gap: "dup"}})
	lines := goapResearchGoalLines(bb)
	if len(lines) != 2 {
		t.Fatalf("goals must accumulate and dedupe across sources, got %v", lines)
	}
	if !strings.Contains(lines[0], "grill coverage") || !strings.Contains(lines[1], "research routing") {
		t.Fatalf("order must be preserved: %v", lines)
	}
}

// A gap whose text spans multiple lines must not desync the parallel
// goap_fusion_notebooklm_goals / goap_fusion_notebooklm_gaps ChainState
// strings. Those two strings are persisted independently and re-split with
// splitNonEmptyLines, then the gap-analysis consumer pairs goal[i] with
// gap[i]. A 2-line gap on the first goal makes the gaps list longer than the
// goals list, so every later goal steals a fragment of an earlier gap instead
// of getting its own — index alignment silently breaks.
func TestAppendGoapResearchGoalsKeepsGoalGapPairsAligned(t *testing.T) {
	bb := &Blackboard{}
	appendGoapResearchGoals(bb, []goapResearchGoal{
		{Goal: "First goal touching internal/engine/a.go", Gap: "root cause line one\nspilled second line"},
		{Goal: "Second goal touching internal/engine/b.go", Gap: "second goal's own gap"},
	})

	goals := goapResearchGoalLines(bb)
	gaps := goapResearchGapLines(bb)
	if len(goals) != len(gaps) {
		t.Fatalf("goal/gap lists must stay the same length so index pairing is safe: %d goals, %d gaps\ngoals=%v\ngaps=%v",
			len(goals), len(gaps), goals, gaps)
	}
	if len(goals) < 2 || !strings.Contains(goals[1], "Second goal") {
		t.Fatalf("second goal mispositioned: %v", goals)
	}
	if gaps[1] != "second goal's own gap" {
		t.Fatalf("gap[1] must stay bound to goal[1]; got %q (skewed by an earlier multi-line gap)", gaps[1])
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

// A successful apply whose changed files never touch the milestone's file
// anchors — and whose tasks never reached the anchored work — must NOT
// complete the milestone. Otherwise a cycle that drifted onto unrelated goals
// silently checks off milestone work it never did.
func TestCompleteGoapProgramMilestoneSkipsRunThatMissedAnchors(t *testing.T) {
	path := withGoapPrograms(t)
	ps, _ := research.OpenPrograms(path)
	p := ps.Add("Auction allocation", "test", []string{
		"Wire bid evaluation in internal/engine/actions_a2a.go",
		"m2",
	})
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}

	bb := &Blackboard{ChainState: map[string]any{
		"goap_fusion_program_milestone": p.ID + ":0",
	}}
	run := &SuperpowersRun{
		ID:           "run-drift",
		ApplyStatus:  "applied",
		ChangedFiles: []string{"internal/other/unrelated.go"},
		Tasks: []SuperpowersTask{
			{Title: "unrelated", Files: []string{"internal/other/unrelated.go"}, Status: "done"},
		},
	}
	completeGoapProgramMilestone(bb, run)

	re, _ := research.OpenPrograms(path)
	if re.Programs[0].Milestones[0].Status != "pending" {
		t.Fatalf("milestone must stay pending when the run touched no anchor files: %+v", re.Programs[0].Milestones[0])
	}
	if done, _ := bb.ChainState["goap_fusion_program_milestone_done"].(string); done != "" {
		t.Fatalf("program_milestone_done must not be stamped for a run that missed the anchors, got %q", done)
	}
}

// The milestone completes when the run demonstrably executed it — either its
// changed files intersect the anchors, or a milestone-tagged task in the run
// reached done on an anchor file.
func TestCompleteGoapProgramMilestoneCompletesWhenRunExecutedAnchors(t *testing.T) {
	anchor := "internal/engine/actions_a2a.go"
	cases := []struct {
		name string
		run  *SuperpowersRun
	}{
		{
			name: "via changed files",
			run:  &SuperpowersRun{ID: "run-changed", ChangedFiles: []string{anchor}},
		},
		{
			name: "via done task on anchor",
			run: &SuperpowersRun{ID: "run-task", Tasks: []SuperpowersTask{
				{Title: "bid eval", Files: []string{anchor}, Status: "done"},
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := withGoapPrograms(t)
			ps, _ := research.OpenPrograms(path)
			p := ps.Add("Auction allocation", "test", []string{
				"Wire bid evaluation in " + anchor,
				"m2",
			})
			if err := ps.Save(); err != nil {
				t.Fatal(err)
			}

			bb := &Blackboard{ChainState: map[string]any{
				"goap_fusion_program_milestone": p.ID + ":0",
			}}
			completeGoapProgramMilestone(bb, tc.run)

			re, _ := research.OpenPrograms(path)
			if re.Programs[0].Milestones[0].Status != "done" || re.Programs[0].Milestones[0].CompletedRun != tc.run.ID {
				t.Fatalf("milestone must complete when the run executed it: %+v", re.Programs[0].Milestones[0])
			}
		})
	}
}

func TestIsActionableGoapGoalRejectsProse(t *testing.T) {
	prose := []string{
		"Review complete. Summary of what I found across the commits.",
		"Here is my analysis of the recent changes.",
		"Overall the code is in good shape with a few minor issues.",
		"In summary, the pipeline works as intended.",
		"The following findings were identified during review.",
		"ok",
	}
	for _, p := range prose {
		if isActionableGoapGoal(p) {
			t.Fatalf("prose must not become a goal: %q", p)
		}
	}
	actionable := []string{
		"Add regression tests for the auction bid evaluator in internal/a2a/auction.go",
		"Ensure all domain trees have smoke tests, descriptions, and condition coverage",
		"fix the stale index handling in internal/engine/superpowers_worktree.go",
		"Persist the CIRCUITPOLICY state-hash history across cron ticks (files: internal/engine/actions_superpowers.go)",
	}
	for _, a := range actionable {
		if !isActionableGoapGoal(a) {
			t.Fatalf("actionable goal wrongly rejected: %q", a)
		}
	}
}

func TestFallbackGoapGoalSkipsProseToActionableLine(t *testing.T) {
	answer := `Review complete. Summary of what I found across the commits.

The recent changes look solid overall.

Fix the flaky retry timing in internal/reliability/errors.go and pin it with a regression test.`
	got := fallbackGoapGoal(answer)
	if !strings.Contains(got, "internal/reliability/errors.go") {
		t.Fatalf("fallback must skip prose and pick the actionable line, got %q", got)
	}
	if fallbackGoapGoal("Review complete.\n\nEverything looks good.") != "" {
		t.Fatal("all-prose answers must yield no goal at all")
	}
}

func TestAppendGoapResearchGoalsDropsProseGoals(t *testing.T) {
	bb := &Blackboard{}
	appendGoapResearchGoals(bb, []goapResearchGoal{
		{Goal: "Review complete. Summary of what I found.", Gap: "n/a"},
		{Goal: "Add auction bidder evaluation in internal/a2a/auction.go", Gap: "milestone"},
	})
	lines := goapResearchGoalLines(bb)
	if len(lines) != 1 || !strings.Contains(lines[0], "auction bidder") {
		t.Fatalf("prose goals must be dropped at the accumulator: %v", lines)
	}
}
