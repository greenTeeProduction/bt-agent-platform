package engine

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
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

// A NotebookLM research answer often echoes source-citation prose — a
// "(Community NN)" cluster label, a bracketed reference range like "[1-4]",
// LaTeX math delimiters ("$RFC 6902$"), and a "NotebookLM research:" meta
// prefix — describing a speculative architecture rather than an engineering
// instruction. Such a line is NOT an actionable goal even when the deterministic
// file-scoper has appended "(files: …)" paths to it: the appended paths must not
// launder research-paper prose into a planned task. This is the exact class of
// degenerate goal (KernelNode/VALIDPATCH/"PatchBoard" transition of the
// Blackboard) that repeatedly reaches the plan builder and fabricates scope for
// files that do not exist (internal/engine/kernel.go, blackboard.go, types.go).
func TestIsActionableGoapGoalRejectsResearchCitationProse(t *testing.T) {
	degenerate := "NotebookLM research: Implement the `KernelNode` composite and `VALIDPATCH` kernel function in `internal/engine/kernel.go` to transition the Blackboard (Community 51) to a PatchBoard architecture using validated JSON Patch mutations ($RFC 6902$) and role-specific Write Contracts [1-4]. (files: internal/engine/blackboard.go, internal/engine/types.go, internal/engine/registry.go)"
	if isActionableGoapGoal(degenerate) {
		t.Errorf("NotebookLM citation prose must be rejected as non-actionable even with appended file paths:\n%q", degenerate)
	}

	// The pipeline must drop it too, so a citation-prose answer never queues a task.
	bb := &Blackboard{}
	appendGoapResearchGoals(bb, []goapResearchGoal{{Goal: degenerate, Gap: "research-backed improvement"}})
	if lines := goapResearchGoalLines(bb); len(lines) != 0 {
		t.Errorf("citation-prose goal must not be queued, got %v", lines)
	}

	// Guard against over-filtering: a genuine, file-scoped imperative goal that
	// merely happens to mention a bracketed token must still pass.
	legit := "Add -short to the generated RED/GREEN test commands in internal/engine/actions_goap_fusion.go"
	if !isActionableGoapGoal(legit) {
		t.Errorf("legitimate file-scoped imperative goal must remain actionable: %q", legit)
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
	if ref, _ := bb.ChainState["goap_fusion_program_milestone"].(string); !strings.Contains(ref, ":0") {
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

// A RESUMED plan (preflight RunScheduledGoapFusionCycle) applies milestone
// work in a cycle whose fresh ChainState carries NO milestone stamp — the
// stamp died with the planning cycle. Completion must fall back to anchor
// evidence against pending milestones instead of silently no-opping: on
// 2026-07-10 the 12:00 cycle landed milestones 1-3 (28bc7d0) and left all of
// them pending, so the same cycle re-queued and re-implemented shipped work.
func TestCompleteGoapProgramMilestoneResumedRunFallsBackToAnchorEvidence(t *testing.T) {
	path := withGoapPrograms(t)
	ps, _ := research.OpenPrograms(path)
	ps.Add("DLQ cross-process replay", "auto-seed", []string{
		"Make DLQ persistence atomic in internal/reliability/reliability.go",
		"Wire replay scan in cmd/bt-agent/main.go",
		"Untouched milestone in internal/other/elsewhere.go",
	})
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}

	bb := &Blackboard{ChainState: map[string]any{}} // resumed cycle: no stamp
	run := &SuperpowersRun{
		ID:           "run-resume",
		ApplyStatus:  "committed",
		ChangedFiles: []string{"internal/reliability/reliability.go", "cmd/bt-agent/main.go"},
	}
	completeGoapProgramMilestone(bb, run)

	re, _ := research.OpenPrograms(path)
	ms := re.Programs[0].Milestones
	if ms[0].Status != "done" || ms[0].CompletedRun != "run-resume" {
		t.Fatalf("anchored milestone 0 must complete on resume-path evidence: %+v", ms[0])
	}
	if ms[1].Status != "done" {
		t.Fatalf("anchored milestone 1 must complete on resume-path evidence: %+v", ms[1])
	}
	if ms[2].Status != "pending" {
		t.Fatalf("milestone with untouched anchors must stay pending: %+v", ms[2])
	}
	if done, _ := bb.ChainState["goap_fusion_program_milestone_done"].(string); done == "" {
		t.Fatal("fallback completion must stamp program_milestone_done")
	}
}

// The evidence fallback must be strictly positive: a pending milestone naming
// NO Go-file anchors gets a free pass under the stamped path (trust the
// queued apply) but must NOT be checked off by an unstamped resume apply —
// there is no stamp tying it to the run.
func TestCompleteGoapProgramMilestoneResumeFallbackSkipsAnchorlessMilestones(t *testing.T) {
	path := withGoapPrograms(t)
	ps, _ := research.OpenPrograms(path)
	ps.Add("Vague program", "test", []string{"m1 with no file anchors", "m2 also anchorless"})
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}

	bb := &Blackboard{ChainState: map[string]any{}}
	completeGoapProgramMilestone(bb, &SuperpowersRun{
		ID: "run-x", ApplyStatus: "committed",
		ChangedFiles: []string{"internal/engine/tree.go"},
	})

	re, _ := research.OpenPrograms(path)
	for i, m := range re.Programs[0].Milestones {
		if m.Status != "pending" {
			t.Fatalf("anchorless milestone %d must stay pending in the fallback: %+v", i, m)
		}
	}
}

// A cached run that already reached finish (applied, worktree cleaned) is
// consumed: reusing it sends the next implementation into a deleted worktree
// ("red-phase claude failed: chdir ...: no such file or directory", 12:44:56
// on 2026-07-10). currentSuperpowersRun must start a fresh run instead.
func TestCurrentSuperpowersRunRefusesFinishedRun(t *testing.T) {
	bb := &Blackboard{Task: "improve things", ChainState: map[string]any{}}
	finished := &SuperpowersRun{
		ID: "run-finished", Phase: SuperpowersPhaseFinish,
		ApplyStatus: "committed", WorktreePath: "/tmp/worktrees/gone",
	}
	setSuperpowersRun(bb, finished)

	run, err := currentSuperpowersRun(bb)
	if err != nil {
		t.Fatal(err)
	}
	if run.ID == "run-finished" {
		t.Fatal("a finished run must not be reused for new implementation work")
	}
	if run.Phase != SuperpowersPhaseDesign || run.WorktreePath != "" {
		t.Fatalf("replacement run must start fresh: %+v", run)
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

func TestPrioritizeGoapGoalsBatchesPendingMilestones(t *testing.T) {
	path := withGoapPrograms(t)
	ps, _ := research.OpenPrograms(path)
	ps.Add("Big program", "test", []string{
		"Milestone A in internal/a2a/a.go",
		"Milestone B in internal/a2a/b.go",
		"Milestone C in internal/a2a/c.go",
		"Milestone D in internal/a2a/d.go",
	})
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}
	bb := &Blackboard{Task: "improve", ChainState: map[string]any{
		"goap_fusion_improvement_gaps": "",
	}}
	fn := GetAction("PrioritizeGoapGoals")
	if got := fn(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("status = %d; %s", got, bb.Result)
	}
	queue, _ := bb.ChainState["goap_fusion_goal_queue"].(string)
	for _, want := range []string{"milestone 1/4", "milestone 2/4", "milestone 3/4"} {
		if !strings.Contains(queue, want) {
			t.Fatalf("queue must batch pending milestones up to task capacity, missing %s:\n%s", want, queue)
		}
	}
	if strings.Contains(queue, "milestone 4/4") {
		t.Fatalf("batch must cap at %d milestones:\n%s", maxGoalDrivenTasks, queue)
	}
	ref, _ := bb.ChainState["goap_fusion_program_milestone"].(string)
	if len(strings.Split(ref, ",")) != 3 {
		t.Fatalf("all batched refs must be stamped for completion: %q", ref)
	}
}

func TestCompleteGoapProgramMilestoneBatchCompletion(t *testing.T) {
	path := withGoapPrograms(t)
	ps, _ := research.OpenPrograms(path)
	p := ps.Add("Big program", "test", []string{
		"Milestone A in internal/a2a/a.go",
		"Milestone B in internal/a2a/b.go",
		"Milestone C in internal/a2a/c.go",
	})
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}
	bb := &Blackboard{ChainState: map[string]any{
		"goap_fusion_program_milestone": p.ID + ":0," + p.ID + ":1," + p.ID + ":2",
	}}
	// The run only executed milestones A and B (anchor files changed);
	// C's anchor is untouched and must stay pending.
	run := &SuperpowersRun{ID: "run-batch", ChangedFiles: []string{"internal/a2a/a.go", "internal/a2a/b.go"}}
	completeGoapProgramMilestone(bb, run)

	re, _ := research.OpenPrograms(path)
	got := []string{re.Programs[0].Milestones[0].Status, re.Programs[0].Milestones[1].Status, re.Programs[0].Milestones[2].Status}
	if got[0] != "done" || got[1] != "done" || got[2] != "pending" {
		t.Fatalf("anchor-verified batch completion wrong: %v", got)
	}
}

func TestProgramContinueNote(t *testing.T) {
	path := withGoapPrograms(t)
	ps, _ := research.OpenPrograms(path)
	ps.Add("Big program", "test", []string{"Milestone A in internal/a2a/a.go"})
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}
	if note := programContinueNote(); !strings.Contains(note, "PROGRAM-CONTINUE") {
		t.Fatalf("pending milestones must emit the continue marker, got %q", note)
	}
	ps.MarkDone(ps.Programs[0].ID, 0, "run-x")
	_ = ps.Save()
	if note := programContinueNote(); note != "" {
		t.Fatalf("completed programs must not emit the marker: %q", note)
	}
}

