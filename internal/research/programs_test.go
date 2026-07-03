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
