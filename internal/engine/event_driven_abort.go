package engine

import (
	"fmt"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// BuildEventDrivenAbort builds a go-bt Command for an AbortOnEvent node.
// Plan #3: Event-driven interrupt decorator that aborts a running child
// when a specified event fires on the blackboard's EventBus.
func BuildEventDrivenAbort(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	if len(node.Children) == 0 {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int { return 1 })
	}

	child := buildNode(&node.Children[0], bb, node.Name)

	// Parse event sources from metadata
	events := parseEventSources(node)
	abortOnMatch := true
	if node.Metadata != nil {
		if v, ok := node.Metadata["abort_on_match"].(bool); ok {
			abortOnMatch = v
		}
	}

	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		// Ensure EventBus exists
		if bb.EventBus == nil {
			bb.EventBus = NewEventBus()
		}
		// Also store in ChainState for backward compatibility with tests
		if bb.ChainState == nil {
			bb.ChainState = make(map[string]any)
		}
		bb.ChainState["event_bus"] = bb.EventBus

		// Subscribe to all configured event keys
		eventCh := make(chan EventMessage, len(events)*2)
		for _, es := range events {
			bb.EventBus.Subscribe(es.Key, eventCh)
		}
		defer func() {
			for _, es := range events {
				bb.EventBus.Unsubscribe(es.Key, eventCh)
			}
		}()

		// Check for pre-existing fired events (published before this tick)
		// Also check blackboard-key event sources directly on ChainState
		for _, es := range events {
			if es.Type == EventSourceBlackboard {
				if evaluateEventCondition(es, bb) {
					bb.Result, _ = bb.ChainState[es.Key].(string)
					if bb.Result == "" {
						bb.Result = fmt.Sprintf("aborted: %s blackboard_key %s", es.Source, es.Key)
					}
					bb.Outcome = "aborted"
					return -1
				}
			} else if bb.EventBus.HasFired(es.Key) {
				// Try to propagate event data from the last published event
				if msg, ok := bb.EventBus.GetLastEvent(es.Key); ok {
					if reason, ok := msg.Data["reason"].(string); ok {
						bb.Result = reason
					} else {
						bb.Result = msg.Type
					}
				} else {
					bb.Result = fmt.Sprintf("aborted: %s event %s", es.Source, es.Key)
				}
				bb.Outcome = "aborted"
				return -1
			}
		}

		// Execute child
		result := child.Run(ctx)

		// If child is running, check for abort events
		if result == 0 {
			// Poll event channel (non-blocking)
			matched := false
			for _, es := range events {
				select {
				case msg := <-eventCh:
					if eventMatches(es, msg, bb) {
						matched = true
						// Store abort reason in blackboard
						bb.Result = fmt.Sprintf("aborted: %s event %s", es.Source, es.Key)
						bb.Outcome = "aborted"
					}
				default:
				}
			}
			if abortOnMatch && matched {
				return -1 // aborted → treated as failure
			}
			return 0 // Still running, no event
		}

		return result
	})
}

// EventSource defines a source of events for AbortOnEvent.
type EventSource struct {
	Type      EventSourceType `json:"type"`      // "blackboard_key", "timer", "channel"
	Key       string          `json:"key"`       // EventBus key to subscribe to
	Predicate string          `json:"predicate"` // Optional condition (e.g., "<= 0")
	Source    string          `json:"source"`    // Human-readable source name
	BoolEval  bool            `json:"bool_eval"` // true: event fires when key is true/truthy
}

// EventSourceType is the type of an event source.
type EventSourceType string

// Event source type constants.
const (
	EventSourceBlackboard EventSourceType = "blackboard_key"
	EventSourceTimer      EventSourceType = "timer"
	EventSourceChannel    EventSourceType = "channel"
)

