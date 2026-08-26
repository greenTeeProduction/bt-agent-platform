package evolution

import "testing"

// Consecutive failures on one tree must not disable the gate for other trees:
// the gardener runs ~50 trees through one gate instance, and a handful of
// chronically low-fitness trees must not freeze evolution fleet-wide.
func TestQualityGatePerTreeIsolation(t *testing.T) {
	q := NewQualityGate(t.TempDir())
	q.ConsecutiveFails = 3

	// Tree A regresses hard 3 times -> disabled for A only.
	for range 3 {
		if got := q.ValidateFor("tree-a", 90, 60); got != GateRollback {
			t.Fatalf("ValidateFor(tree-a) = %v, want GateRollback", got)
		}
	}

	if !q.IsDisabledFor("tree-a") {
		t.Error("tree-a should be disabled after 3 consecutive rollbacks")
	}
	if q.IsDisabledFor("tree-b") {
		t.Error("tree-b must not be disabled by tree-a failures")
	}
	if got := q.ValidateFor("tree-b", 50, 55); got != GateAccepted {
		t.Errorf("ValidateFor(tree-b) = %v, want GateAccepted", got)
	}
}

// Failures recorded through the legacy tree-agnostic Validate act as a global
// kill switch: they disable every tree (A2 fail-closed compatibility).
func TestQualityGateGlobalStreakDisablesAllTrees(t *testing.T) {
	q := NewQualityGate(t.TempDir())
	q.ConsecutiveFails = 1

	if got := q.Validate(50, 0.01); got != GateRejected {
		t.Fatalf("Validate = %v, want GateRejected", got)
	}
	if !q.IsDisabledFor("any-tree") {
		t.Error("global failure streak must disable all trees (kill-switch semantics)")
	}
}

// A pass resets only that tree's failure streak.
func TestQualityGatePerTreeResetOnPass(t *testing.T) {
	q := NewQualityGate(t.TempDir())
	q.ConsecutiveFails = 2

	q.ValidateFor("tree-a", 90, 60) // fail 1
	if got := q.ValidateFor("tree-a", 50, 55); got != GateAccepted {
		t.Fatalf("ValidateFor = %v, want GateAccepted", got)
	}
	q.ValidateFor("tree-a", 90, 60) // fail 1 again (streak was reset)

	if q.IsDisabledFor("tree-a") {
		t.Error("tree-a disabled after non-consecutive failures — pass must reset the streak")
	}
	if q.FailCountFor("tree-a") != 1 {
		t.Errorf("FailCountFor(tree-a) = %d, want 1", q.FailCountFor("tree-a"))
	}
}
