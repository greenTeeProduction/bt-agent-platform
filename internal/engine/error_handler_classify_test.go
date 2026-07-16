package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestGoapFailureCategory(t *testing.T) {
	cases := []struct {
		name    string
		outcome string
		result  string
		want    string
	}{
		{"rate limit outcome", "goap_fusion_rate_limited", "paused", "rate_limit"},
		{"rate limit text", "", "hit the rate limit again", "rate_limit"},
		{"impl gate", "", "✗ golangci-lint found issues in staged files", "impl_gate"},
		{"infra pending_patch outcome", "pending_patch", "fast-forward refused", "infra"},
		{"infra result marker", "", "applied_uncommitted: gate rejected", "infra"},
		{"quality gate", "", "output quality failed at evidence gate", "quality_gate"},
		{"default", "", "zzq unrecognized goap failure zzq", "goap_fusion_failure"},
	}
	for _, tc := range cases {
		bb := &Blackboard{Outcome: tc.outcome, Result: tc.result}
		if got := goapFailureCategory(bb); got != tc.want {
			t.Errorf("%s: goapFailureCategory(outcome=%q,result=%q) = %q, want %q", tc.name, tc.outcome, tc.result, got, tc.want)
		}
	}
	if goapFailureCategory(nil) != "goap_fusion_failure" {
		t.Error("nil bb must be safe")
	}
}

func TestIsGoapFusionCycle(t *testing.T) {
	if !isGoapFusionCycle(&Blackboard{ChainState: map[string]any{"goap_fusion_impl_degraded": "true"}}) {
		t.Error("goap_fusion_ key must be detected")
	}
	if isGoapFusionCycle(&Blackboard{ChainState: map[string]any{"other": "x"}}) {
		t.Error("non-goap must be false")
	}
	if isGoapFusionCycle(&Blackboard{}) {
		t.Error("nil ChainState must be false")
	}
	if isGoapFusionCycle(nil) {
		t.Error("nil bb must be false")
	}
}

func TestClassifyErrorHandlerFailure(t *testing.T) {
	goap := &Blackboard{Outcome: "goap_fusion_rate_limited", ChainState: map[string]any{"goap_fusion_impl_degraded": "true"}}
	if got := classifyErrorHandlerFailure(goap); got != "rate_limit" {
		t.Errorf("goap cycle = %q, want rate_limit", got)
	}
	plain := &Blackboard{Result: "zzq opaque nondescript failure zzq", ChainState: map[string]any{}}
	if got := classifyErrorHandlerFailure(plain); got != "unclassified" {
		t.Errorf("plain opaque = %q, want unclassified", got)
	}
}

func init() {
	RegisterAction("eh_test_unclassified_failing_action", func(ctx *btcore.BTContext[Blackboard]) int {
		// Fails WITHOUT setting last_error_category/node — the unclassified case.
		ctx.Blackboard.Result = "zzq opaque nondescript failure zzq"
		return -1
	})
	RegisterAction("eh_test_goap_failing_action", func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		if b.ChainState == nil {
			b.ChainState = map[string]any{}
		}
		b.ChainState["goap_fusion_impl_degraded"] = "true" // marks a goap cycle
		b.Outcome = "goap_fusion_rate_limited"
		b.Result = "rate limited; carryover"
		return -1
	})
}

func TestClaudeErrorHandler_ClassifiesUnclassifiedFailure(t *testing.T) {
	withTempErrorHandlerDir(t)
	t.Setenv("BT_CLAUDE_ERROR_HANDLER", "off") // isolate classification from any proposal
	node := &evolution.SerializableNode{
		Type: "ClaudeErrorHandler", Name: "eh_unclassified_tree_ErrorHandler",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "eh_test_unclassified_failing_action"}},
	}
	bb := &Blackboard{ChainState: map[string]any{}}
	cmd := BuildClaudeErrorHandler(node, bb)
	if code := cmd.Run(&btcore.BTContext[Blackboard]{Blackboard: bb}); code != -1 {
		t.Fatalf("failure passes through, got %d", code)
	}
	cat, _ := bb.ChainState["last_error_category"].(string)
	if cat == "" {
		t.Fatal("handler must classify an unclassified failure (empty category → guard could never match → always unresolvable)")
	}
	if cat != "unclassified" {
		t.Errorf("non-goap opaque failure → %q, want unclassified", cat)
	}
	if n, _ := bb.ChainState["last_error_node"].(string); n == "" {
		t.Error("last_error_node must be set to the protected root")
	}
}

func TestClaudeErrorHandler_GoapFailureGetsGoapCategory(t *testing.T) {
	withTempErrorHandlerDir(t)
	t.Setenv("BT_CLAUDE_ERROR_HANDLER", "off")
	node := &evolution.SerializableNode{
		Type: "ClaudeErrorHandler", Name: "goap_fusion_loop_ErrorHandler",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "eh_test_goap_failing_action"}},
	}
	bb := &Blackboard{ChainState: map[string]any{}}
	cmd := BuildClaudeErrorHandler(node, bb)
	cmd.Run(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if cat, _ := bb.ChainState["last_error_category"].(string); cat != "rate_limit" {
		t.Fatalf("goap failure category = %q, want rate_limit", cat)
	}
}

func TestClaudeErrorHandler_DoesNotClobberExistingCategory(t *testing.T) {
	withTempErrorHandlerDir(t)
	t.Setenv("BT_CLAUDE_ERROR_HANDLER", "off")
	// ehTestHandlerNode's child (eh_test_failing_action) sets last_error_category
	// = "testcat"; the handler classifier must not overwrite it.
	bb := &Blackboard{ChainState: map[string]any{}}
	cmd := BuildClaudeErrorHandler(ehTestHandlerNode(), bb)
	cmd.Run(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if cat, _ := bb.ChainState["last_error_category"].(string); cat != "testcat" {
		t.Fatalf("existing category clobbered: got %q, want testcat", cat)
	}
}
