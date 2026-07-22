// internal/engine/error_handler_node_test.go
package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/research"
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
	// Failing action that leaves quality-reject markers ("error:", "failed to")
	// in bb.Result — the common shape of a real failure. Used to prove recovered
	// runs survive RunTask's validateOutputQuality backstop (C1).
	RegisterAction("eh_test_dirty_failing_action", func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		if b.ChainState == nil {
			b.ChainState = map[string]any{}
		}
		b.ChainState["last_error_category"] = "testcat"
		b.ChainState["last_error_node"] = "eh_test_dirty_failing_action"
		b.ChainState["last_error"] = "error: boom failed to reach service"
		b.Result = "error: boom failed to reach service"
		return -1
	})
	// Recovery action that never terminates (returns Running). Used to pin that
	// a running recovery child folds into failure, not a hung handler.
	RegisterAction("eh_test_running_action", func(ctx *btcore.BTContext[Blackboard]) int {
		return 0
	})
	// Failing action whose bb.Result contains BOTH a quality-reject marker AND
	// an inner ``` fence — the shape that would defeat fence-based scrubbing
	// if the inner fence weren't neutralized first (C: fence-parity hardening).
	RegisterAction("eh_test_inner_fence_failing_action", func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		if b.ChainState == nil {
			b.ChainState = map[string]any{}
		}
		b.ChainState["last_error_category"] = "testcat"
		b.ChainState["last_error_node"] = "eh_test_inner_fence_failing_action"
		b.ChainState["last_error"] = "error: boom\n```\ninner\n```\n"
		b.Result = "error: boom\n```\ninner\n```\n"
		return -1
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
	// eh_test_recover_action is a test-only recovery action, deliberately NOT
	// in the production errorHandlerActionAllowlist — widen the test seam so
	// this proposal still validates/grafts for the calling test.
	allowErrorHandlerTestActions(t, "eh_test_recover_action")
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

// A persisted extension whose action is no longer allowed (a tightened policy,
// or a hand-edited extensions.json) must be re-validated and skipped on graft —
// the allowlist is the security boundary and must apply to already-granted
// extensions, not only to freshly-proposed nodes.
func TestClaudeErrorHandler_SkipsPersistedExtensionFailingCurrentPolicy(t *testing.T) {
	withTempErrorHandlerDir(t)
	t.Setenv("BT_CLAUDE_ERROR_HANDLER", "off") // no fresh proposal; isolate graft re-validation
	// Seed directly (no validation at write time), using a registered action that
	// is deliberately NOT in the production allowlist and NOT widened via the test
	// seam here — simulating a stale-policy or tampered store entry. Its guard
	// matches eh_test_failing_action's category, so without re-validation it would
	// graft and tick.
	seed := ErrorHandlerExtension{Node: evolution.SerializableNode{
		Type: "Sequence", Name: "Handle_stale_policy",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "LastErrorCategoryIs:testcat"},
			{Type: "Action", Name: "eh_test_recover_action"},
		},
	}}
	if err := appendErrorHandlerExtension("eh_test_tree_ErrorHandler", seed); err != nil {
		t.Fatal(err)
	}
	ehTestRecoverRan.Store(0)
	bb := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb); code != -1 {
		t.Fatalf("a persisted extension failing current policy must be skipped and the failure pass through; got %d", code)
	}
	if ehTestRecoverRan.Load() != 0 {
		t.Fatal("the skipped extension's action must never tick")
	}
	if _, stamped := bb.ChainState["error_handler_recovered"]; stamped {
		t.Fatal("no recovery must be recorded for a policy-rejected extension")
	}
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
	if entry, ok := errorHandlerLedgerGet(errorHandlerSignatureFromBB(bb, "eh_test_tree_ErrorHandler", "eh_test_failing_action")); !ok || entry.LastVerdict != "rejected" {
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

// C1: a recovered run whose pre-recovery bb.Result contains quality-reject
// markers ("error:", "failed to") must still report Success through RunTask —
// validateOutputQuality's marker scan (tree.go) would otherwise flip the
// outcome back to Failure, making the handler inert for the common case.
// markRecovered must fence the pre-recovery text so stripFencedBlocks removes
// it from the quality scan.
func TestClaudeErrorHandler_RecoveredRunSurvivesRunTaskQualityGate(t *testing.T) {
	withTempErrorHandlerDir(t)
	fake := &fakeClaudeRunner{output: ehTestProposalJSON(t)}
	swapErrorHandlerRunner(t, fake)
	node := &evolution.SerializableNode{
		Type: "ClaudeErrorHandler",
		Name: "eh_dirty_tree_ErrorHandler",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "eh_test_dirty_failing_action"},
		},
	}
	bb := &Blackboard{Task: "dirty recovery", ChainState: map[string]any{}}
	tree, err := BuildAndValidate(node, bb)
	if err != nil {
		t.Fatal(err)
	}
	RunTask(bb, tree)
	if bb.Outcome != string(evolution.Success) {
		t.Fatalf("recovered run must survive RunTask's quality backstop; outcome=%q result=%q", bb.Outcome, bb.Result)
	}
	if sig, _ := bb.ChainState["error_handler_recovered"].(string); sig == "" {
		t.Fatal("recovery must stamp error_handler_recovered")
	}
	if !strings.Contains(bb.Result, "## Error Handler Recovery") {
		t.Fatalf("recovery note missing from Result: %q", bb.Result)
	}
}

