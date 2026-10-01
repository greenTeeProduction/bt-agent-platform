package research

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestTraceTransactionsPreserveConcurrentSources(t *testing.T) {
	path := TracePath(filepath.Join(t.TempDir(), "knowledge.json"), "owner")
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := range 12 {
		wg.Go(func() {
			errs <- UpdateTraces(context.Background(), path, "owner", func(s *TraceStore) error {
				s.Observe(Key(fmt.Sprint(i)), fmt.Sprint(i), "research", fmt.Sprint(i))
				return nil
			})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	s, err := OpenTraces(path, "owner")
	if err != nil || len(s.Goals) != 12 {
		t.Fatalf("lost sources: %v %v", s, err)
	}
	if _, err := OpenTraces(path, "other"); err == nil {
		t.Fatal("wrong owner accepted")
	}
	if TracePath("knowledge", "a/b") == TracePath("knowledge", "a_b") {
		t.Fatal("owner path collision")
	}
}

func TestTraceLockRespectsDeadlineAndPreservesCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.json")
	release, err := reliability.AcquireFileLockWithContext(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	err = UpdateTraces(ctx, path, "", func(*TraceStore) error { t.Error("ran under foreign lock"); return nil })
	release()
	if err == nil {
		t.Fatal("ignored lock deadline")
	}
	if err := os.WriteFile(path, []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := UpdateTraces(context.Background(), path, "", func(*TraceStore) error { return nil }); err == nil {
		t.Fatal("clobbered corrupt ledger")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "{corrupt" {
		t.Fatalf("corruption evidence lost: %s %v", data, err)
	}
}

func TestResearchStoresRejectStaleWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knowledge.json")
	a, _ := Open(path)
	b, _ := Open(path)
	a.Record("research", "a", "a")
	b.Record("research", "b", "b")
	if err := a.Save(); err != nil {
		t.Fatal(err)
	}
	if err := b.Save(); err == nil {
		t.Fatal("stale knowledge writer accepted")
	}
	s, err := Open(path)
	if err != nil || !s.Known("a") || s.Known("b") {
		t.Fatalf("knowledge overwritten: %v %v", s, err)
	}
	a.Record("research", "c", "c")
	if err := a.Save(); err != nil {
		t.Fatalf("same writer should advance snapshot: %v", err)
	}
	path = filepath.Join(t.TempDir(), "attempts.json")
	x, _ := OpenGoalAttempts(path)
	y, _ := OpenGoalAttempts(path)
	x.RecordFailure("x", "failure")
	y.RecordRedPass("y")
	if err := x.Save(); err != nil {
		t.Fatal(err)
	}
	if err := y.Save(); err == nil {
		t.Fatal("stale budget writer accepted")
	}
	bgt, err := OpenGoalAttempts(path)
	if err != nil || bgt.Count("x") != 1 {
		t.Fatalf("budget overwritten: %v %v", bgt, err)
	}
}

func TestDefaultKnowledgePathUsesConfiguredHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("BT_AGENT_HOME", root)
	if got := DefaultPath(); got != filepath.Join(root, "research", "knowledge.json") {
		t.Fatal(got)
	}
}
