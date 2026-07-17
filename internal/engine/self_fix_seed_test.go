package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nico/go-bt-evolve/internal/research"
)

// withTempSelfFix points the self-fix ledger dir and the goap program store at
// per-test temp paths, and clears the BT_SELF_FIX* env so the test is
// deterministic regardless of the host environment. Returns the ledger dir and
// the program-store path.
func withTempSelfFix(t *testing.T) (dir, programsPath string) {
	t.Helper()
	dir = t.TempDir()
	oldDir := selfFixDirOverride
	selfFixDirOverride = dir
	t.Cleanup(func() { selfFixDirOverride = oldDir })

	programsPath = filepath.Join(t.TempDir(), "programs.json")
	oldPath := goapProgramsPath
	goapProgramsPath = programsPath
	t.Cleanup(func() { goapProgramsPath = oldPath })

	t.Setenv("BT_SELF_FIX", "")
	t.Setenv("BT_SELF_FIX_COOLDOWN", "")
	t.Setenv("BT_SELF_FIX_MAX_OPEN", "")
	return dir, programsPath
}

func countSelfFixPrograms(t *testing.T, programsPath string) int {
	t.Helper()
	ps, err := research.OpenPrograms(programsPath)
	if err != nil {
		t.Fatalf("open programs: %v", err)
	}
	n := 0
	for _, p := range ps.Programs {
		if strings.HasPrefix(p.Source, "self-fix:") {
			n++
		}
	}
	return n
}

func TestSeedCodeFixProgram_SeedsProgram(t *testing.T) {
	_, programsPath := withTempSelfFix(t)

	seeded, reason := seedCodeFixProgram("sig1", "Fix X", "fix file y.go: guard nil deref", "self-fix:test:sig1")
	if !seeded {
		t.Fatalf("expected seeded, got (%v, %q)", seeded, reason)
	}

	ps, err := research.OpenPrograms(programsPath)
	if err != nil {
		t.Fatal(err)
	}
	var found *research.Program
	for _, p := range ps.Programs {
		if p.Source == "self-fix:test:sig1" {
			found = p
		}
	}
	if found == nil {
		t.Fatalf("program not persisted with tagged source: %+v", ps.Programs)
	}
	if found.Title != "Fix X" {
		t.Fatalf("title = %q, want %q", found.Title, "Fix X")
	}
	if len(found.Milestones) != 1 {
		t.Fatalf("want 1 milestone, got %d", len(found.Milestones))
	}
	if m := found.Milestones[0]; m.Status != "pending" || m.Goal != "fix file y.go: guard nil deref" {
		t.Fatalf("milestone = %+v", m)
	}

	// Ledger records the seed keyed by sig.
	ledger := map[string]selfFixLedgerEntry{}
	if err := readErrorHandlerJSONStrict(selfFixLedgerPath(), &ledger); err != nil {
		t.Fatal(err)
	}
	e, ok := ledger["sig1"]
	if !ok || e.ProgramID != found.ID || e.Title != "Fix X" || e.Count != 1 || e.LastSeeded.IsZero() {
		t.Fatalf("ledger entry = %+v (ok=%v)", e, ok)
	}
}

func TestSeedCodeFixProgram_DedupWithinCooldown(t *testing.T) {
	_, programsPath := withTempSelfFix(t)

	if seeded, reason := seedCodeFixProgram("sig1", "Fix X", "fix file y.go", "self-fix:test:sig1"); !seeded {
		t.Fatalf("first seed must succeed: %q", reason)
	}
	seeded, reason := seedCodeFixProgram("sig1", "Fix X differently", "fix file y.go another way", "self-fix:test:sig1")
	if seeded || reason != "within cooldown" {
		t.Fatalf("expected (false, within cooldown), got (%v, %q)", seeded, reason)
	}
	if n := countSelfFixPrograms(t, programsPath); n != 1 {
		t.Fatalf("store changed on dedup: %d self-fix programs, want 1", n)
	}
}

