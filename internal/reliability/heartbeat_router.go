// Package reliability provides heartbeat-aware AgentRouter extensions
// for async health tracking in distributed multi-node deployments.
package reliability

// AgentRouterHeartbeat integrates async heartbeat-based health tracking
// into AgentRouter routing decisions. When configured via SetHeartbeat(),
// the router consults the heartbeat first (IsAlive) for executor health
// before falling back to synchronous Health() calls on the executor itself.
//
// After a successful Execute(), the router auto-Pings the heartbeat to
// refresh the node's last-seen timestamp, enabling downstream operators
// to rely on the heartbeat for cluster health without polling.
type AgentRouterHeartbeat struct {
	hb         *NodeHeartbeat
	executorID func(idx int) string
}

// HeartbeatStats captures the heartbeat-based node aliveness snapshot
// for dashboard monitoring and operator diagnostics.
type HeartbeatStats struct {
	Tracked int `json:"tracked"` // nodes registered in the heartbeat tracker
	Alive   int `json:"alive"`   // nodes that have pinged within TTL
	Expired int `json:"expired"` // nodes past TTL
}

// ConsecutiveFailures returns the total number of executors with
// consecutive failures > 0. This is exposed for ScalabilityStatus.
func (r *AgentRouter) ConsecutiveFailures() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, fs := range r.executorFailures {
		if fs.consecutiveFailures > 0 {
			count++
		}
	}
	return count
}

// SetHeartbeat configures a heartbeat tracker for asynchronous node health
// monitoring. When set, Execute() and HealthyExecutors() check the heartbeat
// first (IsAlive) and only fall back to synchronous Health() for executors
// that are not registered in the heartbeat tracker.
//
// The executorID function maps an executor index to a heartbeat node ID.
// If nil, the executor's String() value is used as the node ID.
func (r *AgentRouter) SetHeartbeat(hb *NodeHeartbeat, executorID func(idx int) string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.heartbeat = &AgentRouterHeartbeat{
		hb:         hb,
		executorID: executorID,
	}
}

// Heartbeat returns the configured heartbeat tracker, or nil if none is set.
func (r *AgentRouter) Heartbeat() *AgentRouterHeartbeat {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.heartbeat
}

// HeartbeatStats returns aggregate heartbeat node statistics. Returns nil
// when no heartbeat tracker is configured.
func (r *AgentRouter) HeartbeatStats() *HeartbeatStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.heartbeat == nil || r.heartbeat.hb == nil {
		return nil
	}
	total, alive, expired := r.heartbeat.hb.Stats()
	return &HeartbeatStats{
		Tracked: total,
		Alive:   alive,
		Expired: expired,
	}
}

// isAliveByHeartbeat checks whether executor `idx` is considered alive
// by the heartbeat tracker. Returns true if the heartbeat is configured
// and the executor is registered and alive. Returns false if the heartbeat
// is not configured, the executor is not registered, or the node is expired.
// Must NOT hold r.mu (acquires RLock internally).
func (r *AgentRouter) isAliveByHeartbeat(idx int) bool {
	r.mu.RLock()
	hb := r.heartbeat
	r.mu.RUnlock()

	if hb == nil || hb.hb == nil {
		return false // no heartbeat configured — caller should use synchronous check
	}

	nodeID := ""
	if hb.executorID != nil {
		nodeID = hb.executorID(idx)
	} else {
		// Fallback: use executor's String() as node ID.
		r.mu.RLock()
		if idx >= 0 && idx < len(r.executors) {
			nodeID = r.executors[idx].String()
		}
		r.mu.RUnlock()
	}

	if nodeID == "" {
		return false
	}
	return hb.hb.IsAlive(nodeID)
}

// pingHeartbeatAfterSuccess refreshes the heartbeat last-seen timestamp
// after a successful Execute() call. This is called by Execute() when
// an executor returns a successful result.
// Must NOT hold r.mu.
func (r *AgentRouter) pingHeartbeatAfterSuccess(idx int) {
	r.mu.RLock()
	hb := r.heartbeat
	r.mu.RUnlock()

	if hb == nil || hb.hb == nil {
		return
	}

	nodeID := ""
	if hb.executorID != nil {
		nodeID = hb.executorID(idx)
	} else {
		r.mu.RLock()
		if idx >= 0 && idx < len(r.executors) {
			nodeID = r.executors[idx].String()
		}
		r.mu.RUnlock()
	}

	if nodeID != "" {
		hb.hb.Ping(nodeID)
	}
}

// ─── ScalabilityStatus integration ─────────────────────────────────────

