package research

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The goal-attempt store is the durable budget behind research-goal abandon:
// program milestones already block after repeated failures, but notebooklm-lane
// P0 goals had no budget at all — on 2026-07-10 one goal burned 11 blind
// implementation attempts on the same lint failure. The store must persist
// attempts + the last failure tail (so the next attempt can be steered), and
// clear on landing.
func TestGoalAttempts_RecordPersistAndClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goal-attempts.json")

	s, err := OpenGoalAttempts(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := s.Count("k1"); got != 0 {
		t.Fatalf("fresh store Count = %d, want 0", got)
	}
	if got := s.RecordFailure("k1", "golangci-lint: nilerr at tools.go:90"); got != 1 {
		t.Fatalf("first RecordFailure = %d, want 1", got)
	}
	if got := s.RecordFailure("k1", "golangci-lint: nilerr again"); got != 2 {
		t.Fatalf("second RecordFailure = %d, want 2", got)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	re, err := OpenGoalAttempts(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := re.Count("k1"); got != 2 {
		t.Fatalf("Count after reload = %d, want 2", got)
	}
	if got := re.LastFailure("k1"); !strings.Contains(got, "nilerr again") {
		t.Fatalf("LastFailure after reload = %q, want the most recent tail", got)
	}
	if re.Count("unknown") != 0 || re.LastFailure("unknown") != "" {
		t.Fatal("unknown keys must read as zero-budget")
	}

	// Landing clears the budget so a re-proposed (now-implemented) goal starts
	// fresh, and clearing an absent key is a no-op.
	if !re.Clear("k1") {
		t.Fatal("Clear of a recorded key must report a change")
	}
	if re.Clear("k1") {
		t.Fatal("second Clear must be a no-op")
	}
	if err := re.Save(); err != nil {
		t.Fatalf("save after clear: %v", err)
	}
	final, _ := OpenGoalAttempts(path)
	if final.Count("k1") != 0 {
		t.Fatal("cleared budget must not survive a reload")
	}
}

// Failure tails are bounded so a giant commit-gate transcript cannot bloat the
// store; the TAIL is kept (the actionable lint/test lines come last).
func TestGoalAttempts_FailureTailBounded(t *testing.T) {
	s, _ := OpenGoalAttempts(filepath.Join(t.TempDir(), "g.json"))
	long := strings.Repeat("x", 5000) + "ACTIONABLE-SUFFIX"
	s.RecordFailure("k", long)
	got := s.LastFailure("k")
	if len(got) > 1300 {
		t.Fatalf("stored failure tail length = %d, want bounded (~1200)", len(got))
	}
	if !strings.HasSuffix(got, "ACTIONABLE-SUFFIX") {
		t.Fatalf("bounded tail must keep the END of the failure output; got suffix %q", got[len(got)-40:])
	}
}

// DefaultGoalAttemptsPath lives beside the other research stores under the
// ADR-003 home.
func TestDefaultGoalAttemptsPath(t *testing.T) {
	got := DefaultGoalAttemptsPath()
	if !strings.HasSuffix(got, filepath.Join(".go-bt-evolve", "research", "goal-attempts.json")) {
		t.Fatalf("DefaultGoalAttemptsPath = %q, want …/.go-bt-evolve/research/goal-attempts.json", got)
	}
	if _, err := os.Stat(filepath.Dir(got)); err != nil && !os.IsNotExist(err) {
		t.Fatalf("unexpected stat error for %q: %v", got, err)
	}
}
