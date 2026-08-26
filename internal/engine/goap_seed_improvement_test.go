package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"

	"github.com/nico/go-bt-evolve/internal/research"
)

// The 2026-07-10 starvation: both seeders rejected proposals ALL-OR-NOTHING
// (arc42 08:22 discarded a 5-milestone program over 1 malformed milestone),
// retried only on the next 4h schedule / next cycle, and the loop had no
// deterministic floor — 15 silent idle-seed attempts, zero programs, an idle
// fleet. These tests pin the fix: tolerant validation, one in-run retry with
// concrete feedback, a deterministic coverage fallback so an idle fleet
// ALWAYS gets grounded work, and seed outcomes visible in the analysis note.

func withSeederStores(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "programs.json")
	oldP := goapProgramsPath
	goapProgramsPath = path
	t.Cleanup(func() { goapProgramsPath = oldP })
	return path
}

func withSeedFetchSequence(t *testing.T, answers ...string) *[]string {
	t.Helper()
	old := seedProgramFetchFn
	var prompts []string
	seedProgramFetchFn = func(prompt string) string {
		prompts = append(prompts, prompt)
		i := len(prompts) - 1
		if i >= len(answers) {
			i = len(answers) - 1
		}
		return answers[i]
	}
	t.Cleanup(func() { seedProgramFetchFn = old })
	return &prompts
}

func withGroundingAlwaysTrue(t *testing.T) {
	t.Helper()
	old := goapFusionRepoFileExistsFn
	goapFusionRepoFileExistsFn = func(string) bool { return true }
	t.Cleanup(func() { goapFusionRepoFileExistsFn = old })
}

func withRepoFileList(t *testing.T, files []string) {
	t.Helper()
	old := goapFusionListRepoGoFilesFn
	goapFusionListRepoGoFilesFn = func() []string { return files }
	t.Cleanup(func() { goapFusionListRepoGoFilesFn = old })
}

// Validation partitions milestones instead of judging the whole proposal:
// enough valid milestones (>= 2, the store's own minimum) make a proposal
// acceptable with the bad ones dropped.
func TestValidateGoapProgramMilestones_PartitionsAndAccepts(t *testing.T) {
	withGroundingAlwaysTrue(t)
	oldEx := goapFusionRepoFileExistsFn
	goapFusionRepoFileExistsFn = func(p string) bool { return p != "internal/ghost/missing.go" }
	t.Cleanup(func() { goapFusionRepoFileExistsFn = oldEx })

	v := validateGoapProgramMilestones([]string{
		"Add retry classification to internal/reliability/reliability.go with tests",
		"Make everything better",                               // malformed: no file, prose
		"Extend the ghost module in internal/ghost/missing.go", // ungrounded
		"Harden config parsing in internal/config/config.go",
	})
	if len(v.Valid) != 2 || len(v.Malformed) != 1 || len(v.Ungrounded) != 1 {
		t.Fatalf("partition = valid:%d malformed:%d ungrounded:%d, want 2/1/1 (%+v)", len(v.Valid), len(v.Malformed), len(v.Ungrounded), v)
	}
	if !v.acceptable() {
		t.Fatal("2 valid milestones must be acceptable (bad ones dropped)")
	}
	if len(v.dropped()) != 2 {
		t.Fatalf("dropped = %d, want 2", len(v.dropped()))
	}
	if validateGoapProgramMilestones([]string{"Make everything better"}).acceptable() {
		t.Fatal("fewer than 2 valid milestones must stay unacceptable")
	}
}

