// internal/engine/error_handler_claude_test.go
package engine

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"

	btcore "github.com/rvitorper/go-bt/core"
)

func TestErrorHandlerSignature_StableAndDigitInsensitive(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{
		"last_error_category": "rate_limit",
		"last_error_node":     "CallClaude",
		"last_error":          "429 after 17 attempts at 2026-07-15T17:32:37",
	}}
	sig1 := errorHandlerSignatureFromBB(bb, "h", "subtree")
	bb.ChainState["last_error"] = "429 after 99 attempts at 2026-07-16T09:00:00"
	sig2 := errorHandlerSignatureFromBB(bb, "h", "subtree")
	if sig1 != sig2 {
		t.Fatalf("digit-only differences must not change the signature: %s vs %s", sig1, sig2)
	}
	if len(sig1) != 12 {
		t.Fatalf("signature length = %d, want 12", len(sig1))
	}
	bb.ChainState["last_error_category"] = "timeout"
	if errorHandlerSignatureFromBB(bb, "h", "subtree") == sig1 {
		t.Fatal("different category must change the signature")
	}
}

// I5: without reliability wiring (no last_error_category/last_error_node), the
// signature must NOT hash free-text bb.Result — a fresh signature per failing
// run would defeat the cooldown and grow the ledger unbounded. All of a
// handler+subtree's unclassified failures collapse to ONE coarse signature.
func TestErrorHandlerSignature_CoarseFallbackIgnoresFreeText(t *testing.T) {
	bb1 := &Blackboard{Result: "run a1b2c3 failed at /tmp/xyz-deadbeef"}
	bb2 := &Blackboard{Result: "completely different free text f00dfaced11"}
	sig1 := errorHandlerSignatureFromBB(bb1, "h", "subtree")
	sig2 := errorHandlerSignatureFromBB(bb2, "h", "subtree")
	if sig1 == "" || len(sig1) != 12 {
		t.Fatalf("coarse signature malformed: %q", sig1)
	}
	if sig1 != sig2 {
		t.Fatalf("unclassified failures of the same handler+subtree must share one signature: %s vs %s", sig1, sig2)
	}
	if errorHandlerSignatureFromBB(bb1, "h", "other_subtree") == sig1 {
		t.Fatal("different protected subtree must change the coarse signature")
	}
}

func TestParseErrorHandlerProposal(t *testing.T) {
	out := "Here is my analysis.\n```json\n{\"resolvable\": true, \"node\": {\"type\": \"Sequence\", \"name\": \"Handle_x\"}}\n```\n"
	p, err := parseErrorHandlerProposal(out)
	if err != nil || !p.Resolvable || p.Node == nil || p.Node.Name != "Handle_x" {
		t.Fatalf("p=%+v err=%v", p, err)
	}
	p, err = parseErrorHandlerProposal(`{"resolvable": false, "reason": "needs new Go code"}`)
	if err != nil || p.Resolvable || p.Reason == "" {
		t.Fatalf("unresolvable parse: p=%+v err=%v", p, err)
	}
	if _, err = parseErrorHandlerProposal("no json here"); err == nil {
		t.Fatal("garbage must error")
	}
	if _, err = parseErrorHandlerProposal(`{"resolvable": true}`); err == nil {
		t.Fatal("resolvable without node must error")
	}
	manyStray := `thinking {a} {b} {c} {d} {e} {f} then: {"resolvable": false, "reason": "x"}`
	p, err = parseErrorHandlerProposal(manyStray)
	if err != nil || p.Resolvable || p.Reason != "x" {
		t.Fatalf("must skip past every stray '{' to find the real JSON object: p=%+v err=%v", p, err)
	}
	// A JSON object without the "resolvable" contract key (a bare {} or an
	// echoed example node) must NOT be accepted as {resolvable:false}.
	if _, err = parseErrorHandlerProposal(`{}`); err == nil {
		t.Fatal("bare {} must be a parse error, not a false-resolvable proposal")
	}
	echoed := `{"type": "Sequence", "name": "example"} and the answer: {"resolvable": false, "reason": "y"}`
	p, err = parseErrorHandlerProposal(echoed)
	if err != nil || p.Resolvable || p.Reason != "y" {
		t.Fatalf("must skip past objects without the resolvable key: p=%+v err=%v", p, err)
	}
}

func guardedSeq(guard, action string) *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "Handle_test", Children: []evolution.SerializableNode{
		{Type: "Condition", Name: guard},
		{Type: "Action", Name: action},
	}}
}

