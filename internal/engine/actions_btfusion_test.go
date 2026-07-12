package engine

import (
	"strings"
	"testing"
)

// CheckCodebaseFit runs a diagnostic-only shell probe (fusionCodebaseFitCmd)
// to gather codebase-fit evidence. The probe's exit code hinges on live,
// external state it doesn't control (systemctl --user daemon status, agent
// YAML presence under ~/.go-bt-evolve/agents) — a transient nonzero exit
// there must not hard-fail the whole bt_fusion cycle. Regression: before
// this fix, CheckCodebaseFit propagated any nonzero probe exit as
// bb.Outcome = "fusion_codebase_fit_failed" and returned -1, killing the
// cycle over what is only best-effort evidence gathering.
//
// fusionCodebaseFitRun is the seam CheckCodebaseFit must use to run the
// probe so this test can force a nonzero exit deterministically — the real
// fusionCodebaseFitCmd depends on live daemon/service state that already
// exits 0 in dev sandboxes, making the failure path otherwise unreachable
// from a hermetic test.
func TestCheckCodebaseFitToleratesNonzeroProbeExit(t *testing.T) {
	old := fusionCodebaseFitRun
	fusionCodebaseFitRun = func(command string) (string, int) {
		return "simulated probe failure: systemctl --user unavailable\n", 1
	}
	t.Cleanup(func() { fusionCodebaseFitRun = old })

	bb := &Blackboard{Task: "fusion", ChainState: map[string]any{}}
	code := runFusionAction(t, "CheckCodebaseFit", bb)

	if code != 1 {
		t.Fatalf("CheckCodebaseFit on nonzero probe exit = %d, want 1 (best-effort, continue)", code)
	}
	if bb.Outcome == "fusion_codebase_fit_failed" {
		t.Fatalf("CheckCodebaseFit must not fail the cycle over a diagnostic-only probe error, got outcome %q", bb.Outcome)
	}
	if !strings.Contains(bb.Result, "Codebase Fit Evidence") {
		t.Fatalf("expected evidence section in result even on nonzero exit, got: %s", bb.Result)
	}
}

// A clean probe exit must still report success and leave no failure outcome.
func TestCheckCodebaseFitSucceedsOnZeroProbeExit(t *testing.T) {
	old := fusionCodebaseFitRun
	fusionCodebaseFitRun = func(command string) (string, int) {
		return "trees=ok\nagents=ok\nservice=ok\n", 0
	}
	t.Cleanup(func() { fusionCodebaseFitRun = old })

	bb := &Blackboard{Task: "fusion", ChainState: map[string]any{}}
	code := runFusionAction(t, "CheckCodebaseFit", bb)

	if code != 1 {
		t.Fatalf("CheckCodebaseFit on zero probe exit = %d, want 1", code)
	}
	if bb.Outcome == "fusion_codebase_fit_failed" {
		t.Fatalf("CheckCodebaseFit set failure outcome on a clean probe exit: %q", bb.Outcome)
	}
}
