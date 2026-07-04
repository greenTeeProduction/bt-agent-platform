package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// writeGoapFusionCycleReport builds a `## GOAP Fusion Cycle Complete` report
// whose Analysis path exists on disk (so the os.Stat guard passes) and whose
// Verification block carries the supplied verify/graphify lines. It mirrors the
// shape ReportFusionCycle emits in production, including the normalized
// `Build/tests: DELEGATED ...` line that ReportFusionCycle appends whenever the
// verify_result carries VerifyGoapBuild's bare-repo delegation marker.
func writeGoapFusionCycleReport(t *testing.T, verify, graphify string) string {
	t.Helper()
	analysisPath := filepath.Join(t.TempDir(), "analysis.md")
	if err := os.WriteFile(analysisPath, []byte("# analysis\n"), 0o644); err != nil {
		t.Fatalf("write analysis artifact: %v", err)
	}
	report := fmt.Sprintf("## GOAP Fusion Cycle Complete\n\nAnalysis: `%s`\n\nVerification:\n```\n%s\n%s\n```", analysisPath, verify, graphify)
	if strings.Contains(verify, "delegated to apply-stage worktree verification") {
		report += "\n\nBuild/tests: DELEGATED (bare main repo, verified in apply worktree)"
	}
	return report
}

// TestVerifyGoapFusionEvidenceAcceptsDelegatedVerification mirrors
// TestVerifyGoapBuildDelegatesOnBareRepo at the evidence-gate layer: on the
// bare main repo VerifyGoapBuild passes through with the delegation note
// "delegated to apply-stage worktree verification" instead of the two build/
// test PASSED strings. The deterministic evidence gate must accept that
// delegation marker as valid build/test evidence — otherwise every
// ScheduledAnalysisPath cycle dead-letters after a successful apply.
func TestVerifyGoapFusionEvidenceAcceptsDelegatedVerification(t *testing.T) {
	fn := GetAction("VerifyGoapFusionEvidence")
	if fn == nil {
		t.Fatal("VerifyGoapFusionEvidence not registered")
	}
	report := writeGoapFusionCycleReport(t,
		"delegated to apply-stage worktree verification (bare main repo)",
		"graphify update .: PASSED")
	bb := &Blackboard{Result: report}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code != 1 {
		t.Fatalf("VerifyGoapFusionEvidence with delegated verification = %d, want 1; result: %s",
			code, bb.Result[:min(len(bb.Result), 400)])
	}
}

// TestReportFusionCycleAppendsNormalizedDelegationLine pins the new contract:
// when goap_fusion_verify_result carries VerifyGoapBuild's bare-repo delegation
// marker, ReportFusionCycle must append an explicit, self-describing
// `Build/tests: DELEGATED (bare main repo, verified in apply worktree)` line to
// the Verification block instead of leaving the raw internal note as the only
// evidence. This makes the report self-describing and gives GOAL1's gate a
// stable token to key on.
func TestReportFusionCycleAppendsNormalizedDelegationLine(t *testing.T) {
	fn := GetAction("ReportFusionCycle")
	if fn == nil {
		t.Fatal("ReportFusionCycle not registered")
	}
	bb := &Blackboard{ChainState: map[string]any{
		"goap_fusion_fusion_analysis_path":   "/tmp/analysis.md",
		"goap_fusion_verify_result":          "delegated to apply-stage worktree verification (bare main repo)",
		"goap_fusion_graphify_update_result": "graphify update .: PASSED",
	}}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code != 1 {
		t.Fatalf("ReportFusionCycle = %d, want 1", code)
	}
	want := "Build/tests: DELEGATED (bare main repo, verified in apply worktree)"
	if !strings.Contains(bb.Result, want) {
		t.Fatalf("ReportFusionCycle output missing normalized delegation line %q; got:\n%s", want, bb.Result)
	}
}

// TestReportFusionCycleOmitsDelegationLineWhenGenuinelyVerified is the negative
// side: a cycle that produced real build/test PASSED evidence (no delegation
// marker) must NOT gain a spurious DELEGATED line.
func TestReportFusionCycleOmitsDelegationLineWhenGenuinelyVerified(t *testing.T) {
	fn := GetAction("ReportFusionCycle")
	if fn == nil {
		t.Fatal("ReportFusionCycle not registered")
	}
	bb := &Blackboard{ChainState: map[string]any{
		"goap_fusion_fusion_analysis_path":   "/tmp/analysis.md",
		"goap_fusion_verify_result":          "go build ./cmd/bt-agent ./cmd/bt-agent-cli: PASSED\nfocused go tests: PASSED",
		"goap_fusion_graphify_update_result": "graphify update .: PASSED",
	}}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code != 1 {
		t.Fatalf("ReportFusionCycle = %d, want 1", code)
	}
	if strings.Contains(bb.Result, "Build/tests: DELEGATED") {
		t.Fatalf("ReportFusionCycle appended a spurious DELEGATED line for a genuinely verified cycle; got:\n%s", bb.Result)
	}
}

// TestVerifyGoapFusionEvidenceKeysOnNormalizedDelegationToken proves the
// decoupling: the evidence gate must accept a report whose Verification block
// carries the normalized `Build/tests: DELEGATED ...` token even when
// VerifyGoapBuild's raw internal note has been reworded away. Keying on the
// normalized token (not the exact wording of VerifyGoapBuild's note) means a
// future reword of one doesn't silently re-break the other.
func TestVerifyGoapFusionEvidenceKeysOnNormalizedDelegationToken(t *testing.T) {
	fn := GetAction("VerifyGoapFusionEvidence")
	if fn == nil {
		t.Fatal("VerifyGoapFusionEvidence not registered")
	}
	// The verify line here deliberately does NOT contain the legacy
	// "delegated to apply-stage worktree verification" wording — only the
	// normalized token appears, standing in for a future reword of
	// VerifyGoapBuild's internal note.
	report := writeGoapFusionCycleReport(t,
		"Build/tests: DELEGATED (bare main repo, verified in apply worktree)\n(verification handed to the apply-stage worktree)",
		"graphify update .: PASSED")
	if strings.Contains(report, "delegated to apply-stage worktree verification") {
		t.Fatalf("test setup leaked the legacy marker; report:\n%s", report)
	}
	bb := &Blackboard{Result: report}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code != 1 {
		t.Fatalf("VerifyGoapFusionEvidence keyed on normalized token = %d, want 1; result: %s",
			code, bb.Result[:min(len(bb.Result), 400)])
	}
}

// TestVerifyGoapFusionEvidenceRejectsBogusVerification pins the negative side of
// the either/or check: a `## GOAP Fusion Cycle Complete` report whose
// Verification block carries neither the two build/test PASSED strings nor the
// delegation marker must still fail the gate.
func TestVerifyGoapFusionEvidenceRejectsBogusVerification(t *testing.T) {
	fn := GetAction("VerifyGoapFusionEvidence")
	if fn == nil {
		t.Fatal("VerifyGoapFusionEvidence not registered")
	}
	report := writeGoapFusionCycleReport(t,
		"nothing was actually verified here",
		"graphify update .: PASSED")
	bb := &Blackboard{Result: report}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code == 1 {
		t.Fatalf("VerifyGoapFusionEvidence with bogus verification = 1, want failure (-1)")
	}
	if !strings.Contains(bb.Result, "GOAP Fusion Evidence Failed") {
		t.Fatalf("expected evidence-failed report, got: %s", bb.Result[:min(len(bb.Result), 400)])
	}
}
