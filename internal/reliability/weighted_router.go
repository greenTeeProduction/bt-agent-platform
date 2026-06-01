// Package reliability provides weighted routing strategies for AgentRouter.
package reliability

import (
	"fmt"
	"sync/atomic"
)

// RoutingStrategy determines how AgentRouter selects executors for task dispatch.
type RoutingStrategy int

const (
	// RoutingRoundRobin distributes tasks evenly across healthy executors in order.
	// This is the default strategy and preserves backward compatibility.
	RoutingRoundRobin RoutingStrategy = iota

	// RoutingLeastConnections routes each task to the healthy executor with the
	// fewest in-flight (active) requests. This balances load when executors have
	// different processing capacities or varying response times.
	RoutingLeastConnections
)

func (s RoutingStrategy) String() string {
	switch s {
	case RoutingRoundRobin:
		return "round_robin"
	case RoutingLeastConnections:
		return "least_connections"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

// SetStrategy sets the routing strategy for this router.
// Default is RoutingRoundRobin for backward compatibility.
// When switching to RoutingLeastConnections, per-executor active connection
// counters are initialized.
func (r *AgentRouter) SetStrategy(s RoutingStrategy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.strategy = s
	if s == RoutingLeastConnections && r.activeCounts == nil {
		r.activeCounts = make([]int64, len(r.executors))
	}
}

// Strategy returns the current routing strategy.
func (r *AgentRouter) Strategy() RoutingStrategy {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.strategy
}

// ActiveCounts returns the current in-flight request count per executor.
// Only meaningful when strategy is RoutingLeastConnections.
// Returns nil if least-connections has never been activated.
func (r *AgentRouter) ActiveCounts() []int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.activeCounts == nil {
		return nil
	}
	result := make([]int64, len(r.activeCounts))
	for i := range r.activeCounts {
		result[i] = atomic.LoadInt64(&r.activeCounts[i])
	}
	return result
}

// ensureActiveCounts resizes the activeCounts slice when executors are added.
// Must be called with r.mu held.
func (r *AgentRouter) ensureActiveCounts() {
	if r.activeCounts == nil {
		return
	}
	if len(r.activeCounts) < len(r.executors) {
		// Executors were added — extend the slice.
		// Existing counts are preserved, new entries start at 0.
		extended := make([]int64, len(r.executors))
		copy(extended, r.activeCounts)
		r.activeCounts = extended
	}
}

// pickLeastConnections returns the index of the healthy executor with the
// fewest in-flight requests. Returns -1 if no executor is healthy.
// Caller must NOT hold r.mu since Health() may perform network calls.
// activeCountsSnapshot provides a snapshot of active connection counts.
// Uses heartbeat-aware health checking when available.
func (r *AgentRouter) pickLeastConnections(executors []AgentExecutor, activeCountsSnapshot []int64) int {
	bestIdx := -1
	var bestCount int64 = 1<<63 - 1 // max int64

	for i, e := range executors {
		// Heartbeat-aware health check first.
		if r.isAliveByHeartbeat(i) {
			// Heartbeat says alive — proceed.
		} else if e.Health() != nil {
			continue // skip unhealthy executors
		}
		count := int64(0)
		if i < len(activeCountsSnapshot) {
			count = activeCountsSnapshot[i]
		}
		if count < bestCount {
			bestCount = count
			bestIdx = i
		}
	}
	return bestIdx
}