// C: an inner ``` fence inside the pre-recovery bb.Result must be neutralized
// before wrapping, or it toggles stripFencedBlocks' in/out-of-fence state and
// leaks the failure text (with its quality-reject marker) back into RunTask's
// quality scan — able to re-trip the recovered Success back to Failure.
func TestClaudeErrorHandler_RecoveredRunSurvivesInnerFenceInPriorResult(t *testing.T) {
	withTempErrorHandlerDir(t)
	fake := &fakeClaudeRunner{output: ehTestProposalJSON(t)}
	swapErrorHandlerRunner(t, fake)
	node := &evolution.SerializableNode{
		Type: "ClaudeErrorHandler",
		Name: "eh_inner_fence_tree_ErrorHandler",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "eh_test_inner_fence_failing_action"},
		},
	}
	bb := &Blackboard{Task: "inner fence recovery", ChainState: map[string]any{}}
	tree, err := BuildAndValidate(node, bb)
	if err != nil {
		t.Fatal(err)
	}
	RunTask(bb, tree)
	if bb.Outcome != string(evolution.Success) {
		t.Fatalf("recovered run must survive RunTask's quality backstop even with an inner ``` fence in the prior result; outcome=%q result=%q", bb.Outcome, bb.Result)
	}
	if sig, _ := bb.ChainState["error_handler_recovered"].(string); sig == "" {
		t.Fatal("recovery must stamp error_handler_recovered")
	}
	if !strings.Contains(bb.Result, "## Error Handler Recovery") {
		t.Fatalf("recovery note missing from Result: %q", bb.Result)
	}
}

// T5 gap: a grafted recovery child that returns 0 (Running) must fold into
// failure (-1), not leak Running out of the handler.
func TestClaudeErrorHandler_RunningRecoveryFoldsIntoFailure(t *testing.T) {
	withTempErrorHandlerDir(t)
	allowErrorHandlerTestActions(t, "eh_test_running_action")
	prop := `{"resolvable": true, "reason": "r", "node": {"type": "Sequence", "name": "Handle_running", "children": [` +
		`{"type": "Condition", "name": "LastErrorCategoryIs:testcat"},` +
		`{"type": "Action", "name": "eh_test_running_action"}]}}`
	fake := &fakeClaudeRunner{output: prop}
	swapErrorHandlerRunner(t, fake)
	bb := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb); code != -1 {
		t.Fatalf("running recovery child must fold into failure, got %d", code)
	}
	exts := loadErrorHandlerExtensions("eh_test_tree_ErrorHandler")
	if len(exts) != 1 || exts[0].ConsecutiveFailures != 1 {
		t.Fatalf("running tick must count as a recovery failure: %+v", exts)
	}
}

