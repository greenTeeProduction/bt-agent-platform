package research

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
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

// I3: Save's tmp file must be RANDOMIZED (os.CreateTemp), not a fixed
// ps.path+".tmp" — this feature adds a second scheduled writer (the
// self-review agent) to the SAME programs.json alongside the existing
// arc42/goap seeders, and a fixed tmp name lets two concurrent writers
// interleave their writes to the SAME tmp file, so whichever rename lands
// last can persist a torn/corrupt (or simply lost) update. Randomizing the
// tmp name isolates each writer's in-flight bytes; this test hammers Save
// from many goroutines and requires the final store to always be valid,
// parseable JSON with no leftover tmp file and the documented 0o644 perms
// (os.CreateTemp defaults to 0600, so Save must restore them before rename).
func TestSave_ConcurrentWritersDoNotCorruptStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "programs.json")

	const workers = 30
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := range workers {
		i := i
		wg.Go(func() {
			ps := &ProgramStore{path: path}
			ps.Add(fmt.Sprintf("Program %d", i), "test", []string{"m1"})
			if err := ps.Save(); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent save failed: %v", err)
	}

	if _, err := OpenPrograms(path); err != nil {
		t.Fatalf("programs.json corrupt after concurrent saves (torn/interleaved tmp write?): %v", err)
	}

	matches, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(matches) != 0 {
		t.Fatalf("leftover tmp file(s) after concurrent saves: %v", matches)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat final programs.json: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("perm = %o, want 0o644 (os.CreateTemp defaults to 0600 — Save must restore 0644)", perm)
	}
}

// TestSave_ConcurrentWritersDoNotCorruptStore above only proves the FILE stays
// parseable under concurrent Save — it never proves updates survive, because
// each goroutine there starts from a blank in-memory ProgramStore instead of
// reading the shared file first. That is the lost-update gap: a real
// read-modify-write caller (load → mutate → Save) racing a sibling can have
// its own Save clobber the sibling's already-persisted change, because
// ProgramStore.Save (unlike reliability.DeadLetterQueue.save) never merges
// against what is currently on disk. UpdatePrograms must close that gap by
// holding one exclusive lock across open→fn→save so no writer's save can land
// between another writer's open and save.
func TestUpdatePrograms_ConcurrentWritersAllSurvive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "programs.json")

	seed, err := OpenPrograms(path)
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	seed.Add("Base", "test", []string{"m1"})
	if err := seed.Save(); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	const workers = 30
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := range workers {
		i := i
		wg.Go(func() {
			err := UpdatePrograms(path, func(ps *ProgramStore) error {
				ps.Add(fmt.Sprintf("Program %d", i), "test", []string{"m1"})
				return nil
			})
			if err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("UpdatePrograms failed: %v", err)
	}

	final, err := OpenPrograms(path)
	if err != nil {
		t.Fatalf("reopen after concurrent updates: %v", err)
	}
	if got, want := len(final.Programs), workers+1; got != want {
		t.Fatalf("lost updates under concurrent UpdatePrograms: got %d programs, want %d (base + %d workers)", got, want, workers)
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
	for range 3 {
		ps.RecordAttemptAndMaybeBlock(p.ID, 2, 3)
	}
	if ps.Active() != nil {
		t.Fatal("a program with all milestones done-or-blocked must not be Active (frees the self-seeder)")
	}
}

// RecordRedPass persists the RED command alongside the streak so the next
// cycle's charge-time pre-check can re-run it without a Claude plan phase
// (2026-07-23 review gap 5); an empty command keeps the previous one, and
// ResetRedPassStreak clears BOTH — a genuine failure kills the whole
// already-landed hypothesis, command included.
func TestRecordRedPass_PersistsLastRedCmd(t *testing.T) {
	ps, err := OpenPrograms(filepath.Join(t.TempDir(), "programs.json"))
	if err != nil {
		t.Fatal(err)
	}
	p := ps.Add("Red cmd program", "test", []string{"m0"})

	if streak := ps.RecordRedPass(p.ID, 0, "go test ./x -run TestY"); streak != 1 {
		t.Fatalf("streak = %d, want 1", streak)
	}
	if got := ps.Programs[0].Milestones[0].LastRedCmd; got != "go test ./x -run TestY" {
		t.Fatalf("LastRedCmd = %q", got)
	}

	// Empty command (extraction failed) keeps the recorded one.
	if streak := ps.RecordRedPass(p.ID, 0, ""); streak != 2 {
		t.Fatalf("streak = %d, want 2", streak)
	}
	if got := ps.Programs[0].Milestones[0].LastRedCmd; got != "go test ./x -run TestY" {
		t.Fatalf("LastRedCmd after empty record = %q, want previous kept", got)
	}

	ps.ResetRedPassStreak(p.ID, 0)
	m := ps.Programs[0].Milestones[0]
	if m.RedPassStreak != 0 || m.LastRedCmd != "" {
		t.Fatalf("reset must clear streak AND command, got streak=%d cmd=%q", m.RedPassStreak, m.LastRedCmd)
	}
}

// TestActive_SelfFixProgramsPreemptGeneralQueue pins fixes-first scheduling:
// a program whose Source carries SelfFixSourcePrefix must be picked by
// Active() ahead of every general program, regardless of array position —
// the platform repairs itself before building more. Among self-fix programs
// (and again in the general fallback) array order still decides, and a
// self-fix program with no pending milestone left never shadows the queue.
func TestActive_SelfFixProgramsPreemptGeneralQueue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "programs.json")
	ps, err := OpenPrograms(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	general := ps.Add("General feature work", "auto-seed", []string{"Build X in internal/engine/x.go"})
	fixA := ps.Add("Fix broken gate", SelfFixSourcePrefix+"review:sig-a", []string{"Repair internal/engine/gate.go"})
	fixB := ps.Add("Fix race", SelfFixSourcePrefix+"error-handler:sig-b", []string{"Lock internal/engine/race.go"})

	if got := ps.Active(); got == nil || got.ID != fixA.ID {
		t.Fatalf("Active() = %v, want first self-fix program %s ahead of earlier general program", got, fixA.ID)
	}

	// Drain fixA: the NEXT self-fix program wins, still ahead of the general one.
	fixA.Milestones[0].Status = "done"
	if got := ps.Active(); got == nil || got.ID != fixB.ID {
		t.Fatalf("Active() after fixA done = %v, want %s", got, fixB.ID)
	}

	// Drain fixB too: the general queue resumes in array order.
	fixB.Milestones[0].Status = "blocked"
	if got := ps.Active(); got == nil || got.ID != general.ID {
		t.Fatalf("Active() after self-fix drained = %v, want general %s", got, general.ID)
	}
}

// ClaimActiveForCycle is how PrioritizeGoapGoals selects (and claims) the
// program to charge each cycle. A sibling cycle's overlapping cron tick must
// not also plan/charge the SAME program while this one is still landing it —
// the loop-runner burned 3 cycles 2026-07-23 12:38-14:55 doing exactly that.
// The claim records which agent (cycle) holds it and when, so a later cycle
// for a DIFFERENT agent is turned away until the lease (default: the cycle
// budget) expires; the SAME agent may always re-claim (e.g. a retried cycle
// reuses its own RunID).
func TestClaimActiveForCycle_SkipsProgramClaimedBySiblingCycle(t *testing.T) {
	ps, _ := OpenPrograms(filepath.Join(t.TempDir(), "p.json"))
	p := ps.Add("Shared program", "test", []string{"m1"})

	got := ps.ClaimActiveForCycle("cycle-a", time.Hour)
	if got == nil || got.ID != p.ID {
		t.Fatalf("first claim must succeed and return the active program, got %v", got)
	}
	if ps.Programs[0].ClaimedBy != "cycle-a" || ps.Programs[0].ClaimedAt.IsZero() {
		t.Fatalf("claim must stamp agent+timestamp, got %+v", ps.Programs[0])
	}

	if got := ps.ClaimActiveForCycle("cycle-b", time.Hour); got != nil {
		t.Fatalf("a program claimed by another agent within the lease window must not be re-claimed, got %v", got)
	}

	if got := ps.ClaimActiveForCycle("cycle-a", time.Hour); got == nil {
		t.Fatal("the claiming agent must be able to re-claim its own program")
	}
}

// A claim older than the lease window is stale — evidence the claiming cycle
// is no longer in flight (crashed, or simply long done) — so a different
// agent must be able to take it over rather than starving the program forever.
func TestClaimActiveForCycle_ExpiredLeaseIsReclaimable(t *testing.T) {
	ps, _ := OpenPrograms(filepath.Join(t.TempDir(), "p.json"))
	p := ps.Add("Stale claim", "test", []string{"m1"})
	ps.Programs[0].ClaimedBy = "cycle-a"
	ps.Programs[0].ClaimedAt = time.Now().Add(-2 * time.Hour)

	got := ps.ClaimActiveForCycle("cycle-b", time.Hour)
	if got == nil || got.ID != p.ID {
		t.Fatalf("an expired claim must be reclaimable by a different agent, got %v", got)
	}
	if ps.Programs[0].ClaimedBy != "cycle-b" {
		t.Fatalf("reclaim must overwrite the stale ClaimedBy, got %q", ps.Programs[0].ClaimedBy)
	}
}
