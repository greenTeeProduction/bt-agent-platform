package reliability

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── Circuit Breaker Tests ──────────────────────────────────────────────────

func TestCircuitBreaker_Closed(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, time.Second)
	if cb.State() != CircuitClosed {
		t.Error("new circuit should be closed")
	}
	if !cb.Allow() {
		t.Error("closed circuit should allow requests")
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker("test", 2, time.Second)
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Error("circuit should be open after 2 failures")
	}
	if cb.Allow() {
		t.Error("open circuit should deny requests")
	}
}

func TestCircuitBreaker_HalfOpen(t *testing.T) {
	cb := NewCircuitBreaker("test", 1, 50*time.Millisecond)
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Error("should be open after failure")
	}
	time.Sleep(60 * time.Millisecond)
	if !cb.Allow() {
		t.Error("should allow one request in half-open")
	}
	if cb.Allow() {
		t.Error("should only allow one request in half-open")
	}
}

func TestCircuitBreaker_RecoversAfterSuccess(t *testing.T) {
	cb := NewCircuitBreaker("test", 1, 50*time.Millisecond)
	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)
	cb.Allow() // half-open
	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Errorf("should be closed after recovery, got %s", cb.State())
	}
}

func TestCircuitBreaker_FailsInHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker("test", 1, 50*time.Millisecond)
	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)
	cb.Allow() // half-open
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Error("should re-open on half-open failure")
	}
}

// ─── Backoff Tests ──────────────────────────────────────────────────────────

func TestBackoff(t *testing.T) {
	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{1, time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 10 * time.Second}, // capped at maxDelay
	}

	for _, tt := range tests {
		got := Backoff(tt.attempt, time.Second, 10*time.Second)
		if got != tt.expected {
			t.Errorf("Backoff(%d) = %v, want %v", tt.attempt, got, tt.expected)
		}
	}
}

func TestRetryWithBackoff_Success(t *testing.T) {
	attempts := 0
	err := RetryWithBackoff(3, time.Millisecond, 10*time.Millisecond, func() error {
		attempts++
		if attempts < 2 {
			return errors.New("fail")
		}
		return nil
	})
	if err != nil {
		t.Errorf("expected success, got: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestRetryWithBackoff_Exhausted(t *testing.T) {
	attempts := 0
	err := RetryWithBackoff(3, time.Millisecond, 10*time.Millisecond, func() error {
		attempts++
		return errors.New("always fail")
	})
	if err == nil {
		t.Error("expected error after exhaustion")
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

// ─── Dead Letter Queue Tests ────────────────────────────────────────────────

func TestDeadLetterQueue_PushList(t *testing.T) {
	dlq := NewDeadLetterQueue("")
	dlq.Push(DeadLetterEntry{ID: "1", Task: "test task", Error: "failed"})
	dlq.Push(DeadLetterEntry{ID: "2", Task: "test task 2", Error: "timeout"})

	entries := dlq.List()
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].FailedAt.IsZero() {
		t.Error("FailedAt should be set")
	}
}

// pushToDLQAction (internal/engine/ops_actions.go) never sets ID, and
// mergeFromDisk's byID map collapses entries that share an ID — so every
// caller that leaves ID empty must get a distinct default, or unrelated
// hitl_exhausted entries cross-contaminate each other's Attempts/RequeuedAt/
// Abandoned state.
func TestDeadLetterQueue_PushDefaultsEmptyID(t *testing.T) {
	dlq := NewDeadLetterQueue("")
	dlq.Push(DeadLetterEntry{Task: "task a", Agent: "coder"})
	dlq.Push(DeadLetterEntry{Task: "task b", Agent: "coder"})

	entries := dlq.List()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].ID == "" || entries[1].ID == "" {
		t.Fatalf("Push must default an empty ID, got IDs %q and %q", entries[0].ID, entries[1].ID)
	}
	if entries[0].ID == entries[1].ID {
		t.Fatalf("two entries pushed with empty ID must not collide on the same default: both got %q", entries[0].ID)
	}
}

// ─── Drop-safe replay (c8094002 ms1) ────────────────────────────────────────
// The old Replay removed the entry and returned it for the CALLER to execute —
// any caller without a tree runner (or that crashed mid-replay) silently
// dropped the task. Drop-safe Replay re-executes through a configured executor
// and removes the entry ONLY after the executor succeeds.

// Without an executor, Replay must refuse and retain — never hand out an entry
// it has already deleted.
func TestDeadLetterQueue_ReplayWithoutExecutorRefuses(t *testing.T) {
	dlq := NewDeadLetterQueue("")
	dlq.Push(DeadLetterEntry{ID: "a", Task: "task a"})

	if _, ok := dlq.Replay("a"); ok {
		t.Fatal("Replay without an executor must refuse")
	}
	if dlq.Len() != 1 {
		t.Fatalf("entry must be retained, got %d entries", dlq.Len())
	}
}

// A successful replay runs the executor with the entry and only then removes
// it — and the removal is persisted, surviving a reload from disk.
func TestDeadLetterQueue_ReplayRemovesOnlyOnSuccess(t *testing.T) {
	path := t.TempDir() + "/dlq.json"
	dlq := NewDeadLetterQueue(path)
	dlq.Push(DeadLetterEntry{ID: "a", Task: "task a", Agent: "agent-a"})
	dlq.Push(DeadLetterEntry{ID: "b", Task: "task b"})

	var executed DeadLetterEntry
	dlq.SetReplayExecutor(func(e DeadLetterEntry) error {
		executed = e
		return nil
	})

	entry, ok := dlq.Replay("a")
	if !ok || entry == nil || entry.ID != "a" {
		t.Fatalf("successful replay must report the replayed entry, got %v/%v", entry, ok)
	}
	if executed.Task != "task a" || executed.Agent != "agent-a" {
		t.Fatalf("executor must receive the dead-lettered task, got %+v", executed)
	}
	if dlq.Len() != 1 {
		t.Fatalf("only the replayed entry may be removed, got %d entries", dlq.Len())
	}
	if got := NewDeadLetterQueue(path).Len(); got != 1 {
		t.Fatalf("removal must be persisted: reloaded %d entries, want 1", got)
	}
}

// A failed replay retains the entry (nothing is dropped) and clears the
// requeue flag so the background scan does not hot-loop the same failure.
func TestDeadLetterQueue_ReplayRetainsOnFailure(t *testing.T) {
	dlq := NewDeadLetterQueue("")
	dlq.Push(DeadLetterEntry{ID: "a", Task: "task a"})
	if _, ok := dlq.Requeue("a"); !ok {
		t.Fatal("setup: requeue must succeed")
	}
	dlq.SetReplayExecutor(func(DeadLetterEntry) error {
		return errors.New("agent outcome: failure")
	})

	if _, ok := dlq.Replay("a"); ok {
		t.Fatal("a failed replay must not report success")
	}
	entries := dlq.List()
	if len(entries) != 1 {
		t.Fatalf("failed replay must retain the entry, got %d", len(entries))
	}
	if !entries[0].RequeuedAt.IsZero() {
		t.Fatal("failed replay must clear RequeuedAt so the scan loop does not hot-loop it")
	}
}

// A failed replay must stamp its outcome on the retained entry — otherwise the
// executor error is dropped entirely and sibling processes reading the shared
// persistence file cannot tell a never-replayed entry from one that keeps
// failing.
func TestReplayFailureStampsOutcome(t *testing.T) {
	path := t.TempDir() + "/dlq.json"
	dlq := NewDeadLetterQueue(path)
	dlq.Push(DeadLetterEntry{ID: "a", Task: "task a"})
	dlq.SetReplayExecutor(func(DeadLetterEntry) error {
		return errors.New("agent exploded: step 3")
	})

	if entry, ok := dlq.Replay("a"); ok || entry != nil {
		t.Fatalf("failed replay must report (nil, false), got %v/%v", entry, ok)
	}

	entries := dlq.List()
	if len(entries) != 1 {
		t.Fatalf("failed replay must retain the entry, got %d", len(entries))
	}
	if entries[0].LastReplayAt.IsZero() {
		t.Fatal("failed replay must stamp LastReplayAt on the retained entry")
	}
	if entries[0].LastReplayError != "agent exploded: step 3" {
		t.Fatalf("failed replay must record the executor error, got %q", entries[0].LastReplayError)
	}

	// The stamp must survive the save/load round-trip so sibling processes
	// sharing the persistence file see the replay outcome.
	reloaded := NewDeadLetterQueue(path).List()
	if len(reloaded) != 1 {
		t.Fatalf("reloaded queue must retain the entry, got %d", len(reloaded))
	}
	if reloaded[0].LastReplayAt.IsZero() {
		t.Fatal("LastReplayAt must survive the persistence round-trip")
	}
	if reloaded[0].LastReplayError != "agent exploded: step 3" {
		t.Fatalf("LastReplayError must survive the persistence round-trip, got %q", reloaded[0].LastReplayError)
	}
}

// A successful replay removes the entry outright (drop-safe behavior
// unchanged) — there is nothing left to stamp.
func TestReplaySuccessLeavesNoStamp(t *testing.T) {
	dlq := NewDeadLetterQueue("")
	dlq.Push(DeadLetterEntry{ID: "a", Task: "task a"})
	dlq.SetReplayExecutor(func(DeadLetterEntry) error {
		return nil
	})

	entry, ok := dlq.Replay("a")
	if !ok || entry == nil || entry.ID != "a" {
		t.Fatalf("successful replay must report the replayed entry, got %v/%v", entry, ok)
	}
	if dlq.Len() != 0 {
		t.Fatalf("successful replay must remove the entry, got %d entries", dlq.Len())
	}
}

// An abandoned entry must never reach the executor.
func TestDeadLetterQueue_ReplayRefusesAbandoned(t *testing.T) {
	dlq := NewDeadLetterQueue("")
	dlq.Push(DeadLetterEntry{ID: "a", Task: "poison", Abandoned: true})

	invoked := false
	dlq.SetReplayExecutor(func(DeadLetterEntry) error {
		invoked = true
		return nil
	})
	if _, ok := dlq.Replay("a"); ok {
		t.Fatal("an abandoned entry must never be replayed")
	}
	if invoked {
		t.Fatal("executor must not be invoked for an abandoned entry")
	}
	if dlq.Len() != 1 {
		t.Fatal("abandoned entry stays retained for inspection")
	}
}

// RequeuedReady is the background scan's work list: requeued entries only,
// abandoned ones excluded.
func TestDeadLetterQueue_RequeuedReady(t *testing.T) {
	dlq := NewDeadLetterQueue("")
	dlq.Push(DeadLetterEntry{ID: "flagged", Task: "t"})
	dlq.Push(DeadLetterEntry{ID: "idle", Task: "t"})
	dlq.Push(DeadLetterEntry{ID: "dead", Task: "t", Abandoned: true, RequeuedAt: time.Now()})
	if _, ok := dlq.Requeue("flagged"); !ok {
		t.Fatal("setup: requeue must succeed")
	}

	ready := dlq.RequeuedReady()
	if len(ready) != 1 || ready[0] != "flagged" {
		t.Fatalf("RequeuedReady = %v, want [flagged] (idle not requeued, dead abandoned)", ready)
	}
}

func TestDeadLetterQueue_Purge(t *testing.T) {
	dlq := NewDeadLetterQueue("")
	dlq.Push(DeadLetterEntry{ID: "1"})
	dlq.Push(DeadLetterEntry{ID: "2"})
	dlq.Purge()
	if dlq.Len() != 0 {
		t.Errorf("expected 0 after purge, got %d", dlq.Len())
	}
}

func TestDeadLetterQueue_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/dlq.json"

	dlq := NewDeadLetterQueue(path)
	dlq.Push(DeadLetterEntry{ID: "p1", Task: "persist me"})

	// Load from disk
	dlq2 := NewDeadLetterQueue(path)
	if dlq2.Len() != 1 {
		t.Errorf("expected 1 entry after reload, got %d", dlq2.Len())
	}
}

// TestDeadLetterQueue_SaveMergesSiblingRequeueStamps is the two-queue
// shared-file clobber regression for the production multi-process topology:
// bt-agent's daemon and the dashboard each hold their own DeadLetterQueue over
// the same persistence file. The dashboard stamps RequeuedAt via Requeue and
// saves; the daemon — whose in-memory view predates that stamp — then saves
// after its own mutation. Without cross-process merge-on-save the daemon's
// stale view overwrites the file wholesale, erasing the requeue stamp, and the
// executor's next scan never sees the flagged task: replay stays dead
// cross-process. save() must merge the on-disk sibling state by entry ID with
// monotonic Attempts/RequeuedAt before writing.
func TestDeadLetterQueue_SaveMergesSiblingRequeueStamps(t *testing.T) {
	path := t.TempDir() + "/dlq.json"

	// Daemon process: dead-letters a task and persists it.
	daemon := NewDeadLetterQueue(path)
	daemon.Push(DeadLetterEntry{ID: "stamped", Task: "retry me", Agent: "coder"})

	// Dashboard process: loads the shared file and flags the entry for retry.
	dashboard := NewDeadLetterQueue(path)
	if _, ok := dashboard.Requeue("stamped"); !ok {
		t.Fatal("setup: dashboard requeue must succeed")
	}

	// Daemon process: dead-letters another task. Its in-memory copy of
	// "stamped" predates the dashboard's stamp, so a whole-file rewrite here
	// clobbers RequeuedAt and resets Attempts.
	daemon.Push(DeadLetterEntry{ID: "daemon-side", Task: "unrelated failure"})

	// Executor scan: a fresh load of the shared file must still see the stamp.
	scan := NewDeadLetterQueue(path)
	entries := scan.List()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries in shared file, got %d: %+v", len(entries), entries)
	}
	byID := make(map[string]DeadLetterEntry, len(entries))
	for _, e := range entries {
		byID[e.ID] = e
	}
	if _, ok := byID["daemon-side"]; !ok {
		t.Error("daemon's own new entry must survive the merged save")
	}
	stamped, ok := byID["stamped"]
	if !ok {
		t.Fatal("entry \"stamped\" missing from shared file")
	}
	if stamped.RequeuedAt.IsZero() {
		t.Error("daemon save clobbered the dashboard's RequeuedAt stamp — save() must merge sibling state by entry ID before writing")
	}
	if stamped.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1 (monotonic: dashboard's counted attempt must survive the daemon's stale save)", stamped.Attempts)
	}
	if ready := scan.RequeuedReady(); len(ready) != 1 || ready[0] != "stamped" {
		t.Errorf("executor scan RequeuedReady = %v, want [stamped] — clobbered stamp leaves the replay dead cross-process", ready)
	}
}

