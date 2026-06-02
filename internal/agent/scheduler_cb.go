// This file extends the Scheduler with per-agent circuit breakers.
// After repeated consecutive failures, the circuit breaker opens and
// prevents the agent from running again until the cooldown expires.
// This prevents the scheduler from hammering a persistently broken
// agent every tick cycle.

package agent

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// CircuitState represents the state of an agent circuit breaker.
type CircuitState int

const (
	// CircuitClosed means the agent is allowed to run normally.
	CircuitClosed CircuitState = iota
	// CircuitOpen means the agent is blocked from running until cooldown.
	CircuitOpen
	// CircuitHalfOpen means a single test request is allowed.
	CircuitHalfOpen
)

// String returns the human-readable circuit state name.
func (cs CircuitState) String() string {
	switch cs {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// AgentCircuitBreaker tracks consecutive failures for a single agent job.
// When the threshold is exceeded, the circuit opens and remains open
// for the cooldown duration before transitioning to half-open.
type AgentCircuitBreaker struct {
	mu              sync.Mutex
	name            string
	state           CircuitState
	failureCount    int
	successCount    int
	threshold       int           // consecutive failures before opening
	cooldown        time.Duration // time to stay open
	lastFailureTime time.Time
	lastStateChange time.Time
}

// NewAgentCircuitBreaker creates a circuit breaker for an agent.
func NewAgentCircuitBreaker(name string, threshold int, cooldown time.Duration) *AgentCircuitBreaker {
	if threshold <= 0 {
		threshold = 3 // default: open after 3 consecutive failures
	}
	if cooldown <= 0 {
		cooldown = 5 * time.Minute // default: 5 minute cooldown
	}
	return &AgentCircuitBreaker{
		name:      name,
		state:     CircuitClosed,
		threshold: threshold,
		cooldown:  cooldown,
	}
}

// Allow checks whether this agent is allowed to execute.
func (cb *AgentCircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if time.Since(cb.lastStateChange) >= cb.cooldown {
			cb.state = CircuitHalfOpen
			cb.lastStateChange = time.Now()
			return true // allow one test request
		}
		return false
	case CircuitHalfOpen:
		// Half-open only allows one request; this is a subsequent one.
		return false
	}
	return false
}

// RecordSuccess records a successful execution and resets the circuit.
func (cb *AgentCircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount = 0
	cb.successCount++
	switch cb.state {
	case CircuitHalfOpen:
		cb.state = CircuitClosed
		cb.lastStateChange = time.Now()
	case CircuitOpen:
		cb.state = CircuitClosed
		cb.lastStateChange = time.Now()
	}
}

// RecordFailure records a failed execution and potentially opens the circuit.
func (cb *AgentCircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureTime = time.Now()

	if cb.state == CircuitHalfOpen || (cb.state == CircuitClosed && cb.failureCount >= cb.threshold) {
		cb.state = CircuitOpen
		cb.lastStateChange = time.Now()
	}
}

// State returns the current circuit state.
func (cb *AgentCircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// FailureCount returns the current consecutive failure count.
func (cb *AgentCircuitBreaker) FailureCount() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failureCount
}

// Reset resets the circuit breaker to closed state.
func (cb *AgentCircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = CircuitClosed
	cb.failureCount = 0
	cb.lastStateChange = time.Now()
}

// AgentCircuitBreakerStore manages per-agent circuit breakers for the scheduler.
type AgentCircuitBreakerStore struct {
	mu      sync.RWMutex
	agents  map[string]*AgentCircuitBreaker
	options CircuitBreakerOptions
}

// CircuitBreakerOptions configures default circuit breaker behavior for agents.
type CircuitBreakerOptions struct {
	// Threshold is the default consecutive failure count before opening (default: 3).
	Threshold int
	// Cooldown is the default duration the circuit stays open (default: 5m).
	Cooldown time.Duration
}

// DefaultCircuitBreakerOptions returns sensible defaults.
func DefaultCircuitBreakerOptions() CircuitBreakerOptions {
	return CircuitBreakerOptions{
		Threshold: 3,
		Cooldown:  5 * time.Minute,
	}
}

// NewAgentCircuitBreakerStore creates a new circuit breaker store.
func NewAgentCircuitBreakerStore(opts CircuitBreakerOptions) *AgentCircuitBreakerStore {
	if opts.Threshold <= 0 {
		opts.Threshold = 3
	}
	if opts.Cooldown <= 0 {
		opts.Cooldown = 5 * time.Minute
	}
	return &AgentCircuitBreakerStore{
		agents:  make(map[string]*AgentCircuitBreaker),
		options: opts,
	}
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
		cb.mu.Lock()
		result[name] = CircuitSummary{
			State:        cb.state,
			FailureCount: cb.failureCount,
			SuccessCount: cb.successCount,
			Threshold:    cb.threshold,
			Cooldown:     cb.cooldown,
		}
		cb.mu.Unlock()
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

// CircuitSummary provides a snapshot of a circuit breaker's state.
type CircuitSummary struct {
	State        CircuitState  `json:"state"`
	FailureCount int           `json:"failure_count"`
	SuccessCount int           `json:"success_count"`
	Threshold    int           `json:"threshold"`
	Cooldown     time.Duration `json:"cooldown"`
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
			agentName, cb.State(), cb.FailureCount(), cb.cooldown)
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
		log.Printf("Circuit breaker: agent %q is now OPEN (%d consecutive failures)", agentName, cb.FailureCount())
	} else if cb.State() == CircuitClosed && cb.FailureCount() == 0 {
		log.Printf("Circuit breaker: agent %q recovered to CLOSED", agentName)
	}
}
