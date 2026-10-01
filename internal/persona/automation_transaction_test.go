package persona

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestAutomationLedgerIndependentWritersPreserveRecords(t *testing.T) {
	ws := Workspace{Root: t.TempDir(), User: "alice"}
	var wg sync.WaitGroup
	for i := range 24 {
		wg.Go(func() {
			ledger, err := NewAutomationStore(ws)
			if err == nil {
				err = ledger.Upsert(AutomationRecord{Signature: fmt.Sprint(i), Status: AutomationPending})
			}
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	ledger, err := NewAutomationStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	records, err := ledger.All()
	if err != nil || len(records) != 24 {
		t.Fatalf("lost concurrent writes: %d, %v", len(records), err)
	}
	info, err := os.Stat(ws.AutomationsPath())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("ledger permissions: %v %v", info, err)
	}
}

func TestAutomationReservationSerializesCapAndOwnership(t *testing.T) {
	ws := Workspace{Root: t.TempDir(), User: "alice"}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			ledger, err := NewAutomationStore(ws)
			if err != nil {
				t.Error(err)
				return
			}
			rec := AutomationRecord{Signature: fmt.Sprint(i), TreeID: fmt.Sprint(i), AgentName: fmt.Sprint(i), Representative: "exact task", Schedule: "0 9 * * *", Status: AutomationPending}
			if ledger.Reserve(rec, 3) == nil {
				successes.Add(1)
			}
		})
	}
	wg.Wait()
	if successes.Load() != 3 {
		t.Fatalf("admitted %d proposals for three slots", successes.Load())
	}
	foreign, err := NewAutomationStore(Workspace{Root: ws.Root, User: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foreign.All(); err == nil {
		t.Fatal("foreign owner accepted a colliding workspace")
	}
}

func TestAutomationTransactionCommitFailureIsReported(t *testing.T) {
	ws := Workspace{Root: t.TempDir(), User: "alice"}
	ledger, err := NewAutomationStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Upsert(AutomationRecord{Signature: "sig", Status: AutomationPending}); err != nil {
		t.Fatal(err)
	}
	// Force the commit to fail after the callback has prepared its state.
	err = ledger.transition("sig", func(rec *AutomationRecord) error {
		rec.Status = AutomationApproved
		if err := os.Rename(ws.AutomationsPath(), ws.AutomationsPath()+".preserved"); err != nil {
			return err
		}
		return os.Mkdir(ws.AutomationsPath(), 0700)
	})
	if err == nil {
		t.Fatal("failed ledger commit acknowledged as approved")
	}
	saved, err := os.ReadFile(ws.AutomationsPath() + ".preserved")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(ws.AutomationsPath()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ws.AutomationsPath(), saved, 0600); err != nil {
		t.Fatal(err)
	}
	rec, _, err := ledger.Get("sig")
	if err != nil || rec.Status != AutomationPending {
		t.Fatalf("previous approval gate changed: %+v %v", rec, err)
	}
}

func TestAutomationLedgerBoundsContendedLock(t *testing.T) {
	ledger, err := NewAutomationStore(Workspace{Root: t.TempDir(), User: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	release, err := reliability.AcquireFileLock(ledger.path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	start := time.Now()
	if err := ledger.Upsert(AutomationRecord{Signature: "blocked"}); err == nil {
		t.Fatal("contended writer succeeded")
	}
	if time.Since(start) > 7*time.Second {
		t.Fatal("lock wait exceeded bound")
	}
}

func TestAutomationLedgerRejectsEscapingReadSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "automations.json")); err != nil {
		t.Fatal(err)
	}
	ledger, err := NewAutomationStore(Workspace{Root: root, User: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.All(); err == nil {
		t.Fatal("escaped ledger root")
	}
}
