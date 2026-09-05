package engine

import "testing"

// ─── Characterization tests for conditions_superpowers.go ───
//
// These tests pin the current exported (registry) behavior of every
// condition registered by conditions_superpowers.go's registerSuperpowersConditions().
// Conditions are looked up via GetCondition (the same registry-first path
// conditionForName uses in production), so a test failure here reflects a
// real change to the registered predicate.

// superpowersCondCase is one assertion against a single registered condition.
type superpowersCondCase struct {
	name string
	cond string
	bb   *Blackboard
	want bool
}

// runSuperpowersCondCases resolves each case's condition and compares
// against the current registered behavior.
func runSuperpowersCondCases(t *testing.T, cases []superpowersCondCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fn := GetCondition(c.cond)
			if fn == nil {
				t.Fatalf("GetCondition(%q) returned nil; expected it to be registered by conditions_superpowers.go", c.cond)
			}
			if got := fn(c.bb); got != c.want {
				t.Errorf("%s(%+v) = %v, want %v", c.cond, c.bb, got, c.want)
			}
		})
	}
}

func TestConditionsSuperpowers_IsCreativeTask(t *testing.T) {
	runSuperpowersCondCases(t, []superpowersCondCase{
		{"build_keyword", "IsCreativeTask", &Blackboard{Task: "build a new dashboard"}, true},
		{"create_keyword", "IsCreativeTask", &Blackboard{Task: "create a widget"}, true},
		{"implement_keyword", "IsCreativeTask", &Blackboard{Task: "implement the feature"}, true},
		{"design_keyword", "IsCreativeTask", &Blackboard{Task: "design the schema"}, true},
		{"feature_keyword", "IsCreativeTask", &Blackboard{Task: "ship this feature"}, true},
		{"add_keyword", "IsCreativeTask", &Blackboard{Task: "add a button"}, true},
		{"make_keyword", "IsCreativeTask", &Blackboard{Task: "make it faster"}, true},
		{"develop_keyword", "IsCreativeTask", &Blackboard{Task: "develop the module"}, true},
		{"write_keyword", "IsCreativeTask", &Blackboard{Task: "write a parser"}, true},
		{"refactor_keyword", "IsCreativeTask", &Blackboard{Task: "refactor the engine"}, true},
		{"no_keyword", "IsCreativeTask", &Blackboard{Task: "run the tests"}, false},
		{"empty_task", "IsCreativeTask", &Blackboard{Task: ""}, false},
		// Matching is case-insensitive: bb.Task is lowercased before the
		// substring check.
		{"case_insensitive", "IsCreativeTask", &Blackboard{Task: "BUILD a new dashboard"}, true},
		// Substring match, not word-boundary: "add" inside "address" counts.
		{"substring_not_word_boundary", "IsCreativeTask", &Blackboard{Task: "update the address book"}, true},
	})
}

func TestConditionsSuperpowers_DesignApproved(t *testing.T) {
	runSuperpowersCondCases(t, []superpowersCondCase{
		{"approved_true", "DesignApproved", &Blackboard{ChainState: map[string]any{"design_approved": true}}, true},
		{"approved_false", "DesignApproved", &Blackboard{ChainState: map[string]any{"design_approved": false}}, false},
		{"missing_key", "DesignApproved", &Blackboard{ChainState: map[string]any{}}, false},
		{"nil_chain_state", "DesignApproved", &Blackboard{}, false},
		// Wrong value type fails the type assertion and is treated as false,
		// not as an error.
		{"wrong_type", "DesignApproved", &Blackboard{ChainState: map[string]any{"design_approved": "yes"}}, false},
	})
}

func TestConditionsSuperpowers_WorktreeReady(t *testing.T) {
	runSuperpowersCondCases(t, []superpowersCondCase{
		{"ready_true", "WorktreeReady", &Blackboard{ChainState: map[string]any{"worktree_ready": true}}, true},
		{"ready_false", "WorktreeReady", &Blackboard{ChainState: map[string]any{"worktree_ready": false}}, false},
		{"missing_key", "WorktreeReady", &Blackboard{ChainState: map[string]any{}}, false},
		{"nil_chain_state", "WorktreeReady", &Blackboard{}, false},
		{"wrong_type", "WorktreeReady", &Blackboard{ChainState: map[string]any{"worktree_ready": 1}}, false},
	})
}

func TestConditionsSuperpowers_DesignExists(t *testing.T) {
	runSuperpowersCondCases(t, []superpowersCondCase{
		{"non_empty_path", "DesignExists", &Blackboard{ChainState: map[string]any{"design_path": "docs/design.md"}}, true},
		{"empty_path", "DesignExists", &Blackboard{ChainState: map[string]any{"design_path": ""}}, false},
		{"missing_key", "DesignExists", &Blackboard{ChainState: map[string]any{}}, false},
		{"nil_chain_state", "DesignExists", &Blackboard{}, false},
		{"wrong_type", "DesignExists", &Blackboard{ChainState: map[string]any{"design_path": 42}}, false},
	})
}

