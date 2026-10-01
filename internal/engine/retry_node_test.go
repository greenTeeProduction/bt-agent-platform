package engine

import (
	"errors"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestRetryRetriesFailuresWithoutRepeatingSuccess(t *testing.T) {
	for _, test := range []struct {
		name     string
		statuses []int
		calls    int
		result   int
	}{
		{"success_once", []int{1}, 1, 1},
		{"recovers", []int{-1, -1, 1}, 3, 1},
		{"exhausted", []int{-1, -1, -1, 1}, 3, -1},
		{"running_attempt", []int{0, -1, 1}, 3, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			name := "RetryContract_" + test.name
			RegisterAction(name, func(*btcore.BTContext[Blackboard]) int {
				i := calls
				calls++
				return test.statuses[min(i, len(test.statuses)-1)]
			})
			bb := &Blackboard{}
			tree := BuildTree(&evolution.SerializableNode{Type: "Retry", Name: "bounded", MaxRetries: 3, Children: []evolution.SerializableNode{{Type: "Action", Name: name}}}, bb)
			ctx := btcore.NewBTContext(t.Context(), bb)
			result := tree.Run(ctx)
			if result == 0 {
				result = tree.Run(ctx)
			}
			if calls != test.calls || result != test.result {
				t.Fatalf("calls=%d result=%d; want %d/%d", calls, result, test.calls, test.result)
			}
		})
	}
}

func TestRetryPreservesTerminalStop(t *testing.T) {
	bb := &Blackboard{}
	calls := 0
	RegisterAction("RetryContract_Stop", func(ctx *btcore.BTContext[Blackboard]) int {
		calls++
		ctx.Blackboard.stopExecution("approval required", &reliability.ExecutionStoppedError{Outcome: "input-required", Err: errors.New("approval required")})
		return -1
	})
	tree := BuildTree(&evolution.SerializableNode{Type: "Retry", Name: "bounded", MaxRetries: 3, Children: []evolution.SerializableNode{{Type: "Action", Name: "RetryContract_Stop"}}}, bb)
	if result := tree.Run(btcore.NewBTContext(t.Context(), bb)); result != -1 || calls != 1 || bb.Outcome != "input-required" {
		t.Fatalf("terminal stop retried: result=%d calls=%d outcome=%s", result, calls, bb.Outcome)
	}
}
