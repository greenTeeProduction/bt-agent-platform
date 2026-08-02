package research

import "testing"

// 2026-08-01: the arc42 seeder skipped 15 of its last 18 runs with "a program is
// still active" and has seeded nothing since 2026-07-18. The deterministic
// coverage fallback — the "auto-seed:coverage" floor that exists precisely for
// when research is unusable — keeps the backlog permanently non-empty, so the
// one-program-at-a-time rule starves the research-grounded seeders it was
// supposed to coexist with. Programs created per day since 07-22 are almost
// entirely coverage: the fleet spends Opus-at-max-effort cycles writing
// characterization tests for its own files because nothing else can get queued.
//
// Filler is filler: it must neither block seeding nor outrank real work.

func addProgram(t *testing.T, ps *ProgramStore, title, source string) *Program {
	t.Helper()
	return ps.Add(title, source, []string{"milestone one for " + title, "milestone two for " + title})
}

// A coverage program alone must not read as "busy" to the seeders.
func TestActiveExcludingFiller_CoverageAloneDoesNotBlockSeeding(t *testing.T) {
	ps := &ProgramStore{}
	addProgram(t, ps, "Deterministic coverage backlog: characterization tests", CoverageFillerSource)

	if got := ps.Active(); got == nil {
		t.Fatal("Active() must still return the coverage program — the loop should work on filler when there is nothing better")
	}
	if got := ps.ActiveExcludingFiller(); got != nil {
		t.Fatalf("ActiveExcludingFiller() = %q, want nil: an active coverage program must not "+
			"block the arc42/research seeders, or the filler crowds out the work it was meant to stand in for", got.Title)
	}
}

// Real work still blocks seeding — one-program-at-a-time is intact for it.
func TestActiveExcludingFiller_RealProgramStillBlocksSeeding(t *testing.T) {
	ps := &ProgramStore{}
	addProgram(t, ps, "Q3 Reliability — harden the webhook path", "arc42-seeder:Q3")

	if got := ps.ActiveExcludingFiller(); got == nil {
		t.Fatal("a real active program must still block seeding")
	}
}

// Ordering: once a real program exists alongside filler, the loop must work on
// the real one. Otherwise seeding past the filler would queue arc42 goals that
// never get picked up.
func TestActive_PrefersRealWorkOverFiller(t *testing.T) {
	ps := &ProgramStore{}
	addProgram(t, ps, "Deterministic coverage backlog: characterization tests", CoverageFillerSource)
	addProgram(t, ps, "Q2 Evolvability — close the fitness feedback loop", "arc42-seeder:Q2")

	got := ps.Active()
	if got == nil || got.Source != "arc42-seeder:Q2" {
		t.Fatalf("Active() = %v, want the arc42 program: filler must never outrank real work", got)
	}
}

// Self-fix keeps its existing top priority over both.
func TestActive_SelfFixStillOutranksEverything(t *testing.T) {
	ps := &ProgramStore{}
	addProgram(t, ps, "Deterministic coverage backlog", CoverageFillerSource)
	addProgram(t, ps, "Q2 Evolvability — feedback loop", "arc42-seeder:Q2")
	addProgram(t, ps, "Fix the shepherd race", SelfFixSourcePrefix+"self-review:abc")

	got := ps.Active()
	if got == nil || got.Title != "Fix the shepherd race" {
		t.Fatalf("Active() = %v, want the self-fix program to keep top priority", got)
	}
}
