package engine

import (
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// BuildEventDrivenAbort builds an event-driven abort decorator.
// It wraps a child command and aborts it when any configured event source fires.
// Events can come from: blackboard key changes, EventBus subscriptions, or timer signals.
func BuildEventDrivenAbort(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	if len(node.Children) == 0 {
		return btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 1 })
	}

	child := buildNode(&node.Children[0], bb, node.Name)

	abortOnMatch := true
	propagateEvent := false
	if node.Metadata != nil {
		if a, ok := node.Metadata["abort_on_match"].(bool); ok {
			abortOnMatch = a
		}
		if p, ok := node.Metadata["propagate_event"].(bool); ok {
			propagateEvent = p
		}
	}

	// Parse event sources from metadata
	eventSources := parseEventSources(node)

	// Ensure blackboard has an EventBus
	if bb.ChainState == nil {
		bb.ChainState = make(map[string]any)
	}
	eb, ok := bb.ChainState["event_bus"].(*EventBus)
	if !ok || eb == nil {
		eb = NewEventBus()
		bb.ChainState["event_bus"] = eb
	}

	// Subscribe to event sources
	abortCh := make(chan EventMessage, 1)
	for _, src := range eventSources {
		if src.Type == EventSourceChannel || src.Type == EventSourceBlackboard {
			eb.Subscribe(src.Key, abortCh)
		}
	}

	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		// Check for abort events
		select {
		case msg := <-abortCh:
			shouldAbort := abortOnMatch
			if shouldAbort {
				// Propagate event data to blackboard
				if propagateEvent {
					ctx.Blackboard.Result = msg.Source + ": " + msg.Type
					if msg.Data != nil {
						if reason, ok := msg.Data["reason"].(string); ok {
							ctx.Blackboard.Result = reason
						}
					}
				}
				return -1 // Aborted → Failure
			}
		default:
			// No event pending
		}

		// Check blackboard key conditions
		for _, src := range eventSources {
			if src.Type == EventSourceBlackboard && evaluateEventCondition(src, ctx.Blackboard) {
				if abortOnMatch {
					return -1
				}
			}
		}

		// Run the child command
		result := child.Run(ctx)

		// If child is still running and a source fired since, abort
		if result == 0 { // Running
			for _, src := range eventSources {
				if src.Type == EventSourceChannel && eb.HasFired(src.Key) {
					return -1
				}
			}
		}

		return result
	})
}

// EventSourceType defines the type of event source.
type EventSourceType string

const (
	EventSourceBlackboard EventSourceType = "blackboard_key"
	EventSourceChannel    EventSourceType = "channel"
	EventSourceSignal     EventSourceType = "signal"
	EventSourceTimer      EventSourceType = "timer"
)

// EventSource describes an event source configuration.
type EventSource struct {
	Type      EventSourceType `json:"type"`
	Key       string          `json:"key"`
	Predicate string          `json:"predicate,omitempty"`
	BoolEval  bool            `json:"bool_eval,omitempty"`
}

// parseEventSources extracts event source configs from node metadata.
func parseEventSources(node *evolution.SerializableNode) []EventSource {
	if node.Metadata == nil {
		return nil
	}

	var sources []EventSource

	// Try "events" field first
	if raw, ok := node.Metadata["events"]; ok {
		if list, ok := raw.([]interface{}); ok {
			for _, item := range list {
				if m, ok := item.(map[string]interface{}); ok {
					src := EventSource{
						Key: stringFromMap(m, "key"),
					}
					if t, ok := m["type"].(string); ok {
						src.Type = EventSourceType(t)
					}
					if p, ok := m["predicate"].(string); ok {
						src.Predicate = p
					}
					if b, ok := m["bool_eval"].(bool); ok {
						src.BoolEval = b
					}
					sources = append(sources, src)
				}
			}
			return sources
		}
	}

	// Legacy fallback: single "event_key" + "event_type"
	if key, ok := node.Metadata["event_key"].(string); ok {
		src := EventSource{Key: key}
		if t, ok := node.Metadata["event_type"].(string); ok {
			src.Type = EventSourceType(t)
		}
		sources = append(sources, src)
	}

	return sources
}

// evaluateEventCondition checks if a blackboard-key event source should fire.
func evaluateEventCondition(src EventSource, bb *Blackboard) bool {
	if bb.ChainState == nil {
		return false
	}

	// Check if value exists
	val, ok := bb.ChainState[src.Key]
	if !ok {
		return false
	}

	// Bool eval
	if src.BoolEval {
		if b, ok := val.(bool); ok {
			return b
		}
		return false
	}

	// Simple predicate evaluation: "<= 0", "> 0", "== true"
	if src.Predicate == "" {
		return true // just existing is enough
	}

	if fv, ok := valAsFloat64(val); ok {
		switch src.Predicate {
		case "<= 0":
			return fv <= 0
		case "> 0":
			return fv > 0
		case "< 0":
			return fv < 0
		case ">= 0":
			return fv >= 0
		case "== 0":
			return fv == 0
		}
	}

	return false
}

func valAsFloat64(val interface{}) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}
