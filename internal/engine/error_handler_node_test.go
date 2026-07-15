// internal/engine/error_handler_node_test.go
package engine

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

var ehTestRecoverRan atomic.Int64

func init() {
	RegisterAction("eh_test_failing_action", func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		if b.ChainState == nil {
			b.ChainState = map[string]any{}
		}
		b.ChainState["last_error_category"] = "testcat"
		b.ChainState["last_error_node"] = "eh_test_failing_action"
		b.ChainState["last_error"] = "synthetic failure 42"
		return -1
	})
	RegisterAction("eh_test_recover_action", func(ctx *btcore.BTContext[Blackboard]) int {
		ehTestRecoverRan.Add(1)
		return 1
	})
}

func ehTestHandlerNode() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "ClaudeErrorHandler",
		Name: "eh_test_tree_ErrorHandler",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "eh_test_failing_action"},
		},
	}
}

func ehTestProposalJSON(t *testing.T) string {
	t.Helper()
	prop := map[string]any{
		"resolvable": true,
		"reason":     "guarded recovery",
		"node": map[string]any{
			"type": "Sequence", "name": "Handle_testcat",
			"children": []map[string]any{
				{"type": "Condition", "name": "LastErrorCategoryIs:testcat"},
				{"type": "Action", "name": "eh_test_recover_action"},
			},
		},
	}
	data, err := json.Marshal(prop)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func runHandler(t *testing.T, bb *Blackboard) int {
	t.Helper()
	cmd := BuildClaudeErrorHandler(ehTestHandlerNode(), bb)
	return cmd.Run(&btcore.BTContext[Blackboard]{Blackboard: bb})
}

func TestClaudeErrorHandler_ProposalGraftedAndTicked(t *testing.T) {
	withTempErrorHandlerDir(t)
	fake := &fakeClaudeRunner{output: ehTestProposalJSON(t)}
	swapErrorHandlerRunner(t, fake)
	ehTestRecoverRan.Store(0)

	bb := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb); code != 1 {
		t.Fatalf("recovered run must return 1, got %d", code)
	}
	if fake.calls.Load() != 1 {
		t.Fatalf("exactly one Claude call, got %d", fake.calls.Load())
	}
	if ehTestRecoverRan.Load() != 1 {
		t.Fatal("recovery action must have ticked")
	}
	if sig, _ := bb.ChainState["error_handler_recovered"].(string); sig == "" {
		t.Fatal("recovery must stamp error_handler_recovered")
	}
	if !strings.Contains(bb.Result, "## Error Handler Recovery") {
		t.Fatal("recovery must append a Result note")
	}
	if bb.OutcomeRefinement != "" {
		t.Fatal("recovery must NOT set OutcomeRefinement (runner dead-letters novel refinements)")
	}
	exts := loadErrorHandlerExtensions("eh_test_tree_ErrorHandler")
	if len(exts) != 1 || exts[0].Node.Name != "Handle_testcat" || exts[0].Successes != 1 {
		t.Fatalf("persisted extension = %+v", exts)
	}
}

func TestClaudeErrorHandler_GraftedExtensionHandlesNextRunWithoutClaude(t *testing.T) {
	withTempErrorHandlerDir(t)
	fake := &fakeClaudeRunner{output: ehTestProposalJSON(t)}
	swapErrorHandlerRunner(t, fake)
	bb := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb); code != 1 {
		t.Fatal("first run must recover")
	}
	// Fresh build (simulates the next scheduled run): extension grafted from
	// the store, error handled with ZERO further Claude calls.
	bb2 := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb2); code != 1 {
		t.Fatal("second run must recover via the grafted extension")
	}
	if fake.calls.Load() != 1 {
		t.Fatalf("no second Claude call expected, got %d", fake.calls.Load())
	}
}

func TestClaudeErrorHandler_UnresolvableStampsCooldownAndPassesFailureThrough(t *testing.T) {
	withTempErrorHandlerDir(t)
	fake := &fakeClaudeRunner{output: `{"resolvable": false, "reason": "needs new Go action"}`}
	swapErrorHandlerRunner(t, fake)
	bb := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb); code != -1 {
		t.Fatal("unresolvable must pass the failure through")
	}
	// Same error again within cooldown: no second Claude call.
	bb2 := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb2); code != -1 {
		t.Fatal("still failing")
	}
	if fake.calls.Load() != 1 {
		t.Fatalf("cooldown must suppress the second call, got %d", fake.calls.Load())
	}
}