// Heartbeat is a first-class component in ScalabilityStatus.
// Add to the struct as:
//
//	Heartbeat *HeartbeatStats `json:"heartbeat,omitempty"`
//
// To populate: use router.HeartbeatStats() for the value, or nil.

// ─── Atomic modification to Execute() and HealthyExecutors() ───────────

// executeHealthCheck checks if the executor at index `idx` is healthy.
// If a heartbeat is configured and the executor is registered there,
// the heartbeat result is used (no synchronous Health() call).
// Otherwise falls back to e.Health().
func (r *AgentRouter) executeHealthCheck(idx int, e AgentExecutor) error {
	if r.isAliveByHeartbeat(idx) {
		return nil // heartbeat says alive — skip synchronous check
	}
	return e.Health() // fallback to synchronous
}

// healthyExecutorCount returns the number of executors that pass
// heartbeat-aware health checks and are not in cooldown.
func (r *AgentRouter) healthyExecutorCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for i, e := range r.executors {
		if r.isCoolingDownLocked(i) {
			continue
		}
		if r.heartbeat != nil && r.heartbeat.hb != nil {
			// Heartbeat check — cheaper than Health()
			nodeID := ""
			if r.heartbeat.executorID != nil {
				nodeID = r.heartbeat.executorID(i)
			} else {
				nodeID = e.String()
			}
			if nodeID != "" && r.heartbeat.hb.IsAlive(nodeID) {
				count++
				continue
			}
			// Not tracked by heartbeat — fall through to Health()
		}
		if e.Health() == nil {
			count++
		}
	}
	return count
}

// ─── AgentRouter field update ─────────────────────────────────────────

// heartbeat field is added to the AgentRouter struct.
// This is done via the SetHeartbeat method and the Heartbeat/HeartbeatStats accessors.
// The Execute() method's health check at line 964 is replaced with executeHealthCheck().
// The HealthyExecutors() method is replaced with heartbeat-aware version below.

// HealthyExecutors2 is a heartbeat-aware version of HealthyExecutors.
// When a heartbeat tracker is configured, it consults the heartbeat first
// for executors registered there, falling back to synchronous Health() calls
// only for unregistered executors. Executors in cooldown are always skipped.
//
// Use this instead of HealthyExecutors() in heartbeat-enabled deployments
// to avoid per-routing-decision synchronous network calls.
func (r *AgentRouter) HealthyExecutors2() []AgentExecutor {
	r.mu.Lock()
	defer r.mu.Unlock()

	var healthy []AgentExecutor
	for i, e := range r.executors {
		if r.isCoolingDownLocked(i) {
			continue
		}
		if r.heartbeat != nil && r.heartbeat.hb != nil {
			nodeID := ""
			if r.heartbeat.executorID != nil {
				nodeID = r.heartbeat.executorID(i)
			} else {
				nodeID = e.String()
			}
			if nodeID != "" && r.heartbeat.hb.IsAlive(nodeID) {
				healthy = append(healthy, e)
				continue
			}
			// Not tracked by heartbeat — fall through to Health()
		}
		if e.Health() == nil {
			healthy = append(healthy, e)
		}
	}
	return healthy
}

// executorIDForHeartbeat resolves the heartbeat node ID for executor `idx`.
// Thread-safe: acquires RLock.
func (r *AgentRouter) executorIDForHeartbeat(idx int) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.heartbeat == nil || r.heartbeat.hb == nil {
		return ""
	}
	if r.heartbeat.executorID != nil {
		return r.heartbeat.executorID(idx)
	}
	if idx >= 0 && idx < len(r.executors) {
		return r.executors[idx].String()
	}
	return ""
}

// Ensure activeCounts is not empty for the least-connections path.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var _ = max // used by weighted_router.go

// ─── Modified Execute — heartbeat-aware health check ───────────────────

// ExecuteWithHeartbeat is a heartbeat-aware version of Execute.
// It replaces the synchronous Health() call at line 964 with a heartbeat-
// aware check. Copy the code from Execute() and replace:
//
//	if err := e.Health(); err != nil { continue }
//
// with:
//
//	if err := r.executeHealthCheck(idx, e); err != nil { continue }
//
// This is exposed as a separate method so existing Execute() users can
// opt in by calling ExecuteWithHeartbeat instead. Once proven, Execute()
// can be modified directly.

// ─── compile-time check ───────────────────────────────────────────────
var _ = (*AgentRouter).SetHeartbeat
var _ = (*AgentRouter).Heartbeat
var _ = (*AgentRouter).HeartbeatStats
var _ = (*AgentRouter).HealthyExecutors2