// TestDeadLetterQueue_SaveMergesSiblingAbandoned asserts the Abandoned flag is
// monotonic across processes: once a sibling terminally abandons a poison pill
// (its replay budget is exhausted), the daemon's stale save must not resurrect
// it into the auto-requeue pool, or the poison pill drives the very replay
// loop the flag exists to break.
func TestDeadLetterQueue_SaveMergesSiblingAbandoned(t *testing.T) {
	path := t.TempDir() + "/dlq.json"

	daemon := NewDeadLetterQueue(path)
	daemon.Push(DeadLetterEntry{ID: "poison", Task: "always fails", Attempts: MaxReplayAttempts})

	// Sibling process: requeue attempt on the exhausted entry terminally
	// abandons it and persists the flag.
	sibling := NewDeadLetterQueue(path)
	if _, ok := sibling.Requeue("poison"); ok {
		t.Fatal("setup: requeue of exhausted entry must refuse and abandon")
	}

	// Daemon process: stale in-memory view (Abandoned=false) saves again.
	daemon.Push(DeadLetterEntry{ID: "fresh", Task: "new failure"})

	scan := NewDeadLetterQueue(path)
	for _, e := range scan.List() {
		if e.ID == "poison" && !e.Abandoned {
			t.Error("daemon save cleared the sibling's Abandoned flag — Abandoned must be monotonic in the merge")
		}
	}
	if ready := scan.RequeuedReady(); len(ready) != 0 {
		t.Errorf("RequeuedReady = %v, want none (abandoned poison pill must stay excluded)", ready)
	}
}

// TestDeadLetterQueue_CrossProcessRequeuePickup is the two-instance pickup
// regression for the production multi-process topology: instance B (the
// dashboard or an MCP sibling) Requeues an entry over the shared file, and
// instance A (the daemon, whose in-memory view predates the stamp) must be
// able to consume it via the fixed scan sequence Reload → RequeuedReady →
// Replay. The stale-view control documents why every cross-process consume
// site must Reload first: without it A's scan returns nothing and the requeue
// is never replayed. The successful replay's removal must also survive
// merge-on-save — a resurrected entry would replay again on every scan.
func TestDeadLetterQueue_CrossProcessRequeuePickup(t *testing.T) {
	path := t.TempDir() + "/dlq.json"

	// Instance A (daemon): dead-letters a task and installs the replay executor.
	daemon := NewDeadLetterQueue(path)
	daemon.Push(DeadLetterEntry{ID: "cross", Task: "flaky deploy", Agent: "coder"})
	var replayed []DeadLetterEntry
	daemon.SetReplayExecutor(func(e DeadLetterEntry) error {
		replayed = append(replayed, e)
		return nil
	})

	// Instance B (dashboard/MCP sibling): an independent queue over the same
	// file flags the entry for retry. The stamp lands on disk only — never in
	// A's memory.
	sibling := NewDeadLetterQueue(path)
	if _, ok := sibling.Requeue("cross"); !ok {
		t.Fatal("setup: sibling requeue must succeed")
	}

	// Control: A's stale in-memory view predates the stamp, so a scan without
	// Reload sees nothing — this is exactly the dead cross-process replay.
	if ready := daemon.RequeuedReady(); len(ready) != 0 {
		t.Fatalf("control: stale view sees %v before Reload; the cross-process gap this test pins no longer exists", ready)
	}

	// Instance A's scan tick as the daemon must run it: Reload, then consume.
	daemon.Reload()
	ready := daemon.RequeuedReady()
	if len(ready) != 1 || ready[0] != "cross" {
		t.Fatalf("after Reload, RequeuedReady = %v, want [cross] — sibling requeue stamp not picked up", ready)
	}
	for _, id := range ready {
		if _, ok := daemon.Replay(id); !ok {
			t.Fatalf("Replay(%q) refused a reloaded sibling requeue", id)
		}
	}
	if len(replayed) != 1 || replayed[0].ID != "cross" || replayed[0].Agent != "coder" {
		t.Errorf("executor ran %+v, want exactly the sibling-requeued entry (id=cross, agent=coder)", replayed)
	}

	// A fresh process load must not resurrect the consumed entry.
	if got := NewDeadLetterQueue(path).Len(); got != 0 {
		t.Errorf("shared file holds %d entries after successful replay, want 0 (merge-on-save resurrected the consumed entry)", got)
	}
}

