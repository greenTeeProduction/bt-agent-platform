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