func TestActiveProgramMilestoneNeverRoutesToAnalysisOnUnchangedGoals(t *testing.T) {
	path := withGoapPrograms(t)
	ps, _ := research.OpenPrograms(path)
	ps.Add("Prog", "test", []string{
		"Milestone one in internal/a2a/one.go",
		"Milestone two in internal/engine/two.go",
	})
	// milestone 1 done → milestone 2 is the active pending one.
	ps.MarkDone(ps.Programs[0].ID, 0, "run-prior")
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}

	fn := GetAction("PrioritizeGoapGoals")
	// Run twice with an IDENTICAL gap set: milestone 2 produces the same goal
	// queue both times. Without the guard, the second run would set
	// goals_unchanged=true and route to analysis, never implementing it.
	for i := 0; i < 2; i++ {
		bb := &Blackboard{Task: "improve", ChainState: map[string]any{
			"goap_fusion_improvement_gaps": "",
		}}
		if got := fn(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
			t.Fatalf("run %d status=%d", i, got)
		}
		if uc, _ := bb.ChainState["goap_fusion_goals_unchanged"].(string); uc == "true" {
			t.Fatalf("run %d: an active program milestone must never set goals_unchanged=true (would skip implementation)", i)
		}
		if ref, _ := bb.ChainState["goap_fusion_program_milestone"].(string); ref == "" {
			t.Fatalf("run %d: active milestone must be queued", i)
		}
	}
}

