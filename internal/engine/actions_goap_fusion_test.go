package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/research"
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

// PrioritizeGoapGoals charges the active program's head milestone
// (RecordAttemptAndMaybeBlock) through research.UpdatePrograms' flock-held
// read-modify-write, the same primitive every other program-store writer
// (persistGoapProgram, RefundAttempt, RecordRedPass, MarkDone) uses. This
// test hammers the real call site: many concurrent persistGoapProgram
// registrations (flock-protected, each adding a distinct new program) race
// against many concurrent PrioritizeGoapGoals charges against a pre-seeded
// active program, sharing one programs.json. Every registered program must
// survive.
func TestPrioritizeGoapGoals_ConcurrentWithPersistGoapProgramAllSurvive(t *testing.T) {
	isolateGoapProgramStore(t)

	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		t.Fatal(err)
	}
	ps.Add("Active head program", "test", []string{"head milestone", "tail milestone"})
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}

	prioritize := GetAction("PrioritizeGoapGoals")
	if prioritize == nil {
		t.Fatal("action \"PrioritizeGoapGoals\" not registered")
	}

	const writers = 30
	const chargers = 30
	var wg sync.WaitGroup
	for i := range writers {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			bb := &Blackboard{ChainState: map[string]any{}}
			spec := &goapProgramSpec{
				Title:      fmt.Sprintf("Concurrent registered program %d", i),
				Milestones: []string{"m1"},
			}
			persistGoapProgram(bb, spec, "test")
		}()
	}
	for range chargers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bb := &Blackboard{ChainState: map[string]any{}}
			prioritize(&btcore.BTContext[Blackboard]{Blackboard: bb})
		}()
	}
	wg.Wait()

	final, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		t.Fatal(err)
	}
	// +1 for the pre-seeded "Active head program".
	if got, want := len(final.Programs), writers+1; got != want {
		t.Fatalf("lost %d registered program(s) under concurrent PrioritizeGoapGoals charges "+
			"and persistGoapProgram registrations: want %d programs, got %d", want-got, want, got)
	}
}

