package engine

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// 2026-08-01: one minute after the eMMC filled, the handler grafted
//
//	GoapFusionResourceExhaustedHandler = Sequence[
//	    Condition LastErrorCategoryIs:resource_exhausted,
//	    SendAlert, EscalateToOperator, UpdateBlackboard ]
//
// SendAlert and EscalateToOperator only append a string to bb.Result and always
// return 1, so the Sequence always succeeded, error_handler_node.go accepted it
// as a recovery, and 24 consecutive cycles were reported success/q=0.9 on a
// 100%-full disk — breaker reset to CLOSED, alerts suppressed as routine, SLO
// 100%. Nothing about the fault changed.
//
// The gate is STATIC, at graft-validation time, not a runtime probe: a runtime
// probe reads blackboard fields that allowlisted actions (ClearNodeError ->
// recordNodeSuccess) are themselves allowed to blank, so it is bypassable by a
// single allowlisted action. A recovery composed ENTIRELY of self-declaring
// actions cannot address any fault, whatever the blackboard says afterwards.

func seq(name string, children ...evolution.SerializableNode) *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: name, Children: children}
}

func act(name string) evolution.SerializableNode {
	return evolution.SerializableNode{Type: "Action", Name: name}
}

func cond(name string) evolution.SerializableNode {
	return evolution.SerializableNode{Type: "Condition", Name: name}
}

func TestValidateErrorHandlerProposal_RejectsPurelyDeclarativeRecovery(t *testing.T) {
	cases := []struct {
		name    string
		node    *evolution.SerializableNode
		wantErr bool
	}{
		{
			// The exact node the live fleet grafted on 2026-07-31T19:08:37.
			name: "the live resource-exhausted handler is rejected",
			node: seq("GoapFusionResourceExhaustedHandler",
				cond("LastErrorCategoryIs:resource_exhausted"),
				act("SendAlert"), act("EscalateToOperator"), act("UpdateBlackboard")),
			wantErr: true,
		},
		{
			name: "clear-and-declare-success is rejected",
			node: seq("HaltRecovery",
				cond("LastErrorCategoryIs:goap_fusion_failure"),
				act("ClearNodeError"), act("MarkSuccessful")),
			wantErr: true,
		},
		{
			name: "alert-then-clear is rejected",
			node: seq("AuthErrorRecovery",
				cond("LastErrorCategoryIs:auth"),
				act("SendAlert"), act("EscalateToOperator"), act("ClearNodeError")),
			wantErr: true,
		},
		{
			// The live rate-limit backoff (successes=134) is genuinely useful:
			// HandleTransientError degrades the outcome to Partial rather than
			// merely declaring victory. It must keep validating.
			name: "rate-limit backoff with a real handler is accepted",
			node: seq("GoapFusionRateLimitBackoff",
				cond("LastErrorCategoryIs:rate_limit"),
				act("HandleTransientError"), act("UpdateBlackboard"), act("MarkSuccessful")),
			wantErr: false,
		},
		{
			name: "an LLM-backed correction is accepted",
			node: seq("SelfCorrectRecovery",
				cond("LastErrorCategoryIs:validation"),
				act("SelfCorrect"), act("MarkSuccessful")),
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateErrorHandlerProposal(tc.node, map[string]bool{})
			if tc.wantErr && err == nil {
				t.Fatalf("validateErrorHandlerProposal accepted a recovery whose every action is "+
					"self-declaring: %s — this is the node that faked 24 successes on a full disk", tc.node.Name)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateErrorHandlerProposal rejected a legitimate recovery %s: %v\n"+
					"over-rejection is not harmless: every refusal feeds the auto-disable streak",
					tc.node.Name, err)
			}
			// Two rules can refuse these nodes — the declarative-only rule and
			// the unrecoverable-category rule, whichever fires first. Either is
			// a correct and informative refusal; require only that the reason
			// names one of them, so the Warn an operator reads is actionable.
			if tc.wantErr && err != nil &&
				!strings.Contains(err.Error(), "declarative") &&
				!strings.Contains(err.Error(), "not recoverable") {
				t.Fatalf("rejection reason %q names neither the declarative-only rule nor the "+
					"unrecoverable-category rule; an operator reading the log cannot act on it", err)
			}
		})
	}
}

// The declarative gate alone is one substitution away from being defeated:
// tree.go's terminal switch rewrites Outcome to Success whenever the tree
// returns 1, so HandleTransientError's Partial is unobservable and a
// Sequence[resource_exhausted, HandleTransientError] would satisfy the
// effectful-leaf rule while doing nothing about a full disk. Categories no
// allowlisted action can touch must therefore be refused outright.
func TestValidateErrorHandlerProposal_RefusesUnrecoverableCategories(t *testing.T) {
	cases := []struct {
		name    string
		node    *evolution.SerializableNode
		wantErr bool
	}{
		{
			// The substitution that defeats the declarative gate.
			name: "one Handle* leaf cannot buy a resource_exhausted recovery",
			node: seq("ResourceExhaustedViaTransient",
				cond("LastErrorCategoryIs:resource_exhausted"), act("HandleTransientError")),
			wantErr: true,
		},
		{
			name: "even a real LLM call cannot recover a full disk",
			node: seq("ResourceExhaustedViaLLM",
				cond("LastErrorCategoryIs:resource_exhausted"), act("SelfCorrect")),
			wantErr: true,
		},
		{
			name: "auth is equally external",
			node: seq("AuthViaSelfCorrect",
				cond("LastErrorCategoryIs:auth"), act("SelfCorrect")),
			wantErr: true,
		},
		{
			// Categories that ARE recoverable must be untouched.
			name: "rate_limit stays recoverable",
			node: seq("RateLimitBackoff",
				cond("LastErrorCategoryIs:rate_limit"), act("HandleTransientError"), act("MarkSuccessful")),
			wantErr: false,
		},
		{
			name: "a node-guarded recovery is unaffected",
			node: seq("NodeGuarded",
				cond("LastErrorNodeIs:NotebookLM_Main"), act("DefaultFallback"), act("MarkSuccessful")),
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateErrorHandlerProposal(tc.node, map[string]bool{})
			if tc.wantErr && err == nil {
				t.Fatalf("accepted a recovery for a fault no allowlisted action can address: %s — "+
					"the allowlist is blackboard-only, so this can only ever fake success", tc.node.Name)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("rejected a legitimately recoverable category in %s: %v", tc.node.Name, err)
			}
		})
	}
}

// Pin the classification itself, so adding an action to the allowlist without
// classifying it is a visible decision rather than a silent default.
func TestErrorHandlerDeclarativeActions_CoversEveryAllowlistedAction(t *testing.T) {
	for name := range errorHandlerActionAllowlist {
		if _, ok := errorHandlerDeclarativeActions[name]; !ok {
			t.Errorf("allowlisted action %q is not classified in errorHandlerDeclarativeActions; "+
				"classify it (true = self-declaring only, false = actually attempts something)", name)
		}
	}
}