// A rejected first proposal earns exactly ONE in-run retry whose prompt
// carries the concrete rejection reasons — instead of waiting 4h (arc42) or a
// full cycle (loop) for a blind re-ask.
func TestFetchAcceptableProgram_RetriesOnceWithFeedback(t *testing.T) {
	withGroundingAlwaysTrue(t)
	prompts := withSeedFetchSequence(t,
		"PROGRAM: Vague ambitions\nMILESTONE1: Make everything better\nMILESTONE2: Improve quality",
		"PROGRAM: Grounded plan\nMILESTONE1: Add retries to internal/reliability/reliability.go\nMILESTONE2: Cover internal/config/config.go with tests",
	)

	att := fetchAcceptableGoapProgram("BASE PROMPT", nil)
	if att.Fetches != 2 {
		t.Fatalf("fetches = %d, want 2 (one retry)", att.Fetches)
	}
	if att.Spec == nil || att.Spec.Title != "Grounded plan" || len(att.Spec.Milestones) != 2 {
		t.Fatalf("retry's acceptable proposal must be adopted: %+v", att.Spec)
	}
	retryPrompt := (*prompts)[1]
	if !strings.Contains(retryPrompt, "BASE PROMPT") {
		t.Fatal("retry must re-carry the base prompt")
	}
	if !strings.Contains(retryPrompt, "rejected") || !strings.Contains(retryPrompt, "Make everything better") {
		t.Fatalf("retry prompt must carry the concrete rejection feedback; got tail: %s", retryPrompt[len(retryPrompt)-min(len(retryPrompt), 400):])
	}

	// A persistently bad source stops after the single retry.
	bad := withSeedFetchSequence(t, "no program here at all")
	att = fetchAcceptableGoapProgram("BASE", nil)
	if att.Spec != nil || att.Fetches != 2 || len(*bad) != 2 {
		t.Fatalf("persistent garbage must stop after one retry: %+v (fetches=%d)", att.Spec, att.Fetches)
	}
}

// The loop seeder accepts a partially-valid proposal by pruning the bad
// milestones (the arc42 08:22 rejection discarded 4 good milestones over 1
// malformed one).
func TestSeedNextProgram_ToleratesPartiallyValidProposal(t *testing.T) {
	path := withSeederStores(t)
	withGroundingAlwaysTrue(t)
	withSeedFetchSequence(t, `PROGRAM: Mostly good plan
MILESTONE1: Add retry classification to internal/reliability/reliability.go with tests
MILESTONE2: Make everything better
MILESTONE3: Harden config parsing in internal/config/config.go`)

	bb := &Blackboard{ChainState: map[string]any{}}
	if got := GetAction("SeedNextProgram")(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("status = %d: %s", got, bb.Result)
	}
	ps, _ := research.OpenPrograms(path)
	p := ps.Active()
	if p == nil || len(p.Milestones) != 2 {
		t.Fatalf("proposal must persist with the malformed milestone dropped: %+v", p)
	}
	if !strings.Contains(bb.Result, "dropped 1") {
		t.Fatalf("result must report the dropped milestone count: %s", bb.Result)
	}
	if outcome, _ := bb.ChainState["goap_fusion_seed_outcome"].(string); !strings.Contains(outcome, "Mostly good plan") {
		t.Fatalf("seed outcome must be stamped for the analysis note; got %q", outcome)
	}
}

// untestedProductionGoFiles: only non-test .go production files lacking a
// sibling _test.go, sorted.
func TestUntestedProductionGoFiles(t *testing.T) {
	got := untestedProductionGoFiles([]string{
		"internal/b/covered.go",
		"internal/b/covered_test.go",
		"internal/a/naked.go",
		"cmd/tool/main.go",
		"internal/a/naked_helper_test.go", // tests someone else, not naked.go
		"docs/readme.md",
		"internal/c/only_test.go",
	})
	want := []string{"cmd/tool/main.go", "internal/a/naked.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("untested = %v, want %v", got, want)
	}
}