// ehTestCodeFixJSON is an unresolvable verdict carrying a valid code_fix
// escalation (real is_bug, file-scoped milestone naming the file). Uses a
// fabricated non-guard file name (eh_test_target.go, not error_handler_node.go
// itself) — error_handler_node.go is one of the I2(b) self-fix guard files
// mentionsSelfFixGuardFile denies, and this test's escalate-and-seed path is
// exercising the ORDINARY-bug case, not the guard-file-rejection case (that's
// TestValidateCodeFix_RejectsSelfFixGuardFileTargets in
// error_handler_claude_test.go).
func ehTestCodeFixJSON() string {
	return `{"resolvable": false, "reason": "genuine source bug", "code_fix": {` +
		`"is_bug": true, "title": "Fix eh_test defect", ` +
		`"milestone": "In internal/engine/eh_test_target.go guard the nil case; write a failing test then fix", ` +
		`"files": ["internal/engine/eh_test_target.go"], "rationale": "unconditional deref"}}`
}

// Part A escalation: an unresolvable verdict with a valid code_fix seeds a
// self-fix:error-handler:* program, still passes the tree failure through (-1),
// stamps the ledger verdict "escalated" only AFTER a successful seed, and
// re-firing within cooldown does not re-call Claude or re-seed.
func TestClaudeErrorHandler_UnresolvableWithCodeFixEscalates(t *testing.T) {
	withTempErrorHandlerDir(t)
	_, programsPath := withTempSelfFix(t)
	fake := &fakeClaudeRunner{output: ehTestCodeFixJSON()}
	swapErrorHandlerRunner(t, fake)

	bb := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb); code != -1 {
		t.Fatalf("escalation must still pass the tree failure through; got %d", code)
	}
	if n := countSelfFixPrograms(t, programsPath); n != 1 {
		t.Fatalf("expected exactly one seeded self-fix program, got %d", n)
	}
	ps, err := research.OpenPrograms(programsPath)
	if err != nil {
		t.Fatal(err)
	}
	var seeded *research.Program
	for _, p := range ps.Programs {
		if strings.HasPrefix(p.Source, "self-fix:error-handler:") {
			seeded = p
		}
	}
	if seeded == nil {
		t.Fatalf("no self-fix:error-handler:* program seeded: %+v", ps.Programs)
	}
	if seeded.Title != "Fix eh_test defect" {
		t.Fatalf("seeded program title = %q", seeded.Title)
	}
	sig := errorHandlerSignatureFromBB(bb, "eh_test_tree_ErrorHandler", "eh_test_failing_action")
	if entry, ok := errorHandlerLedgerGet(sig); !ok || entry.LastVerdict != "escalated" {
		t.Fatalf("ledger verdict = %+v ok=%v, want escalated", entry, ok)
	}

	// Re-firing within cooldown: no second Claude call, no second seed.
	bb2 := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb2); code != -1 {
		t.Fatal("second failure must still pass through")
	}
	if fake.calls.Load() != 1 {
		t.Fatalf("cooldown must suppress the second Claude call, got %d", fake.calls.Load())
	}
	if n := countSelfFixPrograms(t, programsPath); n != 1 {
		t.Fatalf("cooldown must suppress a second seed, got %d programs", n)
	}
}

