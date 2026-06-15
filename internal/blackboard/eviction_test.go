package blackboard

import (
	"strings"
	"testing"
	"time"
)

// With Evict enabled, exceeding MaxEntries drops the oldest key instead of
// failing the write — keeping the scope usable across long multi-subtask runs.
func TestSet_EvictsOldestOnEntryLimit(t *testing.T) {
	m := NewManager(map[ScopeKind]Limits{
		ScopeRun: {MaxEntries: 2, MaxTotalBytes: 1 << 20, Evict: true},
	})
	scope := Scope{Kind: ScopeRun, ID: "run"}

	for _, k := range []string{"a", "b", "c"} {
		if err := m.Set(scope, k, "v", "", "text"); err != nil {
			t.Fatalf("set %s: %v", k, err)
		}
		time.Sleep(time.Millisecond) // ensure distinct UpdatedAt ordering
	}

	if _, err := m.Get(scope, "a"); err == nil {
		t.Fatal("expected oldest key 'a' to be evicted")
	}
	for _, k := range []string{"b", "c"} {
		if _, err := m.Get(scope, k); err != nil {
			t.Fatalf("expected %s to survive: %v", k, err)
		}
	}
	list, _ := m.List(scope, "", 10)
	if len(list) != 2 {
		t.Fatalf("expected 2 entries after eviction, got %d", len(list))
	}
}

// Updating an existing key never triggers eviction (it reuses its own slot).
func TestSet_UpdateDoesNotEvictAtEntryLimit(t *testing.T) {
	m := NewManager(map[ScopeKind]Limits{
		ScopeRun: {MaxEntries: 2, MaxTotalBytes: 1 << 20, Evict: true},
	})
	scope := Scope{Kind: ScopeRun, ID: "run"}
	_ = m.Set(scope, "a", "1", "", "text")
	_ = m.Set(scope, "b", "1", "", "text")

	if err := m.Set(scope, "a", "updated", "", "text"); err != nil {
		t.Fatalf("update at cap should succeed: %v", err)
	}
	e, err := m.Get(scope, "a")
	if err != nil || e.Value != "updated" {
		t.Fatalf("get a: %+v err=%v", e, err)
	}
	if _, err := m.Get(scope, "b"); err != nil {
		t.Fatal("b should not have been evicted by an in-place update")
	}
}

// Byte-limit pressure also evicts oldest until the incoming value fits.
func TestSet_EvictsOnByteLimit(t *testing.T) {
	m := NewManager(map[ScopeKind]Limits{
		ScopeRun: {MaxEntries: 100, MaxTotalBytes: 20, Evict: true},
	})
	scope := Scope{Kind: ScopeRun, ID: "run"}
	if err := m.Set(scope, "a", strings.Repeat("x", 10), "", "text"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if err := m.Set(scope, "b", strings.Repeat("y", 15), "", "text"); err != nil {
		t.Fatalf("byte eviction should make room: %v", err)
	}
	if _, err := m.Get(scope, "a"); err == nil {
		t.Fatal("expected 'a' evicted under byte pressure")
	}
	if _, err := m.Get(scope, "b"); err != nil {
		t.Fatalf("expected 'b' to fit after eviction: %v", err)
	}
}

// A single value larger than the whole byte budget still fails (nothing to evict).
func TestSet_ByteLimitTooLargeStillErrors(t *testing.T) {
	m := NewManager(map[ScopeKind]Limits{
		ScopeRun: {MaxEntries: 100, MaxTotalBytes: 10, Evict: true},
	})
	scope := Scope{Kind: ScopeRun, ID: "run"}
	if err := m.Set(scope, "big", strings.Repeat("x", 50), "", "text"); err == nil {
		t.Fatal("expected error for value exceeding total byte budget")
	}
}

// Without Evict the original strict contract is preserved.
func TestSet_NoEvictKeepsStrictLimit(t *testing.T) {
	m := NewManager(map[ScopeKind]Limits{
		ScopeRun: {MaxEntries: 1, MaxTotalBytes: 100},
	})
	scope := Scope{Kind: ScopeRun, ID: "run"}
	if err := m.Set(scope, "a", "x", "", "text"); err != nil {
		t.Fatal(err)
	}
	if err := m.Set(scope, "b", "y", "", "text"); err == nil {
		t.Fatal("expected strict entry-limit error when Evict is false")
	}
}
