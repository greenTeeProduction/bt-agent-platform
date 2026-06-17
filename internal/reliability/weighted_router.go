// Package reliability provides weighted routing strategies for AgentRouter.
package reliability

import (
	"fmt"
	"math"
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

	// RoutingAuction routes each task to the healthy executor that submits the
	// lowest bid for it — a Contract Net Protocol style allocation. Executors
	// implementing the Bidder interface compute a task-specific cost (e.g. queue
	// depth, model affinity, estimated latency); executors that do not implement
	// Bidder fall back to a default bid derived from in-flight load and recent
	// failure history. This lets heterogeneous agents self-select work they are
	// best suited for, rather than relying on position or raw connection count.
	RoutingAuction
)

func (s RoutingStrategy) String() string {
	switch s {
	case RoutingRoundRobin:
		return "round_robin"
	case RoutingLeastConnections:
		return "least_connections"
	case RoutingAuction:
		return "auction"
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
	// Both least-connections and auction routing consult per-executor in-flight
	// counts, so initialize the counter slice for either strategy.
	if (s == RoutingLeastConnections || s == RoutingAuction) && r.activeCounts == nil {
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
		if !r.isAliveByHeartbeat(i) && e.Health() != nil {
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

// Bidder is an optional interface that AgentExecutors may implement to take
// part in auction-based task allocation (RoutingAuction). Bid returns the
// executor's cost estimate for handling (agent, task) — lower is better. A
// negative bid signals abstention: the executor declines the task and is
// excluded from this auction. Executors that do not implement Bidder receive a
// default bid computed by the router from in-flight load and failure history.
type Bidder interface {
	Bid(agent, task string) float64
}

// executorBid computes the auction bid for executor idx. If the executor
// implements Bidder, its task-specific bid is used; otherwise a default bid is
// derived from current in-flight load plus a penalty for recent consecutive
// failures, so flaky executors are deprioritized without being hard-excluded.
// Lower bids win. Returns a negative value when the executor abstains.
func (r *AgentRouter) executorBid(idx int, e AgentExecutor, activeCountsSnapshot []int64, agent, task string) float64 {
	if b, ok := e.(Bidder); ok {
		return b.Bid(agent, task)
	}
	load := 0.0
	if idx >= 0 && idx < len(activeCountsSnapshot) {
		load = float64(activeCountsSnapshot[idx])
	}
	penalty := 0.0
	r.mu.RLock()
	if fs := r.executorFailures[idx]; fs != nil {
		penalty = float64(fs.consecutiveFailures)
	}
	r.mu.RUnlock()
	return load + penalty
}

// pickAuctionWinner returns the index of the healthy executor with the lowest
// bid for (agent, task). Executors that abstain (negative bid) or fail the
// heartbeat-aware health check are skipped. Ties resolve to the earliest
// executor, matching least-connections behavior. Returns -1 if no executor
// submits a valid bid. Caller must NOT hold r.mu since Health() may make
// network calls.
func (r *AgentRouter) pickAuctionWinner(executors []AgentExecutor, activeCountsSnapshot []int64, agent, task string) int {
	bestIdx := -1
	bestBid := math.Inf(1)
	for i, e := range executors {
		// Heartbeat-aware health check first.
		if !r.isAliveByHeartbeat(i) && e.Health() != nil {
			continue // skip unhealthy executors
		}
		bid := r.executorBid(i, e, activeCountsSnapshot, agent, task)
		if bid < 0 {
			continue // executor abstained
		}
		if bid < bestBid {
			bestBid = bid
			bestIdx = i
		}
	}
	return bestIdx
}
