package engine

import (
	"path/filepath"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// A cycle with zero new knowledge is a healthy no-op, not a full success:
// recording it as success/0.9 made the hourly Telegram stream 80% identical
// "0 new findings" messages (13/15 on 2026-07-15). ReportNoNewResearch must
// refine the outcome to no_change — the SLO-deferred healthy terminal state —
// with the honest-signal quality convention (0.5 authoritative, matching the
// goap analysis-only path), so notification and stats layers can tell a quiet
// cycle from real work.
func TestReportNoNewResearchRefinesOutcomeToNoChange(t *testing.T) {
	old := btFusionKnowledgePath
	btFusionKnowledgePath = filepath.Join(t.TempDir(), "knowledge.json")
	t.Cleanup(func() { btFusionKnowledgePath = old })

	fn := GetAction("ReportNoNewResearch")
	if fn == nil {
		t.Fatal("ReportNoNewResearch not registered")
	}
	bb := &Blackboard{}
	if got := fn(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("ReportNoNewResearch = %d, want 1", got)
	}
	if bb.OutcomeRefinement != "no_change" {
		t.Fatalf("OutcomeRefinement = %q, want no_change", bb.OutcomeRefinement)
	}
	if !bb.QualityAuthoritative || bb.QualityScore != 0.5 {
		t.Fatalf("quality = (%v, authoritative=%v), want (0.5, true)", bb.QualityScore, bb.QualityAuthoritative)
	}
}
