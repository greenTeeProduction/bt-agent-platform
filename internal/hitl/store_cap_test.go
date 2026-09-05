package hitl

import (
	"fmt"
	"testing"
	"time"
)

// The HITL store retains every request forever — 1,514 requests / 9.4 MB on
// the production box by 2026-07-09, 99% of them auto-skipped. save() must cap
// persisted TERMINAL records (skipped/expired/approved/rejected) at
// hitlMaxStoredTerminal, keeping the newest by UpdatedAt, while never dropping
// non-terminal (pending) requests.
func TestStoreSaveCompactsTerminalRecords(t *testing.T) {
	prevDefault := DefaultStore
	t.Cleanup(func() { DefaultStore = prevDefault })

	dir := t.TempDir()
	s, err := InitStore(dir)
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	base := time.Now().Add(-24 * time.Hour)
	s.mu.Lock()
	for i := range hitlMaxStoredTerminal + 150 {
		id := fmt.Sprintf("hitl-skip-%05d", i)
		s.records[id] = &Request{
			ID:        id,
			Status:    StatusSkipped,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
			UpdatedAt: base.Add(time.Duration(i) * time.Second),
		}
	}
	// Pending requests are OLDER than every terminal record and must still
	// survive compaction.
	for i := range 3 {
		id := fmt.Sprintf("hitl-pending-%d", i)
		s.records[id] = &Request{
			ID:        id,
			Status:    StatusPending,
			CreatedAt: base.Add(-time.Hour),
			UpdatedAt: base.Add(-time.Hour),
			ExpiresAt: time.Now().Add(time.Hour),
		}
	}
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatalf("save: %v", err)
	}
	s.mu.Unlock()

	reloaded, err := InitStore(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	pending, terminal := 0, 0
	newestKept, oldestDropped := false, false
	for id, r := range reloaded.records {
		switch r.Status {
		case StatusPending:
			pending++
		default:
			terminal++
		}
		if id == fmt.Sprintf("hitl-skip-%05d", hitlMaxStoredTerminal+149) {
			newestKept = true
		}
		if id == "hitl-skip-00000" {
			oldestDropped = true
		}
	}
	if pending != 3 {
		t.Fatalf("pending after compaction = %d, want 3 (never dropped)", pending)
	}
	if terminal > hitlMaxStoredTerminal {
		t.Fatalf("terminal records after compaction = %d, want <= %d", terminal, hitlMaxStoredTerminal)
	}
	if !newestKept {
		t.Fatal("compaction must keep the NEWEST terminal records")
	}
	if oldestDropped {
		t.Fatal("compaction must drop the OLDEST terminal records first")
	}
}