func TestValidateErrorHandlerProposal(t *testing.T) {
	RegisterAction("eh_validate_known_action", func(*btcore.BTContext[Blackboard]) int { return 1 })
	RegisterCondition("eh_validate_registered_cond", func(*Blackboard) bool { return true })
	valid := guardedSeq("LastErrorCategoryIs:testcat", "eh_validate_known_action")
	if err := validateErrorHandlerProposal(valid, map[string]bool{}); err != nil {
		t.Fatalf("valid proposal rejected: %v", err)
	}
	validNode := guardedSeq("LastErrorNodeIs:SomeNode", "eh_validate_known_action")
	validNode.Name = "Handle_node_guard"
	if err := validateErrorHandlerProposal(validNode, map[string]bool{}); err != nil {
		t.Fatalf("LastErrorNodeIs guard rejected: %v", err)
	}
	// The root/first-ticked-leaf guard checks pass (childless disallowed/unknown
	// roots above are rejected earlier by that guard check), but a disallowed,
	// known node type deeper in the tree must still be caught by the walk's
	// allowlist check.
	deeplyDisallowed := &evolution.SerializableNode{Type: "Sequence", Name: "outer", Children: []evolution.SerializableNode{
		{Type: "Condition", Name: "LastErrorCategoryIs:x"},
		{Type: "Parallel", Name: "par", Children: []evolution.SerializableNode{
			{Type: "Action", Name: "eh_validate_known_action"},
		}},
	}}
	if err := validateErrorHandlerProposal(deeplyDisallowed, map[string]bool{}); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected rejection with 'not allowed' for disallowed node type deeper in tree, got: %v", err)
	}
	cases := []struct {
		desc string
		node *evolution.SerializableNode
	}{
		{"nil node", nil},
		{"unknown action", guardedSeq("LastErrorCategoryIs:x", "definitely_not_registered_xyz")},
		{"disallowed node type", &evolution.SerializableNode{Type: "HumanApprovalGate", Name: "n"}},
		{"unknown node type", &evolution.SerializableNode{Type: "Bogus", Name: "n"}},
		{"first leaf not a guard", &evolution.SerializableNode{Type: "Sequence", Name: "n", Children: []evolution.SerializableNode{
			{Type: "Action", Name: "eh_validate_known_action"},
		}}},
		{"empty name", func() *evolution.SerializableNode {
			n := guardedSeq("LastErrorCategoryIs:x", "eh_validate_known_action")
			n.Name = ""
			return n
		}()},
		{"taken name", guardedSeq("LastErrorCategoryIs:x", "eh_validate_known_action")},
		// I3: a registered (possibly broadly-true) condition is NOT an
		// acceptable guard — only parameterized error guards are.
		{"registered-condition guard", guardedSeq("eh_validate_registered_cond", "eh_validate_known_action")},
		// I3: an empty-param error guard is always-false — a dead extension
		// consuming a cap slot.
		{"empty-param guard", guardedSeq("LastErrorCategoryIs:", "eh_validate_known_action")},
		// I3: a Succeeder above the guard neutralizes it — the composition
		// succeeds even when the guard fails.
		{"succeeder-wrapped guard", &evolution.SerializableNode{Type: "Sequence", Name: "outer_succ", Children: []evolution.SerializableNode{
			{Type: "Succeeder", Name: "neutralize", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "LastErrorCategoryIs:x"},
			}},
			{Type: "Action", Name: "eh_validate_known_action"},
		}}},
		{"selector above guard", &evolution.SerializableNode{Type: "Selector", Name: "sel_root", Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "LastErrorCategoryIs:x"},
			{Type: "Action", Name: "eh_validate_known_action"},
		}}},
	}
	for _, tc := range cases {
		names := map[string]bool{}
		if tc.desc == "taken name" {
			names["Handle_test"] = true
		}
		if err := validateErrorHandlerProposal(tc.node, names); err == nil {
			t.Errorf("%s: expected rejection", tc.desc)
		}
	}
	// Size cap: a chain of 11 nested sequences exceeds maxProposalNodes=10.
	deep := &evolution.SerializableNode{Type: "Condition", Name: "LastErrorCategoryIs:x"}
	node := *deep
	for i := 0; i < 10; i++ {
		node = evolution.SerializableNode{Type: "Sequence", Name: "s" + strings.Repeat("x", i+1), Children: []evolution.SerializableNode{node}}
	}
	if err := validateErrorHandlerProposal(&node, map[string]bool{}); err == nil {
		t.Error("oversized/deep proposal must be rejected")
	}
}