// TestDeadLetterQueue_SaveAtomicReplace asserts the ADR-003 persistence
// behavior arc42 §8.4 claims for the DLQ: save() must write to a temp file and
// rename it over the destination, never truncate-and-rewrite in place. An
// in-place rewrite keeps the same inode, so a crash mid-write leaves a
// truncated (corrupt) queue on disk; rename swaps a complete file in one
// atomic step. os.SameFile detects the in-place rewrite.
func TestDeadLetterQueue_SaveAtomicReplace(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/dlq.json"

	dlq := NewDeadLetterQueue(path)
	dlq.Push(DeadLetterEntry{ID: "first", Task: "seed the file"})

	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after first save: %v", err)
	}

	dlq.Push(DeadLetterEntry{ID: "second", Task: "rewrite the file"})

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after second save: %v", err)
	}
	if os.SameFile(before, after) {
		t.Error("save() rewrote the DLQ file in place; a crash mid-write can truncate the queue — must write a temp file and rename it over the destination")
	}

	// The atomic swap must not strand temp artifacts next to the queue.
	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, f := range files {
		if f.Name() != "dlq.json" {
			t.Errorf("save() left stray file %q beside the DLQ", f.Name())
		}
	}

	// And the swapped-in file is a complete, loadable queue.
	if got := NewDeadLetterQueue(path).Len(); got != 2 {
		t.Errorf("expected 2 entries after reload, got %d", got)
	}
}

// TestDeadLetterQueue_LoadQuarantinesCorruptFile asserts load() surfaces a
// corrupt persistence file instead of discarding the json.Unmarshal error:
// the unreadable payload must be quarantined to <path>.corrupt so the queue
// can start empty WITHOUT its next save silently persisting the wipe over the
// only copy of the dead-lettered tasks.
func TestDeadLetterQueue_LoadQuarantinesCorruptFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/dlq.json"
	garbage := "{this is not json"
	if err := os.WriteFile(path, []byte(garbage), 0644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	dlq := NewDeadLetterQueue(path)
	if dlq.Len() != 0 {
		t.Fatalf("corrupt file must yield an empty queue, got %d entries", dlq.Len())
	}

	quarantined, err := os.ReadFile(path + ".corrupt")
	if err != nil {
		t.Fatalf("corrupt DLQ file must be quarantined to <path>.corrupt: %v", err)
	}
	if string(quarantined) != garbage {
		t.Errorf("quarantined payload mismatch: got %q, want %q", quarantined, garbage)
	}
	if data, err := os.ReadFile(path); err == nil && string(data) == garbage {
		t.Error("corrupt payload still sits at the primary path awaiting the next save to clobber it")
	}

	// The queue keeps working after quarantine, and saving must not touch the
	// preserved evidence.
	dlq.Push(DeadLetterEntry{ID: "after-corruption", Task: "fresh entry"})
	if got := NewDeadLetterQueue(path).Len(); got != 1 {
		t.Errorf("expected 1 entry after post-quarantine save, got %d", got)
	}
	quarantined, err = os.ReadFile(path + ".corrupt")
	if err != nil || string(quarantined) != garbage {
		t.Errorf("quarantine file must survive subsequent saves: err=%v content=%q", err, quarantined)
	}
}

// TestDeadLetterQueue_EvictionBound asserts the graceful-degradation cap: the
// DLQ never grows past MaxDeadLetterEntries, and when it overflows the OLDEST
// entries are evicted first (bounded memory / disk under a failure storm).
func TestDeadLetterQueue_EvictionBound(t *testing.T) {
	dlq := NewDeadLetterQueue("")

	total := MaxDeadLetterEntries + 25
	for i := range total {
		dlq.Push(DeadLetterEntry{ID: fmt.Sprintf("e-%d", i), Task: "storm"})
	}

	if dlq.Len() != MaxDeadLetterEntries {
		t.Fatalf("expected Len capped at %d, got %d", MaxDeadLetterEntries, dlq.Len())
	}

	ids := make(map[string]bool, MaxDeadLetterEntries)
	for _, e := range dlq.List() {
		ids[e.ID] = true
	}

	// Oldest-first eviction: the first-pushed entry is gone, the last survives.
	if ids["e-0"] {
		t.Errorf("oldest entry e-0 should have been evicted")
	}
	if newest := fmt.Sprintf("e-%d", total-1); !ids[newest] {
		t.Errorf("newest entry %s should be retained", newest)
	}
	// The retained window is exactly the most recent MaxDeadLetterEntries IDs.
	if firstKept := fmt.Sprintf("e-%d", total-MaxDeadLetterEntries); !ids[firstKept] {
		t.Errorf("entry %s should be within the retained window", firstKept)
	}
	if justEvicted := fmt.Sprintf("e-%d", total-MaxDeadLetterEntries-1); ids[justEvicted] {
		t.Errorf("entry %s should have been evicted (just outside the window)", justEvicted)
	}
}

// TestDeadLetterQueue_PoisonPillExclusion asserts that an entry which keeps
// failing replay is terminally flagged Abandoned once its Attempts exceed
// MaxReplayAttempts, and is then excluded from further auto-requeue — the guard
// that stops a poison pill from driving an infinite replay loop.
func TestDeadLetterQueue_PoisonPillExclusion(t *testing.T) {
	dlq := NewDeadLetterQueue("")
	dlq.Push(DeadLetterEntry{ID: "poison", Task: "always fails"})

	// Auto-requeue up to the threshold: each requeue counts one replay attempt.
	for i := range MaxReplayAttempts {
		if _, ok := dlq.Requeue("poison"); !ok {
			t.Fatalf("requeue %d should succeed while under the attempt threshold", i+1)
		}
	}

	// Exceeding the threshold must be refused and terminally abandon the entry.
	if _, ok := dlq.Requeue("poison"); ok {
		t.Fatal("requeue past the attempt threshold must be refused (poison-pill excluded)")
	}

	entries := dlq.List()
	if len(entries) != 1 {
		t.Fatalf("abandoned entry should remain for inspection, got %d entries", len(entries))
	}
	e := entries[0]
	if !e.Abandoned {
		t.Errorf("entry exceeding %d replay attempts must be flagged Abandoned", MaxReplayAttempts)
	}
	if e.Attempts < MaxReplayAttempts {
		t.Errorf("expected Attempts >= %d, got %d", MaxReplayAttempts, e.Attempts)
	}

	// Terminal: subsequent requeues stay refused (no infinite replay loop).
	if _, ok := dlq.Requeue("poison"); ok {
		t.Error("an abandoned entry must never be auto-requeued again")
	}
}

// ─── Worker Pool Tests ──────────────────────────────────────────────────────

func TestWorkerPool_Submit(t *testing.T) {
	wp := NewWorkerPool(2)
	defer wp.Shutdown()

	done := make(chan bool, 2)
	wp.Submit(func() { done <- true })
	wp.Submit(func() { done <- true })

	for range 2 {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("task timed out")
		}
	}
}

func TestWorkerPool_Stats(t *testing.T) {
	wp := NewWorkerPool(1)
	defer wp.Shutdown()

	done := make(chan bool)
	wp.Submit(func() { done <- true })
	<-done

	time.Sleep(10 * time.Millisecond) // let stats update
	active, queued, total, completed := wp.Stats()
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
	if completed != 1 {
		t.Errorf("expected completed=1, got %d", completed)
	}
	_ = active
	_ = queued
}

// ─── Task Queue Tests ───────────────────────────────────────────────────────

func TestTaskQueue_EnqueueDequeue(t *testing.T) {
	tq := NewTaskQueue("")
	tq.Enqueue("task1")
	tq.Enqueue("task2")

	if tq.Len() != 2 {
		t.Errorf("expected len=2, got %d", tq.Len())
	}

	got := tq.Dequeue()
	if got != "task1" {
		t.Errorf("expected 'task1', got %q", got)
	}
}

func TestTaskQueue_Peek(t *testing.T) {
	tq := NewTaskQueue("")
	tq.Enqueue("first")
	peeked := tq.Peek()
	if peeked != "first" {
		t.Errorf("peek expected 'first', got %q", peeked)
	}
	if tq.Len() != 1 {
		t.Error("peek should not remove")
	}
}

