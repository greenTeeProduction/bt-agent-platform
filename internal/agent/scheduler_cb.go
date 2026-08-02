// This file extends the Scheduler with per-agent circuit breakers, built on
// internal/reliability.CircuitBreaker — the platform's canonical 3-state
// circuit breaker. After repeated consecutive failures, the circuit breaker
// opens and prevents the agent from running again until the cooldown
// expires. This prevents the scheduler from hammering a persistently broken
// agent every tick cycle.
//
// The types below are aliases onto internal/reliability so the state machine
// is implemented in exactly one place; this file retains only what is
// specific to the scheduler/dashboard: the named-agent registry, the
// dashboard-shaped JSON persistence (Save/Load), and the A2A winner-breaker
// key coexistence rules for the shared on-disk file.

package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

// CircuitState is an alias for reliability.CircuitState.
type CircuitState = reliability.CircuitState

// Circuit state constants, aliased from reliability so existing callers
// (internal/a2a, internal/dashboard, cmd/bt-agent, cmd/bt-dashboard) keep
// compiling unchanged.
const (
	CircuitClosed   = reliability.CircuitClosed
	CircuitOpen     = reliability.CircuitOpen
	CircuitHalfOpen = reliability.CircuitHalfOpen
)

// AgentCircuitBreaker is an alias for the platform's canonical circuit
// breaker, internal/reliability.CircuitBreaker.
type AgentCircuitBreaker = reliability.CircuitBreaker

// NewAgentCircuitBreaker creates a circuit breaker for an agent, applying
// the scheduler's historical defaults (3 consecutive failures / 5 minute
// cooldown) when threshold or cooldown are not positive.
func NewAgentCircuitBreaker(name string, threshold int, cooldown time.Duration) *AgentCircuitBreaker {
	if threshold <= 0 {
		threshold = 3 // default: open after 3 consecutive failures
	}
	if cooldown <= 0 {
		cooldown = 5 * time.Minute // default: 5 minute cooldown
	}
	return reliability.NewCircuitBreaker(name, threshold, cooldown)
}

// CircuitBreakerOptions is an alias for reliability.CircuitBreakerOptions.
type CircuitBreakerOptions = reliability.CircuitBreakerOptions

// DefaultCircuitBreakerOptions returns sensible defaults.
func DefaultCircuitBreakerOptions() CircuitBreakerOptions {
	return reliability.DefaultCircuitBreakerOptions()
}

// CircuitSummary is an alias for reliability.CircuitSummary.
type CircuitSummary = reliability.CircuitSummary

// AgentCircuitBreakerStore manages per-agent circuit breakers for the
// scheduler. It is a thin named registry over reliability.CircuitBreaker;
// what it adds beyond the state machine itself is the dashboard-shaped JSON
// persistence (Save/Load) and the A2A winner-breaker key coexistence rules
// for the file the two owners share.
//
// This type is the ONLY implementation of circuit_breakers.json persistence in
// the platform. internal/a2a's winnerBreakerStore used to hand-roll a second,
// subtly different load/save against the same path; it now delegates here via
// NewWinnerCircuitBreakerStore, so the merge rules, the half-open un-wedging,
// the timestamp restore and the cross-process lock exist once.
type AgentCircuitBreakerStore struct {
	mu      sync.RWMutex
	agents  map[string]*AgentCircuitBreaker
	options CircuitBreakerOptions

	// keyPrefix is the key space in the shared file this store owns. Empty —
	// the scheduler/dashboard default — means "every key that is not somebody
	// else's", i.e. everything except A2AWinnerBreakerKeyPrefix. A non-empty
	// prefix means the store owns exactly the keys carrying it. Either way
	// Load restores only owned keys and Save merges rather than rewrites, so
	// the two owners never absorb or clobber each other's entries.
	keyPrefix string
}

// NewAgentCircuitBreakerStore creates a new circuit breaker store owning the
// plain agent-name key space of CircuitBreakersFile().
func NewAgentCircuitBreakerStore(opts CircuitBreakerOptions) *AgentCircuitBreakerStore {
	return newCircuitBreakerStore("", opts)
}

// NewWinnerCircuitBreakerStore creates a store owning the
// A2AWinnerBreakerKeyPrefix key space of CircuitBreakersFile() — the auction
// winner breakers internal/a2a tracks. Callers pass already-prefixed keys to
// Get/RecordFailure/…; this store does not rewrite names, it only decides
// which of the file's entries are its own to restore on Load.
func NewWinnerCircuitBreakerStore(opts CircuitBreakerOptions) *AgentCircuitBreakerStore {
	return newCircuitBreakerStore(A2AWinnerBreakerKeyPrefix, opts)
}