func TestSeedCodeFixProgram_CooldownOverrideAllowsReseed(t *testing.T) {
	_, programsPath := withTempSelfFix(t)
	t.Setenv("BT_SELF_FIX_COOLDOWN", "1ns")

	if seeded, _ := seedCodeFixProgram("sig1", "Fix X", "fix file y.go", "self-fix:test:sig1"); !seeded {
		t.Fatal("first seed must succeed")
	}
	// Same sig, but a re-proposal with the SAME title dedupes in the store (Add
	// is title-keyed); use a fresh title so a second program would appear if the
	// cooldown no longer blocks.
	if seeded, reason := seedCodeFixProgram("sig1", "Fix X take two", "fix file y.go later", "self-fix:test:sig1"); !seeded {
		t.Fatalf("expired cooldown must allow reseed, got %q", reason)
	}
	if n := countSelfFixPrograms(t, programsPath); n != 2 {
		t.Fatalf("want 2 self-fix programs after cooldown expiry, got %d", n)
	}
}

func TestSeedCodeFixProgram_CapReached(t *testing.T) {
	_, programsPath := withTempSelfFix(t)
	t.Setenv("BT_SELF_FIX_MAX_OPEN", "2")

	// Pre-seed two OPEN self-fix programs (each has a pending milestone).
	ps, err := research.OpenPrograms(programsPath)
	if err != nil {
		t.Fatal(err)
	}
	ps.Add("Pre one", "self-fix:test:pre1", []string{"fix a.go"})
	ps.Add("Pre two", "self-fix:test:pre2", []string{"fix b.go"})
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}

	seeded, reason := seedCodeFixProgram("sig-new", "Fix Z", "fix c.go", "self-fix:test:sig-new")
	if seeded || reason != "self-fix backlog cap reached" {
		t.Fatalf("expected cap skip, got (%v, %q)", seeded, reason)
	}
	if n := countSelfFixPrograms(t, programsPath); n != 2 {
		t.Fatalf("store changed at cap: %d self-fix programs, want 2", n)
	}
}

func TestSeedCodeFixProgram_CapIgnoresClosedPrograms(t *testing.T) {
	_, programsPath := withTempSelfFix(t)
	t.Setenv("BT_SELF_FIX_MAX_OPEN", "1")

	// A self-fix program whose only milestone is done is NOT open, so it must
	// not count against the cap.
	ps, err := research.OpenPrograms(programsPath)
	if err != nil {
		t.Fatal(err)
	}
	done := ps.Add("Already fixed", "self-fix:test:done", []string{"fix a.go"})
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}
	if !ps.MarkDone(done.ID, 0, "run1") {
		t.Fatal("mark done failed")
	}
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}

	seeded, reason := seedCodeFixProgram("sig-new", "Fix Z", "fix c.go", "self-fix:test:sig-new")
	if !seeded {
		t.Fatalf("closed self-fix program must not count against the cap: %q", reason)
	}
}

func TestSeedCodeFixProgram_KillSwitch(t *testing.T) {
	dir, programsPath := withTempSelfFix(t)
	t.Setenv("BT_SELF_FIX", "off")

	seeded, reason := seedCodeFixProgram("sig1", "Fix X", "fix y.go", "self-fix:test:sig1")
	if seeded || reason != "self-fix disabled" {
		t.Fatalf("expected (false, self-fix disabled), got (%v, %q)", seeded, reason)
	}
	if _, err := os.Stat(programsPath); !os.IsNotExist(err) {
		t.Fatalf("program store must not be written under kill switch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ledger.json")); !os.IsNotExist(err) {
		t.Fatalf("ledger must not be written under kill switch: %v", err)
	}
}

func TestSeedCodeFixProgram_EmptyInputs(t *testing.T) {
	_, programsPath := withTempSelfFix(t)

	cases := [][3]string{
		{"", "title", "goal"},
		{"sig", "", "goal"},
		{"sig", "title", ""},
		{"   ", "title", "goal"},
		{"sig", "  ", "goal"},
		{"sig", "title", "\t"},
	}
	for _, c := range cases {
		seeded, reason := seedCodeFixProgram(c[0], c[1], c[2], "self-fix:test:x")
		if seeded || reason != "incomplete seed request" {
			t.Fatalf("inputs %q: expected (false, incomplete seed request), got (%v, %q)", c, seeded, reason)
		}
	}
	if n := countSelfFixPrograms(t, programsPath); n != 0 {
		t.Fatalf("empty inputs must not seed: %d programs", n)
	}
}

