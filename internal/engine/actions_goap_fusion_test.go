package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	btcore "github.com/rvitorper/go-bt/core"
)

// A graph report that merely mentions "test" (as almost every report does)
// must NOT fabricate a bogus "engine tests executable / import cycle" blocker.
// Regression context: AnalyzeImprovementGaps used a brittle
// `Contains(report,"test") && !Contains(report,"engine test")` heuristic that
// emitted a CHECK gap no real blocker justified; it then flowed into
// PrioritizeGoapGoals as a P0 "Unblock engine tests" goal that is
// un-implementable (no import cycle exists) and dead-letters the loop.
// See memory: goap-fusion-engine-test-blocker-false-goal.
func TestAnalyzeImprovementGaps_NoFabricatedEngineTestBlocker(t *testing.T) {
	analyze := GetAction("AnalyzeImprovementGaps")
	if analyze == nil {
		t.Fatal("action \"AnalyzeImprovementGaps\" not registered")
	}

	// A typical graph report: contains "test" but no genuine import-cycle or
	// test-compilation blocker.
	bb := &Blackboard{ChainState: map[string]any{
		"goap_fusion_graphify_report": "GRAPH_REPORT: 412 nodes, smoke test coverage across domain trees, condition tests present.",
	}}

	if got := analyze(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("AnalyzeImprovementGaps status = %d, want 1; result: %s", got, bb.Result)
	}

	gaps, _ := bb.ChainState["goap_fusion_improvement_gaps"].(string)
	if strings.Contains(gaps, "Engine tests executable") ||
		strings.Contains(gaps, "import cycles block test compilation") {
		t.Fatalf("gap analysis fabricated a bogus engine-test blocker:\n%s", gaps)
	}

	// And it must not survive prioritization into a P0 "Unblock engine tests"
	// goal fed to the un-implementable failure path.
	isolateGoapProgramStore(t)
	prioritize := GetAction("PrioritizeGoapGoals")
	if prioritize == nil {
		t.Fatal("action \"PrioritizeGoapGoals\" not registered")
	}
	if got := prioritize(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("PrioritizeGoapGoals status = %d, want 1; result: %s", got, bb.Result)
	}
	goals, _ := bb.ChainState["goap_fusion_goal_queue"].(string)
	if strings.Contains(goals, "Unblock engine tests") {
		t.Fatalf("prioritization produced an un-implementable engine-test P0 goal:\n%s", goals)
	}
}

// PrioritizeGoapGoals must not fabricate the un-implementable "Unblock engine
// tests" P0 goal just because a gap line mentions the phrase "import cycle".
// The removed AnalyzeImprovementGaps heuristic was only one source of that
// phrase; research review (grill/NotebookLM/Claude) routinely appends free-form
// gap text, and this codebase uses "import cycle" constantly in its own design
// notes ("avoid import cycle", "engine → domains import cycle"). The downstream
// Contains(gaps,"import cycle") matcher in PrioritizeGoapGoals turns any such
// mention into a P0 goal for a blocker that does not exist — the engine package
// builds cleanly — and that goal dead-letters the loop.
// Regression context: memory goap-fusion-engine-test-blocker-false-goal.
func TestPrioritizeGoapGoals_NoImportCycleFalseGoalFromResearchGap(t *testing.T) {
	isolateGoapProgramStore(t)
	prioritize := GetAction("PrioritizeGoapGoals")
	if prioritize == nil {
		t.Fatal("action \"PrioritizeGoapGoals\" not registered")
	}

	// Gaps as they arrive from research review: free-form text that merely
	// mentions "import cycle" as a design note, with no genuine blocker behind
	// it and no other goal-trigger keyword present.
	bb := &Blackboard{ChainState: map[string]any{
		"goap_fusion_improvement_gaps": "GAP: research notes the engine and domains packages must avoid an " +
			"import cycle when guard builders move — no blocker exists today, just a boundary design note.",
	}}

	if got := prioritize(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("PrioritizeGoapGoals status = %d, want 1; result: %s", got, bb.Result)
	}

	goals, _ := bb.ChainState["goap_fusion_goal_queue"].(string)
	if strings.Contains(goals, "Unblock engine tests") {
		t.Fatalf("prioritization fabricated an un-implementable engine-test P0 goal from a research gap "+
			"that merely mentions \"import cycle\":\n%s", goals)
	}
}

