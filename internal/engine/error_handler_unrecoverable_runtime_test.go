package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func init() {
	RegisterAction("eh_test_runtime_fault", func(ctx *btcore.BTContext[Blackboard]) int { return -1 })
}

// A node-name guard must not bypass the category policy. In production a
// SelfCorrect recovery masked a dirty build-tree preflight without repairing it.
func TestClaudeErrorHandler_UnrecoverableFaultCannotUseNodeGuard(t *testing.T) {
	for _, tc := range []struct{ name, category, result string }{
		{"disk", "resource_exhausted", "no space left on device"},
		{"auth", "auth", "credentials expired"},
		{"drift", "working_tree_drift", "build tree differs"},
		{"unclassified preflight", "", "## Scheduled GOAP Fusion Build Tree Preflight Failed\n\nThe on-disk build tree in `/repo` is not materialized to HEAD; the following tracked file(s) differ from the committed HEAD and would be compiled stale, so the deployed binary would not match HEAD:\n\ninternal/blackboard/manager.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withTempErrorHandlerDir(t)
			t.Setenv("BT_CLAUDE_ERROR_HANDLER", "on")
			fake := &fakeClaudeRunner{}
			swapErrorHandlerRunner(t, fake)
			allowErrorHandlerTestActions(t, "eh_test_recover_action")
			node := &evolution.SerializableNode{Type: "ClaudeErrorHandler", Name: "runtime_policy", Children: []evolution.SerializableNode{{Type: "Action", Name: "eh_test_runtime_fault"}}}

			ext := ErrorHandlerExtension{Node: evolution.SerializableNode{Type: "Sequence", Name: "NodeGuardRecovery", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "LastErrorNodeIs:eh_test_runtime_fault"},
				{Type: "Action", Name: "eh_test_recover_action"},
			}}}
			if err := appendErrorHandlerExtension(node.Name, ext); err != nil {
				t.Fatal(err)
			}
			ehTestRecoverRan.Store(0)
			bb := &Blackboard{Result: tc.result, ChainState: map[string]any{"last_error_category": tc.category, "last_error_node": "eh_test_runtime_fault"}}
			if code := BuildClaudeErrorHandler(node, bb).Run(&btcore.BTContext[Blackboard]{Blackboard: bb}); code != -1 {
				t.Errorf("unrepaired fault became success: %d", code)
			}
			if ehTestRecoverRan.Load() != 0 {
				t.Error("recovery executed for an externally repairable fault")
			}
			if _, ok := bb.ChainState["error_handler_recovered"]; ok {
				t.Error("false recovery stamped")
			}
			if fake.calls.Load() != 0 {
				t.Error("external repair must not spend a new proposal call")
			}
			if bb.Result != tc.result {
				t.Error("original failure evidence was hidden")
			}
		})
	}
}

func TestErrorHandlerProposalRejectsWorkingTreeDrift(t *testing.T) {
	node := &evolution.SerializableNode{Type: "Sequence", Name: "DriftRecovery", Children: []evolution.SerializableNode{
		{Type: "Condition", Name: "LastErrorCategoryIs:working_tree_drift"},
		{Type: "Action", Name: "SelfCorrect"},
	}}
	if err := validateErrorHandlerProposal(node, map[string]bool{}); err == nil {
		t.Fatal("LLM-only action cannot repair the build tree")
	}
}
