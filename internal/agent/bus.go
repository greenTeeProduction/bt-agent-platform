// Package agent provides the AgentBus — a pub/sub event bus for inter-agent communication.
// Agents can publish events and subscribe to receive notifications about system state changes.
package agent

import (
	"sync"
	"time"
)

// AgentEvent represents an event published on the AgentBus.
type AgentEvent struct {
	Type      string    // "service_down", "health_alert", "task_complete", "error_detected"
	Source    string    // agent name that published
	Target    string    // optional target agent (empty = broadcast)
	Message   string    // human-readable message
	Data      any       // optional structured data
	Timestamp time.Time
}

// AgentBus is a simple publish/subscribe event bus for agent-to-agent communication.
// Agents can subscribe to specific event types or receive all events.
type AgentBus struct {
	mu          sync.RWMutex
	subscribers map[string][]chan AgentEvent   // event type → subscriber channels
	allSubs     []chan AgentEvent              // subscribers receiving all events
	history     []AgentEvent                   // recent events (ring buffer)
	maxHistory  int
	closed      bool
}

// GlobalAgentBus is the singleton event bus. Initialize with InitAgentBus().
var GlobalAgentBus *AgentBus

// InitAgentBus initializes the global agent event bus.
func InitAgentBus(maxHistory int) *AgentBus {
	if maxHistory <= 0 {
		maxHistory = 100
	}
	GlobalAgentBus = &AgentBus{
		subscribers: make(map[string][]chan AgentEvent),
		maxHistory:  maxHistory,
	}
	return GlobalAgentBus
}

// Subscribe returns a channel that receives events of the given type.
// If eventType is "", receives all events. Channel buffer size is 16.
func (b *AgentBus) Subscribe(eventType string) <-chan AgentEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan AgentEvent, 16)
	if b.closed {
		close(ch)
		return ch
	}

	if eventType == "" {
		b.allSubs = append(b.allSubs, ch)
	} else {
		b.subscribers[eventType] = append(b.subscribers[eventType], ch)
	}
	return ch
}

// Publish sends an event to all matching subscribers.
func (b *AgentBus) Publish(event AgentEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Store in history ring buffer
	b.history = append(b.history, event)
	if len(b.history) > b.maxHistory {
		b.history = b.history[len(b.history)-b.maxHistory:]
	}

	// Send to type-specific subscribers
	for _, ch := range b.subscribers[event.Type] {
		select {
		case ch <- event:
		default:
			// channel full, drop event (non-blocking)
		}
	}

	// Send to all-subscribers
	for _, ch := range b.allSubs {
		select {
		case ch <- event:
		default:
		}
	}
}

// History returns recent events.
func (b *AgentBus) History(limit int) []AgentEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if limit <= 0 || limit > len(b.history) {
		limit = len(b.history)
	}
	start := len(b.history) - limit
	if start < 0 {
		start = 0
	}
	result := make([]AgentEvent, limit)
	copy(result, b.history[start:])
	return result
}

// Close shuts down the bus and closes all subscriber channels.
func (b *AgentBus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.closed = true
	for _, subs := range b.subscribers {
		for _, ch := range subs {
			close(ch)
		}
	}
	for _, ch := range b.allSubs {
		close(ch)
	}
}

// PublishServiceDown is a convenience method for publishing service_down events.
func PublishServiceDown(source, serviceName string) {
	if GlobalAgentBus == nil {
		return
	}
	GlobalAgentBus.Publish(AgentEvent{
		Type:    "service_down",
		Source:  source,
		Message: serviceName + " is not running",
		Data:    map[string]string{"service": serviceName},
	})
}

// PublishHealthAlert publishes a health alert event.
func PublishHealthAlert(source, message string) {
	if GlobalAgentBus == nil {
		return
	}
	GlobalAgentBus.Publish(AgentEvent{
		Type:    "health_alert",
		Source:  source,
		Message: message,
	})
}
