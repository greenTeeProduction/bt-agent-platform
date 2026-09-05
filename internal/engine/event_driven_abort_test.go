package engine

import (
	"context"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestNewEventBus_SubsFiredInitialized(t *testing.T) {
	eb := NewEventBus()
	if eb.subscribers == nil {
		t.Error("subs map nil")
	}
	if eb.fired == nil {
		t.Error("fired map nil")
	}
}

// ----- EventDrivenAbort tests -----

func TestBuildEventDrivenAbort_NoChildren(t *testing.T) {
	bb := &Blackboard{}
	node := &evolution.SerializableNode{
		Type:     "EventDrivenAbort",
		Children: []evolution.SerializableNode{},
	}
	cmd := BuildEventDrivenAbort(node, bb)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	ctx := btcore.NewBTContext(context.Background(), bb)
	result := cmd.Run(ctx)
	if result != 1 {
		t.Errorf("expected 1 (success) for no-children case, got %d", result)
	}
}

func TestBuildEventDrivenAbort_SuccessChild(t *testing.T) {
	bb := &Blackboard{}
	childNode := evolution.SerializableNode{
		Type: "AlwaysSucceed",
	}
	node := &evolution.SerializableNode{
		Type:     "EventDrivenAbort",
		Name:     "test_event_abort",
		Children: []evolution.SerializableNode{childNode},
	}
	cmd := BuildEventDrivenAbort(node, bb)
	ctx := btcore.NewBTContext(context.Background(), bb)
	result := cmd.Run(ctx)
	if result != 1 {
		t.Errorf("expected 1 (success), got %d", result)
	}
}

func TestBuildEventDrivenAbort_EventChannelAbort(t *testing.T) {
	bb := &Blackboard{
		ChainState: make(map[string]any),
		EventBus:   NewEventBus(),
	}
	bb.ChainState["event_bus"] = bb.EventBus
	childNode := evolution.SerializableNode{
		Type: "RunningAction", // stays running until aborted
	}
	node := &evolution.SerializableNode{
		Type: "EventDrivenAbort",
		Name: "test_abort",
		Metadata: map[string]any{
			"abort_on_match":  true,
			"propagate_event": true,
			"events": []any{
				map[string]any{
					"key":  "abort_signal",
					"type": string(EventSourceChannel),
				},
			},
		},
		Children: []evolution.SerializableNode{childNode},
	}
	cmd := BuildEventDrivenAbort(node, bb)
	ctx := btcore.NewBTContext(context.Background(), bb)

	// Get the event bus and publish an abort signal
	eb := bb.ChainState["event_bus"].(*EventBus)
	eb.Publish("abort_signal", EventMessage{
		Source:    "test_src",
		Timestamp: time.Now(),
		Type:      "abort",
		Data:      map[string]any{"reason": "timeout"},
		Priority:  0,
	})

	result := cmd.Run(ctx)
	if result != -1 {
		t.Errorf("expected -1 (aborted/failure), got %d", result)
	}
}

func TestBuildEventDrivenAbort_PropagateEventData(t *testing.T) {
	bb := &Blackboard{
		ChainState: make(map[string]any),
		EventBus:   NewEventBus(),
	}
	bb.ChainState["event_bus"] = bb.EventBus
	childNode := evolution.SerializableNode{
		Type: "RunningAction",
	}
	node := &evolution.SerializableNode{
		Type: "EventDrivenAbort",
		Name: "test_propagate",
		Metadata: map[string]any{
			"abort_on_match":  true,
			"propagate_event": true,
			"events": []any{
				map[string]any{
					"key":  "propagate_signal",
					"type": string(EventSourceChannel),
				},
			},
		},
		Children: []evolution.SerializableNode{childNode},
	}
	cmd := BuildEventDrivenAbort(node, bb)
	ctx := btcore.NewBTContext(context.Background(), bb)

	eb := bb.ChainState["event_bus"].(*EventBus)
	eb.Publish("propagate_signal", EventMessage{
		Source: "prop_src",
		Type:   "alert",
		Data:   map[string]any{"reason": "critical_failure"},
	})

	cmd.Run(ctx)
	if bb.Result != "critical_failure" {
		t.Errorf("expected Result='critical_failure', got %q", bb.Result)
	}
}

func TestBuildEventDrivenAbort_BlackboardKeyCondition(t *testing.T) {
	bb := &Blackboard{
		ChainState: map[string]any{
			"should_abort": true,
		},
	}
	childNode := evolution.SerializableNode{
		Type: "AlwaysSucceed",
	}
	node := &evolution.SerializableNode{
		Type: "EventDrivenAbort",
		Name: "test_bb_condition",
		Metadata: map[string]any{
			"abort_on_match": true,
			"events": []any{
				map[string]any{
					"key":       "should_abort",
					"type":      string(EventSourceBlackboard),
					"bool_eval": true,
				},
			},
		},
		Children: []evolution.SerializableNode{childNode},
	}
	cmd := BuildEventDrivenAbort(node, bb)
	ctx := btcore.NewBTContext(context.Background(), bb)

	result := cmd.Run(ctx)
	if result != -1 {
		t.Errorf("expected -1 (abort), got %d", result)
	}
}

func TestBuildEventDrivenAbort_BlackboardKeyNotPresent(t *testing.T) {
	bb := &Blackboard{
		ChainState: map[string]any{},
	}
	childNode := evolution.SerializableNode{
		Type: "AlwaysSucceed",
	}
	node := &evolution.SerializableNode{
		Type: "EventDrivenAbort",
		Name: "test_no_bb_key",
		Metadata: map[string]any{
			"abort_on_match": true,
			"events": []any{
				map[string]any{
					"key":       "nonexistent_key",
					"type":      string(EventSourceBlackboard),
					"bool_eval": true,
				},
			},
		},
		Children: []evolution.SerializableNode{childNode},
	}
	cmd := BuildEventDrivenAbort(node, bb)
	ctx := btcore.NewBTContext(context.Background(), bb)

	result := cmd.Run(ctx)
	if result != 1 {
		t.Errorf("expected 1 (child succeeds), got %d", result)
	}
}

func TestBuildEventDrivenAbort_LegacyEventKey(t *testing.T) {
	bb := &Blackboard{
		ChainState: make(map[string]any),
		EventBus:   NewEventBus(),
	}
	bb.ChainState["event_bus"] = bb.EventBus
	childNode := evolution.SerializableNode{
		Type: "RunningAction",
	}
	node := &evolution.SerializableNode{
		Type: "EventDrivenAbort",
		Name: "test_legacy",
		Metadata: map[string]any{
			"abort_on_match": true,
			"event_key":      "legacy_signal",
			"event_type":     string(EventSourceChannel),
		},
		Children: []evolution.SerializableNode{childNode},
	}
	cmd := BuildEventDrivenAbort(node, bb)
	ctx := btcore.NewBTContext(context.Background(), bb)

	eb := bb.ChainState["event_bus"].(*EventBus)
	eb.Publish("legacy_signal", EventMessage{
		Source: "legacy",
		Type:   "legacy_event",
	})

	result := cmd.Run(ctx)
	if result != -1 {
		t.Errorf("expected -1 (abort), got %d", result)
	}
}

// ----- parseEventSources tests -----

func TestParseEventSources_NilMetadata(t *testing.T) {
	node := &evolution.SerializableNode{
		Metadata: nil,
	}
	sources := parseEventSources(node)
	if sources != nil {
		t.Errorf("expected nil, got %v", sources)
	}
}

func TestParseEventSources_Empty(t *testing.T) {
	node := &evolution.SerializableNode{
		Metadata: map[string]any{},
	}
	sources := parseEventSources(node)
	if sources != nil {
		t.Errorf("expected nil, got %v", sources)
	}
}

func TestParseEventSources_List(t *testing.T) {
	node := &evolution.SerializableNode{
		Metadata: map[string]any{
			"events": []any{
				map[string]any{
					"key":       "event1",
					"type":      "channel",
					"predicate": "> 0",
					"bool_eval": true,
				},
				map[string]any{
					"key":  "event2",
					"type": "blackboard_key",
				},
			},
		},
	}
	sources := parseEventSources(node)
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}
	if sources[0].Key != "event1" {
		t.Errorf("expected source[0].Key='event1', got %q", sources[0].Key)
	}
	if sources[0].Type != EventSourceType("channel") {
		t.Errorf("expected source[0].Type='channel', got %q", sources[0].Type)
	}
	if sources[0].Predicate != "> 0" {
		t.Errorf("expected source[0].Predicate='> 0', got %q", sources[0].Predicate)
	}
	if !sources[0].BoolEval {
		t.Error("expected source[0].BoolEval=true")
	}
	if sources[1].Key != "event2" {
		t.Errorf("expected source[1].Key='event2', got %q", sources[1].Key)
	}
}