// Kill switch: a valid code_fix must NOT stamp "escalated" when seeding is
// disabled — that would cool out Claude+seed under a false success. Stamp
// escalate_deferred instead (policy cool-out; no program queued).
func TestClaudeErrorHandler_CodeFixKillSwitchDefersNotEscalates(t *testing.T) {
	withTempErrorHandlerDir(t)
	_, programsPath := withTempSelfFix(t)
	t.Setenv("BT_SELF_FIX", "off")
	fake := &fakeClaudeRunner{output: ehTestCodeFixJSON()}
	swapErrorHandlerRunner(t, fake)

	bb := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb); code != -1 {
		t.Fatalf("must pass failure through; got %d", code)
	}
	if n := countSelfFixPrograms(t, programsPath); n != 0 {
		t.Fatalf("kill switch must seed nothing, got %d", n)
	}
	sig := errorHandlerSignatureFromBB(bb, "eh_test_tree_ErrorHandler", "eh_test_failing_action")
	if entry, ok := errorHandlerLedgerGet(sig); !ok || entry.LastVerdict != "escalate_deferred" {
		t.Fatalf("ledger verdict = %+v ok=%v, want escalate_deferred", entry, ok)
	}

	// Deferred cools out: no second Claude call within EH cooldown.
	bb2 := &Blackboard{ChainState: map[string]any{}}
	_ = runHandler(t, bb2)
	if fake.calls.Load() != 1 {
		t.Fatalf("escalate_deferred must cool out Claude, got %d calls", fake.calls.Load())
	}
}

// Transient seed failure (store busy) stamps escalate_failed and does NOT cool
// out — the next firing retries. Simulated by holding the self-fix store lock.
func TestClaudeErrorHandler_CodeFixTransientSeedFailureRetries(t *testing.T) {
	withTempErrorHandlerDir(t)
	dir, programsPath := withTempSelfFix(t)
	fake := &fakeClaudeRunner{output: ehTestCodeFixJSON()}
	swapErrorHandlerRunner(t, fake)

	// Hold the cross-process self-fix store lock so seedCodeFixProgram returns
	// "self-fix store busy" without waiting.
	lockPath := filepath.Join(dir, "store.lock")
	if err := os.WriteFile(lockPath, []byte("held"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(lockPath) })

	bb := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb); code != -1 {
		t.Fatalf("must pass failure through; got %d", code)
	}
	if n := countSelfFixPrograms(t, programsPath); n != 0 {
		t.Fatalf("busy store must seed nothing, got %d", n)
	}
	sig := errorHandlerSignatureFromBB(bb, "eh_test_tree_ErrorHandler", "eh_test_failing_action")
	if entry, ok := errorHandlerLedgerGet(sig); !ok || entry.LastVerdict != "escalate_failed" {
		t.Fatalf("ledger verdict = %+v ok=%v, want escalate_failed", entry, ok)
	}

	// Release the lock and re-fire: escalate_failed must NOT cool out.
	_ = os.Remove(lockPath)
	bb2 := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb2); code != -1 {
		t.Fatal("retry must still pass through")
	}
	if fake.calls.Load() != 2 {
		t.Fatalf("escalate_failed must allow a second Claude call, got %d", fake.calls.Load())
	}
	if n := countSelfFixPrograms(t, programsPath); n != 1 {
		t.Fatalf("retry after lock release must seed, got %d", n)
	}
	if entry, ok := errorHandlerLedgerGet(sig); !ok || entry.LastVerdict != "escalated" {
		t.Fatalf("after successful retry: %+v ok=%v, want escalated", entry, ok)
	}
}

