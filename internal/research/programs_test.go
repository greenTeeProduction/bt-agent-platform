package research

import (
	"path/filepath"
	"testing"
)

func TestProgramLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "programs.json")
	ps, err := OpenPrograms(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if ps.Active() != nil {
		t.Fatal("empty store must have no active program")
	}

	ps.Add("Auction allocation", "notebooklm", []string{
		"Define auction messages in internal/a2a/messages.go",
		"Implement bid evaluation in internal/engine/actions_a2a.go",
	})
	if err := ps.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	re, err := OpenPrograms(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	active := re.Active()
	if active == nil || active.Title != "Auction allocation" {
		t.Fatalf("active program lost on reload: %+v", active)
	}
	idx, milestone := active.NextMilestone()
	if idx != 0 || milestone == nil || milestone.Status != "pending" {
		t.Fatalf("first pending milestone expected, got %d %+v", idx, milestone)
	}

	if !re.MarkDone(active.ID, 0, "run-abc") {
		t.Fatal("MarkDone must succeed for a pending milestone")
	}
	if err := re.Save(); err != nil {
		t.Fatal(err)
	}

	re2, _ := OpenPrograms(path)
	active2 := re2.Active()
	idx2, m2 := active2.NextMilestone()
	if idx2 != 1 || m2 == nil {
		t.Fatalf("second milestone must be next after completion, got %d", idx2)
	}
	if active2.Milestones[0].Status != "done" || active2.Milestones[0].CompletedRun != "run-abc" {
		t.Fatalf("completion must persist: %+v", active2.Milestones[0])
	}

	re2.MarkDone(active2.ID, 1, "run-def")
	if re2.Active() != nil {
		t.Fatal("fully completed program must no longer be active")
	}
}

func TestAddDeduplicatesByTitle(t *testing.T) {
	ps, _ := OpenPrograms(filepath.Join(t.TempDir(), "programs.json"))
	ps.Add("Same program", "nlm", []string{"m1"})
	ps.Add("Same program", "nlm", []string{"m1", "m2"})
	if len(ps.Programs) != 1 {
		t.Fatalf("re-proposed program must not duplicate, got %d", len(ps.Programs))
	}
}

// RefundAttempt exists so *infrastructure* failures (Claude rate limit, commit
// gate wedged by an external landing, apply/sync refusal) can un-charge the
// attempt PrioritizeGoapGoals recorded at queue time. Only genuine
// implementation failures may consume the milestone-abandon budget — on
// 2026-07-09 a 16h doc-drift wedge wrongly blocked 2 programs' milestones ×3
// attempts each, and on 2026-07-08 a rate-limit window blocked all 5 of
// a69ef9d1's.

// A refunded charge restores the pre-charge state: attempts decremented,
// pending stays pending, and a second refund without a new charge is a no-op.
func TestRefundAttempt_UndoesInfraCharge(t *testing.T) {
	ps, _ := OpenPrograms(filepath.Join(t.TempDir(), "p.json"))
	p := ps.Add("Refund", "test", []string{"m1"})

	ps.RecordAttemptAndMaybeBlock(p.ID, 0, 3)
	if got := ps.Programs[0].Milestones[0].Attempts; got != 1 {
		t.Fatalf("setup: attempts = %d, want 1", got)
	}

	if !ps.RefundAttempt(p.ID, 0, 3) {
		t.Fatal("RefundAttempt must report a change when a charge exists")
	}
	m := ps.Programs[0].Milestones[0]
	if m.Attempts != 0 || m.Status != "pending" {
		t.Fatalf("after refund: attempts=%d status=%q, want 0/pending", m.Attempts, m.Status)
	}

	// No charge left to refund: a second refund must be a no-op, so a doubled
	// refund path can never grant a milestone extra attempts.
	if ps.RefundAttempt(p.ID, 0, 3) {
		t.Fatal("refund without a remaining charge must be a no-op")
	}
	if ps.Programs[0].Milestones[0].Attempts != 0 {
		t.Fatalf("attempts went negative-equivalent: %d", ps.Programs[0].Milestones[0].Attempts)
	}
}

// When the refunded charge is the one that pushed the milestone over the
// abandon cap, the refund must also restore it to pending (clearing BlockedAt)
// — the block was an infrastructure artifact, not agent judgment.
func TestRefundAttempt_UnblocksBlockCausedByRefundedCharge(t *testing.T) {
	ps, _ := OpenPrograms(filepath.Join(t.TempDir(), "p.json"))
	p := ps.Add("Unblock", "test", []string{"m1"})
	ps.Programs[0].Milestones[0].Attempts = 2

	if !ps.RecordAttemptAndMaybeBlock(p.ID, 0, 3) {
		t.Fatal("setup: third charge must block the milestone")
	}

	if !ps.RefundAttempt(p.ID, 0, 3) {
		t.Fatal("RefundAttempt must undo the just-created block")
	}
	m := ps.Programs[0].Milestones[0]
	if m.Status != "pending" || m.Attempts != 2 {
		t.Fatalf("after refund: status=%q attempts=%d, want pending/2", m.Status, m.Attempts)
	}
	if !m.BlockedAt.IsZero() {
		t.Fatalf("BlockedAt must be cleared on unblock, got %v", m.BlockedAt)
	}
}

// A block accrued from genuine attempts in EARLIER cycles stays blocked: one
// refund only removes one charge, and the milestone remains over the cap.
func TestRefundAttempt_LeavesOldBlockBlocked(t *testing.T) {
	ps, _ := OpenPrograms(filepath.Join(t.TempDir(), "p.json"))
	p := ps.Add("OldBlock", "test", []string{"m1"})
	m := &ps.Programs[0].Milestones[0]
	m.Attempts = 5
	m.Status = "blocked"

	if !ps.RefundAttempt(p.ID, 0, 3) {
		t.Fatal("refund must still decrement the attempt count")
	}
	if m.Status != "blocked" || m.Attempts != 4 {
		t.Fatalf("old block must survive a single refund: status=%q attempts=%d, want blocked/4", m.Status, m.Attempts)
	}
}

// Done milestones are immutable to refunds, and unknown ids are no-ops.
func TestRefundAttempt_NeverTouchesDoneOrUnknown(t *testing.T) {
	ps, _ := OpenPrograms(filepath.Join(t.TempDir(), "p.json"))
	p := ps.Add("Done", "test", []string{"m1"})
	ps.Programs[0].Milestones[0].Attempts = 1
	ps.MarkDone(p.ID, 0, "run-1")

	if ps.RefundAttempt(p.ID, 0, 3) {
		t.Fatal("a done milestone must never be refunded")
	}
	if ps.RefundAttempt("no-such-program", 0, 3) {
		t.Fatal("unknown program must be a no-op")
	}
	if ps.RefundAttempt(p.ID, 99, 3) {
		t.Fatal("out-of-range milestone must be a no-op")
	}
}

func TestRecordAttemptAndMaybeBlock(t *testing.T) {
	ps, _ := OpenPrograms(filepath.Join(t.TempDir(), "p.json"))
	p := ps.Add("Prog", "test", []string{"m1 buildable", "m2 fabricated", "m3 buildable"})

	// m1 completes normally.
	ps.MarkDone(p.ID, 0, "run-1")

	// m2 fails 3 times → blocked; NextMilestone then skips to m3.
	for i := 1; i <= 3; i++ {
		blocked := ps.RecordAttemptAndMaybeBlock(p.ID, 1, 3)
		if i < 3 && blocked {
			t.Fatalf("m2 blocked too early at attempt %d", i)
		}
		if i == 3 && !blocked {
			t.Fatal("m2 must be blocked after 3 attempts")
		}
	}
	idx, m := ps.Active().NextMilestone()
	if idx != 2 || m == nil {
		t.Fatalf("after m2 blocked, next pending must be m3 (idx 2), got %d", idx)
	}
	if ps.Programs[0].Milestones[1].Status != "blocked" {
		t.Fatalf("m2 status = %q, want blocked", ps.Programs[0].Milestones[1].Status)
	}

	// Block m3 too → program has no pending milestone → not Active → reseed.
	for i := 0; i < 3; i++ {
		ps.RecordAttemptAndMaybeBlock(p.ID, 2, 3)
	}
	if ps.Active() != nil {
		t.Fatal("a program with all milestones done-or-blocked must not be Active (frees the self-seeder)")
	}
}