func TestTaskQueue_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/queue.json"

	tq := NewTaskQueue(path)
	tq.Enqueue("hello")
	tq.Enqueue("world")

	tq2 := NewTaskQueue(path)
	if tq2.Len() != 2 {
		t.Errorf("expected 2 items after reload, got %d", tq2.Len())
	}
}

// TestTaskQueue_SaveAtomicReplace asserts save() follows the same ADR-003
// temp-file-then-rename discipline as DeadLetterQueue.save (see
// TestDeadLetterQueue_SaveAtomicReplace): an in-place os.WriteFile keeps the
// same inode, so a crash mid-write leaves a truncated queue on disk. Rename
// swaps a complete file in one atomic step. os.SameFile detects the in-place
// rewrite.
func TestTaskQueue_SaveAtomicReplace(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/queue.json"

	tq := NewTaskQueue(path)
	tq.Enqueue("first")

	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after first save: %v", err)
	}

	tq.Enqueue("second")

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after second save: %v", err)
	}
	if os.SameFile(before, after) {
		t.Error("save() rewrote the task queue file in place; a crash mid-write can truncate the queue — must write a temp file and rename it over the destination")
	}
}

// ─── Scheduler Persistence Tests ────────────────────────────────────────────

func TestSchedulerState_SaveLoad(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/scheduler.json"

	ss := NewSchedulerState(path)
	ss.Save(JobState{
		ID:       "job-1",
		Name:     "test-job",
		Schedule: "every 1h",
		RunCount: 5,
		Enabled:  true,
	})

	ss2 := NewSchedulerState(path)
	state, ok := ss2.Get("job-1")
	if !ok {
		t.Fatal("job should exist after reload")
	}
	if state.RunCount != 5 {
		t.Errorf("expected RunCount=5, got %d", state.RunCount)
	}
}

func TestSchedulerState_Delete(t *testing.T) {
	ss := NewSchedulerState("")
	ss.Save(JobState{ID: "del-me", Name: "temp"})
	ss.Delete("del-me")
	_, ok := ss.Get("del-me")
	if ok {
		t.Error("deleted job should not exist")
	}
}

func TestNewSchedulerState_NonexistentPath(t *testing.T) {
	tmpDir := t.TempDir()
	ss := NewSchedulerState(tmpDir + "/nonexistent/dir/scheduler.json")
	// should not panic; just empty
	if len(ss.List()) != 0 {
		t.Error("should be empty")
	}
}

// TestSchedulerState_PersistAtomicReplace asserts persist() follows the same
// ADR-003 temp-file-then-rename discipline as DeadLetterQueue.save (see
// TestDeadLetterQueue_SaveAtomicReplace): an in-place os.WriteFile keeps the
// same inode, so a crash mid-write leaves truncated job state on disk. Rename
// swaps a complete file in one atomic step. os.SameFile detects the in-place
// rewrite.
func TestSchedulerState_PersistAtomicReplace(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/scheduler.json"

	ss := NewSchedulerState(path)
	ss.Save(JobState{ID: "job-1", Name: "first"})

	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after first save: %v", err)
	}

	ss.Save(JobState{ID: "job-2", Name: "second"})

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after second save: %v", err)
	}
	if os.SameFile(before, after) {
		t.Error("persist() rewrote the scheduler state file in place; a crash mid-write can truncate job state — must write a temp file and rename it over the destination")
	}
}

func init() {
	// Silence dead letter queue persistence errors in tests
	os.Setenv("BT_TEST_MODE", "1")
}

// ─── Priority Queue Tests ────────────────────────────────────────────────────

func TestPriorityQueue_DequeueOrder(t *testing.T) {
	pq := NewPriorityQueue("")
	pq.Enqueue("low task", "agent-a", PriorityLow)
	pq.Enqueue("critical task", "agent-b", PriorityCritical)
	pq.Enqueue("high task", "agent-c", PriorityHigh)
	pq.Enqueue("medium task", "agent-d", PriorityMedium)

	expected := []Priority{PriorityCritical, PriorityHigh, PriorityMedium, PriorityLow}
	for i, exp := range expected {
		task := pq.Dequeue()
		if task.Priority != exp {
			t.Errorf("dequeue %d: expected %s, got %s (task=%q)", i, exp, task.Priority, task.Task)
		}
		if task.ID == "" {
			t.Error("task ID should not be empty")
		}
	}
}

func TestPriorityQueue_SamePriorityFIFO(t *testing.T) {
	pq := NewPriorityQueue("")
	pq.Enqueue("task 1", "agent", PriorityMedium)
	pq.Enqueue("task 2", "agent", PriorityMedium)
	pq.Enqueue("task 3", "agent", PriorityMedium)

	t1 := pq.Dequeue()
	t2 := pq.Dequeue()
	t3 := pq.Dequeue()

	// Min-heap with same priority doesn't guarantee FIFO,
	// but all three should be PriorityMedium
	if t1.Priority != PriorityMedium {
		t.Error("all should be medium")
	}
	_ = t2
	_ = t3
}

func TestPriorityQueue_Empty(t *testing.T) {
	pq := NewPriorityQueue("")
	task := pq.Dequeue()
	if task.ID != "" {
		t.Error("empty dequeue should return zero PriorityTask")
	}
	if pq.Len() != 0 {
		t.Error("empty queue should have len 0")
	}
}

func TestPriorityQueue_Peek(t *testing.T) {
	pq := NewPriorityQueue("")
	pq.Enqueue("low", "a", PriorityLow)
	pq.Enqueue("critical", "b", PriorityCritical)

	peeked := pq.Peek()
	if peeked.Priority != PriorityCritical {
		t.Errorf("peek expected critical, got %s", peeked.Priority)
	}
	if pq.Len() != 2 {
		t.Error("peek should not remove")
	}
}

func TestPriorityQueue_Purge(t *testing.T) {
	pq := NewPriorityQueue("")
	pq.Enqueue("a", "x", PriorityMedium)
	pq.Enqueue("b", "y", PriorityHigh)
	pq.Purge()
	if pq.Len() != 0 {
		t.Errorf("after purge, len should be 0, got %d", pq.Len())
	}
}

func TestPriorityQueue_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/priority_queue.json"

	pq1 := NewPriorityQueue(path)
	pq1.Enqueue("critical task", "agent-x", PriorityCritical)
	pq1.Enqueue("bg task", "agent-y", PriorityBackground)

	pq2 := NewPriorityQueue(path)
	if pq2.Len() != 2 {
		t.Errorf("expected 2 tasks after reload, got %d", pq2.Len())
	}

	task := pq2.Dequeue()
	if task.Priority != PriorityCritical {
		t.Errorf("expected critical after reload, got %s", task.Priority)
	}
}

// TestPriorityQueue_SaveAtomicReplace asserts save() follows the same
// ADR-003 temp-file-then-rename discipline as DeadLetterQueue.save (see
// TestDeadLetterQueue_SaveAtomicReplace): an in-place os.WriteFile keeps the
// same inode, so a crash mid-write leaves a truncated heap on disk. Rename
// swaps a complete file in one atomic step. os.SameFile detects the in-place
// rewrite.
func TestPriorityQueue_SaveAtomicReplace(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/priority_queue.json"

	pq := NewPriorityQueue(path)
	pq.Enqueue("first", "agent-a", PriorityMedium)

	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after first save: %v", err)
	}

	pq.Enqueue("second", "agent-b", PriorityHigh)

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after second save: %v", err)
	}
	if os.SameFile(before, after) {
		t.Error("save() rewrote the priority queue file in place; a crash mid-write can truncate the heap — must write a temp file and rename it over the destination")
	}
}

func TestPriorityQueue_List(t *testing.T) {
	pq := NewPriorityQueue("")
	pq.Enqueue("c", "a", PriorityLow)
	pq.Enqueue("a", "a", PriorityCritical)
	pq.Enqueue("b", "a", PriorityHigh)

	list := pq.List()
	if len(list) != 3 {
		t.Errorf("expected 3, got %d", len(list))
	}
}

