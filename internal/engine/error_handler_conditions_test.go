package engine

import "testing"

func TestErrorHandlerConditionFor(t *testing.T) {
	cases := []struct {
		name  string
		state map[string]any
		want  bool
	}{
		{"LastErrorCategoryIs:rate_limit", map[string]any{"last_error_category": "rate_limit"}, true},
		{"LastErrorCategoryIs:rate_limit", map[string]any{"last_error_category": "timeout"}, false},
		{"LastErrorCategoryIs:rate_limit", nil, false},
		{"LastErrorCategoryIs:", map[string]any{"last_error_category": ""}, false}, // malformed spec must not pass
		{"LastErrorNodeIs:FetchData", map[string]any{"last_error_node": "FetchData"}, true},
		{"LastErrorNodeIs:FetchData", map[string]any{"last_error_node": "Other"}, false},
	}
	for _, tc := range cases {
		fn := errorHandlerConditionFor(tc.name)
		if fn == nil {
			t.Fatalf("%s: expected a condition func", tc.name)
		}
		bb := &Blackboard{ChainState: tc.state}
		if got := fn(bb); got != tc.want {
			t.Errorf("%s with %v = %v, want %v", tc.name, tc.state, got, tc.want)
		}
	}
	if errorHandlerConditionFor("SomeRegularCondition") != nil {
		t.Fatal("non-prefixed names must return nil")
	}
}

func TestConditionForName_ResolvesErrorHandlerPrefixes(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{"last_error_category": "testcat"}}
	fn := bb.conditionForName("LastErrorCategoryIs:testcat")
	if !fn(bb) {
		t.Fatal("conditionForName must route LastErrorCategoryIs: to the error-handler resolver")
	}
	// Must NOT fall through to the permissive always-true default:
	if bb.conditionForName("LastErrorCategoryIs:other")(bb) {
		t.Fatal("mismatched category must be false, not permissive-true")
	}
}
