package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	btcore "github.com/rvitorper/go-bt/core"
)

// TestSuperpowersTaskRedAction_PopulatesPerTaskArtifactDir proves the
// SuperpowersTaskRed action is self-contained: driving two different tasks
// through it (as ForEachTask would, one BT tick per task index) must leave
// each task with its own distinct, non-empty ArtifactDir instead of the two
// tasks colliding on an empty path. Before the fix, currentSuperpowersForEachTask
// never populated task.ArtifactDir, so both tasks would share the same
// (empty) artifact directory.
func TestSuperpowersTaskRedAction_PopulatesPerTaskArtifactDir(t *testing.T) {
	// Guard against relative-path writes escaping into the repo if
	// ArtifactDir is ever empty (pre-fix behavior) — run from a scratch dir.
	t.Chdir(t.TempDir())

	prevRunner, prevClaude := defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner
	t.Cleanup(func() {
		defaultSuperpowersCommandRunner = prevRunner
		defaultSuperpowersClaudeRunner = prevClaude
	})
	runner := &scriptedSuperpowersRunner{t: t}
	claude := &scriptedClaudeRunner{}
	defaultSuperpowersCommandRunner = runner
	defaultSuperpowersClaudeRunner = claude

	run := &SuperpowersRun{
		ID:           "run-red-artifact",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: t.TempDir(),
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
		Tasks: []SuperpowersTask{
			{Index: 1, Title: "First task", Tests: []string{"true"}},
			{Index: 2, Title: "Second task", Tests: []string{"true"}},
		},
	}

	act := GetAction("SuperpowersTaskRed")
	if act == nil {
		t.Fatal("SuperpowersTaskRed not registered")
	}

	for i := range run.Tasks {
		bb := newTestBlackboard()
		setSuperpowersRun(bb, run)
		bb.ChainState["superpowers_task_index"] = i
		if result := act(&btcore.BTContext[Blackboard]{Blackboard: bb}); result != 1 {
			t.Fatalf("task %d: SuperpowersTaskRed result = %d, want SUCCESS; bb.Result=%s", i, result, bb.Result)
		}
	}

	if run.Tasks[0].ArtifactDir == "" {
		t.Fatalf("task 0 ArtifactDir was not populated")
	}
	if run.Tasks[1].ArtifactDir == "" {
		t.Fatalf("task 1 ArtifactDir was not populated")
	}
	if run.Tasks[0].ArtifactDir == run.Tasks[1].ArtifactDir {
		t.Fatalf("expected distinct per-task ArtifactDir, both tasks got %q", run.Tasks[0].ArtifactDir)
	}
}

// TestSuperpowersTaskVerifyRed_NoTestsFailsWithoutPanic proves that running
// SuperpowersTaskVerifyRed against a task with no Tests degrades to a clear
// FAILURE instead of panicking on the unguarded task.Tests[0] index. This
// mirrors ensureSuperpowersTaskSetup / ExecuteTask's own semantics for a
// no-test task: FAILURE with an explanatory message, not a silent skip.
func TestSuperpowersTaskVerifyRed_NoTestsFailsWithoutPanic(t *testing.T) {
	t.Chdir(t.TempDir())

	run := &SuperpowersRun{
		ID:           "run-notests",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: t.TempDir(),
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
		Tasks: []SuperpowersTask{
			{Index: 1, Title: "No tests task", Files: []string{"internal/engine/foo.go"}},
		},
	}
	bb := newTestBlackboard()
	setSuperpowersRun(bb, run)
	bb.ChainState["superpowers_task_index"] = 0

	act := GetAction("SuperpowersTaskVerifyRed")
	if act == nil {
		t.Fatal("SuperpowersTaskVerifyRed not registered")
	}

	var result int
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SuperpowersTaskVerifyRed panicked on a task with no test commands: %v", r)
			}
		}()
		result = act(&btcore.BTContext[Blackboard]{Blackboard: bb})
	}()

	if result != -1 {
		t.Fatalf("result = %d, want -1 (FAILURE) for a task with no test commands", result)
	}
	if !strings.Contains(bb.Result, "no test command") {
		t.Fatalf("bb.Result = %q, want a failure message mentioning the missing test command", bb.Result)
	}
}