func newCircuitBreakerStore(keyPrefix string, opts CircuitBreakerOptions) *AgentCircuitBreakerStore {
	if opts.Threshold <= 0 {
		opts.Threshold = 3
	}
	if opts.Cooldown <= 0 {
		opts.Cooldown = 5 * time.Minute
	}
	return &AgentCircuitBreakerStore{
		agents:    make(map[string]*AgentCircuitBreaker),
		options:   opts,
		keyPrefix: keyPrefix,
	}
}

// ownsKey reports whether an on-disk entry belongs to this store's key space.
// Keys another owner writes are left strictly alone on Load: absorbing them
// would freeze a boot-time copy here that Save then re-writes stale over the
// real owner's live updates.
func (s *AgentCircuitBreakerStore) ownsKey(name string) bool {
	if s.keyPrefix != "" {
		return strings.HasPrefix(name, s.keyPrefix)
	}
	return !strings.HasPrefix(name, A2AWinnerBreakerKeyPrefix)
}

// Get returns the circuit breaker for the named agent, creating it if needed.
func (s *AgentCircuitBreakerStore) Get(agentName string) *AgentCircuitBreaker {
	s.mu.Lock()
	defer s.mu.Unlock()

	cb, ok := s.agents[agentName]
	if !ok {
		cb = NewAgentCircuitBreaker(agentName, s.options.Threshold, s.options.Cooldown)
		s.agents[agentName] = cb
	}
	return cb
}

// Allowed checks whether the named agent is allowed to execute.
func (s *AgentCircuitBreakerStore) Allowed(agentName string) bool {
	return s.Get(agentName).Allow()
}

// RecordSuccess records a successful execution for the named agent.
func (s *AgentCircuitBreakerStore) RecordSuccess(agentName string) {
	s.Get(agentName).RecordSuccess()
}

// RecordFailure records a failed execution for the named agent.
func (s *AgentCircuitBreakerStore) RecordFailure(agentName string) {
	s.Get(agentName).RecordFailure()
}

// Status returns circuit state summaries for all tracked agents.
func (s *AgentCircuitBreakerStore) Status() map[string]CircuitSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]CircuitSummary, len(s.agents))
	for name, cb := range s.agents {
		result[name] = CircuitSummary{
			State:        cb.State(),
			FailureCount: cb.FailureCount(),
			SuccessCount: cb.SuccessCount(),
			Threshold:    cb.Threshold(),
			Cooldown:     cb.Cooldown(),
		}
	}
	return result
}

// ResetAll resets all circuit breakers to closed state.
func (s *AgentCircuitBreakerStore) ResetAll() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, cb := range s.agents {
		cb.Reset()
	}
}

// A2AWinnerBreakerKeyPrefix namespaces the auction-winner circuit breakers
// internal/a2a persists into the SAME on-disk file (CircuitBreakersFile())
// this store saves. A default store skips keys with this prefix on Load and
// preserves them on Save; a NewWinnerCircuitBreakerStore does the mirror
// image, so the two key spaces share one file — and one persistence
// implementation — without clobbering each other's entries.
const A2AWinnerBreakerKeyPrefix = "a2a.auction.winner."

// circuitBreakerFileEntry mirrors internal/dashboard/agents.go's
// CircuitBreakerEntry JSON shape (status, failures, last_failure) so Save/Load
// round-trip through the same file the dashboard's cb_status column reads.
type circuitBreakerFileEntry struct {
	Status      string `json:"status"`
	Failures    int    `json:"failures"`
	LastFailure string `json:"last_failure"`
}

// circuitBreakersFile is the on-disk wrapper Save writes and Load reads,
// matching internal/dashboard/agents.go's CircuitBreakers struct.
type circuitBreakersFile struct {
	Breakers map[string]circuitBreakerFileEntry `json:"breakers"`
}

