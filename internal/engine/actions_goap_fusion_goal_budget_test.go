package engine

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	btcore "github.com/rvitorper/go-bt/core"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/research"
)

// Research-goal failure budget: program milestones block after repeated failed
// attempts, but notebooklm-lane P0 goals had no budget — on 2026-07-10 one
// goal burned 11 blind implementation attempts on the same lint failure. The
// queue must abandon a goal after goapGoalMaxAttempts GENUINE failures
// (infrastructure failures are exempt, mirroring the milestone refund), and
// the next attempt must be steered by the recorded failure instead of blind.

// seedGoalBudget points goapGoalAttemptsPath at a temp store and returns it.
func seedGoalBudget(t *testing.T) *research.GoalAttemptStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "goal-attempts.json")
	prev := goapGoalAttemptsPath
	goapGoalAttemptsPath = path
	t.Cleanup(func() { goapGoalAttemptsPath = prev })
	s, err := research.OpenGoalAttempts(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// isolateGoapProgramStore points goapProgramsPath at an empty temp store.
// Tests that run PrioritizeGoapGoals MUST call this: the action records a
// milestone attempt against the ACTIVE program at queue time, so a test using
// the default path charges — and can even block — a live milestone in the
// operator's real ~/.go-bt-evolve store on every test run (this silently
// blocked 0977b1fa ms1 during the 2026-07-10 landing gates).
func isolateGoapProgramStore(t *testing.T) {
	t.Helper()
	prev := goapProgramsPath
	goapProgramsPath = filepath.Join(t.TempDir(), "programs.json")
	t.Cleanup(func() { goapProgramsPath = prev })
}

func reloadGoalBudget(t *testing.T) *research.GoalAttemptStore {
	t.Helper()
	s, err := research.OpenGoalAttempts(goapGoalAttemptsPath)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The same goal must key identically wherever it appears: raw in the gaps
// list, prefixed in the queue, or annotated with a previous-failure note in a
// rebuilt plan. Different goals must not collide.
func TestGoapResearchGoalKey_NormalizesPrefixesAndFailureNote(t *testing.T) {
	raw := "Adopt the legacy island archive exactly once (files: cmd/bt-agent/tools.go)"
	variants := []string{
		raw,
		"[P0] NotebookLM research: " + raw,
		"[P0] " + raw,
		raw + " [PREVIOUS-ATTEMPT-FAILURE: golangci-lint nilerr at tools.go:90 — fix this explicitly]",
	}
	want := goapResearchGoalKey(raw)
	for _, v := range variants {
		if got := goapResearchGoalKey(v); got != want {
			t.Errorf("goapResearchGoalKey(%q) = %q, want %q (same goal must key identically)", v, got, want)
		}
	}
	if goapResearchGoalKey("A completely different goal (files: a.go)") == want {
		t.Error("different goals must not share a budget key")
	}
}

// PrioritizeGoapGoals must drop research goals whose budget is exhausted and
// stamp the head SURVIVING research goal for the failure charge.
func TestPrioritizeGoapGoals_AbandonsExhaustedResearchGoal(t *testing.T) {
	isolateGoapProgramStore(t)
	store := seedGoalBudget(t)
	goalA := "Fix the flaky frobnicator (files: internal/engine/tree.go)"
	goalB := "Harden the widget parser (files: internal/engine/chains.go)"
	for range goapGoalMaxAttempts {
		store.RecordFailure(goapResearchGoalKey(goalA), "commit gate: nilerr")
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	prioritize := GetAction("PrioritizeGoapGoals")
	if prioritize == nil {
		t.Fatal("action \"PrioritizeGoapGoals\" not registered")
	}
	bb := &Blackboard{ChainState: map[string]any{
		"goap_fusion_improvement_gaps": "NOTEBOOKLM_GOAL: " + goalA + "\nNOTEBOOKLM_GOAL: " + goalB,
	}}
	if got := prioritize(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("PrioritizeGoapGoals status = %d, want 1; result: %s", got, bb.Result)
	}

	queue, _ := bb.ChainState["goap_fusion_goal_queue"].(string)
	if strings.Contains(queue, "flaky frobnicator") {
		t.Fatalf("budget-exhausted goal must be dropped from the queue:\n%s", queue)
	}
	if !strings.Contains(queue, "widget parser") {
		t.Fatalf("surviving research goal must stay queued:\n%s", queue)
	}
	charged, _ := bb.ChainState["goap_fusion_research_goal_charged"].(string)
	if charged != goapResearchGoalKey(goalB) {
		t.Fatalf("charged stamp = %q, want the head SURVIVING goal's key %q", charged, goapResearchGoalKey(goalB))
	}
	chargedText, _ := bb.ChainState["goap_fusion_research_goal_charged_text"].(string)
	if chargedText != goalB {
		t.Fatalf("charged text stamp = %q, want the goal text %q (red-pass closure records it goap:implemented)", chargedText, goalB)
	}
	abandoned, _ := bb.ChainState["goap_fusion_research_goals_abandoned"].(string)
	if !strings.Contains(abandoned, "frobnicator") {
		t.Fatalf("abandoned goals must be surfaced for the analysis note; got %q", abandoned)
	}
}

// PrioritizeGoapGoals's charge stamps must survive a fresh cron tick's
// ChainState death: a resumed-carryover cycle (RunScheduledGoapFusionCycle)
// builds a brand-new Blackboard whose ChainState is empty, so the queue-time
// research-goal stamp must also land in the durable agent-scope blackboard
// (mirroring saveSuperpowersPlanState/saveGrillState) — not ChainState alone.
func TestPrioritizeGoapGoals_StampsResearchGoalDurably(t *testing.T) {
	isolateGoapProgramStore(t)
	seedGoalBudget(t)
	goal := "Adopt the legacy island archive exactly once (files: cmd/bt-agent/tools.go)"

	prioritize := GetAction("PrioritizeGoapGoals")
	if prioritize == nil {
		t.Fatal("action \"PrioritizeGoapGoals\" not registered")
	}
	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{
		BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop"),
		ChainState: map[string]any{
			"goap_fusion_improvement_gaps": "NOTEBOOKLM_GOAL: " + goal,
		},
	}
	if got := prioritize(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("PrioritizeGoapGoals status = %d, want 1; result: %s", got, bb.Result)
	}

	scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: "goap-loop"}
	wantKey := goapResearchGoalKey(goal)
	e, err := mgr.Get(scope, "goap_fusion_research_goal_charged")
	if err != nil {
		t.Fatalf("agent-scope store must hold the research-goal charge stamp durably (not ChainState-only): %v", err)
	}
	if e.Value != wantKey {
		t.Fatalf("durable research-goal charge stamp = %q, want %q", e.Value, wantKey)
	}
	textEntry, err := mgr.Get(scope, "goap_fusion_research_goal_charged_text")
	if err != nil {
		t.Fatalf("agent-scope store must hold the research-goal charge text durably: %v", err)
	}
	if textEntry.Value != goal {
		t.Fatalf("durable research-goal charge text = %q, want %q", textEntry.Value, goal)
	}
}

// A genuine implementation failure charges the stamped goal exactly once per
// cycle; the recorded tail steers the next attempt.
func TestChargeGoapResearchGoalFailure_ChargesOncePerCycle(t *testing.T) {
	seedGoalBudget(t)
	key := goapResearchGoalKey("some goal (files: a.go)")
	bb := &Blackboard{ChainState: map[string]any{
		"goap_fusion_research_goal_charged": key,
	}}
	bb.Result = "## GOAP Superpowers Pending Patch\n\napplied_uncommitted: golangci-lint nilerr tools.go:90"

	if !chargeGoapResearchGoalFailure(bb) {
		t.Fatal("charge must fire when a stamp exists")
	}
	if got := reloadGoalBudget(t).Count(key); got != 1 {
		t.Fatalf("store attempts = %d, want 1", got)
	}
	if fail := reloadGoalBudget(t).LastFailure(key); !strings.Contains(fail, "nilerr") {
		t.Fatalf("failure tail must be recorded; got %q", fail)
	}
	if chargeGoapResearchGoalFailure(bb) {
		t.Fatal("second charge in the same cycle must be a no-op")
	}
	if got := reloadGoalBudget(t).Count(key); got != 1 {
		t.Fatalf("idempotence violated: attempts = %d, want 1", got)
	}

	// No stamp → nothing to charge.
	if chargeGoapResearchGoalFailure(&Blackboard{ChainState: map[string]any{}}) {
		t.Fatal("a cycle without a research-goal stamp must not charge")
	}
}

// Wiring: a genuine (non-infra) -1 exit from the scheduled runtime charges the
// stamped goal; an infra exit (rate-limit backoff) does NOT.
func TestScheduledRuntime_ChargesGoalOnGenuineFailureOnly(t *testing.T) {
	isolateClaudeBackoffStore(t)
	seedGoalBudget(t)
	key := goapResearchGoalKey("head research goal (files: a.go)")

	// Genuine failure: routed to implementation with no saved plan.
	genuine := &Blackboard{ChainState: map[string]any{
		"goap_fusion_research_goal_charged": key,
	}}
	if got := runSuperpowersRuntimeFromExistingPlanAction(&btcore.BTContext[Blackboard]{Blackboard: genuine}); got != -1 {
		t.Fatalf("no-plan runtime = %d, want -1", got)
	}
	if got := reloadGoalBudget(t).Count(key); got != 1 {
		t.Fatalf("genuine failure must charge the goal; attempts = %d, want 1", got)
	}

	// Infra failure: rate-limit backoff entry guard must NOT charge.
	infra := &Blackboard{ChainState: map[string]any{
		"plan_path":                         "/nonexistent/plan.md",
		"goap_fusion_research_goal_charged": key,
	}}
	saveClaudeBackoffState(infra, time.Now().Add(time.Hour))
	if got := runSuperpowersRuntimeFromExistingPlanAction(&btcore.BTContext[Blackboard]{Blackboard: infra}); got != -1 {
		t.Fatalf("backoff runtime = %d, want -1", got)
	}
	if got := reloadGoalBudget(t).Count(key); got != 1 {
		t.Fatalf("infra failure must NOT charge the goal; attempts = %d, want still 1", got)
	}
}

// A resumed cron tick builds a fresh Blackboard whose ChainState never saw
// PrioritizeGoapGoals stamp the research-goal charge — only the durable
// agent-scope store did (setGoapStateDurable). runSuperpowersRuntimeFromExistingPlanAction
// must load that durable stamp back into ChainState before its deferred
// failure handler runs, or a genuine failure on the resumed tick goes
// uncharged (see goap-fusion-resume-charge-stamps-chainstate-only memory).
func TestScheduledRuntime_ResumedTickChargesGoalOnGenuineFailure(t *testing.T) {
	isolateClaudeBackoffStore(t)
	seedGoalBudget(t)
	goal := "resumed research goal (files: internal/engine/tree.go)"
	key := goapResearchGoalKey(goal)

	mgr := blackboard.NewManager(nil)

	// Originating tick: PrioritizeGoapGoals stamps the charge durably.
	originating := &Blackboard{
		BB:         blackboard.NewHandle(mgr, "run-originating", "", "resume-goal-agent"),
		ChainState: map[string]any{},
	}
	setGoapStateDurable(originating, "research_goal_charged", key)
	setGoapStateDurable(originating, "research_goal_charged_text", goal)

	// Resumed tick: fresh Blackboard, empty ChainState, same agent/BB handle
	// (as RunScheduledGoapFusionCycle builds on a resumed cron tick). Seed the
	// plan path so the runtime does not short-circuit on "no plan", and force
	// a genuine (non-infra) failure by pointing it at a plan file that does
	// not exist.
	resumed := &Blackboard{
		BB: blackboard.NewHandle(mgr, "run-resumed", "", "resume-goal-agent"),
		ChainState: map[string]any{
			"plan_path": filepath.Join(t.TempDir(), "nonexistent-plan.md"),
		},
		Outcome: "goap_fusion_impl_failed",
	}

	if got := runSuperpowersRuntimeFromExistingPlanAction(&btcore.BTContext[Blackboard]{Blackboard: resumed}); got != -1 {
		t.Fatalf("resumed runtime = %d, want -1; result: %s", got, resumed.Result)
	}
	if class := classifyGoapCycleFailure(resumed.Outcome, resumed.Result); class != goapCycleFailureGenuine {
		t.Fatalf("test setup bug: resumed failure classified as %q, want genuine; result: %s", class, resumed.Result)
	}

	if got := reloadGoalBudget(t).Count(key); got != 1 {
		t.Fatalf("resumed tick must charge the goal via the durable stamp; attempts = %d, want 1", got)
	}
}

// The plan builder must steer retries: a goal with a recorded failure carries
// the parse-safe failure note into its task section, and a clean goal does not.
func TestBuildGoalDrivenPlan_InjectsPreviousFailureNote(t *testing.T) {
	store := seedGoalBudget(t)
	goal := "[P0] Migrate the legacy archive (files: internal/engine/tree.go)"
	task := "Improve platform\n" + goal

	clean := buildGoalDrivenImplementationPlan(task)
	if strings.Contains(clean, "PREVIOUS-ATTEMPT-FAILURE") {
		t.Fatalf("clean goal must carry no failure note:\n%s", clean)
	}

	store.RecordFailure(goapResearchGoalKey(goal), "golangci-lint: nilerr — error is not nil but it returns nil")
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	steered := buildGoalDrivenImplementationPlan(task)
	if !strings.Contains(steered, "PREVIOUS-ATTEMPT-FAILURE") || !strings.Contains(steered, "nilerr") {
		t.Fatalf("retry plan must carry the previous failure note:\n%s", steered)
	}
	// The annotated plan must still parse into tasks.
	if _, err := ParseSuperpowersPlan(steered); err != nil {
		t.Fatalf("annotated plan no longer parses: %v", err)
	}
}

// WriteSuperpowersImplementationPlan scopes a pathless goal via
// scopeGoapGoalLine BEFORE buildGoalDrivenImplementationPlan ever sees the
// task text, appending a "(files: …)" suffix the goal never had when
// PrioritizeGoapGoals stamped its failure-charge key (chargeGoapResearchGoalFailure
// keys off the goal as queued, pre-scoping). The failure-note lookup inside
// buildGoalDrivenImplementationPlan must strip that scope suffix the same way
// it strips the failure-note and reuse-note markers, or a scoped retry's
// steering note goes missing because the key shifted.
func TestBuildGoalDrivenPlan_InjectsPreviousFailureNoteThroughScopeSuffix(t *testing.T) {
	store := seedGoalBudget(t)
	rawGoal := "Fix the flaky frobnicator"
	store.RecordFailure(goapResearchGoalKey(rawGoal), "golangci-lint: nilerr — error is not nil but it returns nil")
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	// Simulates WriteSuperpowersImplementationPlan: scopeGoapGoalLine appended
	// "(files: …)" to the originally-pathless goal before this task text ever
	// reached the plan builder.
	scopedGoal := "[P0] " + rawGoal + " (files: internal/engine/tree.go)"
	task := "Improve platform\n" + scopedGoal

	steered := buildGoalDrivenImplementationPlan(task)
	if !strings.Contains(steered, "PREVIOUS-ATTEMPT-FAILURE") || !strings.Contains(steered, "nilerr") {
		t.Fatalf("retry plan must carry the previous failure note even though the goal line carries scopeGoapGoalLine's scope suffix:\n%s", steered)
	}
}

// goapGoalFailureNote must preserve the actionable END of the recorded
// failure tail. RecordFailure already keeps the TAIL of the raw output (the
// actionable lint/test lines of a commit-gate transcript come last), but
// goapGoalFailureNote re-truncates that tail with truncateGoap, which keeps
// the HEAD — discarding the actionable ending a second time.
func TestGoapGoalFailureNote_PreservesActionableEnding(t *testing.T) {
	store := seedGoalBudget(t)
	goal := "Fix the flaky frobnicator (files: internal/engine/tree.go)"
	key := goapResearchGoalKey(goal)

	// Longer than both truncation limits (1200 tail-kept by RecordFailure, 500
	// head-kept by goapGoalFailureNote), with the actionable content at the
	// very end, mirroring a real commit-gate transcript.
	noise := strings.Repeat("x", 1400)
	failure := noise + "ACTIONABLE_MARKER_XYZ"
	store.RecordFailure(key, failure)
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	note := goapGoalFailureNote(goal)
	if !strings.Contains(note, "ACTIONABLE_MARKER_XYZ") {
		t.Fatalf("failure note must preserve the actionable ending of the recorded failure tail, got %q", note)
	}
}

// Landing clears the budget: a goal that finally ships stops carrying stale
// failure state (and its earlier attempts no longer count toward abandon).
func TestRecordImplementedGoals_ClearsGoalBudget(t *testing.T) {
	store := seedGoalBudget(t)
	prevKnowledge := btFusionKnowledgePath
	btFusionKnowledgePath = filepath.Join(t.TempDir(), "knowledge.json")
	t.Cleanup(func() { btFusionKnowledgePath = prevKnowledge })

	goal := "[P0] NotebookLM research: Adopt the legacy archive (files: cmd/bt-agent/tools.go)"
	key := goapResearchGoalKey(goal)
	store.RecordFailure(key, "earlier failure")
	store.RecordFailure(key, "another failure")
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	run := &SuperpowersRun{Tasks: []SuperpowersTask{{
		Title:     "Task 1",
		Objective: goal,
		Status:    "done",
	}}}
	recordImplementedGoals(run)

	if got := reloadGoalBudget(t).Count(key); got != 0 {
		t.Fatalf("landing must clear the goal budget; attempts = %d, want 0", got)
	}
}
