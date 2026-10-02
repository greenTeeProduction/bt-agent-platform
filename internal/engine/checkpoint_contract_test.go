package engine

import (
	"encoding/json"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/goap"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

func TestCheckpointUsesExplicitTypedStateAfterPersistence(t *testing.T) {
	tree := evolution.WrapWithCheckpointVerifier(&evolution.SerializableNode{Type: "AlwaysSucceed", Name: "Task"}, 1, "has_result=true,task_status=completed")
	data, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	var loaded evolution.SerializableNode
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"valid", "wrong_string", "wrong_source", "missing"} {
		t.Run(mode, func(t *testing.T) {
			bb := &Blackboard{ChainState: map[string]any{}}
			state := goap.WorldState{"has_result": true, "task_status": "completed"}
			key := "goap_world_state"
			if mode == "wrong_source" {
				key = "world_state"
			}
			if mode == "wrong_string" {
				state["task_status"] = false
			}
			if mode != "missing" {
				bb.ChainState[key] = state
			}
			command, err := BuildAndValidate(&loaded, bb)
			if err != nil {
				t.Fatal(err)
			}
			want := -1
			if mode == "valid" {
				want = 1
			}
			if got := command.Run(btcore.NewBTContext(t.Context(), bb)); got != want {
				t.Fatalf("status=%d want=%d state=%+v", got, want, bb.ChainState)
			}
		})
	}
}

func TestCheckpointKeepsAttemptSnapshotAndBudgetAcrossRunningTicks(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{"world_state": map[string]any{"count": 7, "nested": map[string]any{"original": true}}}}
	calls := 0
	child := btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		calls++
		state := ctx.Blackboard.ChainState["world_state"].(map[string]any)
		if calls%2 == 1 {
			if state["count"] != 7 || state["nested"].(map[string]any)["original"] != true {
				t.Errorf("attempt snapshot lost across Running ticks: %+v", state)
			}
			state["count"] = 9
			state["nested"].(map[string]any)["original"] = false
			return 0
		}
		return 1 // completed child, but required done is absent
	})
	gate := NewCheckpointVerifier(child, 1, map[string]any{"done": true})
	ctx := btcore.NewBTContext(t.Context(), bb)
	for i, want := range []int{0, 0, -1, -1} {
		if got := gate.Run(ctx); got != want {
			t.Fatalf("tick %d = %d want %d", i, got, want)
		}
	}
	if calls != 4 {
		t.Fatalf("retry budget reset or terminal work replayed: %d calls", calls)
	}
	if _, exists := bb.ChainState["world_state"].(map[string]any)["done"]; exists {
		t.Fatal("missing postcondition was synthesized")
	}
}

func TestCheckpointMalformedContractFailsBeforeChildAndValidation(t *testing.T) {
	for _, raw := range []any{nil, map[string]any{}, "done=true", map[string]any{"done": nil}, map[string]any{"done": []any{true}}} {
		calls := 0
		child := btleaf.NewAction(func(*btcore.BTContext[Blackboard]) int { calls++; return 1 })
		node := &evolution.SerializableNode{Type: "CheckpointVerifier", Name: "gate", Metadata: map[string]any{"postconditions": raw}, Children: []evolution.SerializableNode{{Type: "AlwaysSucceed"}}}
		gate := newCheckpointVerifier(child, node)
		if got := gate.Run(btcore.NewBTContext(t.Context(), &Blackboard{})); got != -1 || calls != 0 {
			t.Fatalf("malformed gate executed: raw=%v status=%d calls=%d", raw, got, calls)
		}
		if ValidateTreeFull(node).Valid() || len(ValidateTree(node)) == 0 {
			t.Fatalf("malformed declaration passed authoring validation: %v", raw)
		}
	}
}