func TestParseEventSources_NonListRaw(t *testing.T) {
	// When 'events' is not a []interface{}, should fall through to legacy
	node := &evolution.SerializableNode{
		Metadata: map[string]any{
			"events": "not_a_list",
		},
	}
	sources := parseEventSources(node)
	if sources != nil {
		t.Errorf("expected nil fallthrough, got %v", sources)
	}
}

func TestParseEventSources_InvalidItemInEvents(t *testing.T) {
	node := &evolution.SerializableNode{
		Metadata: map[string]any{
			"events": []any{
				"not_a_map",
				map[string]any{
					"key":  "valid",
					"type": "channel",
				},
			},
		},
	}
	sources := parseEventSources(node)
	if len(sources) != 1 {
		t.Fatalf("expected 1 valid source, got %d", len(sources))
	}
	if sources[0].Key != "valid" {
		t.Errorf("expected key='valid', got %q", sources[0].Key)
	}
}

func TestParseEventSources_LegacyOnly(t *testing.T) {
	node := &evolution.SerializableNode{
		Metadata: map[string]any{
			"event_key":  "legacy_key",
			"event_type": "channel",
		},
	}
	sources := parseEventSources(node)
	if len(sources) != 1 {
		t.Fatalf("expected 1 legacy source, got %d", len(sources))
	}
	if sources[0].Key != "legacy_key" {
		t.Errorf("expected key='legacy_key', got %q", sources[0].Key)
	}
	if sources[0].Type != EventSourceType("channel") {
		t.Errorf("expected type='channel', got %q", sources[0].Type)
	}
}