// The discriminator must not degenerate to "never fire": when a gap describes a
// genuine, affirmative build blocker (tests fail to compile because of an active
// import cycle), the P0 "Unblock engine tests" goal SHOULD still be produced.
// This locks the affirmative branch so a future refactor cannot silently drop
// the capability while still passing the negative regression tests above.
func TestPrioritizeGoapGoals_AffirmativeBlockerProducesEngineTestGoal(t *testing.T) {
	isolateGoapProgramStore(t)
	prioritize := GetAction("PrioritizeGoapGoals")
	if prioritize == nil {
		t.Fatal("action \"PrioritizeGoapGoals\" not registered")
	}

	bb := &Blackboard{ChainState: map[string]any{
		"goap_fusion_improvement_gaps": "GAP: an import cycle blocks test compilation in internal/engine — " +
			"tests fail to compile and the package cannot run tests until it is broken.",
	}}

	if got := prioritize(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("PrioritizeGoapGoals status = %d, want 1; result: %s", got, bb.Result)
	}

	goals, _ := bb.ChainState["goap_fusion_goal_queue"].(string)
	if !strings.Contains(goals, "Unblock engine tests") {
		t.Fatalf("prioritization dropped the P0 engine-test goal for a genuine, affirmative "+
			"build blocker:\n%s", goals)
	}
}

// --- Durable Claude rate-limit backoff state ---------------------------------
//
// Rate-limited Claude outcomes are recorded (goap_fusion_rate_limited) but
// consumed nowhere, so every cron tick burns 15-minute Claude retry budgets
// against a quota that is known to be closed. The fix is a durable backoff
// primitive following the saveGrillState/saveSuperpowersPlanState agent-scope
// pattern: an RFC3339 goap_fusion_claude_backoff_until timestamp in the
// agent-scope blackboard (primary) with ChainState fallback, and a
// claudeBackoffActive(bb, now) guard the loop can consult before spending a
// Claude attempt.

// TestClaudeBackoffState_PersistsAcrossRuns proves the core contract: the
// backoff deadline saved by one cron tick must be visible to the next tick,
// which runs with a completely fresh Blackboard (RunOnce kills ChainState).
// Only the agent-scope store survives, exactly like grill and plan state.
func TestClaudeBackoffState_PersistsAcrossRuns(t *testing.T) {
	isolateClaudeBackoffStore(t)
	mgr := blackboard.NewManager(nil)
	run1 := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop")}
	run2 := &Blackboard{BB: blackboard.NewHandle(mgr, "run-2", "", "goap-loop")}

	// Second-precision instant: the state is stored as RFC3339 text, so the
	// round-trip contract is at second granularity.
	until := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	saveClaudeBackoffState(run1, until)

	got, ok := loadClaudeBackoffState(run2)
	if !ok {
		t.Fatal("loadClaudeBackoffState on a fresh run = inactive, want the deadline saved by the previous run: backoff must persist via the agent-scope store")
	}
	if !got.Equal(until) {
		t.Fatalf("loadClaudeBackoffState = %v, want %v: RFC3339 round-trip must preserve the deadline", got, until)
	}
	if !claudeBackoffActive(run2, until.Add(-time.Hour)) {
		t.Fatal("claudeBackoffActive before the persisted deadline = false, want true: the next tick must skip Claude while the window is open")
	}
}

// TestClaudeBackoffState_ChainStateFallback: with the scoped blackboard
// disabled (unit paths, scope-off deployments) the state must still round-trip
// within a run via ChainState, mirroring loadGrillState/loadSuperpowersPlanState.
func TestClaudeBackoffState_ChainStateFallback(t *testing.T) {
	isolateClaudeBackoffStore(t)
	bb := &Blackboard{}

	until := time.Date(2026, 7, 8, 18, 30, 0, 0, time.UTC)
	saveClaudeBackoffState(bb, until)

	got, ok := loadClaudeBackoffState(bb)
	if !ok || !got.Equal(until) {
		t.Fatalf("loadClaudeBackoffState via ChainState fallback = (%v, %v), want (%v, true)", got, ok, until)
	}
	if !claudeBackoffActive(bb, until.Add(-time.Minute)) {
		t.Fatal("claudeBackoffActive via ChainState fallback = false inside the window, want true")
	}
}

