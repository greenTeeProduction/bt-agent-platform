package engine

import (
	"path/filepath"
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// TestValidateOutputQuality_RespectsAuthoritativeQuality reproduces the
// 2026-07-15 quality inflation: a domain classifier (VerifyGoapFusionEvidence)
// stamps an authoritative degraded score (0.3), but validateOutputQuality runs
// after the tree completes and clobbered b.QualityScore with its own text-shape
// heuristic (0.5+0.2+0.1+0.1 = 0.8999999999999999 — the exact float recorded
// for the day's degraded cycles). When QualityAuthoritative is set, the
// heuristic must not overwrite the authoritative score.
func TestValidateOutputQuality_RespectsAuthoritativeQuality(t *testing.T) {
	longStructured := "## Report\n\n**Summary** of a degraded cycle with markdown structure\n" +
		"- bullet one with enough detail to cross the length thresholds\n" +
		"- bullet two padding the result out beyond two hundred characters so the\n" +
		"  heuristic would score it high on shape alone, which is precisely the bug\n"

	bb := &Blackboard{
		Result:               longStructured,
		QualityScore:         0.3,
		QualityAuthoritative: true,
	}
	validateOutputQuality(bb)
	if bb.QualityScore != 0.3 {
		t.Fatalf("validateOutputQuality clobbered an authoritative QualityScore: got %v, want 0.3 preserved", bb.QualityScore)
	}

	// Authoritative scores survive the short-output and error-pattern paths too.
	short := &Blackboard{Result: "tiny", QualityScore: 0.3, QualityAuthoritative: true}
	validateOutputQuality(short)
	if short.QualityScore != 0.3 {
		t.Fatalf("short-output path clobbered authoritative score: got %v, want 0.3", short.QualityScore)
	}

	// Control: without the authoritative flag the heuristic still owns the score.
	plain := &Blackboard{Result: longStructured, QualityScore: 0.3}
	validateOutputQuality(plain)
	if plain.QualityScore == 0.3 {
		t.Fatal("non-authoritative score should be recomputed by the heuristic, but stayed at the preset value")
	}
}

// TestValidateOutputQuality_IgnoresErrorWordsInFencedBlocks: reports that
// embed CLI transcripts legitimately contain error words inside ``` fences
// (the notebooklm researcher embedded nlm's import output verbatim). The
// error-pattern scan must ignore fenced content — otherwise a healthy 17KB
// research report scores 0.1 and gets destroyed by the fallback path.
func TestValidateOutputQuality_IgnoresErrorWordsInFencedBlocks(t *testing.T) {
	report := "## Research Report\n\n**Findings**: substantive research content with plenty of length\n" +
		"- finding one, grounded in sources and long enough to be a real report body\n" +
		"- finding two, more grounded detail so the structural checks are satisfied\n\n" +
		"### Import\n\n```\nError: nlm daily import budget exhausted (local cap 2, used 2) — skipping source import\n```\n\n" +
		"### Status\n\nResearch completed and verified.\n"

	bb := &Blackboard{Result: report}
	if !validateOutputQuality(bb) {
		t.Fatalf("a healthy report failed the quality gate because of an error word inside a fenced CLI transcript (score=%v)", bb.QualityScore)
	}

	// Control: an error pattern OUTSIDE a fence still fails the gate.
	bad := &Blackboard{Result: "## Report\n\nError: everything is broken and nothing was produced beyond this apology text."}
	if validateOutputQuality(bad) {
		t.Fatal("an unfenced error pattern must still fail the quality gate")
	}
}

// TestDefaultFallback_PreservesResultAndScoresHonestly: DefaultFallback
// previously REPLACED bb.Result with boilerplate ("## Fallback Executed…")
// that then scored 0.9 — higher than the genuine report it destroyed (the
// notebooklm-researcher lost ~23h of real research output this way). The
// fallback must keep the existing result, append its note, and stamp an
// authoritative degraded quality so dashboards stop overstating it.
func TestDefaultFallback_PreservesResultAndScoresHonestly(t *testing.T) {
	bb := &Blackboard{
		Task:   "research task",
		Result: "NOTEBOOKLM EVIDENCE VERIFIED\n\nreal research content that must survive the fallback",
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}

	fn := bb.actionForName("DefaultFallback")
	if fn == nil {
		t.Fatal("actionForName(DefaultFallback) returned nil")
	}
	if code := fn(ctx); code != 1 {
		t.Fatalf("DefaultFallback returned %d, want 1 (Success)", code)
	}

	if !strings.Contains(bb.Result, "real research content that must survive the fallback") {
		t.Fatalf("DefaultFallback destroyed the pre-existing result; got: %q", bb.Result)
	}
	if !strings.Contains(bb.Result, "Fallback") {
		t.Fatalf("DefaultFallback should still note that the fallback path ran; got: %q", bb.Result)
	}
	if bb.OutcomeRefinement != "degraded" {
		t.Fatalf("a fallback run is a degraded run, not a clean success; OutcomeRefinement=%q, want \"degraded\"", bb.OutcomeRefinement)
	}
	if !bb.QualityAuthoritative || bb.QualityScore != 0.3 {
		t.Fatalf("fallback quality must be authoritative 0.3 (not the boilerplate's shape score); got authoritative=%v score=%v", bb.QualityAuthoritative, bb.QualityScore)
	}
}

// TestNlmBudgetDenyMessagesAreSkipsNotErrors: the local daily budget denials
// are expected steady-state skips, but their "Error:" prefix (a) tripped the
// generic output-quality gate when embedded in reports and (b) read as auth
// failures to operators. They must not be error-prefixed — while STILL being
// detected by isGoapNotebookLMFailure so the goap research path keeps routing
// budget-denied queries to its Claude fallback.
func TestNlmBudgetDenyMessagesAreSkipsNotErrors(t *testing.T) {
	tmp := t.TempDir()
	prevUsage, prevCache := nlmUsagePath, nlmQueryCachePath
	nlmUsagePath = filepath.Join(tmp, "nlm-usage.json")
	nlmQueryCachePath = filepath.Join(tmp, "nlm-query-cache.json")
	t.Cleanup(func() { nlmUsagePath = prevUsage; nlmQueryCachePath = prevCache })

	t.Setenv("BT_NLM_QUERY_BUDGET", "0")
	t.Setenv("BT_NLM_RESEARCH_BUDGET", "0")
	t.Setenv("BT_NLM_IMPORT_BUDGET", "0")

	cases := []struct {
		name string
		args []string
	}{
		{"query", []string{"notebook", "query", "nb-id", "what changed?"}},
		{"research", []string{"research", "start", "nb-id", "topic"}},
		{"import", []string{"research", "import", "nb-id"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, deny, proceed := nlmPreflight(c.args)
			if proceed || deny == "" {
				t.Fatalf("budget 0 must deny the %s call (proceed=%v deny=%q)", c.name, proceed, deny)
			}
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(deny)), "error:") {
				t.Fatalf("budget deny must not be error-prefixed (it trips the output-quality gate): %q", deny)
			}
			if !isGoapNotebookLMFailure(deny) {
				t.Fatalf("budget deny must still register as an nlm miss so goap research falls back to Claude: %q", deny)
			}
		})
	}
}