// TestSeedCodeFixProgram_LedgerWriteFailureLeavesStoreUntouched pins the
// CRITICAL finding: the ledger stamp must be durable BEFORE the program is
// persisted, so a ledger-write failure never leaves a live, un-cooled-down
// program behind (the exact runaway the ledger exists to prevent). We can't
// induce this via OS-level directory permissions: store.lock and ledger.json
// both live directly under selfFixDir(), so a read-only ledger dir blocks the
// on-disk store LOCK's own O_CREATE before the ledger write is ever reached
// (verified: this fails identically whether or not the CRITICAL fix is
// applied, so it can't pin the reordering). selfFixWriteLedger is the
// package's test seam for this specific failure instead.
func TestSeedCodeFixProgram_LedgerWriteFailureLeavesStoreUntouched(t *testing.T) {
	_, programsPath := withTempSelfFix(t)

	oldWrite := selfFixWriteLedger
	selfFixWriteLedger = func(path string, v any) error {
		return fmt.Errorf("simulated disk full")
	}
	t.Cleanup(func() { selfFixWriteLedger = oldWrite })

	seeded, reason := seedCodeFixProgram("sig1", "Fix X", "fix file y.go", "self-fix:test:sig1")
	if seeded || !strings.HasPrefix(reason, "self-fix ledger write failed") {
		t.Fatalf("expected (false, self-fix ledger write failed: ...), got (%v, %q)", seeded, reason)
	}

	ps, err := research.OpenPrograms(programsPath)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(ps.Programs); n != 0 {
		t.Fatalf("program store must be untouched when the ledger write fails (ledger-before-store fail-safe): %d programs, want 0", n)
	}
}

// TestSeedCodeFixProgram_TitleCollisionRecordsNothing pins the IMPORTANT
// finding: ProgramStore.Add dedupes by title-key, so a second sig proposing
// the SAME title must not silently drop its defect while telling the caller
// it succeeded.
func TestSeedCodeFixProgram_TitleCollisionRecordsNothing(t *testing.T) {
	_, programsPath := withTempSelfFix(t)

	if seeded, reason := seedCodeFixProgram("sigA", "Fix X", "fix file y.go", "self-fix:test:sigA"); !seeded {
		t.Fatalf("first seed must succeed: %q", reason)
	}

	seeded, reason := seedCodeFixProgram("sigB", "Fix X", "fix file z.go: a different defect", "self-fix:test:sigB")
	if seeded || !strings.HasPrefix(reason, "title collides with existing program") {
		t.Fatalf("expected (false, title collides with existing program ...), got (%v, %q)", seeded, reason)
	}

	if n := countSelfFixPrograms(t, programsPath); n != 1 {
		t.Fatalf("title collision must not add a second program: %d self-fix programs, want 1", n)
	}

	ledger := map[string]selfFixLedgerEntry{}
	if err := readErrorHandlerJSONStrict(selfFixLedgerPath(), &ledger); err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger["sigB"]; ok {
		t.Fatalf("colliding sig must not record a ledger entry: %+v", ledger["sigB"])
	}
	if _, ok := ledger["sigA"]; !ok {
		t.Fatal("winning sig's ledger entry must be untouched")
	}
}

// TestSeedCodeFixProgram_SameSigConcurrentDoubleSeed pins the mutex/lock
// serialization guarantee for the SAME sig+title under -race: exactly one
// program and one ledger entry, not N.
func TestSeedCodeFixProgram_SameSigConcurrentDoubleSeed(t *testing.T) {
	dir, programsPath := withTempSelfFix(t)

	const workers = 8
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			seedCodeFixProgram("sig-same", "Fix Same", "fix file same.go: recurring defect", "self-fix:test:sig-same")
		}()
	}
	wg.Wait()

	if n := countSelfFixPrograms(t, programsPath); n != 1 {
		t.Fatalf("same-sig concurrent seeds must produce exactly one program: got %d", n)
	}
	ledger := map[string]selfFixLedgerEntry{}
	if err := readErrorHandlerJSONStrict(filepath.Join(dir, "ledger.json"), &ledger); err != nil {
		t.Fatal(err)
	}
	if e, ok := ledger["sig-same"]; !ok || e.Count != 1 {
		t.Fatalf("same-sig concurrent seeds must produce exactly one ledger entry with count 1: %+v (ok=%v)", e, ok)
	}
}