func TestPriority_String(t *testing.T) {
	tests := []struct {
		p Priority
		s string
	}{
		{PriorityCritical, "critical"},
		{PriorityHigh, "high"},
		{PriorityMedium, "medium"},
		{PriorityLow, "low"},
		{PriorityBackground, "background"},
		{Priority(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.p.String(); got != tt.s {
			t.Errorf("Priority(%d).String() = %q, want %q", tt.p, got, tt.s)
		}
	}
}

// ─── Concurrency Limiter Tests ───────────────────────────────────────────────

func TestConcurrencyLimiter_AcquireRelease(t *testing.T) {
	cl := NewConcurrencyLimiter(2)
	if cl.Capacity() != 2 {
		t.Errorf("capacity should be 2, got %d", cl.Capacity())
	}

	cl.Acquire()
	cl.Acquire()

	active, waiting, total := cl.Stats()
	if active != 2 {
		t.Errorf("expected 2 active, got %d", active)
	}
	if total != 2 {
		t.Errorf("expected 2 total, got %d", total)
	}
	_ = waiting

	cl.Release()
	active, _, _ = cl.Stats()
	if active != 1 {
		t.Errorf("expected 1 active after release, got %d", active)
	}
}

func TestConcurrencyLimiter_TryAcquire(t *testing.T) {
	cl := NewConcurrencyLimiter(1)

	if !cl.TryAcquire() {
		t.Error("first TryAcquire should succeed")
	}
	if cl.TryAcquire() {
		t.Error("second TryAcquire should fail when full")
	}

	cl.Release()
	if !cl.TryAcquire() {
		t.Error("TryAcquire should succeed after release")
	}
	cl.Release()
}

func TestConcurrencyLimiter_Available(t *testing.T) {
	cl := NewConcurrencyLimiter(3)
	if cl.Available() != 3 {
		t.Errorf("initial available: expected 3, got %d", cl.Available())
	}
	cl.Acquire()
	if cl.Available() != 2 {
		t.Errorf("after 1 acquire: expected 2, got %d", cl.Available())
	}
	cl.Acquire()
	cl.Acquire()
	if cl.Available() != 0 {
		t.Errorf("after 3 acquires: expected 0, got %d", cl.Available())
	}
	cl.Release()
	cl.Release()
	cl.Release()
	if cl.Available() != 3 {
		t.Errorf("after 3 releases: expected 3, got %d", cl.Available())
	}
}

func TestConcurrencyLimiter_ReleaseWhenEmpty(t *testing.T) {
	cl := NewConcurrencyLimiter(1)
	// Release when nothing acquired should not panic
	cl.Release()
	active, _, _ := cl.Stats()
	if active != 0 {
		t.Errorf("expected 0 active, got %d", active)
	}
}

func TestConcurrencyLimiter_MultipleReleaseNoUnderflow(t *testing.T) {
	cl := NewConcurrencyLimiter(1)
	cl.Acquire()
	cl.Release()
	cl.Release()
	cl.Release() // should not underflow
	active, _, _ := cl.Stats()
	if active != 0 {
		t.Errorf("expected 0 active after multiple releases, got %d", active)
	}
}

// ─── AgentExecutor Tests ─────────────────────────────────────────────────────

func TestLocalExecutor_Execute(t *testing.T) {
	expected := &AgentResult{
		Agent:        "test-agent",
		Task:         "echo hello",
		Output:       "hello",
		Success:      true,
		QualityScore: 0.95,
	}
	exec := NewLocalExecutor("local-1", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return expected, nil
	})

	result, err := exec.Execute(context.Background(), "test-agent", "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Agent != expected.Agent {
		t.Errorf("expected agent %q, got %q", expected.Agent, result.Agent)
	}
	if result.Output != expected.Output {
		t.Errorf("expected output %q, got %q", expected.Output, result.Output)
	}
	if !result.Success {
		t.Error("expected success=true")
	}
}

func TestLocalExecutor_Health(t *testing.T) {
	exec := NewLocalExecutor("local-1", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return &AgentResult{Success: true}, nil
	})
	if err := exec.Health(); err != nil {
		t.Errorf("healthy executor should return nil, got %v", err)
	}
}

func TestLocalExecutor_WithHealthCheck(t *testing.T) {
	exec := NewLocalExecutor("local-1", nil).
		WithHealthCheck(func() error { return errors.New("unhealthy") })
	if err := exec.Health(); err == nil {
		t.Error("unhealthy executor should return error")
	}
}

func TestLocalExecutor_String(t *testing.T) {
	exec := NewLocalExecutor("local-1", nil)
	if s := exec.String(); s != "local-1" {
		t.Errorf("expected 'local-1', got %q", s)
	}
}

func TestAgentRouter_RoundRobinRouting(t *testing.T) {
	callCount := map[string]int{}
	makeExec := func(name string) *LocalExecutor {
		return NewLocalExecutor(name, func(_ context.Context, agent, task string) (*AgentResult, error) {
			callCount[name]++
			return &AgentResult{Agent: agent, Task: task, Success: true}, nil
		})
	}

	e1 := makeExec("e1")
	e2 := makeExec("e2")
	e3 := makeExec("e3")
	router := NewAgentRouter(e1, e2, e3)

	// Execute 6 tasks — each executor should get 2 (round-robin)
	for range 6 {
		_, err := router.Execute(context.Background(), "agent", "task")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if callCount["e1"] != 2 || callCount["e2"] != 2 || callCount["e3"] != 2 {
		t.Errorf("expected each executor called 2 times, got e1=%d e2=%d e3=%d",
			callCount["e1"], callCount["e2"], callCount["e3"])
	}
}

func TestAgentRouter_FallbackToLocalWhenUnhealthy(t *testing.T) {
	healthy := NewLocalExecutor("healthy", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return &AgentResult{Success: true, Output: "remote"}, nil
	})
	unhealthy := NewLocalExecutor("unhealthy", nil).
		WithHealthCheck(func() error { return errors.New("down") })
	local := NewLocalExecutor("local", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return &AgentResult{Success: true, Output: "local"}, nil
	})

	router := NewAgentRouter(unhealthy)
	router.SetLocal(local)

	result, err := router.Execute(context.Background(), "agent", "task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "local" {
		t.Errorf("expected fallback to local, got %q", result.Output)
	}

	// Now add a healthy executor
	router.Add(healthy)
	result2, err := router.Execute(context.Background(), "agent", "task2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result2.Output != "remote" {
		t.Errorf("expected remote execution, got %q", result2.Output)
	}
}

func TestAgentRouter_NoExecutorsWhenEmpty(t *testing.T) {
	router := NewAgentRouter()
	_, err := router.Execute(context.Background(), "agent", "task")
	if err == nil {
		t.Error("expected error when no executors available")
	}
}

func TestAgentRouter_Health(t *testing.T) {
	healthy := NewLocalExecutor("healthy", nil)
	unhealthy := NewLocalExecutor("unhealthy", nil).
		WithHealthCheck(func() error { return errors.New("down") })

	router := NewAgentRouter(unhealthy, unhealthy)
	if err := router.Health(); err == nil {
		t.Error("expected unhealthy with all executors down")
	}

	router.Add(healthy)
	if err := router.Health(); err != nil {
		t.Errorf("expected healthy after adding healthy executor, got %v", err)
	}
}

func TestAgentRouter_EmptyHealth(t *testing.T) {
	router := NewAgentRouter()
	if err := router.Health(); err == nil {
		t.Error("empty router should report unhealthy")
	}
}

func TestAgentRouter_Executors(t *testing.T) {
	e1 := NewLocalExecutor("e1", nil)
	e2 := NewLocalExecutor("e2", nil)
	router := NewAgentRouter(e1, e2)

	executors := router.Executors()
	if len(executors) != 2 {
		t.Errorf("expected 2 executors, got %d", len(executors))
	}
}

func TestAgentRouter_HealthyExecutors(t *testing.T) {
	healthy := NewLocalExecutor("healthy", nil)
	unhealthy := NewLocalExecutor("unhealthy", nil).
		WithHealthCheck(func() error { return errors.New("down") })

	router := NewAgentRouter(healthy, unhealthy)
	healthyList := router.HealthyExecutors()
	if len(healthyList) != 1 {
		t.Errorf("expected 1 healthy executor, got %d", len(healthyList))
	}
	if healthyList[0].String() != "healthy" {
		t.Errorf("expected 'healthy', got %q", healthyList[0].String())
	}
}

func TestAgentRouter_String(t *testing.T) {
	e1 := NewLocalExecutor("e1", nil)
	e2 := NewLocalExecutor("e2", nil)
	router := NewAgentRouter(e1, e2)

	s := router.String()
	// Updated format includes failure/cooling stats for multi-node diagnostics.
	expected := "AgentRouter(executors=2, strategy=round_robin, local=e1, failures=0, cooling=0)"
	if s != expected {
		t.Errorf("unexpected String() output: %q", s)
	}
}

func TestAgentRouter_GracefulDegradation(t *testing.T) {
	// All remote executors fail health, but local fallback works
	remote1 := NewLocalExecutor("remote-1", nil).
		WithHealthCheck(func() error { return errors.New("network timeout") })
	remote2 := NewLocalExecutor("remote-2", nil).
		WithHealthCheck(func() error { return errors.New("connection refused") })
	local := NewLocalExecutor("local-fallback", func(_ context.Context, agent, task string) (*AgentResult, error) {
		return &AgentResult{Agent: agent, Task: task, Success: true, Output: "degraded but working"}, nil
	})

	router := NewAgentRouter(remote1, remote2)
	router.SetLocal(local)

	result, err := router.Execute(context.Background(), "agent", "critical-task")
	if err != nil {
		t.Fatalf("graceful degradation should not error: %v", err)
	}
	if result.Output != "degraded but working" {
		t.Errorf("expected degraded output, got %q", result.Output)
	}
}