func TestErrorHandlerConfigDefaults(t *testing.T) {
	if !errorHandlerEnabled() {
		t.Fatal("enabled by default")
	}
	t.Setenv("BT_CLAUDE_ERROR_HANDLER", "off")
	if errorHandlerEnabled() {
		t.Fatal("BT_CLAUDE_ERROR_HANDLER=off must disable")
	}
	t.Setenv("BT_ERROR_HANDLER_COOLDOWN", "90m")
	if errorHandlerCooldown() != 90*time.Minute {
		t.Fatal("cooldown env override")
	}
	t.Setenv("BT_ERROR_HANDLER_COOLDOWN", "bogus")
	if errorHandlerCooldown() != 6*time.Hour {
		t.Fatal("cooldown default on parse failure")
	}
	t.Setenv("BT_ERROR_HANDLER_MAX_NODES", "2")
	if errorHandlerMaxNodes() != 2 {
		t.Fatal("max nodes env override")
	}
}

// fakeClaudeRunner returns a canned proposal and counts invocations.
type fakeClaudeRunner struct {
	calls  atomic.Int64
	output string
	err    error
}

func (f *fakeClaudeRunner) RunClaude(_ context.Context, _ string, _ string) CommandResult {
	f.calls.Add(1)
	return CommandResult{Output: f.output, Err: f.err}
}

func swapErrorHandlerRunner(t *testing.T, r ClaudeRunner) {
	t.Helper()
	old := errorHandlerClaudeRunner
	errorHandlerClaudeRunner = r
	t.Cleanup(func() { errorHandlerClaudeRunner = old })
}

func TestRequestErrorHandlerProposal_StampsLedgerOnEveryOutcome(t *testing.T) {
	withTempErrorHandlerDir(t)
	failing := &evolution.SerializableNode{Type: "Action", Name: "x"}
	bb := &Blackboard{ChainState: map[string]any{}}

	t.Run("runner error stamps error verdict", func(t *testing.T) {
		sig := "sig-err"
		runner := &fakeClaudeRunner{err: errors.New("boom")}
		swapErrorHandlerRunner(t, runner)
		if _, err := requestErrorHandlerProposal(context.Background(), "h", failing, bb, sig); err == nil {
			t.Fatal("expected error from runner failure")
		}
		entry, ok := errorHandlerLedgerGet(sig)
		if !ok || entry.LastVerdict != "error" {
			t.Fatalf("ledger entry = %+v ok=%v, want verdict=error", entry, ok)
		}
	})

	t.Run("unparseable output stamps error verdict", func(t *testing.T) {
		sig := "sig-parse"
		runner := &fakeClaudeRunner{output: "no json here"}
		swapErrorHandlerRunner(t, runner)
		if _, err := requestErrorHandlerProposal(context.Background(), "h", failing, bb, sig); err == nil {
			t.Fatal("expected error from unparseable output")
		}
		entry, ok := errorHandlerLedgerGet(sig)
		if !ok || entry.LastVerdict != "error" {
			t.Fatalf("ledger entry = %+v ok=%v, want verdict=error", entry, ok)
		}
	})

	t.Run("unresolvable proposal stamps unresolvable verdict", func(t *testing.T) {
		sig := "sig-unres"
		runner := &fakeClaudeRunner{output: `{"resolvable": false, "reason": "r"}`}
		swapErrorHandlerRunner(t, runner)
		p, err := requestErrorHandlerProposal(context.Background(), "h", failing, bb, sig)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Resolvable {
			t.Fatal("expected Resolvable=false")
		}
		entry, ok := errorHandlerLedgerGet(sig)
		if !ok || entry.LastVerdict != "unresolvable" {
			t.Fatalf("ledger entry = %+v ok=%v, want verdict=unresolvable", entry, ok)
		}
	})

	t.Run("resolvable proposal stamps proposed verdict", func(t *testing.T) {
		sig := "sig-prop"
		runner := &fakeClaudeRunner{output: `{"resolvable": true, "reason": "ok", "node": {"type": "Sequence", "name": "Handle_x"}}`}
		swapErrorHandlerRunner(t, runner)
		p, err := requestErrorHandlerProposal(context.Background(), "h", failing, bb, sig)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !p.Resolvable {
			t.Fatal("expected Resolvable=true")
		}
		entry, ok := errorHandlerLedgerGet(sig)
		if !ok || entry.LastVerdict != "proposed" {
			t.Fatalf("ledger entry = %+v ok=%v, want verdict=proposed", entry, ok)
		}
	})
}