// commitScopeFakeRunner records every command it is asked to run so the test
// can assert on the exact `git add` invocation SuperpowersTaskCommit issues.
type commitScopeFakeRunner struct {
	calls []string
}

func (r *commitScopeFakeRunner) Run(_ context.Context, dir string, name string, args ...string) CommandResult {
	cmd := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.calls = append(r.calls, dir+" :: "+cmd)
	res := CommandResult{Command: cmd, Dir: dir, Duration: time.Millisecond}
	switch {
	case name == "bash" && len(args) >= 2 && args[0] == "-c" && strings.Contains(args[1], "git diff --cached --quiet"):
		// Report staged changes present so the action proceeds to commit.
		res.Err = errors.New("exit status 1")
		return res
	default:
		return res
	}
}

// TestSuperpowersTaskCommitAction_ExcludesGeneratedPaths proves the per-task
// commit scopes `git add -A` away from generated Superpowers/graphify
// artifacts (task evidence dirs, graphify-out/, docs/superpowers/**),
// mirroring the exclusion pathspecs commitAppliedSuperpowersRun already uses
// for the whole-run apply commit. Before the fix, the action ran a bare
// `git add -A` with no pathspec at all.
func TestSuperpowersTaskCommitAction_ExcludesGeneratedPaths(t *testing.T) {
	t.Chdir(t.TempDir())

	prevRunner := defaultSuperpowersCommandRunner
	t.Cleanup(func() { defaultSuperpowersCommandRunner = prevRunner })
	runner := &commitScopeFakeRunner{}
	defaultSuperpowersCommandRunner = runner

	run := &SuperpowersRun{
		ID:           "run-commit-scope",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: t.TempDir(),
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
		Tasks: []SuperpowersTask{
			{Index: 1, Title: "Scoped commit task", Tests: []string{"true"}},
		},
	}
	bb := newTestBlackboard()
	setSuperpowersRun(bb, run)
	bb.ChainState["superpowers_task_index"] = 0

	act := GetAction("SuperpowersTaskCommit")
	if act == nil {
		t.Fatal("SuperpowersTaskCommit not registered")
	}
	if result := act(&btcore.BTContext[Blackboard]{Blackboard: bb}); result != 1 {
		t.Fatalf("SuperpowersTaskCommit result = %d, want SUCCESS; bb.Result=%s", result, bb.Result)
	}

	var addCall string
	for _, c := range runner.calls {
		if strings.Contains(c, "git add") {
			addCall = c
			break
		}
	}
	if addCall == "" {
		t.Fatalf("expected a git add call, calls=%v", runner.calls)
	}
	for _, want := range []string{"graphify-out", "docs/superpowers/runs", "docs/superpowers/plans"} {
		if !strings.Contains(addCall, want) {
			t.Fatalf("git add call missing exclusion for %q: %s", want, addCall)
		}
	}

	foundCommit := false
	for _, c := range runner.calls {
		if strings.Contains(c, "git commit") {
			foundCommit = true
		}
	}
	if !foundCommit {
		t.Fatalf("expected a git commit call once changes are staged, calls=%v", runner.calls)
	}
}

