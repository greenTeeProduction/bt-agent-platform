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
