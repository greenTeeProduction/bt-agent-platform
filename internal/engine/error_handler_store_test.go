package engine

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func withTempErrorHandlerDir(t *testing.T) {
	t.Helper()
	old := errorHandlerDirOverride
	errorHandlerDirOverride = t.TempDir()
	t.Cleanup(func() { errorHandlerDirOverride = old })
}

func TestErrorHandlerStore_AppendLoadRoundTrip(t *testing.T) {
	withTempErrorHandlerDir(t)
	ext := ErrorHandlerExtension{
		Node:      evolution.SerializableNode{Type: "Sequence", Name: "Handle_testcat"},
		Signature: "abc123def456",
	}
	if err := appendErrorHandlerExtension("tree_ErrorHandler", ext); err != nil {
		t.Fatalf("append: %v", err)
	}
	got := loadErrorHandlerExtensions("tree_ErrorHandler")
	if len(got) != 1 || got[0].Node.Name != "Handle_testcat" || got[0].Signature != "abc123def456" {
		t.Fatalf("round trip = %+v", got)
	}
	if len(loadErrorHandlerExtensions("other_handler")) != 0 {
		t.Fatal("extensions must be keyed by handler name")
	}
	// Backup written before append
	if _, err := os.Stat(filepath.Join(errorHandlerDir(), "extensions.json.bak")); err != nil {
		t.Fatalf("expected extensions.json.bak: %v", err)
	}
}

func TestErrorHandlerStore_ConsecutiveFailuresDisable(t *testing.T) {
	withTempErrorHandlerDir(t)
	ext := ErrorHandlerExtension{Node: evolution.SerializableNode{Type: "Sequence", Name: "n1"}}
	if err := appendErrorHandlerExtension("h", ext); err != nil {
		t.Fatal(err)
	}
	recordErrorHandlerResult("h", "n1", false)
	recordErrorHandlerResult("h", "n1", false)
	if len(activeErrorHandlerExtensions("h")) != 1 {
		t.Fatal("2 consecutive failures must not disable")
	}
	recordErrorHandlerResult("h", "n1", false)
	if len(activeErrorHandlerExtensions("h")) != 0 {
		t.Fatal("3 consecutive failures must disable the extension")
	}
	all := loadErrorHandlerExtensions("h")
	if len(all) != 1 || !all[0].Disabled || all[0].ConsecutiveFailures != 3 {
		t.Fatalf("persisted state = %+v", all)
	}
}

func TestErrorHandlerStore_SuccessResetsFailureStreak(t *testing.T) {
	withTempErrorHandlerDir(t)
	ext := ErrorHandlerExtension{Node: evolution.SerializableNode{Type: "Sequence", Name: "n1"}}
	if err := appendErrorHandlerExtension("h", ext); err != nil {
		t.Fatal(err)
	}
	recordErrorHandlerResult("h", "n1", false)
	recordErrorHandlerResult("h", "n1", false)
	recordErrorHandlerResult("h", "n1", true)
	all := loadErrorHandlerExtensions("h")
	if all[0].ConsecutiveFailures != 0 || all[0].Successes != 1 || all[0].Disabled {
		t.Fatalf("after success: %+v", all[0])
	}
}

func TestErrorHandlerLedger_StampAndGet(t *testing.T) {
	withTempErrorHandlerDir(t)
	if _, ok := errorHandlerLedgerGet("sig1"); ok {
		t.Fatal("empty ledger must miss")
	}
	errorHandlerLedgerStamp("sig1", "unresolvable")
	entry, ok := errorHandlerLedgerGet("sig1")
	if !ok || entry.Attempts != 1 || entry.LastVerdict != "unresolvable" || entry.LastAttempt.IsZero() {
		t.Fatalf("entry = %+v ok=%v", entry, ok)
	}
	errorHandlerLedgerStamp("sig1", "proposed")
	entry, _ = errorHandlerLedgerGet("sig1")
	if entry.Attempts != 2 || entry.LastVerdict != "proposed" {
		t.Fatalf("after 2nd stamp: %+v", entry)
	}
}

func TestErrorHandlerClaudeLock_ContentionSkips(t *testing.T) {
	withTempErrorHandlerDir(t)
	release, ok := acquireErrorHandlerClaudeLock()
	if !ok {
		t.Fatal("first acquire must succeed")
	}
	if _, ok2 := acquireErrorHandlerClaudeLock(); ok2 {
		t.Fatal("second acquire while held must fail")
	}
	release()
	release2, ok3 := acquireErrorHandlerClaudeLock()
	if !ok3 {
		t.Fatal("acquire after release must succeed")
	}
	release2()
}

func TestErrorHandlerStore_ConcurrentRecordsCountExactly(t *testing.T) {
	withTempErrorHandlerDir(t)
	ext := ErrorHandlerExtension{Node: evolution.SerializableNode{Type: "Sequence", Name: "n1"}}
	if err := appendErrorHandlerExtension("h", ext); err != nil {
		t.Fatal(err)
	}
	const workers = 12
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			recordErrorHandlerResult("h", "n1", false)
		}()
	}
	wg.Wait()
	all := loadErrorHandlerExtensions("h")
	if len(all) != 1 || all[0].ConsecutiveFailures != workers || !all[0].Disabled {
		t.Fatalf("lost updates: %+v", all)
	}
}