// TestActionsGoapFusionTestFile_ChargeRaceCommentNotStale pins that this
// file's own doc comment on TestPrioritizeGoapGoals_ConcurrentWithPersistGoapProgramAllSurvive
// no longer describes PrioritizeGoapGoals' program-store charge as an
// unmigrated, lock-free race. actions_goap_fusion.go's charge site now goes
// through research.UpdatePrograms' shared flock (see its own comment there),
// so language here claiming the call site "never takes the shared lock" or
// has "no flock coordination" is stale and misleads a reader into believing
// the race is still open. Scanning the source directly — rather than
// asserting on test behavior — is necessary because the stale prose doesn't
// affect what the test above actually exercises, which already passes.
func TestActionsGoapFusionTestFile_ChargeRaceCommentNotStale(t *testing.T) {
	src, err := os.ReadFile("actions_goap_fusion_test.go")
	if err != nil {
		t.Fatalf("reading actions_goap_fusion_test.go: %v", err)
	}
	body := string(src)
	// Only scan the prose that precedes this check: the check's own doc
	// comment and stale-phrase list below necessarily quote those phrases,
	// which would otherwise trip the check on itself.
	if idx := strings.Index(body, "TestActionsGoapFusionTestFile_ChargeRaceCommentNotStale"); idx >= 0 {
		body = body[:idx]
	}
	for _, stale := range []string{
		"explicitly left this charge site",
		"call site never takes the shared lock",
		"unlocked charge-and-save race",
		"no flock coordination",
	} {
		if strings.Contains(body, stale) {
			t.Errorf("actions_goap_fusion_test.go still claims the charge race is open (contains %q); "+
				"PrioritizeGoapGoals' charge now goes through research.UpdatePrograms' shared flock", stale)
		}
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

// TestClearSuperpowersPlanState_WipesDurableChargeStamps proves milestone 2 of
// the Q3 Reliability charge-stamp program: clearSuperpowersPlanState is called
// from every path that retires a completed or abandoned Superpowers cycle
// (actions_superpowers_prod.go:804,917,1066), but it only ever wiped the two
// plan-resume keys. PrioritizeGoapGoals stamps FOUR durable charge-stamp keys
// via setGoapStateDurable — program_milestone_charged, program_milestone,
// research_goal_charged, research_goal_charged_text — into the agent-scope
// store so a resumed cron tick (fresh ChainState) can still charge/refund the
// right cycle. If clearSuperpowersPlanState leaves those stamps behind, a
// later, unrelated cycle's failure handler (chargeGoapResearchGoalFailure /
// refundGoapMilestoneAttemptForInfraFailure) reads a stale stamp from a
// completed or abandoned cycle and charges or refunds the wrong goal/milestone.
func TestClearSuperpowersPlanState_WipesDurableChargeStamps(t *testing.T) {
	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop")}

	setGoapStateDurable(bb, "program_milestone_charged", "prog-1:0")
	setGoapStateDurable(bb, "program_milestone", "prog-1:0,prog-1:1")
	setGoapStateDurable(bb, "research_goal_charged", "adopt-the-legacy-island-archive")
	setGoapStateDurable(bb, "research_goal_charged_text", "Adopt the legacy island archive exactly once")

	clearSuperpowersPlanState(bb)

	scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: "goap-loop"}
	for _, key := range []string{
		"goap_fusion_program_milestone_charged",
		"goap_fusion_program_milestone",
		"goap_fusion_research_goal_charged",
		"goap_fusion_research_goal_charged_text",
	} {
		if e, err := mgr.Get(scope, key); err == nil {
			t.Errorf("agent-scope store still holds %q = %q after clearSuperpowersPlanState: a completed/abandoned cycle's charge stamp must not leak into a later cycle's failure handler", key, e.Value)
		}
		if _, ok := bb.ChainState[key]; ok {
			t.Errorf("ChainState still holds %q after clearSuperpowersPlanState", key)
		}
	}
}

// TestLoadGoapChargeStampsDurable_RestoresFromAgentScope proves the read-back
// counterpart of setGoapStateDurable (actions_goap_fusion.go:688): a resumed
// cron tick builds a fresh Blackboard (RunOnce) whose ChainState dies with the
// run, so program_milestone_charged, program_milestone, research_goal_charged,
// and research_goal_charged_text must be restored from the agent-scope store
// into ChainState before the resumed tick's failure handlers
// (chargeGoapResearchGoalFailure / refundGoapMilestoneAttemptForInfraFailure)
// look for them there.
func TestLoadGoapChargeStampsDurable_RestoresFromAgentScope(t *testing.T) {
	mgr := blackboard.NewManager(nil)
	originating := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop")}

	setGoapStateDurable(originating, "program_milestone_charged", "prog-1:0")
	setGoapStateDurable(originating, "program_milestone", "prog-1:0,prog-1:1")
	setGoapStateDurable(originating, "research_goal_charged", "adopt-the-legacy-island-archive")
	setGoapStateDurable(originating, "research_goal_charged_text", "Adopt the legacy island archive exactly once")

	// A resumed tick: same agent, brand-new Blackboard, empty ChainState.
	resumed := &Blackboard{
		BB:         blackboard.NewHandle(mgr, "run-2", "", "goap-loop"),
		ChainState: map[string]any{},
	}

	loadGoapChargeStampsDurable(resumed)

	want := map[string]string{
		"goap_fusion_program_milestone_charged":  "prog-1:0",
		"goap_fusion_program_milestone":          "prog-1:0,prog-1:1",
		"goap_fusion_research_goal_charged":      "adopt-the-legacy-island-archive",
		"goap_fusion_research_goal_charged_text": "Adopt the legacy island archive exactly once",
	}
	for key, wantVal := range want {
		got, _ := resumed.ChainState[key].(string)
		if got != wantVal {
			t.Errorf("ChainState[%q] after loadGoapChargeStampsDurable = %q, want %q restored from the agent-scope store", key, got, wantVal)
		}
	}
}

// TestLoadGoapChargeStampsDurable_DoesNotClobberFresherChainState proves the
// fill-only-if-absent contract: an in-flight originating tick already holds a
// fresher value in ChainState (e.g. it just charged a NEW milestone this very
// tick, before setGoapStateDurable's agent-scope write is even relevant to it)
// and loadGoapChargeStampsDurable must never overwrite that with a stale
// agent-scope value from a prior, unrelated cycle.
func TestLoadGoapChargeStampsDurable_DoesNotClobberFresherChainState(t *testing.T) {
	mgr := blackboard.NewManager(nil)
	prior := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop")}
	setGoapStateDurable(prior, "program_milestone_charged", "prog-1:0")

	fresh := &Blackboard{
		BB: blackboard.NewHandle(mgr, "run-2", "", "goap-loop"),
		ChainState: map[string]any{
			"goap_fusion_program_milestone_charged": "prog-2:5",
		},
	}

	loadGoapChargeStampsDurable(fresh)

	if got := fresh.ChainState["goap_fusion_program_milestone_charged"]; got != "prog-2:5" {
		t.Errorf("ChainState[\"goap_fusion_program_milestone_charged\"] after loadGoapChargeStampsDurable = %q, want the pre-existing %q left untouched: an in-flight tick's fresher value must never be clobbered by a stale durable stamp", got, "prog-2:5")
	}
}

// TestArc42ReadGraphReport_UsesCanonicalGoapFusionReportPath proves the arc42
// "ReadGraphReport" action reads the same canonical report source as
// actions_goap_fusion.go's "ReadGraphifyReport" action — the overridable,
// absolute goapFusionGraphReport path — instead of its own hardcoded
// cwd-relative "graphify-out/GRAPH_REPORT.md". It also proves ReadGraphReport
// applies the same section-aware extraction (sectionAwareGraphContext)
// instead of dumping the full untruncated report into bb.CachedResult.
// Regression context: Q5 Consistency & Reuse milestone 3/5 — one canonical
// graphify-report reader.
func TestArc42ReadGraphReport_UsesCanonicalGoapFusionReportPath(t *testing.T) {
	dir := t.TempDir()
	reportPath := filepath.Join(dir, "GRAPH_REPORT.md")
	const marker = "CANONICAL_READER_MARKER_9f3a"
	if err := os.WriteFile(reportPath, []byte(syntheticGraphifyReportDeep(marker)), 0o644); err != nil {
		t.Fatal(err)
	}

	old := goapFusionGraphReport
	goapFusionGraphReport = reportPath
	t.Cleanup(func() { goapFusionGraphReport = old })

	bb := &Blackboard{}
	status := callArc42Action(t, "ReadGraphReport", bb)
	if status != 1 {
		t.Fatalf("ReadGraphReport status = %d, want 1", status)
	}

	if !strings.Contains(bb.CachedResult, marker) {
		t.Fatalf("ReadGraphReport must read the canonical, overridable goapFusionGraphReport path "+
			"(same one actions_goap_fusion.go uses) instead of the hardcoded cwd-relative "+
			"\"graphify-out/GRAPH_REPORT.md\"; CachedResult = %.200q", bb.CachedResult)
	}
	if strings.Contains(bb.CachedResult, "filler filler filler") {
		t.Fatalf("ReadGraphReport must apply the same section-aware extraction " +
			"(sectionAwareGraphContext) actions_goap_fusion.go uses, not an untruncated raw dump " +
			"of the whole report")
	}
}

// TestGraphIsFresh_ChecksBuiltCommitAgainstHEAD proves GraphIsFresh has a real
// freshness signal — the report's "Built from commit: `<sha>`" line (under
// "## Graph Freshness") compared against the current HEAD of goapFusionRepo —
// instead of merely checking that a report file exists on disk. A report
// built from a stale commit must read as NOT fresh even though the file is
// present; a report built from the current HEAD must read as fresh.
// Regression context: Q5 Consistency & Reuse milestone 3/5.
func TestGraphIsFresh_ChecksBuiltCommitAgainstHEAD(t *testing.T) {
	repo, firstSHA := newReviewTestRepo(t) // two commits; firstSHA = first (now stale) commit
	headOut, err := runGoapGit(repo, 5*time.Second, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	head := strings.TrimSpace(headOut)

	oldRepo := goapFusionRepo
	goapFusionRepo = repo
	t.Cleanup(func() { goapFusionRepo = oldRepo })

	reportPath := filepath.Join(t.TempDir(), "GRAPH_REPORT.md")
	oldReport := goapFusionGraphReport
	goapFusionGraphReport = reportPath
	t.Cleanup(func() { goapFusionGraphReport = oldReport })

	fresh := GetCondition("GraphIsFresh")
	if fresh == nil {
		t.Fatal("GraphIsFresh not registered")
	}

	staleReport := "## Summary\nx\n\n## Graph Freshness\n- Built from commit: `" + firstSHA[:8] +
		"`\n\n## Community Hubs\n"
	if err := os.WriteFile(reportPath, []byte(staleReport), 0o644); err != nil {
		t.Fatal(err)
	}
	if fresh(&Blackboard{}) {
		t.Fatal("GraphIsFresh must be false when the report's built-from commit predates goapFusionRepo's " +
			"current HEAD — file existence alone is not a freshness signal")
	}

	currentReport := "## Summary\nx\n\n## Graph Freshness\n- Built from commit: `" + head[:8] +
		"`\n\n## Community Hubs\n"
	if err := os.WriteFile(reportPath, []byte(currentReport), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fresh(&Blackboard{}) {
		t.Fatal("GraphIsFresh must be true when the report's built-from commit matches goapFusionRepo's current HEAD")
	}
}
