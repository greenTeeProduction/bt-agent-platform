package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

func TestGoapFusion_IsApplyRequestDoesNotTreatReportWritingAsCodeApply(t *testing.T) {
	fn := GetCondition("IsApplyRequest")
	if fn == nil {
		t.Fatal("IsApplyRequest condition not registered")
	}

	shouldNotApply := []string{
		"research goap fusion status and write deterministic analysis only",
		"generate scheduled fusion report only",
		"analyze gaps and do not apply code changes",
		"Scheduled GOAP fusion cycle: read vault research and graphify report, identify improvement gaps, prioritize goals, apply highest-priority improvements via Superpowers runtime, run health checks, and record vault analysis.",
	}
	for _, task := range shouldNotApply {
		if fn(&Blackboard{Task: task}) {
			t.Fatalf("IsApplyRequest(%q) = true; scheduled/report-only tasks must stay on deterministic path", task)
		}
	}
}

func TestGoapFusion_IsApplyRequestAllowsExplicitCodeChangingTasks(t *testing.T) {
	fn := GetCondition("IsApplyRequest")
	if fn == nil {
		t.Fatal("IsApplyRequest condition not registered")
	}

	shouldApply := []string{
		"implement one concrete Superpowers runtime fix",
		"patch engine to add a regression test",
		"create domain tree for the new BT capability",
		"modify code to register the missing action",
	}
	for _, task := range shouldApply {
		if !fn(&Blackboard{Task: task}) {
			t.Fatalf("IsApplyRequest(%q) = false; explicit code-changing tasks must use Superpowers apply path", task)
		}
	}
}

func TestGoapFusionEvidenceRejectsFabricatedSelfCorrectedOutput(t *testing.T) {
	fn := GetAction("VerifyGoapFusionEvidence")
	if fn == nil {
		t.Fatal("VerifyGoapFusionEvidence action not registered")
	}
	bb := &Blackboard{Result: "## GOAP Fusion Cycle Complete — Self-Corrected Output\n\nClaude Code commands executed (simulated). Commit `a3f7c9e` merged."}
	code := fn(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if code != -1 {
		t.Fatalf("VerifyGoapFusionEvidence fabricated output code=%d, want -1", code)
	}
	if !strings.Contains(bb.Result, "GOAP Fusion Evidence Failed") || !strings.Contains(bb.Result, "fabrication marker") {
		t.Fatalf("expected evidence failure with fabrication marker, got: %s", bb.Result)
	}
}

func TestGoapFusionEvidenceAcceptsDeterministicAnalysisArtifact(t *testing.T) {
	fn := GetAction("VerifyGoapFusionEvidence")
	if fn == nil {
		t.Fatal("VerifyGoapFusionEvidence action not registered")
	}
	analysisPath := filepath.Join(t.TempDir(), "analysis.md")
	if err := os.WriteFile(analysisPath, []byte("analysis"), 0o644); err != nil {
		t.Fatal(err)
	}
	bb := &Blackboard{Result: fmt.Sprintf("## GOAP Fusion Cycle Complete\n\nAnalysis: `%s`\n\nVerification:\n```\ngo build ./cmd/bt-agent ./cmd/bt-agent-cli: PASSED\nfocused go tests: PASSED\ngraphify update .: PASSED\n```", analysisPath)}
	code := fn(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if code != 1 {
		t.Fatalf("VerifyGoapFusionEvidence deterministic analysis code=%d, want 1; result=%s", code, bb.Result)
	}
}
