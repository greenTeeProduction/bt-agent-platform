// Package reliability provides a TTL-based node heartbeat system for
// tracking multi-node aliveness in distributed AgentRouter deployments.
//
// NodeHeartbeat enables async health tracking: remote executors register
// with a TTL and periodically ping to refresh. The AgentRouter can check
// IsAlive() without making synchronous HTTP health checks on every routing
// decision, reducing latency and network load in multi-node clusters.
package reliability

import (
	"sync"
	"time"
)

// HeartbeatEntry holds the state of a registered node.
type HeartbeatEntry struct {
	NodeID   string            `json:"node_id"`
	LastSeen time.Time         `json:"last_seen"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Alive    bool              `json:"alive"`
}

// NodeHeartbeat tracks node aliveness using TTL-based heartbeats.
// Nodes register with a TTL and must Ping() before the TTL expires.
// A background goroutine periodically cleans up expired entries.
// All methods are concurrency-safe.
type NodeHeartbeat struct {
	mu              sync.RWMutex
	nodes           map[string]*heartbeatEntry
	ttl             time.Duration
	stopCh          chan struct{}
	cleanupInterval time.Duration
}

type heartbeatEntry struct {
	nodeID   string
	lastSeen time.Time
	metadata map[string]string
}

// NewNodeHeartbeat creates a heartbeat tracker with the given TTL.
// Nodes that don't Ping within TTL are considered dead.
// A background goroutine runs periodic cleanup at cleanupInterval
// (or TTL/2 if cleanupInterval is zero). Call Stop() to shut down.
func NewNodeHeartbeat(ttl time.Duration) *NodeHeartbeat {
	cleanupInterval := ttl / 2
	if cleanupInterval < time.Second {
		cleanupInterval = time.Second
	}
	hb := &NodeHeartbeat{
		nodes:           make(map[string]*heartbeatEntry),
		ttl:             ttl,
		stopCh:          make(chan struct{}),
		cleanupInterval: cleanupInterval,
	}
	go hb.cleanupLoop()
	return hb
}

// NewNodeHeartbeatWithCleanupInterval creates a heartbeat tracker with
// explicit TTL and cleanup interval. Use for testing with faster intervals.
func NewNodeHeartbeatWithCleanupInterval(ttl, cleanupInterval time.Duration) *NodeHeartbeat {
	hb := &NodeHeartbeat{
		nodes:           make(map[string]*heartbeatEntry),
		ttl:             ttl,
		stopCh:          make(chan struct{}),
		cleanupInterval: cleanupInterval,
	}
	go hb.cleanupLoop()
	return hb
}

// Register adds a node to the heartbeat tracker, or refreshes it if
// already present. Metadata is optional (may be nil).
func (hb *NodeHeartbeat) Register(nodeID string, metadata map[string]string) {
	hb.mu.Lock()
	defer hb.mu.Unlock()

	if existing, ok := hb.nodes[nodeID]; ok {
		existing.lastSeen = time.Now()
		if metadata != nil {
			existing.metadata = metadata
		}
		return
	}

	hb.nodes[nodeID] = &heartbeatEntry{
		nodeID:   nodeID,
		lastSeen: time.Now(),
		metadata: metadata,
	}
}

// Ping refreshes the last-seen time of a registered node.
// Returns true if the node was registered, false if not found.
func (hb *NodeHeartbeat) Ping(nodeID string) bool {
	hb.mu.Lock()
	defer hb.mu.Unlock()

	entry, ok := hb.nodes[nodeID]
	if !ok {
		return false
	}
	entry.lastSeen = time.Now()
	return true
}

// Deregister removes a node from the heartbeat tracker.
func (hb *NodeHeartbeat) Deregister(nodeID string) {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	delete(hb.nodes, nodeID)
}

// IsAlive returns true if the node is registered and has pinged within TTL.
func (hb *NodeHeartbeat) IsAlive(nodeID string) bool {
	hb.mu.RLock()
	defer hb.mu.RUnlock()

	entry, ok := hb.nodes[nodeID]
	if !ok {
		return false
	}
	return time.Since(entry.lastSeen) < hb.ttl
}

// ListAlive returns the IDs of all currently alive nodes.
func (hb *NodeHeartbeat) ListAlive() []string {
	hb.mu.RLock()
	defer hb.mu.RUnlock()

	now := time.Now()
	var alive []string
	for _, entry := range hb.nodes {
		if now.Sub(entry.lastSeen) < hb.ttl {
			alive = append(alive, entry.nodeID)
		}
	}
	return alive
}

// ListAll returns full HeartbeatEntry snapshots for all registered nodes.
// Each entry includes aliveness status computed at call time.
func (hb *NodeHeartbeat) ListAll() []HeartbeatEntry {
	hb.mu.RLock()
	defer hb.mu.RUnlock()

	now := time.Now()
	entries := make([]HeartbeatEntry, 0, len(hb.nodes))
	for _, e := range hb.nodes {
		entries = append(entries, HeartbeatEntry{
			NodeID:   e.nodeID,
			LastSeen: e.lastSeen,
			Metadata: e.metadata,
			Alive:    now.Sub(e.lastSeen) < hb.ttl,
		})
	}
	return entries
}

// Stats returns total, alive, and expired node counts.
func (hb *NodeHeartbeat) Stats() (total, alive, expired int) {
	hb.mu.RLock()
	defer hb.mu.RUnlock()

	now := time.Now()
	total = len(hb.nodes)
	for _, entry := range hb.nodes {
		if now.Sub(entry.lastSeen) < hb.ttl {
			alive++
		} else {
			expired++
		}
	}
	return
}

// TTL returns the configured heartbeat TTL.
func (hb *NodeHeartbeat) TTL() time.Duration {
	return hb.ttl
}

// Stop shuts down the background cleanup goroutine.
// After Stop, no automatic cleanup occurs, but all other methods remain usable.
func (hb *NodeHeartbeat) Stop() {
	select {
	case <-hb.stopCh:
		// already stopped
	default:
		close(hb.stopCh)
	}
}

// cleanupLoop periodically removes expired nodes.
func (hb *NodeHeartbeat) cleanupLoop() {
	ticker := time.NewTicker(hb.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-hb.stopCh:
			return
		case <-ticker.C:
			hb.cleanup()
		}
	}
}

func (hb *NodeHeartbeat) cleanup() {
	hb.mu.Lock()
	defer hb.mu.Unlock()

	now := time.Now()
	for id, entry := range hb.nodes {
		if now.Sub(entry.lastSeen) >= hb.ttl {
			delete(hb.nodes, id)
		}
	}
}