// TestSeedCodeFixProgram_CapZeroPauses pins the MINOR finding:
// BT_SELF_FIX_MAX_OPEN=0 (a parseable non-positive value) must be honored as
// an explicit pause dial, not fall back to the default of 3.
func TestSeedCodeFixProgram_CapZeroPauses(t *testing.T) {
	_, programsPath := withTempSelfFix(t)
	t.Setenv("BT_SELF_FIX_MAX_OPEN", "0")

	seeded, reason := seedCodeFixProgram("sig1", "Fix X", "fix file y.go", "self-fix:test:sig1")
	if seeded || reason != "self-fix backlog cap reached" {
		t.Fatalf("expected (false, self-fix backlog cap reached), got (%v, %q)", seeded, reason)
	}
	if n := countSelfFixPrograms(t, programsPath); n != 0 {
		t.Fatalf("cap=0 pause must write nothing: %d self-fix programs, want 0", n)
	}
}

// TestSeedCodeFixProgram_SourceNormalization pins the MINOR finding: a
// non-empty but mis-tagged source (missing the "self-fix:" prefix) must still
// be counted by the backlog cap, which keys on that prefix.
func TestSeedCodeFixProgram_SourceNormalization(t *testing.T) {
	_, programsPath := withTempSelfFix(t)
	t.Setenv("BT_SELF_FIX_MAX_OPEN", "2")

	// Two distinct, mis-tagged (no "self-fix:" prefix) sources fill the cap.
	if seeded, reason := seedCodeFixProgram("sig1", "Fix One", "fix a.go", "mis-tagged-source-1"); !seeded {
		t.Fatalf("first seed must succeed: %q", reason)
	}
	if seeded, reason := seedCodeFixProgram("sig2", "Fix Two", "fix b.go", "mis-tagged-source-2"); !seeded {
		t.Fatalf("second seed must succeed: %q", reason)
	}

	// The persisted source must carry the normalized "self-fix:" prefix.
	ps, err := research.OpenPrograms(programsPath)
	if err != nil {
		t.Fatal(err)
	}
	var found *research.Program
	for _, p := range ps.Programs {
		if p.Title == "Fix One" {
			found = p
		}
	}
	if found == nil || found.Source != "self-fix:mis-tagged-source-1" {
		t.Fatalf("mis-tagged source must be normalized with the self-fix: prefix, got %+v", found)
	}

	// A third, distinct sig must be cap-blocked: if the mis-tagged sources had
	// escaped the cap count, this would incorrectly succeed instead.
	seeded, reason := seedCodeFixProgram("sig3", "Fix Three", "fix c.go", "self-fix:test:sig3")
	if seeded || reason != "self-fix backlog cap reached" {
		t.Fatalf("mis-tagged sources must still count toward the cap: expected (false, self-fix backlog cap reached), got (%v, %q)", seeded, reason)
	}
}

func TestSeedCodeFixProgram_ConcurrentDistinctSigs(t *testing.T) {
	dir, programsPath := withTempSelfFix(t)
	t.Setenv("BT_SELF_FIX_MAX_OPEN", "100")

	const workers = 12
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			sig := fmt.Sprintf("sig%d", i)
			seedCodeFixProgram(sig, "Fix "+sig, "fix file"+sig+".go: defect", "self-fix:test:"+sig)
		}()
	}
	wg.Wait()

	if n := countSelfFixPrograms(t, programsPath); n != workers {
		t.Fatalf("lost updates: %d self-fix programs, want %d", n, workers)
	}
	ledger := map[string]selfFixLedgerEntry{}
	if err := readErrorHandlerJSONStrict(filepath.Join(dir, "ledger.json"), &ledger); err != nil {
		t.Fatal(err)
	}
	if len(ledger) != workers {
		t.Fatalf("ledger corrupted: %d entries, want %d", len(ledger), workers)
	}
}