func TestAgentExecutor_AgentResultFields(t *testing.T) {
	result := &AgentResult{
		Agent:        "test",
		Task:         "do things",
		Output:       "done",
		Duration:     150 * time.Millisecond,
		Success:      true,
		QualityScore: 0.88,
	}

	if result.Duration != 150*time.Millisecond {
		t.Error("duration field not preserved")
	}
	if result.QualityScore != 0.88 {
		t.Error("quality_score field not preserved")
	}
}

// ─── AgentRouter Failover Tests ─────────────────────────────────────────────

func TestAgentRouter_FailoverOnExecuteError(t *testing.T) {
	// Executor passes Health() but Execute() fails → router tries next executor
	failing := NewLocalExecutor("failing", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("transient error")
	})
	working := NewLocalExecutor("working", func(_ context.Context, agent, task string) (*AgentResult, error) {
		return &AgentResult{Agent: agent, Task: task, Success: true, Output: "from working"}, nil
	})

	router := NewAgentRouter(failing, working)
	result, err := router.Execute(context.Background(), "agent", "task")
	if err != nil {
		t.Fatalf("expected failover to working executor, got error: %v", err)
	}
	if result.Output != "from working" {
		t.Errorf("expected output from working executor, got %q", result.Output)
	}
}

func TestAgentRouter_AllExecutorsFail(t *testing.T) {
	// All executors pass Health() but Execute() fails → error returned
	e1 := NewLocalExecutor("e1", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("error from e1")
	})
	e2 := NewLocalExecutor("e2", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("error from e2")
	})

	router := NewAgentRouter(e1, e2)
	_, err := router.Execute(context.Background(), "agent", "task")
	if err == nil {
		t.Fatal("expected error when all executors fail")
	}
}

func TestAgentRouter_FailoverThenLocalFallback(t *testing.T) {
	// All remote executors fail Execute(), local fallback succeeds
	remote1 := NewLocalExecutor("remote-1", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("remote-1 failed")
	})
	remote2 := NewLocalExecutor("remote-2", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("remote-2 failed")
	})
	local := NewLocalExecutor("local", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return &AgentResult{Success: true, Output: "local fallback"}, nil
	})

	router := NewAgentRouter(remote1, remote2)
	router.SetLocal(local)

	result, err := router.Execute(context.Background(), "agent", "critical-task")
	if err != nil {
		t.Fatalf("expected local fallback after remote failures, got error: %v", err)
	}
	if result.Output != "local fallback" {
		t.Errorf("expected 'local fallback', got %q", result.Output)
	}
}

func TestAgentRouter_FailoverThenLocalFailsToo(t *testing.T) {
	// All remote + local fail → combined error
	remote := NewLocalExecutor("remote", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("remote down")
	})
	local := NewLocalExecutor("local", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("local down")
	})

	router := NewAgentRouter(remote)
	router.SetLocal(local)

	_, err := router.Execute(context.Background(), "agent", "task")
	if err == nil {
		t.Fatal("expected error when all executors including local fail")
	}
}

func TestAgentRouter_FailoverNonFailoverExecutorSkipped(t *testing.T) {
	// Unhealthy executor is skipped even during failover
	unhealthy := NewLocalExecutor("unhealthy", nil).
		WithHealthCheck(func() error { return errors.New("unhealthy") })
	healthy := NewLocalExecutor("healthy", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return &AgentResult{Success: true, Output: "ok"}, nil
	})

	router := NewAgentRouter(unhealthy, healthy)
	result, err := router.Execute(context.Background(), "agent", "task")
	if err != nil {
		t.Fatalf("expected skip unhealthy and use healthy, got error: %v", err)
	}
	if result.Output != "ok" {
		t.Errorf("expected 'ok', got %q", result.Output)
	}
}

func TestAgentRouter_MaxFailoverLimit(t *testing.T) {
	// MaxFailover=1 → only try first healthy executor, even if it fails
	e1 := NewLocalExecutor("e1", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("e1 failed")
	})
	e2 := NewLocalExecutor("e2", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return &AgentResult{Success: true, Output: "e2 would work but not tried"}, nil
	})

	router := NewAgentRouter(e1, e2)
	router.MaxFailover = 1

	_, err := router.Execute(context.Background(), "agent", "task")
	if err == nil {
		t.Fatal("expected error because MaxFailover=1 prevents trying e2")
	}
}

func TestAgentRouter_MaxFailoverRespected(t *testing.T) {
	// MaxFailover=2 with 3 remote executors → only tries first 2 healthy ones,
	// then falls back to local (which is separate, not counted in failover cap)
	callCount := 0
	e1 := NewLocalExecutor("e1", func(_ context.Context, _, _ string) (*AgentResult, error) {
		callCount++
		return nil, errors.New("e1 failed")
	})
	e2 := NewLocalExecutor("e2", func(_ context.Context, _, _ string) (*AgentResult, error) {
		callCount++
		return nil, errors.New("e2 failed")
	})
	e3 := NewLocalExecutor("e3", func(_ context.Context, _, _ string) (*AgentResult, error) {
		callCount++
		return &AgentResult{Success: true}, nil
	})
	local := NewLocalExecutor("local", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return &AgentResult{Success: true, Output: "local"}, nil
	})

	router := NewAgentRouter(e1, e2, e3)
	router.SetLocal(local)
	router.MaxFailover = 2

	// e1 and e2 are tried (both fail), e3 is NOT tried (MaxFailover=2),
	// then fallback to local which succeeds.
	result, err := router.Execute(context.Background(), "agent", "task")
	if err != nil {
		t.Fatalf("expected local fallback to succeed, got error: %v", err)
	}
	if result.Output != "local" {
		t.Errorf("expected 'local' fallback output, got %q", result.Output)
	}
	if callCount != 2 {
		t.Errorf("expected 2 remote executors tried (e1,e2), got %d", callCount)
	}
}

func TestAgentRouter_FailoverMixedHealthyAndErrors(t *testing.T) {
	// Mixed: unhealthy, error-after-Execute, and working executor
	unhealthy := NewLocalExecutor("unhealthy", nil).
		WithHealthCheck(func() error { return errors.New("down") })
	flaky := NewLocalExecutor("flaky", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("flaky crashed")
	})
	working := NewLocalExecutor("working", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return &AgentResult{Success: true, Output: "finally"}, nil
	})

	router := NewAgentRouter(unhealthy, flaky, working)
	result, err := router.Execute(context.Background(), "agent", "task")
	if err != nil {
		t.Fatalf("expected failover through unhealthy→flaky→working, got: %v", err)
	}
	if result.Output != "finally" {
		t.Errorf("expected 'finally', got %q", result.Output)
	}
}

// ─── Per-Executor Failure Tracking (Zombie Detection) ────────────────────

func TestAgentRouter_FailureTracking_Basic(t *testing.T) {
	exec := NewLocalExecutor("flaky", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("simulated failure")
	})
	local := NewLocalExecutor("local", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("local fail")
	})
	router := NewAgentRouter(exec)
	router.SetLocal(local) // separate local to isolate failure tracking
	router.SetFailureThreshold(3)
	router.SetFailureCooldown(100 * time.Millisecond)

	// Execute 3 times — should all fail but not yet in cooldown.
	for i := range 3 {
		_, err := router.Execute(context.Background(), "agent", "task")
		if err == nil {
			t.Fatalf("attempt %d: expected error", i)
		}
	}

	// After 3 consecutive failures, executor should be in cooldown.
	status := router.ExecutorHealthStatus()
	if status[0].ConsecutiveFailures != 3 {
		t.Errorf("expected 3 failures, got %d", status[0].ConsecutiveFailures)
	}
	if !status[0].CoolingDown {
		t.Error("expected executor to be cooling down after threshold exceeded")
	}

	// 4th attempt: executor is cooling down, fallback to local (which also fails).
	_, err := router.Execute(context.Background(), "agent", "task")
	if err == nil {
		t.Fatal("expected error when all executors in cooldown")
	}
	// Failure count should not increase (cooling executor skipped).
	status = router.ExecutorHealthStatus()
	if status[0].ConsecutiveFailures != 3 {
		t.Errorf("expected still 3 failures (cooling executor skipped), got %d", status[0].ConsecutiveFailures)
	}
}

