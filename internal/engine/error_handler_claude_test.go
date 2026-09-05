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
	allowErrorHandlerTestActions(t, "eh_validate_known_action")
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
	for i := range 10 {
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

// allowErrorHandlerTestActions widens errorHandlerExtraAllowedActions for the
// duration of t so tests can validate proposals composing eh_* test actions
// without adding those names to the production allowlist. Whole-map swap
// under t.Cleanup is race-clean only because no error-handler test uses
// t.Parallel() (verified: none do).
func allowErrorHandlerTestActions(t *testing.T, names ...string) {
	t.Helper()
	old := errorHandlerExtraAllowedActions
	m := map[string]bool{}
	for k := range old {
		m[k] = true
	}
	for _, n := range names {
		m[n] = true
	}
	errorHandlerExtraAllowedActions = m
	t.Cleanup(func() { errorHandlerExtraAllowedActions = old })
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

// A guard-only proposal (no Action leaf) always succeeds once its guard
// matches, so it would mark every recurrence of that error category as
// recovered Success forever — a failure-masking hole. Must be rejected.
func TestValidateErrorHandlerProposal_RejectsGuardOnlyProposal(t *testing.T) {
	guardOnly := &evolution.SerializableNode{Type: "Sequence", Name: "Handle_guard_only", Children: []evolution.SerializableNode{
		{Type: "Condition", Name: "LastErrorCategoryIs:x"},
	}}
	err := validateErrorHandlerProposal(guardOnly, map[string]bool{})
	if err == nil || !strings.Contains(err.Error(), "at least one Action") {
		t.Fatalf("guard-only proposal must be rejected as masking failures, got: %v", err)
	}
	// The existing guard+Action accept case still passes.
	allowErrorHandlerTestActions(t, "eh_validate_guard_plus_action")
	RegisterAction("eh_validate_guard_plus_action", func(*btcore.BTContext[Blackboard]) int { return 1 })
	guardPlusAction := guardedSeq("LastErrorCategoryIs:x", "eh_validate_guard_plus_action")
	if err := validateErrorHandlerProposal(guardPlusAction, map[string]bool{}); err != nil {
		t.Fatalf("guard+Action proposal must still validate: %v", err)
	}
}

// I6: the proposal vocabulary must exclude repo/fleet-mutating actions even
// when they are registered — proposals auto-execute with no human approval.
// The allowlist is default-deny and exact-name: a registered-but-not-listed
// action is rejected regardless of what it's named.
func TestValidateErrorHandlerProposal_AllowlistRejectsNonAllowlistedActions(t *testing.T) {
	for _, name := range []string{"ApplySuperpowersRunToMainRepo", "RunDeploy"} {
		if GetAction(name) == nil {
			t.Fatalf("expected %s to already be a registered production action", name)
		}
		node := guardedSeq("LastErrorCategoryIs:x", name)
		if err := validateErrorHandlerProposal(node, map[string]bool{}); err == nil || !strings.Contains(err.Error(), "not in the recovery-safe allowlist") {
			t.Fatalf("%s must be rejected with an allowlist error, got: %v", name, err)
		}
	}
}

// An allowlisted action validates with NO test seam — DefaultFallback is a
// real member of the production errorHandlerActionAllowlist. This proves the
// prod allowlist works on its own, independent of the test-only seam.
func TestValidateErrorHandlerProposal_AllowlistAcceptsAllowlistedAction(t *testing.T) {
	known := guardedSeq("LastErrorCategoryIs:x", "DefaultFallback")
	if err := validateErrorHandlerProposal(known, map[string]bool{}); err != nil {
		t.Fatalf("DefaultFallback (allowlisted) must validate without a test seam: %v", err)
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

// validCodeFix returns an escalation verdict that passes validateCodeFix — a
// real is_bug flag, a title, a file-scoped milestone naming a plausible repo
// path that the milestone references.
func validCodeFix() *errorHandlerCodeFix {
	return &errorHandlerCodeFix{
		IsBug:     true,
		Title:     "Fix nil deref",
		Milestone: "In internal/engine/foo.go guard the nil map before write; add a failing test then fix",
		Files:     []string{"internal/engine/foo.go"},
		Rationale: "unconditional map write panics",
	}
}

func TestValidateCodeFix(t *testing.T) {
	if err := validateCodeFix(validCodeFix()); err != nil {
		t.Fatalf("valid code_fix rejected: %v", err)
	}
	cases := []struct {
		desc string
		mut  func(cf *errorHandlerCodeFix)
	}{
		{"nil", nil},
		{"is_bug false", func(cf *errorHandlerCodeFix) { cf.IsBug = false }},
		{"empty title", func(cf *errorHandlerCodeFix) { cf.Title = "   " }},
		{"empty milestone", func(cf *errorHandlerCodeFix) { cf.Milestone = "" }},
		{"no files", func(cf *errorHandlerCodeFix) { cf.Files = nil }},
		{"only implausible files", func(cf *errorHandlerCodeFix) { cf.Files = []string{"", "notapath"} }},
		{"milestone names no file", func(cf *errorHandlerCodeFix) { cf.Milestone = "just fix the bug somewhere" }},
	}
	for _, tc := range cases {
		var cf *errorHandlerCodeFix
		if tc.desc != "nil" {
			cf = validCodeFix()
			tc.mut(cf)
		}
		if err := validateCodeFix(cf); err == nil {
			t.Errorf("%s: expected validation error", tc.desc)
		}
	}
	// A bare basename in files (no slash but ends .go) is a plausible path, and a
	// milestone naming that basename validates — the check is deliberately lenient.
	lenient := &errorHandlerCodeFix{IsBug: true, Title: "t", Milestone: "fix foo.go now", Files: []string{"foo.go"}}
	if err := validateCodeFix(lenient); err != nil {
		t.Fatalf("lenient basename path/milestone must validate: %v", err)
	}
}

// I2(b): validateCodeFix must deny an escalation that targets a self-fix
// guard file itself — the sharpest vector for the loop weakening its own
// guards autonomously (e.g. proposing to raise selfFixMaxOpen's cap).
func TestValidateCodeFix_RejectsSelfFixGuardFileTargets(t *testing.T) {
	guardOnly := &errorHandlerCodeFix{
		IsBug:     true,
		Title:     "Raise self-fix cap",
		Milestone: "In internal/engine/self_fix_seed.go raise selfFixMaxOpen's default from 3 to 10",
		Files:     []string{"internal/engine/self_fix_seed.go"},
		Rationale: "backlog cap too low",
	}
	err := validateCodeFix(guardOnly)
	if err == nil || !strings.Contains(err.Error(), "self-fix guard file") {
		t.Fatalf("code_fix naming a guard file alone must be rejected with the guard-file error, got: %v", err)
	}

	mixed := &errorHandlerCodeFix{
		IsBug: true,
		Title: "Fix bug and touch guard",
		Milestone: "In internal/engine/foo.go fix the nil deref; also update " +
			"internal/engine/error_handler_claude.go's validator",
		Files:     []string{"internal/engine/foo.go", "internal/engine/error_handler_claude.go"},
		Rationale: "bundled change",
	}
	err = validateCodeFix(mixed)
	if err == nil || !strings.Contains(err.Error(), "self-fix guard file") {
		t.Fatalf("code_fix mixing a guard file with a legit file must still be rejected, got: %v", err)
	}

	// A code_fix naming only ordinary (non-guard) files must still pass —
	// validCodeFix() itself (used throughout this file) pins that already.
	if err := validateCodeFix(validCodeFix()); err != nil {
		t.Fatalf("code_fix naming only ordinary files must still validate: %v", err)
	}
}

// I2(b) FINDING-1 regression: the guard must scan the free-text Milestone,
// not just the structured Files field. An innocuous Files entry paired with a
// Milestone that names a guard file is the bypass — the Milestone is the
// actual instruction the downstream TDD implementer executes with
// unrestricted Read/Write/Edit tools, so a files-only scan is bypassable by
// simply never listing the guard file, only instructing the implementer to
// touch it in the milestone text.
func TestValidateCodeFix_RejectsGuardFileNamedOnlyInMilestone(t *testing.T) {
	bypass := &errorHandlerCodeFix{
		IsBug: true,
		Title: "Fix logging helper",
		Milestone: "In internal/engine/logging_helper.go fix the format bug. Also in " +
			"internal/engine/self_fix_seed.go change selfFixMaxOpen's return 3 to return 999",
		Files:     []string{"internal/engine/logging_helper.go"},
		Rationale: "bundled",
	}
	if err := validateCodeFix(bypass); err == nil || !strings.Contains(err.Error(), "self-fix guard file") {
		t.Fatalf("code_fix naming a guard file only in the milestone must be rejected with the guard-file error, got: %v", err)
	}

	// Clean Files AND clean Milestone must still pass — no over-block
	// regression from scanning the milestone text too.
	clean := &errorHandlerCodeFix{
		IsBug:     true,
		Title:     "Fix logging helper",
		Milestone: "In internal/engine/logging_helper.go fix the format bug",
		Files:     []string{"internal/engine/logging_helper.go"},
		Rationale: "isolated fix",
	}
	if err := validateCodeFix(clean); err != nil {
		t.Fatalf("code_fix with clean files and clean milestone must validate: %v", err)
	}
}

// A {resolvable:false} proposal may carry an optional code_fix escalation; it
// must decode into prop.CodeFix and validate. Absent code_fix leaves it nil.
func TestParseErrorHandlerProposal_CodeFix(t *testing.T) {
	out := `{"resolvable": false, "reason": "genuine bug", "code_fix": {"is_bug": true, "title": "T", "milestone": "fix internal/engine/foo.go", "files": ["internal/engine/foo.go"], "rationale": "r"}}`
	p, err := parseErrorHandlerProposal(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Resolvable {
		t.Fatal("must be unresolvable")
	}
	if p.CodeFix == nil {
		t.Fatal("code_fix must decode into prop.CodeFix")
	}
	if !p.CodeFix.IsBug || p.CodeFix.Title != "T" || len(p.CodeFix.Files) != 1 {
		t.Fatalf("code_fix = %+v", p.CodeFix)
	}
	if err := validateCodeFix(p.CodeFix); err != nil {
		t.Fatalf("parsed code_fix must validate: %v", err)
	}
	p2, err := parseErrorHandlerProposal(`{"resolvable": false, "reason": "transient"}`)
	if err != nil || p2.CodeFix != nil {
		t.Fatalf("no code_fix must leave CodeFix nil: p=%+v err=%v", p2, err)
	}
}

// The reply contract must offer the third (code_fix) branch and constrain it to
// genuine source bugs, not transient failures.
func TestBuildErrorHandlerPrompt_DescribesCodeFixBranch(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{}}
	prompt := buildErrorHandlerPrompt("h", &evolution.SerializableNode{Type: "Action", Name: "a"}, bb)
	for _, want := range []string{"code_fix", "is_bug", "milestone"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt must document the %q field of the code_fix escalation", want)
		}
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