// ----- evaluateEventCondition tests -----

func TestEvaluateEventCondition_NilChainState(t *testing.T) {
	result := evaluateEventCondition(EventSource{Key: "test", Type: EventSourceBlackboard}, &Blackboard{})
	if result {
		t.Error("expected false when ChainState is nil")
	}
}

func TestEvaluateEventCondition_KeyNotExists(t *testing.T) {
	result := evaluateEventCondition(EventSource{Key: "missing", Type: EventSourceBlackboard}, &Blackboard{
		ChainState: map[string]any{"existing": true},
	})
	if result {
		t.Error("expected false for missing key")
	}
}

func TestEvaluateEventCondition_BoolEvalTrue(t *testing.T) {
	result := evaluateEventCondition(EventSource{
		Key:      "flag",
		BoolEval: true,
	}, &Blackboard{
		ChainState: map[string]any{"flag": true},
	})
	if !result {
		t.Error("expected true for bool eval with true value")
	}
}

func TestEvaluateEventCondition_BoolEvalFalse(t *testing.T) {
	result := evaluateEventCondition(EventSource{
		Key:      "flag",
		BoolEval: true,
	}, &Blackboard{
		ChainState: map[string]any{"flag": false},
	})
	if result {
		t.Error("expected false for bool eval with false value")
	}
}

func TestEvaluateEventCondition_BoolEvalNonBool(t *testing.T) {
	result := evaluateEventCondition(EventSource{
		Key:      "counter",
		BoolEval: true,
	}, &Blackboard{
		ChainState: map[string]any{"counter": 42},
	})
	if result {
		t.Error("expected false for non-bool value with BoolEval")
	}
}

func TestEvaluateEventCondition_NoPredicate(t *testing.T) {
	result := evaluateEventCondition(EventSource{
		Key:  "status",
		Type: EventSourceBlackboard,
	}, &Blackboard{
		ChainState: map[string]any{"status": "active"},
	})
	if !result {
		t.Error("expected true when predicate is empty and key exists")
	}
}

func TestEvaluateEventCondition_PredicateLTEZero(t *testing.T) {
	result := evaluateEventCondition(EventSource{
		Key:       "count",
		Predicate: "<= 0",
	}, &Blackboard{
		ChainState: map[string]any{"count": float64(-1)},
	})
	if !result {
		t.Error("expected true for count <= 0 with -1")
	}
}

