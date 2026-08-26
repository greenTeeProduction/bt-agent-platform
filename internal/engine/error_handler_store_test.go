package engine

import (
	"fmt"
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

// I5: the ledger must stay bounded — oldest LastAttempt entries are evicted
// once the cap is exceeded, newest retained.
func TestErrorHandlerLedger_EvictsOldestBeyondCap(t *testing.T) {
	withTempErrorHandlerDir(t)
	const extra = 4
	for i := range errorHandlerLedgerMaxEntries + extra {
		errorHandlerLedgerStamp(fmt.Sprintf("sig-%04d", i), "proposed")
	}
	ledger := map[string]errorHandlerLedgerEntry{}
	readErrorHandlerJSON(errorHandlerLedgerPath(), &ledger)
	if len(ledger) != errorHandlerLedgerMaxEntries {
		t.Fatalf("ledger size = %d, want cap %d", len(ledger), errorHandlerLedgerMaxEntries)
	}
	if _, ok := ledger[fmt.Sprintf("sig-%04d", errorHandlerLedgerMaxEntries+extra-1)]; !ok {
		t.Fatal("newest entry must be retained")
	}
	for i := range extra {
		if _, ok := ledger[fmt.Sprintf("sig-%04d", i)]; ok {
			t.Fatalf("oldest entry sig-%04d must have been evicted", i)
		}
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

// I2: a store file that EXISTS but cannot be parsed must never be rewritten
// from an empty map — that would silently destroy every persisted extension.
func TestErrorHandlerStore_CorruptFileIsNeverOverwritten(t *testing.T) {
	withTempErrorHandlerDir(t)
	path := errorHandlerExtensionsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{ broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	ext := ErrorHandlerExtension{Node: evolution.SerializableNode{Type: "Sequence", Name: "n1"}}
	if err := appendErrorHandlerExtension("h", ext); err == nil {
		t.Fatal("append over a corrupt store must return an error")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "{ broken" {
		t.Fatalf("corrupt store must be left untouched, got %q (err=%v)", data, err)
	}
	// Advisory writers must also abort rather than rewrite from empty.
	recordErrorHandlerResult("h", "n1", false)
	data, _ = os.ReadFile(path)
	if string(data) != "{ broken" {
		t.Fatalf("recordErrorHandlerResult rewrote a corrupt store: %q", data)
	}
	ledgerPath := errorHandlerLedgerPath()
	if err := os.WriteFile(ledgerPath, []byte("{ broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	errorHandlerLedgerStamp("sig1", "proposed")
	data, _ = os.ReadFile(ledgerPath)
	if string(data) != "{ broken" {
		t.Fatalf("errorHandlerLedgerStamp rewrote a corrupt ledger: %q", data)
	}
}

// I2: a good .bak (the only rollback copy) must never be clobbered with the
// "{}" first-append placeholder when the main file is absent.
func TestErrorHandlerStore_BakNotClobberedByPlaceholder(t *testing.T) {
	withTempErrorHandlerDir(t)
	ext := ErrorHandlerExtension{Node: evolution.SerializableNode{Type: "Sequence", Name: "n1"}}
	if err := appendErrorHandlerExtension("h", ext); err != nil {
		t.Fatal(err)
	}
	ext2 := ErrorHandlerExtension{Node: evolution.SerializableNode{Type: "Sequence", Name: "n2"}}
	if err := appendErrorHandlerExtension("h", ext2); err != nil {
		t.Fatal(err)
	}
	bakPath := errorHandlerExtensionsPath() + ".bak"
	good, err := os.ReadFile(bakPath)
	if err != nil || string(good) == "{}" {
		t.Fatalf("expected a good .bak after second append, got %q (err=%v)", good, err)
	}
	// Main file vanishes (partial wipe); the next append must NOT reset the
	// good .bak to "{}".
	if err := os.Remove(errorHandlerExtensionsPath()); err != nil {
		t.Fatal(err)
	}
	if err := appendErrorHandlerExtension("h", ext); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(bakPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(good) {
		t.Fatalf(".bak clobbered: had %q, now %q", good, after)
	}
}

// I2: cross-process store lock — writers skip (advisory) or error (append)
// instead of racing a concurrent sibling's read-modify-write.
func TestErrorHandlerStore_LockContention(t *testing.T) {
	withTempErrorHandlerDir(t)
	ext := ErrorHandlerExtension{Node: evolution.SerializableNode{Type: "Sequence", Name: "n1"}}
	if err := appendErrorHandlerExtension("h", ext); err != nil {
		t.Fatal(err)
	}
	release, ok := acquireErrorHandlerStoreLock()
	if !ok {
		t.Fatal("store lock must be acquirable")
	}
	defer release()
	if err := appendErrorHandlerExtension("h", ext); err == nil {
		t.Fatal("append while the store lock is held must return an error")
	}
	recordErrorHandlerResult("h", "n1", false) // advisory: must skip, not block
	if all := loadErrorHandlerExtensions("h"); len(all) != 1 || all[0].ConsecutiveFailures != 0 {
		t.Fatalf("locked store must be untouched: %+v", all)
	}
	errorHandlerLedgerStamp("sig1", "proposed") // advisory: must skip
	if _, ok := errorHandlerLedgerGet("sig1"); ok {
		t.Fatal("ledger stamp under contention must be skipped")
	}
}

func TestErrorHandlerStore_ConcurrentRecordsCountExactly(t *testing.T) {
	withTempErrorHandlerDir(t)
	ext := ErrorHandlerExtension{Node: evolution.SerializableNode{Type: "Sequence", Name: "n1"}}
	if err := appendErrorHandlerExtension("h", ext); err != nil {
		t.Fatal(err)
	}
	const workers = 12
	var wg sync.WaitGroup
	for range workers {
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