func TestClaudeErrorHandler_InvalidProposalRejected(t *testing.T) {
	withTempErrorHandlerDir(t)
	fake := &fakeClaudeRunner{output: `{"resolvable": true, "node": {"type": "Action", "name": "not_registered_anywhere"}}`}
	swapErrorHandlerRunner(t, fake)
	bb := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb); code != -1 {
		t.Fatal("invalid proposal must fail through")
	}
	if len(loadErrorHandlerExtensions("eh_test_tree_ErrorHandler")) != 0 {
		t.Fatal("rejected proposal must not be persisted")
	}
	if entry, ok := errorHandlerLedgerGet(errorHandlerSignatureFromBB(bb, "eh_test_tree_ErrorHandler")); !ok || entry.LastVerdict != "rejected" {
		t.Fatalf("ledger verdict = %+v ok=%v, want rejected", entry, ok)
	}
}

func TestClaudeErrorHandler_KillSwitch(t *testing.T) {
	withTempErrorHandlerDir(t)
	t.Setenv("BT_CLAUDE_ERROR_HANDLER", "off")
	fake := &fakeClaudeRunner{output: ehTestProposalJSON(t)}
	swapErrorHandlerRunner(t, fake)
	bb := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb); code != -1 {
		t.Fatal("kill switch must pass failure through")
	}
	if fake.calls.Load() != 0 {
		t.Fatal("kill switch must prevent Claude calls")
	}
}

func TestClaudeErrorHandler_CapReachedSkipsClaude(t *testing.T) {
	withTempErrorHandlerDir(t)
	t.Setenv("BT_ERROR_HANDLER_MAX_NODES", "1")
	// Pre-seed one active extension whose guard does NOT match this error, so
	// it can't recover — with the cap at 1, no Claude call may follow.
	seed := ErrorHandlerExtension{Node: evolution.SerializableNode{
		Type: "Sequence", Name: "Handle_othercat",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "LastErrorCategoryIs:othercat"},
			{Type: "Action", Name: "eh_test_recover_action"},
		},
	}, Signature: "seedsig000000"}
	if err := appendErrorHandlerExtension("eh_test_tree_ErrorHandler", seed); err != nil {
		t.Fatal(err)
	}
	fake := &fakeClaudeRunner{output: ehTestProposalJSON(t)}
	swapErrorHandlerRunner(t, fake)
	bb := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb); code != -1 {
		t.Fatal("cap reached with no matching recovery must fail through")
	}
	if fake.calls.Load() != 0 {
		t.Fatalf("cap must suppress the Claude call, got %d", fake.calls.Load())
	}
}

func TestClaudeErrorHandler_SandboxPassthrough(t *testing.T) {
	withTempErrorHandlerDir(t)
	fake := &fakeClaudeRunner{output: ehTestProposalJSON(t)}
	swapErrorHandlerRunner(t, fake)
	bb := &Blackboard{Sandbox: true, ChainState: map[string]any{}}
	code := runHandler(t, bb)
	if fake.calls.Load() != 0 {
		t.Fatal("sandbox mode must never call Claude")
	}
	if code != 1 { // sandbox stubs all actions to success
		t.Fatalf("sandbox passthrough code = %d", code)
	}
}

func TestClaudeErrorHandler_SuccessPassthroughUntouched(t *testing.T) {
	withTempErrorHandlerDir(t)
	fake := &fakeClaudeRunner{output: ehTestProposalJSON(t)}
	swapErrorHandlerRunner(t, fake)
	node := &evolution.SerializableNode{
		Type: "ClaudeErrorHandler", Name: "ok_ErrorHandler",
		Children: []evolution.SerializableNode{{Type: "AlwaysSucceed", Name: "ok"}},
	}
	bb := &Blackboard{ChainState: map[string]any{}}
	cmd := BuildClaudeErrorHandler(node, bb)
	if code := cmd.Run(&btcore.BTContext[Blackboard]{Blackboard: bb}); code != 1 {
		t.Fatal("success must pass through")
	}
	if fake.calls.Load() != 0 {
		t.Fatal("no Claude on success")
	}
}

func TestClaudeErrorHandler_BuildSwitchAndValidation(t *testing.T) {
	withTempErrorHandlerDir(t)
	if !evolution.KnownNodeTypes["ClaudeErrorHandler"] {
		t.Fatal("ClaudeErrorHandler must be in KnownNodeTypes")
	}
	bb := &Blackboard{ChainState: map[string]any{}}
	if _, err := BuildAndValidate(ehTestHandlerNode(), bb); err != nil {
		t.Fatalf("BuildAndValidate must accept the node type: %v", err)
	}
}