func TestEvaluateEventCondition_PredicateGTZero(t *testing.T) {
	result := evaluateEventCondition(EventSource{
		Key:       "count",
		Predicate: "> 0",
	}, &Blackboard{
		ChainState: map[string]any{"count": float64(5)},
	})
	if !result {
		t.Error("expected true for count > 0 with 5")
	}
}

func TestEvaluateEventCondition_PredicateGTZeroFalse(t *testing.T) {
	result := evaluateEventCondition(EventSource{
		Key:       "count",
		Predicate: "> 0",
	}, &Blackboard{
		ChainState: map[string]any{"count": float64(0)},
	})
	if result {
		t.Error("expected false for count > 0 with 0")
	}
}

func TestEvaluateEventCondition_PredicateLTZero(t *testing.T) {
	result := evaluateEventCondition(EventSource{
		Key:       "count",
		Predicate: "< 0",
	}, &Blackboard{
		ChainState: map[string]any{"count": float64(-5)},
	})
	if !result {
		t.Error("expected true for count < 0 with -5")
	}
}

func TestEvaluateEventCondition_PredicateGTEZero(t *testing.T) {
	result := evaluateEventCondition(EventSource{
		Key:       "count",
		Predicate: ">= 0",
	}, &Blackboard{
		ChainState: map[string]any{"count": float64(0)},
	})
	if !result {
		t.Error("expected true for count >= 0 with 0")
	}
}

func TestEvaluateEventCondition_PredicateEQZero(t *testing.T) {
	result := evaluateEventCondition(EventSource{
		Key:       "count",
		Predicate: "== 0",
	}, &Blackboard{
		ChainState: map[string]any{"count": float64(0)},
	})
	if !result {
		t.Error("expected true for count == 0 with 0")
	}
}

func TestEvaluateEventCondition_NonFloatValue(t *testing.T) {
	result := evaluateEventCondition(EventSource{
		Key:       "status",
		Predicate: "> 0",
	}, &Blackboard{
		ChainState: map[string]any{"status": "active"},
	})
	if result {
		t.Error("expected false when value can't be converted to float64")
	}
}

func TestEvaluateEventCondition_IntValue(t *testing.T) {
	result := evaluateEventCondition(EventSource{
		Key:       "count",
		Predicate: "> 0",
	}, &Blackboard{
		ChainState: map[string]any{"count": 3},
	})
	if !result {
		t.Error("expected true for int value 3 with > 0")
	}
}

func TestEvaluateEventCondition_BoolAsFloat(t *testing.T) {
	// true → 1, which is > 0
	result := evaluateEventCondition(EventSource{
		Key:       "flag",
		Predicate: "> 0",
	}, &Blackboard{
		ChainState: map[string]any{"flag": true},
	})
	if !result {
		t.Error("expected true for bool true as float (1 > 0)")
	}

	// false → 0, which is == 0
	result2 := evaluateEventCondition(EventSource{
		Key:       "flag",
		Predicate: "== 0",
	}, &Blackboard{
		ChainState: map[string]any{"flag": false},
	})
	if !result2 {
		t.Error("expected true for bool false as float (0 == 0)")
	}
}

func TestEvaluateEventCondition_UnsupportedPredicate(t *testing.T) {
	result := evaluateEventCondition(EventSource{
		Key:       "count",
		Predicate: "!= 0",
	}, &Blackboard{
		ChainState: map[string]any{"count": float64(5)},
	})
	if result {
		t.Error("expected false for unsupported predicate")
	}
}

// ----- valAsFloat64 tests -----

func TestValAsFloat64_Float64(t *testing.T) {
	v, ok := valAsFloat64(float64(3.14))
	if !ok || v != 3.14 {
		t.Errorf("expected 3.14, true; got %v, %v", v, ok)
	}
}

func TestValAsFloat64_Int(t *testing.T) {
	v, ok := valAsFloat64(42)
	if !ok || v != 42.0 {
		t.Errorf("expected 42.0, true; got %v, %v", v, ok)
	}
}

func TestValAsFloat64_Int64(t *testing.T) {
	v, ok := valAsFloat64(int64(99))
	if !ok || v != 99.0 {
		t.Errorf("expected 99.0, true; got %v, %v", v, ok)
	}
}