// When research produces nothing usable twice, the loop seeder falls back to
// a DETERMINISTIC coverage program built from untested production files —
// grounded by construction, deduped against files already claimed by earlier
// programs — so an idle fleet always gets work.
func TestSeedNextProgram_DeterministicCoverageFallback(t *testing.T) {
	path := withSeederStores(t)
	withGroundingAlwaysTrue(t)
	// A prior program already claims claimed.go — the fallback must skip it.
	pre, _ := research.OpenPrograms(path)
	pre.Add("Earlier work", "auto-seed", []string{
		"Do something in internal/x/claimed.go", "And more in internal/x/claimed.go",
	})
	for i := range pre.Programs[0].Milestones {
		pre.Programs[0].Milestones[i].Status = "done"
	}
	_ = pre.Save()

	withRepoFileList(t, []string{
		"internal/x/claimed.go",
		"internal/y/naked_one.go",
		"internal/y/naked_two.go",
		"internal/z/covered.go",
		"internal/z/covered_test.go",
	})
	withSeedFetchSequence(t, "research is down, no proposal")

	bb := &Blackboard{ChainState: map[string]any{}}
	if got := GetAction("SeedNextProgram")(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("status = %d: %s", got, bb.Result)
	}
	ps, _ := research.OpenPrograms(path)
	p := ps.Active()
	if p == nil || p.Source != "auto-seed:coverage" {
		t.Fatalf("fallback must persist a coverage program: %+v", p)
	}
	var joined strings.Builder
	for _, m := range p.Milestones {
		joined.WriteString(m.Goal + "\n")
	}
	if strings.Contains(joined.String(), "claimed.go") {
		t.Fatalf("fallback must skip files already claimed by earlier programs:\n%s", joined.String())
	}
	if !strings.Contains(joined.String(), "naked_one.go") || !strings.Contains(joined.String(), "naked_two.go") {
		t.Fatalf("fallback milestones must target the untested files:\n%s", joined.String())
	}
	if !strings.Contains(bb.Result, "PROGRAM-CONTINUE") {
		t.Fatalf("fallback seed must carry the continue marker: %s", bb.Result)
	}
	if outcome, _ := bb.ChainState["goap_fusion_seed_outcome"].(string); !strings.Contains(outcome, "coverage") {
		t.Fatalf("seed outcome must be stamped; got %q", outcome)
	}
}

// Seed outcomes must survive into the analysis note: the section renderer
// turns the stamped outcome into a report block (15 seed attempts ran
// invisibly on 2026-07-10 — only selector telemetry revealed them).
func TestGoapFusionSeedSection(t *testing.T) {
	if got := goapFusionSeedSection(&Blackboard{ChainState: map[string]any{}}); got != "" {
		t.Fatalf("no stamp → no section; got %q", got)
	}
	bb := &Blackboard{ChainState: map[string]any{}}
	setGoapState(bb, "seed_outcome", "seeded coverage program \"X\" (3 milestones)")
	sec := goapFusionSeedSection(bb)
	if !strings.Contains(sec, "Seeding") || !strings.Contains(sec, "coverage program") {
		t.Fatalf("section must render the stamped outcome: %q", sec)
	}
}

// ReportFusionCycle composes the run's final bb.Result — the only artifact
// that reaches runs/latest/output and the dashboard. The 11:34 verification
// cycle seeded a program AND degraded its implementation path, yet its final
// report showed neither: both outcomes were stamped in ChainState and then
// discarded. The final report must carry both sections.
func TestReportFusionCycleIncludesSeedAndDegradedSections(t *testing.T) {
	fn := GetAction("ReportFusionCycle")
	if fn == nil {
		t.Fatal("ReportFusionCycle not registered")
	}
	bb := &Blackboard{ChainState: map[string]any{}}
	setGoapState(bb, "fusion_analysis_path", "/tmp/x.md")
	setGoapState(bb, "verify_result", "ok")
	setGoapState(bb, "seed_outcome", `seeded program "DLQ cross-process replay" (4 milestones)`)
	setGoapState(bb, "impl_degraded", "true")
	setGoapState(bb, "impl_degraded_reason", "worktree setup failed")
	if code := fn(&btcore.BTContext[Blackboard]{Blackboard: bb}); code != 1 {
		t.Fatalf("ReportFusionCycle = %d, want 1", code)
	}
	if !strings.Contains(bb.Result, "Backlog Seeding") || !strings.Contains(bb.Result, "DLQ cross-process replay") {
		t.Fatalf("final report must carry the seed outcome:\n%s", bb.Result)
	}
	if !strings.Contains(bb.Result, "worktree setup failed") {
		t.Fatalf("final report must carry the impl-degraded reason:\n%s", bb.Result)
	}
}

