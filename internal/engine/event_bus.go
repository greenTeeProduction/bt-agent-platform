package engine

import (
	"sync"
	"time"
)

// EventMessage represents an event in the behavior tree event bus.
type EventMessage struct {
	Source    string         `json:"source"`
	Timestamp time.Time      `json:"timestamp"`
	Type      string         `json:"type"`
	Data      map[string]any `json:"data,omitempty"`
	Priority  int            `json:"priority"`
}

// EventBus provides inter-node communication for behavior trees.
// Supports publish/subscribe patterns for event-driven aborts and monitoring.
type EventBus struct {
	mu    sync.RWMutex
	subs  map[string][]chan EventMessage
	fired map[string]bool // tracks which events have fired this tick
}

// NewEventBus creates a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{
		subs:  make(map[string][]chan EventMessage),
		fired: make(map[string]bool),
	}
}

// Subscribe registers a channel to receive events for a given key.
func (eb *EventBus) Subscribe(key string, ch chan EventMessage) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.subs[key] = append(eb.subs[key], ch)
}

// Unsubscribe removes a channel from a key's subscriber list.
func (eb *EventBus) Unsubscribe(key string, ch chan EventMessage) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	list := eb.subs[key]
	for i, sub := range list {
		if sub == ch {
			eb.subs[key] = append(list[:i], list[i+1:]...)
			break
		}
	}
}

// Publish sends an event to all subscribers for the given key.
func (eb *EventBus) Publish(key string, msg EventMessage) {
	eb.mu.Lock()
	eb.fired[key] = true
	subs := make([]chan EventMessage, len(eb.subs[key]))
	copy(subs, eb.subs[key])
	eb.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- msg:
		default:
			// don't block; drop if channel is full
		}
	}
}

// HasFired returns true if any event with the given key has fired since last reset.
func (eb *EventBus) HasFired(key string) bool {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return eb.fired[key]
}

// ResetFired clears the fired event map (called at beginning of each tick).
func (eb *EventBus) ResetFired() {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.fired = make(map[string]bool)
}

// Close closes all subscriber channels and cleans up.
func (eb *EventBus) Close() {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	for _, subs := range eb.subs {
		for _, ch := range subs {
			close(ch)
		}
	}
	eb.subs = make(map[string][]chan EventMessage)
	eb.fired = make(map[string]bool)
}