func TestValAsFloat64_BoolTrue(t *testing.T) {
	v, ok := valAsFloat64(true)
	if !ok || v != 1.0 {
		t.Errorf("expected 1.0, true; got %v, %v", v, ok)
	}
}

func TestValAsFloat64_BoolFalse(t *testing.T) {
	v, ok := valAsFloat64(false)
	if !ok || v != 0.0 {
		t.Errorf("expected 0.0, true; got %v, %v", v, ok)
	}
}

func TestValAsFloat64_String(t *testing.T) {
	v, ok := valAsFloat64("not_a_number")
	if ok {
		t.Errorf("expected false for string, got %v", v)
	}
	if v != 0 {
		t.Errorf("expected v=0 for error case, got %v", v)
	}
}

func TestValAsFloat64_Nil(t *testing.T) {
	v, ok := valAsFloat64(nil)
	if ok {
		t.Errorf("expected false for nil, got %v", v)
	}
	if v != 0 {
		t.Errorf("expected v=0 for error case, got %v", v)
	}
}

func TestFromMap_NonMap(t *testing.T) {
	node := &evolution.SerializableNode{
		Metadata: map[string]any{
			"events": []any{
				"just_a_string",
			},
		},
	}
	// Should not panic; item is not a map so it's skipped
	sources := parseEventSources(node)
	if len(sources) != 0 {
		t.Errorf("expected 0 sources for non-map items, got %d", len(sources))
	}
}

func TestBuildEventDrivenAbort_NilChainStateInit(t *testing.T) {
	bb := &Blackboard{
		ChainState: nil,
	}
	childNode := evolution.SerializableNode{
		Type: "AlwaysSucceed",
	}
	node := &evolution.SerializableNode{
		Type:     "EventDrivenAbort",
		Children: []evolution.SerializableNode{childNode},
	}
	cmd := BuildEventDrivenAbort(node, bb)
	ctx := btcore.NewBTContext(context.Background(), bb)
	result := cmd.Run(ctx)
	if result != 1 {
		t.Errorf("expected 1 (success), got %d", result)
	}
	// ChainState should have been initialized
	if bb.ChainState == nil {
		t.Error("ChainState should be initialized")
	}
}

func TestBuildEventDrivenAbort_NoAbortNoMessage(t *testing.T) {
	bb := &Blackboard{
		ChainState: make(map[string]any),
	}
	childNode := evolution.SerializableNode{
		Type: "AlwaysSucceed",
	}
	node := &evolution.SerializableNode{
		Type: "EventDrivenAbort",
		Name: "test_no_abort",
		Metadata: map[string]any{
			"abort_on_match": false,
			"events": []any{
				map[string]any{
					"key":  "never_published",
					"type": string(EventSourceBlackboard),
				},
			},
		},
		Children: []evolution.SerializableNode{childNode},
	}
	cmd := BuildEventDrivenAbort(node, bb)
	ctx := btcore.NewBTContext(context.Background(), bb)
	result := cmd.Run(ctx)
	if result != 1 {
		t.Errorf("expected 1 (child success), got %d", result)
	}
}

func TestBuildEventDrivenAbort_RunningChildAndEventFired(t *testing.T) {
	bb := &Blackboard{
		ChainState: make(map[string]any),
		EventBus:   NewEventBus(),
	}
	bb.ChainState["event_bus"] = bb.EventBus
	childNode := evolution.SerializableNode{
		Type: "RunningAction",
	}
	node := &evolution.SerializableNode{
		Type: "EventDrivenAbort",
		Name: "test_running_abort",
		Metadata: map[string]any{
			"abort_on_match": true,
			"events": []any{
				map[string]any{
					"key":  "running_abort",
					"type": string(EventSourceChannel),
				},
			},
		},
		Children: []evolution.SerializableNode{childNode},
	}
	cmd := BuildEventDrivenAbort(node, bb)
	ctx := btcore.NewBTContext(context.Background(), bb)

	// First tick without event — child runs but returns 0 (running)
	// Then publish event and check on second tick
	eb := bb.ChainState["event_bus"].(*EventBus)
	eb.Publish("running_abort", EventMessage{
		Source: "src",
		Type:   "running_abort_event",
	})

	result := cmd.Run(ctx)
	if result != -1 {
		t.Errorf("expected -1 (abort), got %d", result)
	}
}
