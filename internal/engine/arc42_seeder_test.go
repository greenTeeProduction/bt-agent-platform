package engine

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/research"

	btcore "github.com/rvitorper/go-bt/core"
)

// The arc42 program seeder is a dedicated agent action: it reads the LIVE
// arc42 document (never a copy — arc42GoalsDocPaths resolves the original
// docs/arc42/go-bt-evolve-arc42.md), targets one quality goal per run, and
// seeds ~/.go-bt-evolve/research/programs.json with a program that must
// name that goal. It reuses the loop seeder's grounding gates so fabricated
// greenfield proposals are rejected.

func withSeederEnv(t *testing.T) {
	t.Helper()
	oldPrograms := goapProgramsPath
	goapProgramsPath = filepath.Join(t.TempDir(), "programs.json")
	oldExists := goapFusionRepoFileExistsFn
	goapFusionRepoFileExistsFn = func(rel string) bool { return true }
	t.Cleanup(func() {
		goapProgramsPath = oldPrograms
		goapFusionRepoFileExistsFn = oldExists
	})
}

func runArc42Seeder(t *testing.T) *Blackboard {
	t.Helper()
	fn := GetAction("SeedProgramFromArc42Goals")
	if fn == nil {
		t.Fatal("action SeedProgramFromArc42Goals not registered")
	}
	bb := &Blackboard{Task: "seed next program from arc42 quality goals"}
	if got := fn(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("seeder action must not fail the tree, got %d (result: %s)", got, bb.Result)
	}
	return bb
}

func TestArc42SeederSeedsGoalTargetedProgram(t *testing.T) {
	withSeederEnv(t)
	withArc42Doc(t, arc42GoalsTestDoc)

	goal := arc42SeedTargetGoal()
	if goal == nil {
		t.Fatal("target goal must resolve from the test doc")
	}
	var gotPrompt string
	oldFetch := seedProgramFetchFn
	seedProgramFetchFn = func(prompt string) string {
		gotPrompt = prompt
		return "PROGRAM: Advance " + goal.ID + " " + goal.Name + " gates\n" +
			"MILESTONE1: Extend validation in internal/engine/arc42_goals.go with coverage counters and tests\n" +
			"MILESTONE2: Wire the counters into internal/engine/nlm_quota.go rotation and pin with tests\n"
	}
	t.Cleanup(func() { seedProgramFetchFn = oldFetch })

	bb := runArc42Seeder(t)

	if !strings.Contains(gotPrompt, goal.ID) || !strings.Contains(gotPrompt, goal.Name) {
		t.Fatalf("seed prompt must target the chosen goal %s %s:\n%s", goal.ID, goal.Name, gotPrompt)
	}
	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps.Programs) != 1 {
		t.Fatalf("expected 1 seeded program, got %d (result: %s)", len(ps.Programs), bb.Result)
	}
	p := ps.Programs[0]
	if !strings.HasPrefix(p.Source, "arc42-seeder") {
		t.Fatalf("program source must identify the seeder agent, got %q", p.Source)
	}
	if !strings.Contains(p.Title, goal.ID) {
		t.Fatalf("seeded program title must name the targeted goal %s, got %q", goal.ID, p.Title)
	}
	for _, kw := range []string{"arc42", "program"} {
		if !strings.Contains(strings.ToLower(bb.Result), kw) {
			t.Fatalf("report must contain quality keyword %q: %s", kw, bb.Result)
		}
	}
}

func TestArc42SeederRejectsProposalNotNamingGoal(t *testing.T) {
	withSeederEnv(t)
	withArc42Doc(t, arc42GoalsTestDoc)
	oldFetch := seedProgramFetchFn
	seedProgramFetchFn = func(prompt string) string {
		return "PROGRAM: Generic platform hardening\n" +
			"MILESTONE1: Extend validation in internal/engine/arc42_goals.go with tests\n" +
			"MILESTONE2: Wire counters into internal/engine/nlm_quota.go with tests\n"
	}
	t.Cleanup(func() { seedProgramFetchFn = oldFetch })

	bb := runArc42Seeder(t)
	ps, _ := research.OpenPrograms(goapProgramsPath)
	if len(ps.Programs) != 0 {
		t.Fatalf("proposal that names no quality goal must be rejected, got %d programs", len(ps.Programs))
	}
	if !strings.Contains(strings.ToLower(bb.Result), "reject") {
		t.Fatalf("report must say the proposal was rejected: %s", bb.Result)
	}
}

func TestArc42SeederSkipsWhenProgramActive(t *testing.T) {
	withSeederEnv(t)
	withArc42Doc(t, arc42GoalsTestDoc)
	ps, _ := research.OpenPrograms(goapProgramsPath)
	ps.Add("Existing active program", "test", []string{"Do something in internal/engine/registry.go"})
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}
	fetched := false
	oldFetch := seedProgramFetchFn
	seedProgramFetchFn = func(prompt string) string { fetched = true; return "" }
	t.Cleanup(func() { seedProgramFetchFn = oldFetch })

	bb := runArc42Seeder(t)
	if fetched {
		t.Fatal("must not burn research budget while a program is active")
	}
	if !strings.Contains(strings.ToLower(bb.Result), "active") {
		t.Fatalf("report must say a program is active: %s", bb.Result)
	}
	// A skip because a program is still active is the expected steady state
	// (4 identical Telegram notifications/day on 2026-07-15) — a healthy
	// no-op, refined to no_change so downstream consumers can throttle it.
	if bb.OutcomeRefinement != "no_change" {
		t.Fatalf("OutcomeRefinement = %q, want no_change for the healthy program-active skip", bb.OutcomeRefinement)
	}
	if !bb.QualityAuthoritative || bb.QualityScore != 0.5 {
		t.Fatalf("quality = (%v, authoritative=%v), want (0.5, true)", bb.QualityScore, bb.QualityAuthoritative)
	}
}

func TestArc42SeederReportsWhenGoalsUnavailable(t *testing.T) {
	withSeederEnv(t)
	withArc42Doc(t, "") // no doc — goals unavailable
	fetched := false
	oldFetch := seedProgramFetchFn
	seedProgramFetchFn = func(prompt string) string { fetched = true; return "" }
	t.Cleanup(func() { seedProgramFetchFn = oldFetch })

	bb := runArc42Seeder(t)
	if fetched {
		t.Fatal("must not seed without live arc42 goals")
	}
	if !strings.Contains(strings.ToLower(bb.Result), "arc42") {
		t.Fatalf("report must explain the arc42 doc is unavailable: %s", bb.Result)
	}
	// Unlike the program-active skip, a missing/unparseable arc42 doc is a
	// real problem the operator must see — it must NOT be refined into the
	// throttleable no_change state.
	if bb.OutcomeRefinement != "" {
		t.Fatalf("OutcomeRefinement = %q, want empty — goals-unavailable is not a routine no-op", bb.OutcomeRefinement)
	}
}