// ctxAwareBlockingRunner blocks until the passed ctx is done, then returns its
// error — proving requestErrorHandlerProposal threads the caller's context in
// (I4: the call holds the fleet lock and must respect the tree deadline).
type ctxAwareBlockingRunner struct{}

func (ctxAwareBlockingRunner) RunClaude(ctx context.Context, _ string, _ string) CommandResult {
	<-ctx.Done()
	return CommandResult{Err: ctx.Err()}
}

func TestRequestErrorHandlerProposal_RespectsCallerContext(t *testing.T) {
	withTempErrorHandlerDir(t)
	swapErrorHandlerRunner(t, ctxAwareBlockingRunner{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled: the call must return promptly, not after 180s
	failing := &evolution.SerializableNode{Type: "Action", Name: "x"}
	bb := &Blackboard{ChainState: map[string]any{}}
	start := time.Now()
	_, err := requestErrorHandlerProposal(ctx, "h", failing, bb, "sig-ctx")
	if err == nil {
		t.Fatal("cancelled context must surface as an error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("cancelled context must return promptly, took %s", elapsed)
	}
	entry, ok := errorHandlerLedgerGet("sig-ctx")
	if !ok || entry.LastVerdict != "error" {
		t.Fatalf("ledger entry = %+v ok=%v, want verdict=error", entry, ok)
	}
}

// I6: the proposal vocabulary must exclude repo/fleet-mutating actions even
// when they are registered — proposals auto-execute with no human approval.
func TestValidateErrorHandlerProposal_DeniesMutatingActions(t *testing.T) {
	RegisterAction("eh_test_ApplyDangerousFix", func(*btcore.BTContext[Blackboard]) int { return 1 })
	RegisterAction("eh_test_benign_probe", func(*btcore.BTContext[Blackboard]) int { return 1 })
	denied := guardedSeq("LastErrorCategoryIs:x", "eh_test_ApplyDangerousFix")
	err := validateErrorHandlerProposal(denied, map[string]bool{})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("mutating action must be rejected with a denylist error, got: %v", err)
	}
	benign := guardedSeq("LastErrorCategoryIs:x", "eh_test_benign_probe")
	if err := validateErrorHandlerProposal(benign, map[string]bool{}); err != nil {
		t.Fatalf("benign registered action must still validate: %v", err)
	}
}

// I6: ForceReadOnly pins the explicit --allowedTools list; the
// skip-permissions env override must never strip it.
func TestExecClaudeRunner_ForceReadOnlyIgnoresSkipPermissions(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_CLAUDE_SKIP_PERMISSIONS", "true")
	args := execClaudeRunner{AllowedTools: errorHandlerAllowedTools, ForceReadOnly: true}.buildClaudeArgs("p")
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--dangerously-skip-permissions") {
		t.Fatalf("ForceReadOnly must never emit --dangerously-skip-permissions: %v", args)
	}
	if !strings.Contains(joined, "--allowedTools "+errorHandlerAllowedTools) {
		t.Fatalf("ForceReadOnly must pass the explicit --allowedTools list: %v", args)
	}
	// Other callers keep the existing env-override behavior.
	legacy := execClaudeRunner{}.buildClaudeArgs("p")
	if !strings.Contains(strings.Join(legacy, " "), "--dangerously-skip-permissions") {
		t.Fatalf("non-ForceReadOnly runners must keep honoring the env override: %v", legacy)
	}
}

// I6: untrusted error text (a prompt-injection channel) is excerpted in the
// prompt, not embedded whole.
func TestBuildErrorHandlerPrompt_TruncatesErrorText(t *testing.T) {
	long := strings.Repeat("x", errorHandlerPromptErrLimit+400)
	bb := &Blackboard{Result: long, ChainState: map[string]any{}}
	prompt := buildErrorHandlerPrompt("h", &evolution.SerializableNode{Type: "Action", Name: "a"}, bb)
	if strings.Contains(prompt, long) {
		t.Fatal("prompt must not embed the full untruncated error text")
	}
	if !strings.Contains(prompt, strings.Repeat("x", errorHandlerPromptErrLimit)+"… (truncated)") {
		t.Fatal("prompt must contain the truncated excerpt with a truncation marker")
	}
}
