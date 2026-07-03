package engine

import (
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// Regression: RunScheduledGoapFusionCycle sits at the END of the Phase-0
// preflight — BEFORE the main tree's research/gap-analysis/plan steps ever
// run. It was registered as a bare alias of the existing-plan runtime, which
// FAILS when no plan path exists, so from the moment WireGoapFusionLoopTree
// went live in production (2026-07-03 21:35) every scheduled loop cycle died
// in ~120ms with "No existing plan path found" before doing any work.
// With no saved plan to resume it must succeed as a no-op and let the main
// sequence drive the cycle; with a saved plan it resumes it as before.
func TestRunScheduledGoapFusionCycleNoSavedPlanIsNoOpSuccess(t *testing.T) {
	fn := GetAction("RunScheduledGoapFusionCycle")
	if fn == nil {
		t.Fatal("RunScheduledGoapFusionCycle not registered")
	}
	bb := &Blackboard{Task: "improve the platform"}
	if got := fn(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("preflight cycle driver without a saved plan must no-op succeed, got %d: %s", got, bb.Result)
	}
	if !strings.Contains(bb.Result, "no saved plan") {
		t.Fatalf("result must say the main cycle will plan: %s", bb.Result)
	}
}

func TestRunSuperpowersClaudeImplementationStillRequiresPlan(t *testing.T) {
	// The in-gate implementation step keeps the strict contract: reaching it
	// without a plan path is a real pipeline error, not a no-op.
	fn := GetAction("RunSuperpowersClaudeImplementation")
	bb := &Blackboard{Task: "improve"}
	if got := fn(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != -1 {
		t.Fatalf("implementation step without a plan must fail, got %d", got)
	}
}
