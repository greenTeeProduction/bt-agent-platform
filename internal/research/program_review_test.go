package research

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestProgramReviewPreservesOriginalBytesAndHoldsDependents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "programs.json")
	ps, err := OpenPrograms(path)
	if err != nil {
		t.Fatal(err)
	}
	p := ps.Add("unproven", "research", []string{"implement first", "then use it"})
	seedLegacyProgramDone(ps, p.ID, 0, "red-evidence:old-run")
	prior := p.Milestones[0]
	if err = ps.Save(); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = UpdatePrograms(path, func(*ProgramStore) error { return nil }); err != nil {
		t.Fatal(err)
	}
	loaded, err := OpenPrograms(path)
	if err != nil {
		t.Fatal(err)
	}
	m := loaded.Programs[0].Milestones[0]
	if m.Status != "needs_review" || m.CompletedRun != "" || !m.CompletedAt.IsZero() || m.Review == nil || m.Review.PreviousCompletedRun != prior.CompletedRun || !m.Review.PreviousCompletedAt.Equal(prior.CompletedAt) || loaded.Active() != nil {
		t.Fatalf("unsupported completion survived: %+v", m)
	}
	backup := fmt.Sprintf("%s.before-red-review-%x.json", path, sha256.Sum256(original))
	data, err := os.ReadFile(backup)
	if err != nil || !bytes.Equal(data, original) {
		t.Fatal("original evidence was not retained")
	}
	if err = UpdatePrograms(path, func(p *ProgramStore) error {
		return p.ReviseMilestone(loaded.Programs[0].ID, 0, "stale goal", "new test")
	}); err == nil {
		t.Fatal("stale review overwrote the goal")
	}
	if err = UpdatePrograms(path, func(p *ProgramStore) error {
		return p.ReviseMilestone(loaded.Programs[0].ID, 0, m.Goal, "implement first with a regression that fails before the fix")
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err = OpenPrograms(path)
	if err != nil {
		t.Fatal(err)
	}
	m = loaded.Programs[0].Milestones[0]
	if m.Status != "pending" || m.Review != nil || len(m.ReviewHistory) != 1 || loaded.Active() == nil || m.Delivery != nil {
		t.Fatalf("review did not reopen actual work with retained history: %+v", m)
	}
}

func TestProgramReviewConflictingBackupLeavesOriginalUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "programs.json")
	ps, _ := OpenPrograms(path)
	p := ps.Add("unproven", "research", []string{"first"})
	seedLegacyProgramDone(ps, p.ID, 0, "red-evidence-precheck:old-run")
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	backup := fmt.Sprintf("%s.before-red-review-%x.json", path, sha256.Sum256(original))
	if err := os.WriteFile(backup, []byte("conflicting evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := UpdatePrograms(path, func(*ProgramStore) error { return nil }); err == nil {
		t.Fatal("conflicting backup overwritten")
	}
	current, _ := os.ReadFile(path)
	if !bytes.Equal(original, current) {
		t.Fatal("backlog changed without a trustworthy backup")
	}
}

func TestProgramUpdateHonorsCanceledLockWait(t *testing.T) {
	path := filepath.Join(t.TempDir(), "programs.json")
	release, err := reliability.AcquireFileLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	called := false
	if err = UpdateProgramsWithContext(ctx, path, func(*ProgramStore) error { called = true; return nil }); err == nil || called {
		t.Fatal("canceled lock wait entered the transaction")
	}
}

func TestProgramsUseConfiguredPlatformHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("BT_AGENT_HOME", root)
	if got := DefaultProgramsPath(); got != filepath.Join(root, "research", "programs.json") {
		t.Fatalf("wrong configured owner: %s", got)
	}
}

func TestProgramReviewRejectsCorruptOrMissingContainer(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `{"programs":[null]}`, `{"programs":[{"id":"same"},{"id":"same"}]}`} {
		path := filepath.Join(t.TempDir(), "programs.json")
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if err := UpdatePrograms(path, func(*ProgramStore) error { return nil }); err == nil {
			t.Fatalf("invalid backlog accepted: %s", body)
		}
		after, _ := os.ReadFile(path)
		if string(after) != body {
			t.Fatal("corrupt backlog was overwritten")
		}
	}
}