// A PERSISTENT (not merely transient) self-fix seed failure must not be able
// to retry Claude+seed forever. escalate_failed only bypasses the cooldown for
// a bounded number of CONSECUTIVE misses (matches the errorHandlerDisableAfter
// 3-window convention used elsewhere in this store). Once that streak is
// reached, escalate_failed must cool out like any other verdict — otherwise a
// permanently broken self-fix store (not a one-off transient blip) spams
// Claude every tick for the life of the process, bypassing the 6-hour cooldown
// forever.
func TestClaudeErrorHandler_ConsecutiveEscalateFailedCapsCooldownBypass(t *testing.T) {
	withTempErrorHandlerDir(t)
	dir, programsPath := withTempSelfFix(t)
	fake := &fakeClaudeRunner{output: ehTestCodeFixJSON()}
	swapErrorHandlerRunner(t, fake)

	// Hold the cross-process self-fix store lock for the ENTIRE test so every
	// seed attempt returns "self-fix store busy" -> escalate_failed. Unlike
	// TestClaudeErrorHandler_CodeFixTransientSeedFailureRetries (which releases
	// the lock after one miss to prove a genuinely transient blip retries),
	// this simulates a persistently broken store that never recovers.
	const consecutiveEscalateFailedCap = 3
	lockPath := filepath.Join(dir, "store.lock")
	if err := os.WriteFile(lockPath, []byte("held"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(lockPath) })

	var sig string
	for i := 0; i < consecutiveEscalateFailedCap+2; i++ {
		bb := &Blackboard{ChainState: map[string]any{}}
		if code := runHandler(t, bb); code != -1 {
			t.Fatalf("iteration %d: must pass failure through; got %d", i, code)
		}
		sig = errorHandlerSignatureFromBB(bb, "eh_test_tree_ErrorHandler", "eh_test_failing_action")
	}
	if n := countSelfFixPrograms(t, programsPath); n != 0 {
		t.Fatalf("permanently busy store must never seed, got %d", n)
	}
	if got, want := fake.calls.Load(), int64(consecutiveEscalateFailedCap); got != want {
		t.Fatalf("consecutive escalate_failed must cap Claude retries at %d, got %d calls (cooldown bypass is unbounded)", want, got)
	}
	if entry, ok := errorHandlerLedgerGet(sig); !ok || entry.LastVerdict != "escalate_failed" {
		t.Fatalf("ledger verdict = %+v ok=%v, want escalate_failed", entry, ok)
	}

	// One more firing, still within the 6h cooldown: must remain suppressed.
	bbFinal := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bbFinal); code != -1 {
		t.Fatal("must still pass failure through while capped")
	}
	if got := fake.calls.Load(); got != int64(consecutiveEscalateFailedCap) {
		t.Fatalf("capped escalate_failed must stay cooled out, got %d calls", got)
	}
}

// An unresolvable verdict WITHOUT code_fix (a transient failure) seeds nothing
// and keeps today's "unresolvable" ledger verdict.
func TestClaudeErrorHandler_UnresolvableWithoutCodeFixSeedsNothing(t *testing.T) {
	withTempErrorHandlerDir(t)
	_, programsPath := withTempSelfFix(t)
	fake := &fakeClaudeRunner{output: `{"resolvable": false, "reason": "transient rate limit"}`}
	swapErrorHandlerRunner(t, fake)

	bb := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb); code != -1 {
		t.Fatalf("unresolvable must pass the failure through; got %d", code)
	}
	if n := countSelfFixPrograms(t, programsPath); n != 0 {
		t.Fatalf("no code_fix must seed nothing, got %d", n)
	}
	sig := errorHandlerSignatureFromBB(bb, "eh_test_tree_ErrorHandler", "eh_test_failing_action")
	if entry, ok := errorHandlerLedgerGet(sig); !ok || entry.LastVerdict != "unresolvable" {
		t.Fatalf("ledger verdict = %+v ok=%v, want unresolvable", entry, ok)
	}
}

// An unresolvable verdict carrying an INVALID code_fix (is_bug=false) must not
// seed and must keep the "unresolvable" verdict.
func TestClaudeErrorHandler_InvalidCodeFixSeedsNothing(t *testing.T) {
	withTempErrorHandlerDir(t)
	_, programsPath := withTempSelfFix(t)
	fake := &fakeClaudeRunner{output: `{"resolvable": false, "reason": "r", "code_fix": {"is_bug": false, "title": "t", "milestone": "internal/engine/foo.go", "files": ["internal/engine/foo.go"]}}`}
	swapErrorHandlerRunner(t, fake)

	bb := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb); code != -1 {
		t.Fatalf("invalid code_fix must pass the failure through; got %d", code)
	}
	if n := countSelfFixPrograms(t, programsPath); n != 0 {
		t.Fatalf("invalid code_fix must seed nothing, got %d", n)
	}
	sig := errorHandlerSignatureFromBB(bb, "eh_test_tree_ErrorHandler", "eh_test_failing_action")
	if entry, ok := errorHandlerLedgerGet(sig); !ok || entry.LastVerdict != "unresolvable" {
		t.Fatalf("ledger verdict = %+v ok=%v, want unresolvable", entry, ok)
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