// TestNlmGrillUnavailable_TrueMarkers proves finding 5(a): nlmGrillUnavailable
// must still classify real quota/rate-limit/auth failures as unavailable
// after the marker list is tightened for finding 3.
func TestNlmGrillUnavailable_TrueMarkers(t *testing.T) {
	cases := []struct {
		name string
		out  string
	}{
		{"resource_exhausted_upper", "Error: RESOURCE_EXHAUSTED — daily quota reached"},
		{"quota_exceeded_metric", "Quota exceeded for quota metric 'queries' and limit 'QueriesPerDay'"},
		{"rate_limit", "429: rate limit hit, please slow down"},
		{"stale_auth_marker", `{"auth_status":"stale","detail":"token expired"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !nlmGrillUnavailable(tc.out) {
				t.Fatalf("nlmGrillUnavailable(%q) = false, want true", tc.out)
			}
		})
	}
}

// TestNlmGrillUnavailable_FalseForLegitimateAnswerMentioningQuota is the
// finding-3 regression test: a real NotebookLM answer that happens to
// discuss "quota" as a concept must NOT be classified unavailable. A bare
// "quota" substring match previously misfired on exactly this kind of
// legitimate content.
func TestNlmGrillUnavailable_FalseForLegitimateAnswerMentioningQuota(t *testing.T) {
	out := "A1: set a quota of 5 calls per batch to respect the free-plan daily limit."
	if nlmGrillUnavailable(out) {
		t.Fatalf("nlmGrillUnavailable(%q) = true, want false (legitimate answer mentioning 'quota')", out)
	}
}

// TestParseNumberedAnswers_ExtractsNumberedAnswers proves finding 5(b):
// well-formed "A<n>: ..." output is split back into per-question text.
func TestParseNumberedAnswers_ExtractsNumberedAnswers(t *testing.T) {
	text := "A1: first answer\nA2: second answer\nA3: UNKNOWN\n"
	got := parseNumberedAnswers(text)
	want := map[int]string{1: "first answer", 2: "second answer", 3: "UNKNOWN"}
	if len(got) != len(want) {
		t.Fatalf("parseNumberedAnswers = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("parseNumberedAnswers[%d] = %q, want %q", k, got[k], v)
		}
	}
}

// TestParseNumberedAnswers_ProseOnlyYieldsEmptyMap proves finding 5(b): output
// with no "A<n>:" markers at all (prose, not the requested format) must parse
// to an empty map, not a spuriously "answered" one.
func TestParseNumberedAnswers_ProseOnlyYieldsEmptyMap(t *testing.T) {
	text := "I looked at the sources but there is nothing directly relevant to say here."
	got := parseNumberedAnswers(text)
	if len(got) != 0 {
		t.Fatalf("parseNumberedAnswers(prose) = %v, want empty map", got)
	}
}

// withNlmGrillSeams sets up nlmGrillAnswerer's two test seams — the auth
// guard (bypassed, mirroring a bare context.Context in production) and the
// nlm query invocation (fed canned output) — and restores both on cleanup.
// This keeps nlmGrillAnswerer's real implementation under test while never
// touching the network, per the "never exec real nlm/claude in tests" rule.
func withNlmGrillSeams(t *testing.T, out string) {
	t.Helper()
	prevAuth := nlmGrillAuthGuard
	prevRun := nlmGrillRunFn
	t.Cleanup(func() {
		nlmGrillAuthGuard = prevAuth
		nlmGrillRunFn = prevRun
	})
	nlmGrillAuthGuard = func(_ context.Context) error { return nil }
	nlmGrillRunFn = func(_ time.Duration, _ ...string) string { return out }
}

// TestNlmGrillAnswerer_EmptyOutputIsUnavailable proves finding 2: an empty
// (or whitespace-only) nlm response must stop batching immediately via
// errAnswererUnavailable instead of the pre-fix behavior of treating it as
// "0 answers found" (which let resolveGrillQuestions keep sending later
// batches to a broken answerer).
func TestNlmGrillAnswerer_EmptyOutputIsUnavailable(t *testing.T) {
	withNlmGrillSeams(t, "   \n  ")
	got, err := nlmGrillAnswerer(context.Background(), []grillQuestion{{Branch: "D1", Text: "q1?"}})
	if !errors.Is(err, errAnswererUnavailable) {
		t.Fatalf("err = %v, want errAnswererUnavailable", err)
	}
	if got != nil {
		t.Fatalf("got = %v, want nil map on unavailable", got)
	}
}

// TestNlmGrillAnswerer_NoAnswerMarkersIsUnavailable proves finding 2's other
// half: non-empty output with zero parseable "A<n>:" lines (e.g. prose
// instead of the requested format) is a protocol failure, not "0 answers",
// and must also stop batching.
func TestNlmGrillAnswerer_NoAnswerMarkersIsUnavailable(t *testing.T) {
	withNlmGrillSeams(t, `{"answer":"I don't have a structured response for this."}`)
	got, err := nlmGrillAnswerer(context.Background(), []grillQuestion{{Branch: "D1", Text: "q1?"}})
	if !errors.Is(err, errAnswererUnavailable) {
		t.Fatalf("err = %v, want errAnswererUnavailable", err)
	}
	if got != nil {
		t.Fatalf("got = %v, want nil map on unavailable", got)
	}
}

// TestNlmGrillAnswerer_QuotaErrorIsUnavailable proves finding 5(c): a quota
// error surfaced in the query response is classified unavailable via
// nlmGrillUnavailable, same as before, now routed through the nlmGrillRunFn
// seam instead of an unconditional real nlm exec.
func TestNlmGrillAnswerer_QuotaErrorIsUnavailable(t *testing.T) {
	withNlmGrillSeams(t, "RESOURCE_EXHAUSTED: quota exceeded for today")
	got, err := nlmGrillAnswerer(context.Background(), []grillQuestion{{Branch: "D1", Text: "q1?"}})
	if !errors.Is(err, errAnswererUnavailable) {
		t.Fatalf("err = %v, want errAnswererUnavailable", err)
	}
	if got != nil {
		t.Fatalf("got = %v, want nil map on unavailable", got)
	}
}

// TestNlmGrillAnswerer_ValidOutputReturnsMap proves finding 5(c)'s happy
// path: well-formed "A1:"/"A2:" JSON output maps back to the right question
// indices with no error.
func TestNlmGrillAnswerer_ValidOutputReturnsMap(t *testing.T) {
	withNlmGrillSeams(t, `{"answer":"A1: cursor is persisted to disk\nA2: heuristics are cheaper than an LLM call"}`)
	batch := []grillQuestion{
		{Branch: "D1-persistence", Text: "does the cursor survive restarts?"},
		{Branch: "D2-routing", Text: "why heuristics before LLM?"},
	}
	got, err := nlmGrillAnswerer(context.Background(), batch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got[0] != "cursor is persisted to disk" {
		t.Fatalf("got[0] = %q, want %q", got[0], "cursor is persisted to disk")
	}
	if got[1] != "heuristics are cheaper than an LLM call" {
		t.Fatalf("got[1] = %q, want %q", got[1], "heuristics are cheaper than an LLM call")
	}
}

// TestNlmGrillAnswerer_AuthGuardFailureIsUnavailable proves finding 4: when
// the auth guard (production: CheckNotebookLMAuthAndRefresh) reports failure,
// nlmGrillAnswerer must not proceed to spend the query call at all.
func TestNlmGrillAnswerer_AuthGuardFailureIsUnavailable(t *testing.T) {
	prevAuth := nlmGrillAuthGuard
	prevRun := nlmGrillRunFn
	t.Cleanup(func() {
		nlmGrillAuthGuard = prevAuth
		nlmGrillRunFn = prevRun
	})
	nlmGrillAuthGuard = func(_ context.Context) error { return errAnswererUnavailable }
	queryCalled := false
	nlmGrillRunFn = func(_ time.Duration, _ ...string) string {
		queryCalled = true
		return `{"answer":"A1: should never get here"}`
	}

	got, err := nlmGrillAnswerer(context.Background(), []grillQuestion{{Branch: "D1", Text: "q1?"}})
	if !errors.Is(err, errAnswererUnavailable) {
		t.Fatalf("err = %v, want errAnswererUnavailable", err)
	}
	if got != nil {
		t.Fatalf("got = %v, want nil map on auth-guard failure", got)
	}
	if queryCalled {
		t.Fatal("nlmGrillAnswerer must not spend the query call when the auth guard fails")
	}
}

// TestRunGrillAuthGuardAction_PreservesBlackboardResultAndOutcome proves
// finding 4's blackboard-preservation requirement using a fake ActionFunc
// standing in for CheckNotebookLMAuthAndRefresh (which execs the real nlm
// binary unconditionally and so cannot be driven directly in a test): the
// fake mutates bb.Result/bb.Outcome and fails, and runGrillAuthGuardAction
// must restore the caller's original Result/Outcome while still reporting
// errAnswererUnavailable for the failure.
func TestRunGrillAuthGuardAction_PreservesBlackboardResultAndOutcome(t *testing.T) {
	bb := newTestBlackboard()
	bb.Result = "pristine result"
	bb.Outcome = "pristine outcome"
	btctx := &btcore.BTContext[Blackboard]{Blackboard: bb}

	fakeAuthAction := func(c *btcore.BTContext[Blackboard]) int {
		c.Blackboard.Result = "## NotebookLM Auth\n\nstale, refresh failed"
		c.Blackboard.Outcome = "failure"
		return -1
	}

	err := runGrillAuthGuardAction(btctx, fakeAuthAction)
	if !errors.Is(err, errAnswererUnavailable) {
		t.Fatalf("err = %v, want errAnswererUnavailable", err)
	}
	if bb.Result != "pristine result" {
		t.Fatalf("bb.Result = %q, want untouched %q", bb.Result, "pristine result")
	}
	if bb.Outcome != "pristine outcome" {
		t.Fatalf("bb.Outcome = %q, want untouched %q", bb.Outcome, "pristine outcome")
	}
}

// TestRunGrillAuthGuardAction_SuccessReturnsNilAndPreservesBlackboard proves
// the mirror case: a successful auth-check action must not error, and must
// likewise leave the caller's Result/Outcome untouched.
func TestRunGrillAuthGuardAction_SuccessReturnsNilAndPreservesBlackboard(t *testing.T) {
	bb := newTestBlackboard()
	bb.Result = "pristine result"
	bb.Outcome = "pristine outcome"
	btctx := &btcore.BTContext[Blackboard]{Blackboard: bb}

	fakeAuthAction := func(c *btcore.BTContext[Blackboard]) int {
		c.Blackboard.Result = "## NotebookLM Auth\n\nok"
		c.Blackboard.Outcome = "success"
		return 1
	}

	if err := runGrillAuthGuardAction(btctx, fakeAuthAction); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bb.Result != "pristine result" || bb.Outcome != "pristine outcome" {
		t.Fatalf("bb.Result/Outcome = %q/%q, want untouched", bb.Result, bb.Outcome)
	}
}

// TestParseNumberedAnswers_FewerAnswersThanQuestionsOK proves finding 5(b):
// a partial response (fewer A<n> lines than questions asked) parses fine —
// missing indices are simply absent from the map, letting the caller treat
// them as unanswered rather than erroring out.
func TestParseNumberedAnswers_FewerAnswersThanQuestionsOK(t *testing.T) {
	text := "A1: only this one was answered\n"
	got := parseNumberedAnswers(text)
	if len(got) != 1 || got[1] != "only this one was answered" {
		t.Fatalf("parseNumberedAnswers = %v, want {1: \"only this one was answered\"}", got)
	}
	if _, ok := got[2]; ok {
		t.Fatalf("parseNumberedAnswers should not fabricate an entry for A2: %v", got)
	}
}

// TestNoNewGapsOrImplDegraded_IsFallbackEligibleGuard is the head of this
// task's contract: ScheduledAnalysisPath must be guarded by a fallback-eligible
// condition (NoNewGaps OR impl_degraded) instead of the bare NoNewGaps, so that
// ANY failure of ClaudeSuperpowersPath — not just a Claude rate limit — can
// degrade the cycle to deterministic analysis rather than abort the whole loop.
//
// The condition must be true in BOTH the pre-existing "goals unchanged" case
// AND the new "implementation degraded" case, and false only when goals are
// fresh and no degradation occurred (implementation path still owns the cycle).
// Before the fix this condition does not exist at all.
func TestNoNewGapsOrImplDegraded_IsFallbackEligibleGuard(t *testing.T) {
	cond := GetCondition("NoNewGapsOrImplDegraded")
	if cond == nil {
		t.Fatal("NoNewGapsOrImplDegraded condition not registered: ScheduledAnalysisPath needs a fallback-eligible head guard (NoNewGaps OR impl_degraded)")
	}

	// New behavior: a durable impl-degraded signal alone (fresh goals, no
	// rate-limit "goals unchanged" marker) must open the deterministic fallback.
	bbDegraded := newTestBlackboard()
	bbDegraded.ChainState["goap_fusion_impl_degraded"] = "true"
	if !cond(bbDegraded) {
		t.Fatalf("guard must be true when goap_fusion_impl_degraded is set: any ClaudeSuperpowersPath failure must be catchable by ScheduledAnalysisPath")
	}

	// Preserved behavior: the existing NoNewGaps (goals unchanged) case must
	// still open the fallback.
	bbUnchanged := newTestBlackboard()
	bbUnchanged.ChainState["goap_fusion_goals_unchanged"] = "true"
	if !cond(bbUnchanged) {
		t.Fatalf("guard must remain true when goals are unchanged (must preserve the NoNewGaps fallback)")
	}

	// Fresh goals, nothing degraded: the implementation path owns the cycle and
	// the deterministic fallback must NOT be entered.
	bbFresh := newTestBlackboard()
	if cond(bbFresh) {
		t.Fatalf("guard must be false when goals are fresh and no degradation occurred; ClaudeSuperpowersPath must run, not the analysis fallback")
	}
}

// TestRunSuperpowersRuntime_NonRateLimitFailureSetsImplDegraded proves the
// durable-signal half of this task's contract: when ClaudeSuperpowersPath fails
// for ANY reason other than a Claude rate limit,
// runSuperpowersRuntimeFromExistingPlanAction must set a durable
// goap_fusion_impl_degraded signal so the ExecutionRouter Selector can degrade
// the cycle to deterministic ScheduledAnalysisPath instead of aborting the
// whole loop. Before the fix only the rate-limit branch set a fall-through
// signal (goap_fusion_goals_unchanged); every other failure returned -1 with no
// signal at all, which dead-lettered the goap-fusion loop runner.
func TestRunSuperpowersRuntime_NonRateLimitFailureSetsImplDegraded(t *testing.T) {
	// Guard against any stray relative-path writes escaping into the repo.
	t.Chdir(t.TempDir())

	prevRunner, prevClaude := defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner
	t.Cleanup(func() {
		defaultSuperpowersCommandRunner = prevRunner
		defaultSuperpowersClaudeRunner = prevClaude
	})
	// A RED verify that runs the task's test command and gets a PASSING result
	// fails with "RED command unexpectedly passed" — a deterministic,
	// non-rate-limit ClaudeSuperpowersPath failure (no "session limit"/"rate
	// limit"/"quota exceeded" markers), exactly the catch-all case this task
	// must degrade instead of abort.
	defaultSuperpowersCommandRunner = &scriptedSuperpowersRunner{
		t:           t,
		testResults: []CommandResult{{Output: "ok  \tgithub.com/nico/go-bt-evolve/internal/engine\t0.01s\n"}},
	}
	defaultSuperpowersClaudeRunner = &scriptedClaudeRunner{}

	planPath := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(planPath, []byte(buildDeterministicImplementationPlan("improve the platform")), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	run := &SuperpowersRun{
		ID:           "run-degrade",
		Task:         "improve the platform",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: t.TempDir(), // non-empty so no real worktree is created
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
	}
	bb := newTestBlackboard()
	setSuperpowersRun(bb, run)
	bb.ChainState["goap_fusion_superpowers_plan_path"] = planPath

	fn := GetAction("RunSuperpowersClaudeImplementation")
	if fn == nil {
		t.Fatal("RunSuperpowersClaudeImplementation not registered")
	}
	result := fn(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if result != -1 {
		t.Fatalf("expected FAILURE (-1) on a non-rate-limit ClaudeSuperpowersPath failure, got %d: %s", result, bb.Result)
	}
	// This is NOT a rate limit, so the pre-existing goals_unchanged fall-through
	// must not be relied on — the durable impl-degraded signal must be set so
	// the fallback-eligible ScheduledAnalysisPath guard catches it.
	if got, _ := bb.ChainState["goap_fusion_impl_degraded"].(string); got != "true" {
		t.Fatalf("goap_fusion_impl_degraded = %q, want \"true\": every ClaudeSuperpowersPath failure must set the durable degrade signal so the loop degrades to analysis instead of aborting", got)
	}
}

// TestSuperpowersPlanState_PersistsAcrossRuns proves the core contract of this
// task: the Superpowers plan path AND active plan body must survive across
// scheduled runs. Each cron tick builds a FRESH Blackboard (RunOnce) whose
// ChainState dies with the run, so writing the plan path only to ChainState —
// as the code did before this task — means the 4a60278 preflight resume branch
// re-plans from scratch every tick and can never resume a rate-limited
// carryover. The durable state must live in the agent-scope blackboard (the
// same mechanism as saveGrillState/loadGrillState and saveLastReviewedSHA), so
// a later run reading with an empty ChainState still sees it.
func TestSuperpowersPlanState_PersistsAcrossRuns(t *testing.T) {
	mgr := blackboard.NewManager(nil)
	run1 := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop")}
	run2 := &Blackboard{BB: blackboard.NewHandle(mgr, "run-2", "", "goap-loop")}

	planPath := "/tmp/docs/superpowers/plans/goap-fusion-20260704T010101-improve.md"
	activePlan := "# Superpowers Implementation Plan\n\n### Task 1: durable resume\n"
	saveSuperpowersPlanState(run1, planPath, activePlan)

	// run2 has a completely fresh ChainState — the only way it can recover the
	// plan is the durable agent-scope store.
	gotPath, gotPlan := loadSuperpowersPlanState(run2)
	if gotPath != planPath {
		t.Fatalf("loadSuperpowersPlanState path = %q, want %q: plan path must persist across runs via the agent-scope store", gotPath, planPath)
	}
	if gotPlan != activePlan {
		t.Fatalf("loadSuperpowersPlanState plan = %q, want %q: active plan body must persist across runs via the agent-scope store", gotPlan, activePlan)
	}
}

// TestSuperpowersPlanState_ClearedAfterApply proves the second half of the
// contract: once a plan has been applied successfully the durable plan state
// must be CLEARED, so the next scheduled cycle does not re-resume an already
// completed plan (which would loop forever re-applying finished work). After a
// clear, a fresh run reading the durable store must see nothing.
func TestSuperpowersPlanState_ClearedAfterApply(t *testing.T) {
	mgr := blackboard.NewManager(nil)
	run1 := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop")}
	run2 := &Blackboard{BB: blackboard.NewHandle(mgr, "run-2", "", "goap-loop")}

	saveSuperpowersPlanState(run1, "/tmp/plan.md", "# plan\n")
	clearSuperpowersPlanState(run1)

	gotPath, gotPlan := loadSuperpowersPlanState(run2)
	if gotPath != "" || gotPlan != "" {
		t.Fatalf("after clear, loadSuperpowersPlanState = (%q, %q), want empty: a successfully applied plan must not be resumable by the next cycle", gotPath, gotPlan)
	}
}

// TestSuperpowersPlanState_ChainStateFallback proves the helper degrades to the
// per-run ChainState when no agent-scope blackboard is configured, mirroring
// the loadGrillState / loadLastReviewedSHA fallback so unit paths and
// scope-disabled deployments still round-trip within a single run.
func TestSuperpowersPlanState_ChainStateFallback(t *testing.T) {
	bb := &Blackboard{}
	saveSuperpowersPlanState(bb, "/tmp/fallback-plan.md", "# fallback\n")

	gotPath, gotPlan := loadSuperpowersPlanState(bb)
	if gotPath != "/tmp/fallback-plan.md" || gotPlan != "# fallback\n" {
		t.Fatalf("ChainState fallback round-trip = (%q, %q), want (/tmp/fallback-plan.md, # fallback\\n)", gotPath, gotPlan)
	}

	if got, _ := loadSuperpowersPlanState(&Blackboard{}); got != "" {
		t.Fatalf("empty blackboard must return empty plan path, got %q", got)
	}
}