// WriteFusionAnalysis's goals-unchanged fast path wrote a minimal note that
// silently dropped the seed and impl-degraded sections — exactly the cycle
// shape of the 11:34 verification run (seed + degraded implementation ends in
// the fast path via the impl-degraded goals_unchanged fallback).
func TestWriteFusionAnalysisFastPathIncludesSeedAndDegradedSections(t *testing.T) {
	dir := t.TempDir()
	old := goapFusionVaultDir
	goapFusionVaultDir = dir
	t.Cleanup(func() { goapFusionVaultDir = old })

	fn := GetAction("WriteFusionAnalysis")
	bb := &Blackboard{Task: "improve", ChainState: map[string]any{}}
	setGoapState(bb, "goal_queue", "[P0] Program X milestone 1/4: fix internal/reliability/reliability.go")
	setGoapState(bb, "goals_unchanged", "true")
	setGoapState(bb, "seed_outcome", `seeded program "X" (4 milestones)`)
	setGoapState(bb, "impl_degraded", "true")
	setGoapState(bb, "impl_degraded_reason", "claude rate limited")
	if code := fn(&btcore.BTContext[Blackboard]{Blackboard: bb}); code != 1 {
		t.Fatalf("WriteFusionAnalysis = %d, want 1: %s", code, bb.Result)
	}
	note, err := os.ReadFile(filepath.Join(dir, "goap-fusion-latest.md"))
	if err != nil {
		t.Fatalf("fast path must still write the latest note: %v", err)
	}
	for _, want := range []string{"Backlog Seeding", `seeded program "X"`, "claude rate limited"} {
		if !strings.Contains(string(note), want) {
			t.Fatalf("fast-path note missing %q:\n%s", want, note)
		}
	}
}

// The arc42 seeder gains the same tolerance: a proposal with one bad
// milestone but >= 2 valid goal-named ones seeds with the bad one dropped
// (the 08:22 all-or-nothing rejection cost 4 good milestones and 4 hours).
func TestArc42SeederToleratesDroppedMilestone(t *testing.T) {
	path := withSeederStores(t)
	withGroundingAlwaysTrue(t)
	goal := arc42SeedTargetGoal()
	if goal == nil {
		t.Skip("live arc42 goals unavailable in this checkout")
	}
	withSeedFetchSequence(t, "PROGRAM: Advance "+goal.ID+" resilience\n"+
		"MILESTONE1: Strengthen "+goal.ID+" handling in internal/reliability/reliability.go\n"+
		"MILESTONE2: Make everything better\n"+
		"MILESTONE3: Cover "+goal.ID+" paths in internal/config/config.go with tests")

	bb := &Blackboard{ChainState: map[string]any{}}
	if got := GetAction("SeedProgramFromArc42Goals")(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("status = %d: %s", got, bb.Result)
	}
	ps, _ := research.OpenPrograms(path)
	p := ps.Active()
	if p == nil || len(p.Milestones) != 2 {
		t.Fatalf("arc42 seeder must seed with the malformed milestone dropped: %+v", p)
	}
	if bb.Outcome != "arc42_seeder_seeded" {
		t.Fatalf("outcome = %q, want arc42_seeder_seeded; result: %s", bb.Outcome, bb.Result)
	}
	if !strings.Contains(bb.Result, "dropped 1") {
		t.Fatalf("result must report the dropped milestone: %s", bb.Result)
	}
}