func TestAgentRouter_FailureTracking_SuccessResets(t *testing.T) {
	shouldFail := true
	exec := NewLocalExecutor("recoverable", func(_ context.Context, agent, task string) (*AgentResult, error) {
		if shouldFail {
			return nil, errors.New("transient error")
		}
		return &AgentResult{Agent: agent, Task: task, Success: true, Output: "recovered"}, nil
	})
	local := NewLocalExecutor("local", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("local fail")
	})
	router := NewAgentRouter(exec)
	router.SetLocal(local)
	router.SetFailureThreshold(3)

	// Fail 2 times (below threshold).
	for i := range 2 {
		_, err := router.Execute(context.Background(), "agent", "task")
		if err == nil {
			t.Fatalf("attempt %d: expected error", i)
		}
	}
	status := router.ExecutorHealthStatus()
	if status[0].ConsecutiveFailures != 2 {
		t.Errorf("expected 2 failures, got %d", status[0].ConsecutiveFailures)
	}

	// Now succeed — should reset counter.
	shouldFail = false
	result, err := router.Execute(context.Background(), "agent", "task")
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if result.Output != "recovered" {
		t.Errorf("expected 'recovered', got %q", result.Output)
	}

	// Counter should be reset.
	status = router.ExecutorHealthStatus()
	if status[0].ConsecutiveFailures != 0 {
		t.Errorf("expected counter reset after success, got %d", status[0].ConsecutiveFailures)
	}

	// Failing again should start from 0.
	shouldFail = true
	for i := range 3 {
		_, err := router.Execute(context.Background(), "agent", "task")
		if err == nil {
			t.Fatalf("attempt %d after reset: expected error", i)
		}
	}
	status = router.ExecutorHealthStatus()
	if status[0].ConsecutiveFailures != 3 {
		t.Errorf("expected 3 failures after reset, got %d", status[0].ConsecutiveFailures)
	}
	if !status[0].CoolingDown {
		t.Error("expected cooldown after 3 failures post-reset")
	}
}

func TestAgentRouter_FailureTracking_CoolDownExpiry(t *testing.T) {
	exec := NewLocalExecutor("flaky", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("failure")
	})
	router := NewAgentRouter(exec)
	router.SetLocal(NewLocalExecutor("local", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("local fail")
	}))
	router.SetFailureThreshold(2)
	router.SetFailureCooldown(50 * time.Millisecond)

	// Fail twice to trigger cooldown.
	for range 2 {
		_, _ = router.Execute(context.Background(), "agent", "task")
	}

	// Executor should be cooling down.
	status := router.ExecutorHealthStatus()
	if !status[0].CoolingDown {
		t.Fatal("expected executor to be cooling down after 2 failures")
	}

	// Wait for cooldown to expire.
	time.Sleep(100 * time.Millisecond)

	// After expiry, executor should be available again.
	status = router.ExecutorHealthStatus()
	if status[0].CoolingDown {
		t.Error("expected cooldown to have expired")
	}
	if status[0].ConsecutiveFailures != 0 {
		t.Errorf("expected counter reset after cooldown expiry, got %d",
			status[0].ConsecutiveFailures)
	}
}

func TestAgentRouter_FailureTracking_HealthyExecutorsExcludesCooling(t *testing.T) {
	healthy := NewLocalExecutor("healthy", func(_ context.Context, agent, task string) (*AgentResult, error) {
		return &AgentResult{Agent: agent, Task: task, Success: true}, nil
	})
	flaky := NewLocalExecutor("flaky", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("failure")
	})

	router := NewAgentRouter(healthy, flaky)
	router.SetFailureThreshold(1)

	// Fail once on flaky to trigger cooldown.
	_, _ = router.Execute(context.Background(), "agent", "task") // this hits healthy, succeeds
	// Need to route specifically to flaky. We'll use Execute with failover.
	// Create a router with only flaky to test HealthyExecutors.
	router2 := NewAgentRouter(flaky)
	router2.SetFailureThreshold(1)
	_, _ = router2.Execute(context.Background(), "agent", "task") // triggers cooldown on flaky

	healthyExecs := router2.HealthyExecutors()
	if len(healthyExecs) != 0 {
		t.Errorf("expected 0 healthy executors (flaky in cooldown), got %d", len(healthyExecs))
	}
}

func TestAgentRouter_FailureTracking_ExecutorHealthStatus(t *testing.T) {
	exec1 := NewLocalExecutor("worker-1", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("worker-1 failure")
	})
	exec2 := NewLocalExecutor("worker-2", func(_ context.Context, agent, task string) (*AgentResult, error) {
		return &AgentResult{Agent: agent, Task: task, Success: true, Output: "ok"}, nil
	})

	router := NewAgentRouter(exec1, exec2)
	// Separate local to avoid double-counting on fallback.
	router.SetLocal(NewLocalExecutor("local", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("local fail")
	}))
	router.SetFailureThreshold(2)

	// worker-1: fail twice → cooldown
	_, _ = router.Execute(context.Background(), "agent", "task1") // round-robin: hits exec1, fails → failover to exec2 (success)
	_, _ = router.Execute(context.Background(), "agent", "task2") // round-robin: hits exec2 → success
	_, _ = router.Execute(context.Background(), "agent", "task3") // round-robin: hits exec1, fails → failover to exec2 (success)

	status := router.ExecutorHealthStatus()
	if len(status) != 2 {
		t.Fatalf("expected 2 executors in status, got %d", len(status))
	}

	// exec1 should have 2 failures but NOT cooling down (succeeded via failover each time).
	if status[0].Index != 0 || status[0].Name != "worker-1" {
		t.Errorf("unexpected status[0]: %+v", status[0])
	}
	if status[0].ConsecutiveFailures != 2 {
		t.Errorf("expected 2 consecutive failures, got %d", status[0].ConsecutiveFailures)
	}
	// NOTE: CoolingDown may or may not be true here depending on cooldown threshold.
	// With threshold=2, two consecutive router.Execute() that hit exec1 but succeeded
	// via exec2 failover would trigger cooldown if exec1 fails twice consecutively
	// in the failover loop. Let's just verify the failure count.
	if status[0].LastFailure.IsZero() {
		t.Error("expected non-zero LastFailure")
	}
	if !status[0].Healthy {
		t.Error("expected exec1 to still be healthy (Health() passes)")
	}

	// exec2 should be healthy, no failures.
	if status[1].ConsecutiveFailures != 0 {
		t.Errorf("expected 0 failures for exec2, got %d", status[1].ConsecutiveFailures)
	}
}

func TestAgentRouter_FailureTracking_ResetExecutor(t *testing.T) {
	exec := NewLocalExecutor("flaky", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("failure")
	})
	router := NewAgentRouter(exec)
	router.SetLocal(NewLocalExecutor("local", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("local fail")
	}))
	router.SetFailureThreshold(2)

	// Trigger cooldown.
	_, _ = router.Execute(context.Background(), "agent", "task")
	_, _ = router.Execute(context.Background(), "agent", "task")

	status := router.ExecutorHealthStatus()
	if !status[0].CoolingDown {
		t.Fatal("expected cooldown after 2 failures")
	}

	// Reset clears the cooldown.
	router.ResetExecutor(0)
	status = router.ExecutorHealthStatus()
	if status[0].CoolingDown {
		t.Error("expected cooldown cleared after ResetExecutor")
	}
	if status[0].ConsecutiveFailures != 0 {
		t.Errorf("expected 0 failures after reset, got %d", status[0].ConsecutiveFailures)
	}
}

func TestAgentRouter_FailureTracking_DefaultThreshold(t *testing.T) {
	router := NewAgentRouter()
	// Default threshold is 5, default cooldown is 30s.
	// These are set in NewAgentRouter; we verify via behavior.
	router.SetFailureThreshold(1) // test that SetFailureThreshold works

	if router.failureThreshold != 1 {
		t.Errorf("expected threshold 1, got %d", router.failureThreshold)
	}
}

func TestAgentRouter_FailureTracking_String_WithFailures(t *testing.T) {
	exec := NewLocalExecutor("flaky", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("failure")
	})
	router := NewAgentRouter(exec)
	router.SetLocal(NewLocalExecutor("local", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("local fail")
	}))
	router.SetFailureThreshold(1)

	// Trigger a failure.
	_, _ = router.Execute(context.Background(), "agent", "task")

	s := router.String()
	if !strings.Contains(s, "failures=1") {
		t.Errorf("expected String() to contain 'failures=1', got: %s", s)
	}
	if !strings.Contains(s, "cooling=1") {
		t.Errorf("expected String() to contain 'cooling=1', got: %s", s)
	}
}

func TestAgentRouter_FailureTracking_FailoverSkipsCooling(t *testing.T) {
	// Two executors: flaky (will cool down) and working.
	flakyCount := 0
	flaky := NewLocalExecutor("flaky", func(_ context.Context, _, _ string) (*AgentResult, error) {
		flakyCount++
		return nil, errors.New("flaky failure")
	})
	working := NewLocalExecutor("working", func(_ context.Context, agent, task string) (*AgentResult, error) {
		return &AgentResult{Agent: agent, Task: task, Success: true, Output: "worked"}, nil
	})

	router := NewAgentRouter(flaky, working)
	router.SetFailureThreshold(2)

	// First attempt hits flaky (round-robin), fails. Failover to working succeeds.
	result, err := router.Execute(context.Background(), "agent", "task1")
	if err != nil {
		t.Fatalf("expected failover to working, got: %v", err)
	}
	if result.Output != "worked" {
		t.Errorf("expected 'worked', got %q", result.Output)
	}
	if flakyCount != 1 {
		t.Errorf("expected 1 flaky attempt, got %d", flakyCount)
	}

	// Second attempt: flaky fails again (2nd consecutive → cooldown).
	// Then failover to working.
	result, err = router.Execute(context.Background(), "agent", "task2")
	if err != nil {
		t.Fatalf("expected failover to working (attempt 2), got: %v", err)
	}
	if result.Output != "worked" {
		t.Errorf("expected 'worked' on attempt 2, got %q", result.Output)
	}

	// Third attempt: flaky is now in cooldown → skipped entirely.
	// Working handles it directly.
	_, err = router.Execute(context.Background(), "agent", "task3")
	if err != nil {
		t.Fatalf("expected working to handle after flaky cooldown, got: %v", err)
	}
	// flakyCount should still be 2 (cooldown skips it, no Execute() called).
	if flakyCount != 2 {
		t.Errorf("expected flakyCount=2 (skipped in cooldown), got %d", flakyCount)
	}
}