// evaluateEventCondition evaluates a simple event condition against the blackboard.
// Supports BoolEval filtering, predicate expressions, and various value types.
func evaluateEventCondition(es EventSource, bb *Blackboard) bool {
	if bb == nil {
		return false
	}
	if es.Key != "" && bb.ChainState != nil {
		if val, ok := bb.ChainState[es.Key]; ok {
			// BoolEval: only fires for actual bool values
			if es.BoolEval {
				if b, ok := val.(bool); ok {
					return b
				}
				return false // non-bool value with BoolEval → false
			}
			// Predicate evaluation
			if es.Predicate != "" {
				return evaluatePredicate(es.Predicate, val)
			}
			// Default: key exists = condition met
			switch v := val.(type) {
			case bool:
				return v
			case string:
				return v != "" && v != "false"
			default:
				return true
			}
		}
	}
	return false
}

// evaluatePredicate evaluates simple predicates against a value.
// Supports: "<= 0", "> 0", "!= true", "> X", "< X", etc.
func evaluatePredicate(predicate string, val interface{}) bool {
	// Convert val to float64 for numeric comparisons
	fv, isNumeric := toFloat(val)
	switch {
	case predicate == "<= 0":
		return isNumeric && fv <= 0
	case predicate == "> 0":
		return isNumeric && fv > 0
	case predicate == "< 0":
		return isNumeric && fv < 0
	case predicate == ">= 0":
		return isNumeric && fv >= 0
	case predicate == "= 0" || predicate == "== 0":
		return isNumeric && fv == 0
	case predicate == "!= true":
		b, ok := val.(bool)
		return ok && !b
	default:
		return false
	}
}

// toFloat converts a value to float64 if possible.
func toFloat(val interface{}) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case bool:
		if v {
			return 1.0, true
		}
		return 0.0, true
	default:
		return 0, false
	}
}

func parseEventSources(node *evolution.SerializableNode) []EventSource {
	if node.Metadata == nil {
		return nil
	}
	// New format: "events" array
	raw, ok := node.Metadata["events"]
	if ok {
		eventList, ok := raw.([]interface{})
		if !ok {
			return nil
		}
		var sources []EventSource
		for _, item := range eventList {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			es := EventSource{
				Type:      EventSourceType(stringFromMap(m, "type")),
				Key:       stringFromMap(m, "key"),
				Predicate: stringFromMap(m, "predicate"),
				Source:    stringFromMap(m, "source"),
			}
			if bv, ok := m["bool_eval"].(bool); ok {
				es.BoolEval = bv
			}
			sources = append(sources, es)
		}
		return sources
	}
	// Legacy format: flat "event_key" / "event_type" / "event" fields
	eventKey, _ := node.Metadata["event_key"].(string)
	if eventKey == "" {
		eventKey, _ = node.Metadata["event"].(string)
	}
	eventType, _ := node.Metadata["event_type"].(string)
	if eventKey == "" && eventType == "" {
		return nil
	}
	if eventType == "" {
		eventType = "channel"
	}
	return []EventSource{{
		Type: EventSourceType(eventType),
		Key:  eventKey,
	}}
}

// eventMatches checks if an EventMessage matches the EventSource criteria.
func eventMatches(es EventSource, msg EventMessage, bb *Blackboard) bool {
	// Check key match
	if es.Key != "" && es.Key != msg.Type && es.Key != msg.Source {
		// If key doesn't match type or source in the message, check predicate
		if es.Predicate == "" {
			return false
		}
	}
	// Evaluate predicate
	if es.Predicate != "" {
		return evaluateEventPredicate(es.Predicate, msg, bb)
	}
	return true
}

// evaluateEventPredicate evaluates simple predicates against the blackboard.
// Supports: "<= 0", "> 0", "!= true", "now() > deadline", specific value checks.
func evaluateEventPredicate(predicate string, _ EventMessage, bb *Blackboard) bool {
	// For now, use the guard condition evaluator from utility_selector
	return evaluateGuardCondition(predicate, bb)
}