func TestConditionsSuperpowers_PlanReady(t *testing.T) {
	runSuperpowersCondCases(t, []superpowersCondCase{
		{"ready_true", "PlanReady", &Blackboard{ChainState: map[string]any{"plan_ready": true}}, true},
		{"ready_false", "PlanReady", &Blackboard{ChainState: map[string]any{"plan_ready": false}}, false},
		{"missing_key", "PlanReady", &Blackboard{ChainState: map[string]any{}}, false},
		{"nil_chain_state", "PlanReady", &Blackboard{}, false},
		{"wrong_type", "PlanReady", &Blackboard{ChainState: map[string]any{"plan_ready": "true"}}, false},
	})
}

func TestConditionsSuperpowers_HITLAlreadyApproved(t *testing.T) {
	runSuperpowersCondCases(t, []superpowersCondCase{
		{"approved_true", "HITLAlreadyApproved", &Blackboard{ChainState: map[string]any{"hitl_approved": true}}, true},
		{"approved_false", "HITLAlreadyApproved", &Blackboard{ChainState: map[string]any{"hitl_approved": false}}, false},
		{"missing_key", "HITLAlreadyApproved", &Blackboard{ChainState: map[string]any{}}, false},
		{"nil_chain_state", "HITLAlreadyApproved", &Blackboard{}, false},
		{"wrong_type", "HITLAlreadyApproved", &Blackboard{ChainState: map[string]any{"hitl_approved": 0}}, false},
	})
}

func TestConditionsSuperpowers_VerificationFailed(t *testing.T) {
	runSuperpowersCondCases(t, []superpowersCondCase{
		// No results recorded yet — the condition conservatively assumes
		// failure so the retry gate does not skip verification.
		{"no_results_key_assumes_failed", "VerificationFailed", &Blackboard{ChainState: map[string]any{}}, true},
		{"nil_chain_state_assumes_failed", "VerificationFailed", &Blackboard{}, true},
		{"wrong_type_assumes_failed", "VerificationFailed", &Blackboard{ChainState: map[string]any{"verification_results": "not-a-map"}}, true},
		{"all_passed", "VerificationFailed", &Blackboard{ChainState: map[string]any{"verification_results": map[string]bool{"lint": true, "test": true}}}, false},
		{"one_failed", "VerificationFailed", &Blackboard{ChainState: map[string]any{"verification_results": map[string]bool{"lint": true, "test": false}}}, true},
		{"empty_results_map", "VerificationFailed", &Blackboard{ChainState: map[string]any{"verification_results": map[string]bool{}}}, false},
	})
}

func TestConditionsSuperpowers_CheckIndexInRange(t *testing.T) {
	runSuperpowersCondCases(t, []superpowersCondCase{
		// []map[string]string is the only slice type the primary type
		// assertion recognizes; the index is compared against its length.
		{
			"typed_slice_in_range", "CheckIndexInRange",
			&Blackboard{ChainState: map[string]any{
				"current_task_index": 1,
				"task_batch":         []map[string]string{{"id": "a"}, {"id": "b"}, {"id": "c"}},
			}}, true,
		},
		{
			"typed_slice_out_of_range", "CheckIndexInRange",
			&Blackboard{ChainState: map[string]any{
				"current_task_index": 5,
				"task_batch":         []map[string]string{{"id": "a"}, {"id": "b"}},
			}}, false,
		},
		// Missing task_batch entirely and no size: idx (defaults to 0 via
		// the failed type assertion) compared against size 0 -> false.
		{
			"missing_task_batch_no_size", "CheckIndexInRange",
			&Blackboard{ChainState: map[string]any{}}, false,
		},
		// Falls back to task_batch_size when task_batch isn't the recognized
		// []map[string]string type.
		{
			"falls_back_to_size_when_untyped", "CheckIndexInRange",
			&Blackboard{ChainState: map[string]any{
				"current_task_index": 2,
				"task_batch_size":    5,
			}}, true,
		},
		{
			"falls_back_to_size_out_of_range", "CheckIndexInRange",
			&Blackboard{ChainState: map[string]any{
				"current_task_index": 9,
				"task_batch_size":    5,
			}}, false,
		},
		// task_batch stored as []interface{} (e.g. from JSON decoding) must
		// still be range-checked against its real length — the "Try
		// interface slice" fallback branch exists precisely for this case.
		{
			"interface_slice_in_range", "CheckIndexInRange",
			&Blackboard{ChainState: map[string]any{
				"current_task_index": 1,
				"task_batch":         []any{"a", "b", "c", "d", "e"},
			}}, true,
		},
		{
			"nil_chain_state", "CheckIndexInRange",
			&Blackboard{}, false,
		},
	})
}
