package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// writeCommittedImplReport builds a `## Superpowers Implementation Complete`
// report whose artifact dir exists on disk with run.json + finish.md, so the
// VerifyGoapFusionEvidence committed-impl branch passes its os.Stat guards.
func writeCommittedImplReport(t *testing.T) string {
	t.Helper()
	artifactDir := filepath.Join(t.TempDir(), "run-artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	for _, f := range []string{"run.json", "finish.md"} {
		if err := os.WriteFile(filepath.Join(artifactDir, f), []byte("{}"), 0o644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}
	return fmt.Sprintf("## Superpowers Implementation Complete\n\nRun: `20260713T000000-abc`\nArtifacts: `%s`\nApply status: `committed`\nCommit: `deadbee`\n", artifactDir)
}

// TestVerifyGoapFusionEvidenceClassifiesCommitted: a committed implementation
// keeps outcome success and is scored authoritatively high.
func TestVerifyGoapFusionEvidenceClassifiesCommitted(t *testing.T) {
	fn := GetAction("VerifyGoapFusionEvidence")
	if fn == nil {
		t.Fatal("VerifyGoapFusionEvidence not registered")
	}
	bb := &Blackboard{Result: writeCommittedImplReport(t)}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code != 1 {
		t.Fatalf("committed impl gate = %d, want 1; result: %s", code, bb.Result)
	}
	if bb.OutcomeRefinement != "" {
		t.Fatalf("committed impl must not refine outcome, got %q", bb.OutcomeRefinement)
	}
	if !bb.QualityAuthoritative || bb.QualityScore != 0.9 {
		t.Fatalf("committed impl quality = %v (authoritative=%v), want 0.9 authoritative", bb.QualityScore, bb.QualityAuthoritative)
	}
}

// TestVerifyGoapFusionEvidenceClassifiesNoChange: an analysis-only cycle with no
// degraded marker is a healthy no-code run — refine to "no_change", score 0.5.
func TestVerifyGoapFusionEvidenceClassifiesNoChange(t *testing.T) {
	fn := GetAction("VerifyGoapFusionEvidence")
	report := writeGoapFusionCycleReport(t,
		"delegated to apply-stage worktree verification (bare main repo)",
		"graphify update .: PASSED")
	bb := &Blackboard{Result: report}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code != 1 {
		t.Fatalf("no-change gate = %d, want 1; result: %s", code, bb.Result)
	}
	if bb.OutcomeRefinement != "no_change" {
		t.Fatalf("cycle-only refinement = %q, want no_change", bb.OutcomeRefinement)
	}
	if !bb.QualityAuthoritative || bb.QualityScore != 0.5 {
		t.Fatalf("no-change quality = %v (authoritative=%v), want 0.5 authoritative", bb.QualityScore, bb.QualityAuthoritative)
	}
}

// TestVerifyGoapFusionEvidenceClassifiesDegraded: a cycle that carries the
// impl-degraded fallback marker is refined to "degraded", scored 0.3.
func TestVerifyGoapFusionEvidenceClassifiesDegraded(t *testing.T) {
	fn := GetAction("VerifyGoapFusionEvidence")
	report := writeGoapFusionCycleReport(t,
		"delegated to apply-stage worktree verification (bare main repo)",
		"graphify update .: PASSED")
	report += "\n## Implementation Degraded (Fallback)\nClaudeSuperpowersPath failed; degraded to deterministic analysis.\n"
	bb := &Blackboard{Result: report}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code != 1 {
		t.Fatalf("degraded gate = %d, want 1; result: %s", code, bb.Result)
	}
	if bb.OutcomeRefinement != "degraded" {
		t.Fatalf("degraded refinement = %q, want degraded", bb.OutcomeRefinement)
	}
	if !bb.QualityAuthoritative || bb.QualityScore != 0.3 {
		t.Fatalf("degraded quality = %v (authoritative=%v), want 0.3 authoritative", bb.QualityScore, bb.QualityAuthoritative)
	}
}
