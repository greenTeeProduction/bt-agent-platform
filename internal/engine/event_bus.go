package engine

import (
	"sync"
	"time"
)

// EventMessage represents an event published on the EventBus.
// Plan #3: Events carry metadata and can trigger AbortOnEvent decorators.
type EventMessage struct {
	Source    string         `json:"source"`
	Timestamp time.Time      `json:"timestamp"`
	Type      string         `json:"type"`
	Data      map[string]any `json:"data,omitempty"`
	Priority  int            `json:"priority"`
}

// EventBus provides inter-node event communication within a running BT context.
// Each blackboard has its own EventBus for the duration of a tree execution.
// Plan #3: Used by AbortOnEvent decorators for interrupt propagation.
type EventBus struct {
	subscribers map[string][]chan EventMessage
	mu          sync.RWMutex
	fired       map[string]bool         // tracks which keys have had events published
	lastData    map[string]EventMessage // last event data for each key (propagation)
}

// NewEventBus creates a new EventBus ready for subscriptions.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string][]chan EventMessage),
		fired:       make(map[string]bool),
		lastData:    make(map[string]EventMessage),
	}
}

// Subscribe registers a channel to receive events for the given key.
// The channel MUST be buffered to avoid blocking publishers.
func (eb *EventBus) Subscribe(key string, ch chan EventMessage) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.subscribers[key] = append(eb.subscribers[key], ch)
}

// Unsubscribe removes a previously registered channel for the given key.
// Callers MUST drain the channel after unsubscribe to avoid goroutine leaks.
// The channel is NOT closed — Callers should close it themselves if needed.
func (eb *EventBus) Unsubscribe(key string, ch chan EventMessage) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	subs := eb.subscribers[key]
	for i, sub := range subs {
		if sub == ch {
			eb.subscribers[key] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	if len(eb.subscribers[key]) == 0 {
		delete(eb.subscribers, key)
	}
}

// Publish sends an event to all subscribers of the given key.
// Non-blocking: if a subscriber's channel is full, the event is dropped.
// Also records that the key has fired for HasFired tracking.
func (eb *EventBus) Publish(key string, msg EventMessage) {
	eb.mu.Lock()
	eb.fired[key] = true
	eb.lastData[key] = msg
	eb.mu.Unlock()
	eb.mu.RLock()
	subs := eb.subscribers[key]
	eb.mu.RUnlock()
	for _, sub := range subs {
		select {
		case sub <- msg:
		default:
			// Channel full — drop event (non-blocking)
		}
	}
}

// HasSubscribers returns true if there are any subscribers for the given key.
func (eb *EventBus) HasSubscribers(key string) bool {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return len(eb.subscribers[key]) > 0
}

// SubscriberCount returns the total number of active subscribers across all keys.
func (eb *EventBus) SubscriberCount() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	count := 0
	for _, subs := range eb.subscribers {
		count += len(subs)
	}
	return count
}

// HasFired returns true if the given key has had at least one event published.
func (eb *EventBus) HasFired(key string) bool {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return eb.fired[key]
}

// ResetFired clears the fired tracking map. Used by tests to reset state.
func (eb *EventBus) ResetFired() {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.fired = make(map[string]bool)
}

// Close shuts down the EventBus, unsubscribes all listeners, and closes
// all subscriber channels. No further publications should occur after Close.
func (eb *EventBus) Close() {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	for _, subs := range eb.subscribers {
		for _, ch := range subs {
			close(ch)
		}
	}
	eb.subscribers = make(map[string][]chan EventMessage)
	eb.fired = make(map[string]bool)
	eb.lastData = make(map[string]EventMessage)
}

// GetLastEvent returns the last event published for the given key.
// Returns nil event and false if no event has been published.
func (eb *EventBus) GetLastEvent(key string) (EventMessage, bool) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	msg, ok := eb.lastData[key]
	return msg, ok
}