// Save persists every tracked circuit breaker's state to path as JSON in the
// {"breakers": {name: {status, failures, last_failure}}} shape the dashboard's
// loadCircuitBreakers (internal/dashboard/agents.go) already decodes.
//
// Save MERGES into the existing file rather than rewriting it wholesale:
// entries this store does not own — the a2a winner breakers, or agents another
// process tracks — are preserved, so a scheduler/dashboard save cannot clobber
// a winner breaker that opened after this store loaded. The temp file is
// unique per writer (os.CreateTemp) because the daemon, the dashboard, and the
// a2a winner store all write this path concurrently — a shared fixed ".tmp"
// let interleaved writes publish a torn file.
//
// The whole read-merge-write cycle runs under the shared advisory sidecar lock
// (reliability.AcquireFileLock on `<path>.lock`, the same ADR-024 idiom
// internal/evolution and internal/research use). The atomic rename alone only
// makes each individual write indivisible; it does nothing about the window
// between this store's read and its rename, so without the lock two savers can
// both read the same old file and the second rename silently drops whatever
// the first committed in between.
func (s *AgentCircuitBreakerStore) Save(path string) error {
	// Before the lock: the sidecar is created beside path, so the directory
	// has to exist first.
	dir := filepath.Dir(path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create circuit breaker state dir: %w", err)
		}
	}
	release, err := reliability.AcquireFileLock(path)
	if err != nil {
		return fmt.Errorf("lock circuit breaker state: %w", err)
	}
	defer release()

	out := circuitBreakersFile{Breakers: make(map[string]circuitBreakerFileEntry, len(s.agents))}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &out) // best-effort merge; a corrupt file is simply overwritten
		if out.Breakers == nil {
			out.Breakers = make(map[string]circuitBreakerFileEntry, len(s.agents))
		}
	}

	s.mu.RLock()
	for name, cb := range s.agents {
		entry := circuitBreakerFileEntry{
			Status:   cb.State().String(),
			Failures: cb.FailureCount(),
		}
		if lastFailure := cb.LastFailureTime(); !lastFailure.IsZero() {
			entry.LastFailure = lastFailure.Format(time.RFC3339)
		}
		out.Breakers[name] = entry
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal circuit breaker state: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".circuit_breakers-*.tmp")
	if err != nil {
		return fmt.Errorf("create circuit breaker temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write circuit breaker state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close circuit breaker temp file: %w", err)
	}
	_ = os.Chmod(tmpName, 0644) // CreateTemp defaults to 0600; the dashboard process reads this file
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename circuit breaker state: %w", err)
	}
	return nil
}

// Load restores circuit breaker state previously written by Save. A missing
// file is not an error — it's the expected first-boot state before any
// breaker has ever tripped.
func (s *AgentCircuitBreakerStore) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read circuit breaker state: %w", err)
	}
	var in circuitBreakersFile
	if err := json.Unmarshal(data, &in); err != nil {
		return fmt.Errorf("parse circuit breaker state: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for name, entry := range in.Breakers {
		if !s.ownsKey(name) {
			continue
		}
		cb := NewAgentCircuitBreaker(name, s.options.Threshold, s.options.Cooldown)
		state := CircuitClosed
		switch entry.Status {
		case "open":
			state = CircuitOpen
		case "half_open":
			// A persisted half-open has no in-flight probe in this process and
			// no time-based escape (Allow() in HalfOpen always refuses), so
			// restoring it verbatim would wedge the breaker forever. Map it to
			// Open with a fresh cooldown clock so a probe can be re-issued.
			state = CircuitOpen
		}
		var lastFailure time.Time
		if entry.LastFailure != "" {
			if t, err := time.Parse(time.RFC3339, entry.LastFailure); err == nil {
				lastFailure = t
			}
		}
		cb.RestoreState(state, entry.Failures, lastFailure)
		s.agents[name] = cb
	}
	return nil
}

// validateAgentRun checks if an agent run is allowed by the circuit breaker.
// Returns an error if the circuit is open (the run should be skipped).
// After checking, the store is released so it doesn't block other callers.
func validateAgentRun(store *AgentCircuitBreakerStore, agentName string) error {
	if store == nil {
		return nil // circuit breakers not configured
	}
	if !store.Allowed(agentName) {
		cb := store.Get(agentName)
		return fmt.Errorf("agent %q circuit breaker is %s (%d consecutive failures, cooldown %v)",
			agentName, cb.State(), cb.FailureCount(), cb.Cooldown())
	}
	return nil
}

// reportAgentOutcome records the outcome of an agent run with the circuit breaker store.
func reportAgentOutcome(store *AgentCircuitBreakerStore, agentName string, success bool) {
	if store == nil {
		return
	}
	if success {
		store.RecordSuccess(agentName)
	} else {
		store.RecordFailure(agentName)
	}
	// Log state changes for operator visibility
	cb := store.Get(agentName)
	if cb.State() == CircuitOpen {
		slog.Warn("circuit breaker: agent is now OPEN", "agent", agentName, "consecutive_failures", cb.FailureCount())
	} else if cb.State() == CircuitClosed && cb.FailureCount() == 0 {
		slog.Info("circuit breaker: agent recovered to CLOSED", "agent", agentName)
	}
}