// TestClaudeBackoffActive_WindowExpiry: active strictly inside the window,
// inactive once now passes until — and, mirroring the half-open lesson from the
// runaway-backstop fix, an expired window must SELF-CLEAR so stale state can
// never wedge the loop into a permanent skip.
func TestClaudeBackoffActive_WindowExpiry(t *testing.T) {
	isolateClaudeBackoffStore(t)
	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop")}

	until := time.Date(2026, 7, 8, 6, 0, 0, 0, time.UTC)
	saveClaudeBackoffState(bb, until)

	if !claudeBackoffActive(bb, until.Add(-30*time.Minute)) {
		t.Fatal("claudeBackoffActive 30m before the deadline = false, want true")
	}
	if claudeBackoffActive(bb, until.Add(30*time.Minute)) {
		t.Fatal("claudeBackoffActive 30m after the deadline = true, want false: an elapsed window must reopen Claude attempts")
	}
	// The expired check must have cleared the durable state (self-clearing /
	// half-open), so a fresh run sees no backoff at all.
	fresh := &Blackboard{BB: blackboard.NewHandle(mgr, "run-2", "", "goap-loop")}
	if _, ok := loadClaudeBackoffState(fresh); ok {
		t.Fatal("expired backoff state survived the active check: it must self-clear so stale state cannot wedge future ticks")
	}
}

// TestClaudeBackoffState_ClearCounterpart: clearClaudeBackoffState wipes both
// the agent-scope entry and the ChainState fallback, like clearSuperpowersPlanState.
func TestClaudeBackoffState_ClearCounterpart(t *testing.T) {
	isolateClaudeBackoffStore(t)
	mgr := blackboard.NewManager(nil)
	run1 := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop")}
	run2 := &Blackboard{BB: blackboard.NewHandle(mgr, "run-2", "", "goap-loop")}

	saveClaudeBackoffState(run1, time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC))
	clearClaudeBackoffState(run1)

	if _, ok := loadClaudeBackoffState(run2); ok {
		t.Fatal("loadClaudeBackoffState after clear = active, want inactive on both stores")
	}
}

// TestClaudeBackoffState_MissingOrMalformedIsInactive: no state and garbage
// state must both read as "no backoff" — a malformed timestamp must never
// wedge the loop into skipping Claude forever (nor panic).
func TestClaudeBackoffState_MissingOrMalformedIsInactive(t *testing.T) {
	isolateClaudeBackoffStore(t)
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)

	// Missing entirely.
	empty := &Blackboard{}
	if _, ok := loadClaudeBackoffState(empty); ok {
		t.Fatal("loadClaudeBackoffState on an empty blackboard = active, want inactive")
	}
	if claudeBackoffActive(empty, now) {
		t.Fatal("claudeBackoffActive with no saved state = true, want false")
	}

	// Malformed in the agent-scope store.
	mgr := blackboard.NewManager(nil)
	scoped := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop")}
	scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: "goap-loop"}
	if err := scoped.BB.Mgr.Set(scope, "goap_fusion_claude_backoff_until", "not-a-timestamp", "corrupt backoff state", "text"); err != nil {
		t.Fatalf("seeding malformed agent-scope state: %v", err)
	}
	if _, ok := loadClaudeBackoffState(scoped); ok {
		t.Fatal("loadClaudeBackoffState with a malformed agent-scope timestamp = active, want inactive")
	}
	if claudeBackoffActive(scoped, now) {
		t.Fatal("claudeBackoffActive with a malformed agent-scope timestamp = true, want false: corrupt state must never block Claude attempts")
	}

	// Malformed in the ChainState fallback.
	fallback := &Blackboard{ChainState: map[string]any{
		"goap_fusion_claude_backoff_until": "yesterday-ish",
	}}
	if claudeBackoffActive(fallback, now) {
		t.Fatal("claudeBackoffActive with a malformed ChainState timestamp = true, want false")
	}
}

// TestClaudeBackoffWindow_EnvOverride: the backoff window defaults to 6h,
// honors a parsable BT_GOAP_CLAUDE_BACKOFF override, and falls back to 6h on
// garbage (mirroring the getenvDefault convention used for runner config).
func TestClaudeBackoffWindow_EnvOverride(t *testing.T) {
	t.Setenv("BT_GOAP_CLAUDE_BACKOFF", "")
	if got := claudeBackoffWindow(); got != 6*time.Hour {
		t.Fatalf("claudeBackoffWindow with no env = %v, want 6h default", got)
	}

	t.Setenv("BT_GOAP_CLAUDE_BACKOFF", "45m")
	if got := claudeBackoffWindow(); got != 45*time.Minute {
		t.Fatalf("claudeBackoffWindow with BT_GOAP_CLAUDE_BACKOFF=45m = %v, want 45m", got)
	}

	t.Setenv("BT_GOAP_CLAUDE_BACKOFF", "banana")
	if got := claudeBackoffWindow(); got != 6*time.Hour {
		t.Fatalf("claudeBackoffWindow with an unparsable override = %v, want the 6h fallback", got)
	}
}