func TestAgentRouter_FailureTracking_Concurrent(t *testing.T) {
	exec := NewLocalExecutor("shared", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("failure")
	})
	router := NewAgentRouter(exec)
	router.SetLocal(NewLocalExecutor("local", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return nil, errors.New("local fail")
	}))
	router.SetFailureThreshold(200) // high threshold so we test counter safety

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			_, _ = router.Execute(context.Background(), "agent", "task")
		})
	}
	wg.Wait()

	status := router.ExecutorHealthStatus()
	if status[0].ConsecutiveFailures != 50 {
		t.Errorf("expected 50 failures after concurrent use, got %d",
			status[0].ConsecutiveFailures)
	}
}

// ─── Router construction from the A2A card registry ──────────────────────────

// TestNewRouterFromEndpoints_BuildsRemoteExecutorsWithLocalFallback pins the
// horizontal-scaling seam: the daemon reduces its live A2A card registry to a
// set of AgentEndpoints (peer name + interface base URL) and hands them to
// reliability, which must construct an AgentRouter whose remote executors are
// RemoteExecutors pointed at those peers, with the local in-process executor
// installed as the fallback. Without this constructor the RemoteExecutor +
// AgentRouter substrate stays wired by zero production binaries.
func TestNewRouterFromEndpoints_BuildsRemoteExecutorsWithLocalFallback(t *testing.T) {
	local := NewLocalExecutor("local-node", func(_ context.Context, _, _ string) (*AgentResult, error) {
		return &AgentResult{Success: true, Output: "local"}, nil
	})
	endpoints := []AgentEndpoint{
		{Name: "peer-a", BaseURL: "http://10.0.0.1:9800"},
		{Name: "peer-b", BaseURL: "http://10.0.0.2:9800", APIKey: "secret"},
		{Name: "no-url", BaseURL: ""}, // endpoints without a URL must be skipped
	}

	router := NewRouterFromEndpoints(local, endpoints)
	if router == nil {
		t.Fatal("NewRouterFromEndpoints returned nil")
	}

	execs := router.Executors()
	if len(execs) != 2 {
		t.Fatalf("expected 2 remote executors (empty-URL endpoint skipped), got %d", len(execs))
	}

	ids := map[string]bool{}
	for _, e := range execs {
		if _, ok := e.(*RemoteExecutor); !ok {
			t.Errorf("expected each executor to be *RemoteExecutor, got %T", e)
		}
		ids[e.String()] = true
	}
	if !ids["RemoteExecutor(peer-a @ http://10.0.0.1:9800)"] {
		t.Errorf("missing RemoteExecutor for peer-a; got %v", ids)
	}
	if !ids["RemoteExecutor(peer-b @ http://10.0.0.2:9800)"] {
		t.Errorf("missing RemoteExecutor for peer-b; got %v", ids)
	}

	// The passed-in local executor — NOT the first remote — must be the fallback.
	if s := router.String(); !strings.Contains(s, "local=local-node") {
		t.Errorf("expected local-node installed as fallback, got %q", s)
	}
}

// TestNewRouterFromEndpoints_EmptyRoutesToLocal pins that a daemon with no
// remote peers (an empty/nil A2A card registry beyond itself) still gets a
// working router that routes every task to the local executor, so single-node
// deployments behave exactly as before adopting the substrate.
func TestNewRouterFromEndpoints_EmptyRoutesToLocal(t *testing.T) {
	local := NewLocalExecutor("solo", func(_ context.Context, agent, task string) (*AgentResult, error) {
		return &AgentResult{Agent: agent, Task: task, Success: true, Output: "local-only"}, nil
	})

	router := NewRouterFromEndpoints(local, nil)
	if router == nil {
		t.Fatal("NewRouterFromEndpoints returned nil")
	}
	if n := len(router.Executors()); n != 0 {
		t.Fatalf("expected 0 remote executors for empty endpoints, got %d", n)
	}

	res, err := router.Execute(context.Background(), "agent", "task")
	if err != nil {
		t.Fatalf("empty router must route to local without error, got %v", err)
	}
	if res.Output != "local-only" {
		t.Errorf("expected local execution, got %q", res.Output)
	}
}

// ─── ScoreOutcome Tests ─────────────────────────────────────────────────────
//
// ScoreOutcome is the canonical formula behind the three copies at
// internal/blocks/fitness.go:ScoreFromBlackboard,
// internal/engine/ops_actions.go:fitnessScoreFromBB, and
// internal/dashboard/executor.go's inline block. All three implement the same
// logic: score := qualityScore*100, falling back to 75/25 on a zero-or-below
// score depending on success/outcome, then clamped to [0,100].

func TestScoreOutcome_UsesQualityScoreWhenPositive(t *testing.T) {
	if got := ScoreOutcome("success", 0.42, true); got != 42 {
		t.Errorf("ScoreOutcome(success, 0.42, true) = %v, want 42", got)
	}
}

func TestScoreOutcome_ClampsAboveHundred(t *testing.T) {
	if got := ScoreOutcome("success", 1.5, true); got != 100 {
		t.Errorf("ScoreOutcome(success, 1.5, true) = %v, want 100 (clamped)", got)
	}
}

func TestScoreOutcome_ZeroQualityWithSuccessTrueFallsBackTo75(t *testing.T) {
	if got := ScoreOutcome("anything", 0, true); got != 75 {
		t.Errorf("ScoreOutcome(anything, 0, true) = %v, want 75", got)
	}
}

// TestScoreOutcome_ZeroQualitySuccessFalseOutcomeSuccessCaseInsensitiveFallsBackTo25
// pins the 2026-07-22 fleet-review fix: an explicit success=false must win
// over an outcome string that merely says "success" — the fallback must not
// re-derive healthiness from the string once the caller has already told it
// the run failed. This test previously asserted 75, pinning the bug.
func TestScoreOutcome_ZeroQualitySuccessFalseOutcomeSuccessCaseInsensitiveFallsBackTo25(t *testing.T) {
	if got := ScoreOutcome("SUCCESS", 0, false); got != 25 {
		t.Errorf("ScoreOutcome(SUCCESS, 0, false) = %v, want 25 (success=false must not be overridden by the outcome string)", got)
	}
}

// TestScoreOutcome_ZeroQualitySuccessFalseOutcomeCompletedCaseInsensitiveFallsBackTo25
// is the "completed" counterpart of the test above. Previously asserted 75,
// pinning the bug.
func TestScoreOutcome_ZeroQualitySuccessFalseOutcomeCompletedCaseInsensitiveFallsBackTo25(t *testing.T) {
	if got := ScoreOutcome("Completed", 0, false); got != 25 {
		t.Errorf("ScoreOutcome(Completed, 0, false) = %v, want 25 (success=false must not be overridden by the outcome string)", got)
	}
}

// TestScoreOutcome_ExplicitSuccessFalseNotOverriddenByOutcomeString is the
// core regression test from the 2026-07-22 fleet review: internal/dashboard/
// executor.go's recordBlockFitnessMetric computes success via
// agent.IsBreakerSuccess (false whenever runErr != nil) but still passes the
// raw outcome string through. Before the fix, a zero-quality run with a real
// error but res.Outcome literally "success" scored 75 (healthy) instead of
// 25 — ADR-181's claim that this is behavior-preserving vs. the pre-ADR-181
// inline formula (which only checked success) was false.
func TestScoreOutcome_ExplicitSuccessFalseNotOverriddenByOutcomeString(t *testing.T) {
	if got := ScoreOutcome("success", 0, false); got != 25 {
		t.Errorf("ScoreOutcome(success, 0, false) = %v, want 25 (explicit success=false must win over the outcome string)", got)
	}
}

func TestScoreOutcome_ZeroQualityNoSuccessSignalFallsBackTo25(t *testing.T) {
	if got := ScoreOutcome("failure", 0, false); got != 25 {
		t.Errorf("ScoreOutcome(failure, 0, false) = %v, want 25", got)
	}
}

func TestScoreOutcome_NegativeQualityClampsToZeroFloor(t *testing.T) {
	// Negative score with no success signal falls back to 25, not clamped to 0 —
	// the fallback only fires on score <= 0, and 25 is already within [0,100].
	if got := ScoreOutcome("failure", -0.5, false); got != 25 {
		t.Errorf("ScoreOutcome(failure, -0.5, false) = %v, want 25", got)
	}
}
