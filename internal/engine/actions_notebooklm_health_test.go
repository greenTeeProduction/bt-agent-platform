package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	btcore "github.com/rvitorper/go-bt/core"
)

// 2026-08-01: notebooklm-pipeline-monitor recorded outcome=success on all 35 of
// its runs since 07-30 while its OWN report said the pipeline was broken —
// "Source Count: 3 (target 40)", "LOCK_TIMEOUT on research query after 120s",
// "Write lock held by nlm-consumer (PID 18422) until 2026-07-31T18:11:28Z"
// (~9h), "NEW PLANS NEEDED: YES". "success" was true of the monitor (it did
// produce a report) and false of the thing it monitors, and the outcome is what
// the throttle, the breaker and the dashboard read.
//
// The verdict is derived from the synthesis files themselves, NOT by parsing the
// agent's prose: the report is LLM output whose wording changes run to run,
// while the producer's own file carries the facts.

func writeSynthesis(t *testing.T, dir, name, body string, age time.Duration) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mt := time.Now().Add(-age)
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
	return p
}

func withSynthesesDir(t *testing.T, dir string) {
	t.Helper()
	prev := nlmHealthSynthesesDir
	nlmHealthSynthesesDir = dir
	t.Cleanup(func() { nlmHealthSynthesesDir = prev })
}

func runHealthAction(t *testing.T) *Blackboard {
	t.Helper()
	act := GetAction("AssessNotebookLMPipelineHealth")
	if act == nil {
		t.Fatal("AssessNotebookLMPipelineHealth not registered")
	}
	bb := &Blackboard{ChainState: map[string]any{}}
	if got := act(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("action status = %d, want 1 — assessing health must never fail the monitor's tree", got)
	}
	return bb
}

func TestAssessNotebookLMPipelineHealth(t *testing.T) {
	healthy := "Source Count: 42 (target 40)\nImported: 42/42 successful\n"

	t.Run("healthy pipeline leaves the outcome alone", func(t *testing.T) {
		dir := t.TempDir()
		withSynthesesDir(t, dir)
		writeSynthesis(t, dir, "nlm-research-2026-08-01T021152.md", healthy, time.Hour)

		bb := runHealthAction(t)
		if bb.OutcomeRefinement != "" {
			t.Fatalf("OutcomeRefinement = %q, want empty for a healthy pipeline", bb.OutcomeRefinement)
		}
	})

	t.Run("source shortfall degrades", func(t *testing.T) {
		dir := t.TempDir()
		withSynthesesDir(t, dir)
		writeSynthesis(t, dir, "nlm-research-2026-08-01T021152.md",
			"Source Count: 3 (target 40)\nResearch tables queried: 0\n", time.Hour)

		bb := runHealthAction(t)
		if bb.OutcomeRefinement != "degraded" {
			t.Fatalf("OutcomeRefinement = %q, want degraded: the live run reported 3 of a target 40 "+
				"and still recorded success", bb.OutcomeRefinement)
		}
	})

	t.Run("producer-side failure marker degrades", func(t *testing.T) {
		dir := t.TempDir()
		withSynthesesDir(t, dir)
		writeSynthesis(t, dir, "nlm-research-2026-08-01T021152.md",
			"Status: STALE (import blocked)\nLOCK_TIMEOUT on research query after 120s\nSource Count: 41 (target 40)\n", time.Hour)

		bb := runHealthAction(t)
		if bb.OutcomeRefinement != "degraded" {
			t.Fatalf("OutcomeRefinement = %q, want degraded: the synthesis self-labels a blocked import", bb.OutcomeRefinement)
		}
	})

	t.Run("stale research degrades", func(t *testing.T) {
		dir := t.TempDir()
		withSynthesesDir(t, dir)
		writeSynthesis(t, dir, "nlm-research-2026-07-20T021152.md", healthy, 72*time.Hour)

		bb := runHealthAction(t)
		if bb.OutcomeRefinement != "degraded" {
			t.Fatalf("OutcomeRefinement = %q, want degraded: newest research is 72h old (threshold 48h)", bb.OutcomeRefinement)
		}
	})

	t.Run("no research at all degrades", func(t *testing.T) {
		withSynthesesDir(t, t.TempDir())

		bb := runHealthAction(t)
		if bb.OutcomeRefinement != "degraded" {
			t.Fatalf("OutcomeRefinement = %q, want degraded: an empty vault is not a healthy pipeline", bb.OutcomeRefinement)
		}
	})

	t.Run("the monitor's own reports are never mistaken for research", func(t *testing.T) {
		dir := t.TempDir()
		withSynthesesDir(t, dir)
		writeSynthesis(t, dir, "nlm-research-2026-07-20T021152.md", healthy, 72*time.Hour)
		// The monitor writes its own summaries into the vault; counting one as
		// fresh research is the self-read that produced 39 days of false
		// "research is fine" verdicts in the 2026-07-15 stale-glob incident.
		writeSynthesis(t, dir, "nlm-consumer-summary.md", healthy, time.Minute)

		bb := runHealthAction(t)
		if bb.OutcomeRefinement != "degraded" {
			t.Fatalf("OutcomeRefinement = %q, want degraded: only nlm-research-*.md is producer output", bb.OutcomeRefinement)
		}
	})
}
