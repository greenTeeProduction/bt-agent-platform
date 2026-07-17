package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAgentCircuitBreakerStore_LoadHalfOpenBecomesOpen pins the un-wedge rule:
// a persisted "half_open" state has no in-flight probe in the loading process
// and no time-based escape (Allow() in HalfOpen always returns false), so
// restoring it verbatim permanently wedges the breaker. Load must map it to
// Open with a fresh cooldown clock so Allow() can re-issue a probe.
func TestAgentCircuitBreakerStore_LoadHalfOpenBecomesOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "circuit_breakers.json")
	blob := `{"breakers":{"wedged-agent":{"status":"half_open","failures":3,"last_failure":"2026-07-17T10:00:00Z"}}}`
	if err := os.WriteFile(path, []byte(blob), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{Threshold: 3, Cooldown: 10 * time.Millisecond})
	if err := store.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	cb := store.Get("wedged-agent")
	if cb.State() != CircuitOpen {
		t.Fatalf("state after Load = %v, want %v (open, so the cooldown clock can escape)", cb.State(), CircuitOpen)
	}
	time.Sleep(20 * time.Millisecond)
	if !cb.Allow() {
		t.Fatal("Allow() must grant a probe once the restarted cooldown elapses — a restored half_open state has no escape at all")
	}
}

// TestAgentCircuitBreakerStore_SavePreservesForeignWinnerKeys pins the merge
// contract with internal/a2a's winnerBreakerStore, which shares the same
// on-disk file: winner-breaker entries written after this store loaded must
// survive a Save, and Load must not absorb (and later re-write stale copies
// of) keys the a2a store owns.
func TestAgentCircuitBreakerStore_SavePreservesForeignWinnerKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "circuit_breakers.json")

	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{Threshold: 2, Cooldown: time.Minute})
	if err := store.Load(path); err != nil { // missing file: first boot
		t.Fatalf("Load: %v", err)
	}

	// a2a writes a winner breaker AFTER the store loaded (the normal case of a
	// winner breaker opening mid-process).
	winnerKey := A2AWinnerBreakerKeyPrefix + "peer-agent"
	blob := `{"breakers":{"` + winnerKey + `":{"status":"open","failures":5,"last_failure":"2026-07-17T12:00:00Z"}}}`
	if err := os.WriteFile(path, []byte(blob), 0644); err != nil {
		t.Fatal(err)
	}

	store.RecordFailure("own-agent")
	if err := store.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Breakers map[string]struct {
			Status   string `json:"status"`
			Failures int    `json:"failures"`
		} `json:"breakers"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("parse saved file: %v", err)
	}
	if got, ok := file.Breakers[winnerKey]; !ok || got.Status != "open" || got.Failures != 5 {
		t.Fatalf("winner key %q = %+v (present=%v), want status=open failures=5 preserved — Save must merge, not rewrite the file from its own agents only", winnerKey, got, ok)
	}
	if _, ok := file.Breakers["own-agent"]; !ok {
		t.Fatal("own agent entry missing from saved file")
	}
}

// TestAgentCircuitBreakerStore_LoadSkipsWinnerKeys: keys the a2a
// winnerBreakerStore owns must not be absorbed into this store — an absorbed
// copy is frozen at boot-time values and would be re-written stale on every
// Save.
func TestAgentCircuitBreakerStore_LoadSkipsWinnerKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "circuit_breakers.json")
	winnerKey := A2AWinnerBreakerKeyPrefix + "peer-agent"
	blob := `{"breakers":{"` + winnerKey + `":{"status":"open","failures":5},"regular-agent":{"status":"closed","failures":0}}}`
	if err := os.WriteFile(path, []byte(blob), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{Threshold: 2, Cooldown: time.Minute})
	if err := store.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	store.mu.Lock()
	_, absorbed := store.agents[winnerKey]
	_, regular := store.agents["regular-agent"]
	store.mu.Unlock()
	if absorbed {
		t.Fatalf("Load absorbed a2a-owned key %q; it must be skipped so Save cannot re-write a stale copy", winnerKey)
	}
	if !regular {
		t.Fatal("Load must still restore regular agent keys")
	}
}

// TestRunJob_MissingAgentDoesNotLeakBreakerProbe reproduces the tick-time
// probe leak: tick() consumes the half-open probe via cbStore.Allowed(), then
// runJob's registry-lookup early return recorded no outcome, leaving the
// breaker wedged HalfOpen (no time-based escape) so the scheduler skipped the
// agent forever. runJob must resolve the probe even when the registry lookup
// fails.
func TestRunJob_MissingAgentDoesNotLeakBreakerProbe(t *testing.T) {
	reg, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cbStore := NewAgentCircuitBreakerStore(CircuitBreakerOptions{Threshold: 3, Cooldown: 10 * time.Millisecond})
	s := NewScheduler(SchedulerConfig{Registry: reg, CBStore: cbStore, TickInterval: time.Hour})

	const name = "vanished-agent"
	// Prime: breaker open, cooldown elapsed, probe consumed by the tick-time
	// Allowed() gate — exactly the state runJob is entered with.
	cb := cbStore.Get(name)
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure() // threshold 3 -> Open
	time.Sleep(20 * time.Millisecond)
	if !cbStore.Allowed(name) {
		t.Fatal("expected the elapsed cooldown to grant a half-open probe")
	}

	// The agent is not in the registry: runJob's lookup fails.
	s.runJob(&ScheduledJob{ID: "j1", AgentName: name, Timeout: "1s"}, nil)

	if cb.State() == CircuitHalfOpen {
		t.Fatal("breaker left HalfOpen after runJob's registry-lookup early return — the consumed probe leaked and the agent is skipped forever")
	}
	// The recorded failure must restart the cooldown clock so a later probe
	// can still be granted.
	time.Sleep(20 * time.Millisecond)
	if !cb.Allow() {
		t.Fatalf("breaker state %v must grant a probe after cooldown; the missing-agent path has no other escape", cb.State())
	}
}