// persistGoapProgram and completeGoapProgramMilestone both do a bare
// OpenPrograms + mutate + Save with no cross-writer coordination — the same
// lost-update gap research.UpdatePrograms exists to close (see its doc
// comment, and TestRefundGoapMilestoneAttempt_ConcurrentWritersAllSurvive in
// actions_goap_fusion_refund_test.go for the sibling call sites already
// migrated). persistGoapProgram in particular is reached from many distinct
// sources in the same running fleet — notebooklm/grill research proposals,
// claude_review, design-followup, arc42-seeder, and auto-seed coverage — so
// two of those firing around the same moment against the SAME programs.json
// race: whichever Save's in-memory copy was loaded before a sibling's Save
// landed clobbers that sibling's already-persisted program registration with
// its own stale copy, silently dropping it.
func TestPersistGoapProgram_ConcurrentCallersAllSurvive(t *testing.T) {
	path := withGoapPrograms(t)

	const workers = 30
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			bb := &Blackboard{ChainState: map[string]any{}}
			spec := &goapProgramSpec{
				Title:      fmt.Sprintf("Concurrent program %d", i),
				Milestones: []string{"m1"},
			}
			persistGoapProgram(bb, spec, "test")
		}()
	}
	wg.Wait()

	ps, err := research.OpenPrograms(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(ps.Programs), workers; got != want {
		t.Fatalf("lost programs under concurrent persistGoapProgram (no flock coordination): got %d, want %d", got, want)
	}
}

// Sibling to the persist race above: completeGoapProgramMilestone's own
// OpenPrograms+Save (marking a DIFFERENT program's milestone done per
// goroutine, sharing one store) must not let concurrent completions clobber
// each other.
func TestCompleteGoapProgramMilestone_ConcurrentCallersAllSurvive(t *testing.T) {
	path := withGoapPrograms(t)
	ps, err := research.OpenPrograms(path)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 30
	ids := make([]string, workers)
	anchors := make([]string, workers)
	for i := 0; i < workers; i++ {
		anchor := fmt.Sprintf("internal/engine/complete_race_%d.go", i)
		anchors[i] = anchor
		p := ps.Add(fmt.Sprintf("Complete race program %d", i), "test", []string{"Wire work in " + anchor})
		ids[i] = p.ID
	}
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			bb := &Blackboard{ChainState: map[string]any{
				"goap_fusion_program_milestone": ids[i] + ":0",
			}}
			run := &SuperpowersRun{ID: fmt.Sprintf("run-%d", i), ChangedFiles: []string{anchors[i]}}
			completeGoapProgramMilestone(bb, run)
		}()
	}
	wg.Wait()

	final, err := research.OpenPrograms(path)
	if err != nil {
		t.Fatal(err)
	}
	lost := 0
	for _, id := range ids {
		for _, p := range final.Programs {
			if p.ID != id {
				continue
			}
			if p.Milestones[0].Status != "done" {
				lost++
			}
		}
	}
	if lost != 0 {
		t.Fatalf("lost %d/%d milestone completions under concurrent OpenPrograms+Save (no flock coordination): want 0 lost", lost, workers)
	}
}
