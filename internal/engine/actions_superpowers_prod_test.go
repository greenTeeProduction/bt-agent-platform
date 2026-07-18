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
	"github.com/nico/go-bt-evolve/internal/research"
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

// rateLimitedClaudeRunner makes every Claude call fail with a session/rate-limit
// error so runSuperpowersRuntimeFromExistingPlanAction takes its rate-limit
// carryover branch (isClaudeRateLimit → true, bb.Outcome = goap_fusion_rate_limited).
type rateLimitedClaudeRunner struct{}

func (rateLimitedClaudeRunner) RunClaude(_ context.Context, repoDir string, _ string) CommandResult {
	return CommandResult{
		Command:  "claude <prompt>",
		Dir:      repoDir,
		Output:   "Claude Code session limit reached.",
		Err:      errors.New("session limit reached; resets at 3pm"),
		Duration: time.Millisecond,
	}
}

// TestRunSuperpowersRuntime_NonRateLimitFailureClearsDurablePlanState proves this
// task's core contract: when ClaudeSuperpowersPath fails for ANY reason other than
// a Claude rate limit, runSuperpowersRuntimeFromExistingPlanAction must CLEAR the
// durable plan state so the next scheduled cycle re-plans from scratch instead of
// re-resuming a doomed plan forever. Before the fix the durable plan state was
// cleared ONLY on a successful apply; every non-rate-limit failure left it in
// place, so the preflight resume branch re-resumed the same failing plan (and
// dropped any freshly-analyzed goals) on every subsequent cron tick.
func TestRunSuperpowersRuntime_NonRateLimitFailureClearsDurablePlanState(t *testing.T) {
	// Guard against any stray relative-path writes escaping into the repo.
	t.Chdir(t.TempDir())

	prevRunner, prevClaude := defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner
	t.Cleanup(func() {
		defaultSuperpowersCommandRunner = prevRunner
		defaultSuperpowersClaudeRunner = prevClaude
	})
	// A RED verify that runs the task's test command and gets a PASSING result
	// fails with "RED command unexpectedly passed" — a deterministic,
	// non-rate-limit ClaudeSuperpowersPath failure (no session/rate/quota
	// markers), exactly the catch-all case this task must clear the plan for.
	defaultSuperpowersCommandRunner = &scriptedSuperpowersRunner{
		t:           t,
		testResults: []CommandResult{{Output: "ok  \tgithub.com/nico/go-bt-evolve/internal/engine\t0.01s\n"}},
	}
	defaultSuperpowersClaudeRunner = &scriptedClaudeRunner{}

	planPath := filepath.Join(t.TempDir(), "plan.md")
	activePlan := buildDeterministicImplementationPlan("improve the platform")
	if err := os.WriteFile(planPath, []byte(activePlan), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	// Agent-scope blackboard so the durable plan store is exercised for real, the
	// same way a rate-limited carryover would have left it before this cycle.
	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{
		ChainState: map[string]any{},
		BB:         blackboard.NewHandle(mgr, "run-clear", "", "goap-loop"),
	}
	saveSuperpowersPlanState(bb, planPath, activePlan)

	run := &SuperpowersRun{
		ID:           "run-clear",
		Task:         "improve the platform",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: t.TempDir(), // non-empty so no real worktree is created
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
	}
	setSuperpowersRun(bb, run)

	fn := GetAction("RunSuperpowersClaudeImplementation")
	if fn == nil {
		t.Fatal("RunSuperpowersClaudeImplementation not registered")
	}
	result := fn(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if result != -1 {
		t.Fatalf("expected FAILURE (-1) on a non-rate-limit ClaudeSuperpowersPath failure, got %d: %s", result, bb.Result)
	}
	if bb.Outcome == "goap_fusion_rate_limited" {
		t.Fatalf("test setup wrong: this failure must NOT be classified rate-limited, got outcome %q", bb.Outcome)
	}

	// The durable plan state must be CLEARED: a fresh run reading the agent-scope
	// store must see nothing, so the next cycle re-plans instead of re-resuming a
	// doomed plan.
	fresh := &Blackboard{BB: blackboard.NewHandle(mgr, "run-next", "", "goap-loop")}
	gotPath, gotPlan := loadSuperpowersPlanState(fresh)
	if gotPath != "" || gotPlan != "" {
		t.Fatalf("after a non-rate-limit failure, durable plan state = (%q, %q), want empty: it must be cleared so the next cycle does not re-resume a doomed plan", gotPath, gotPlan)
	}
}

// TestRunSuperpowersRuntime_RateLimitFailurePreservesDurablePlanState is the other
// half of the contract: a Claude rate-limit failure must PRESERVE the durable plan
// state so the next cycle can resume the carryover. The clear-on-failure behavior
// added by this task must fire ONLY for non-rate-limit failures; the rate-limit
// branch (bb.Outcome == goap_fusion_rate_limited) must never wipe the saved plan.
func TestRunSuperpowersRuntime_RateLimitFailurePreservesDurablePlanState(t *testing.T) {
	isolateClaudeBackoffStore(t)
	t.Chdir(t.TempDir())

	prevRunner, prevClaude := defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner
	t.Cleanup(func() {
		defaultSuperpowersCommandRunner = prevRunner
		defaultSuperpowersClaudeRunner = prevClaude
	})
	defaultSuperpowersCommandRunner = &scriptedSuperpowersRunner{t: t}
	defaultSuperpowersClaudeRunner = rateLimitedClaudeRunner{}

	planPath := filepath.Join(t.TempDir(), "plan.md")
	activePlan := buildDeterministicImplementationPlan("improve the platform")
	if err := os.WriteFile(planPath, []byte(activePlan), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{
		ChainState: map[string]any{},
		BB:         blackboard.NewHandle(mgr, "run-carry", "", "goap-loop"),
	}
	saveSuperpowersPlanState(bb, planPath, activePlan)

	run := &SuperpowersRun{
		ID:           "run-carry",
		Task:         "improve the platform",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: t.TempDir(),
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
	}
	setSuperpowersRun(bb, run)

	fn := GetAction("RunSuperpowersClaudeImplementation")
	if fn == nil {
		t.Fatal("RunSuperpowersClaudeImplementation not registered")
	}
	result := fn(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if result != -1 {
		t.Fatalf("expected FAILURE (-1) on a rate-limited ClaudeSuperpowersPath failure, got %d: %s", result, bb.Result)
	}
	if bb.Outcome != "goap_fusion_rate_limited" {
		t.Fatalf("expected outcome goap_fusion_rate_limited, got %q (bb.Result=%s)", bb.Outcome, bb.Result)
	}

	// The durable plan state MUST survive so the next cycle resumes the carryover.
	fresh := &Blackboard{BB: blackboard.NewHandle(mgr, "run-next", "", "goap-loop")}
	gotPath, gotPlan := loadSuperpowersPlanState(fresh)
	if gotPath != planPath || gotPlan != activePlan {
		t.Fatalf("after a rate-limit failure, durable plan state = (%q, %q), want (%q, %q): the rate-limit carryover must be preserved", gotPath, gotPlan, planPath, activePlan)
	}
}

// TestRunSuperpowersRuntime_RateLimitRecordsBackoff proves the recording half
// of the durable Claude backoff contract: when the Superpowers batch execution
// hits a Claude rate limit, runSuperpowersRuntimeFromExistingPlanAction must
// call saveClaudeBackoffState alongside the plan carryover, so the NEXT cron
// tick can short-circuit at the entry guard instead of burning another
// 45-minute doomed run against a quota known to be closed. Before the fix only
// the Claude-review fallback recorded a backoff; the runtime's rate-limit
// branch saved the plan but left the backoff store empty, so every subsequent
// tick re-resumed the plan and hit the closed quota again (3×15-min retries
// per tick).
func TestRunSuperpowersRuntime_RateLimitRecordsBackoff(t *testing.T) {
	isolateClaudeBackoffStore(t)
	t.Chdir(t.TempDir())

	prevRunner, prevClaude := defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner
	t.Cleanup(func() {
		defaultSuperpowersCommandRunner = prevRunner
		defaultSuperpowersClaudeRunner = prevClaude
	})
	defaultSuperpowersCommandRunner = &scriptedSuperpowersRunner{t: t}
	defaultSuperpowersClaudeRunner = rateLimitedClaudeRunner{}

	planPath := filepath.Join(t.TempDir(), "plan.md")
	activePlan := buildDeterministicImplementationPlan("improve the platform")
	if err := os.WriteFile(planPath, []byte(activePlan), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{
		ChainState: map[string]any{},
		BB:         blackboard.NewHandle(mgr, "run-backoff-record", "", "goap-loop"),
	}
	saveSuperpowersPlanState(bb, planPath, activePlan)

	run := &SuperpowersRun{
		ID:           "run-backoff-record",
		Task:         "improve the platform",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: t.TempDir(),
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
	}
	setSuperpowersRun(bb, run)

	fn := GetAction("RunSuperpowersClaudeImplementation")
	if fn == nil {
		t.Fatal("RunSuperpowersClaudeImplementation not registered")
	}
	result := fn(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if result != -1 {
		t.Fatalf("expected FAILURE (-1) on a rate-limited batch execution, got %d: %s", result, bb.Result)
	}
	if bb.Outcome != "goap_fusion_rate_limited" {
		t.Fatalf("expected outcome goap_fusion_rate_limited, got %q (bb.Result=%s)", bb.Outcome, bb.Result)
	}

	// A fresh run (empty ChainState) must see the backoff via the agent-scope
	// store, with a deadline in the future — that is what lets the next tick
	// skip Claude entirely.
	fresh := &Blackboard{BB: blackboard.NewHandle(mgr, "run-next", "", "goap-loop")}
	until, ok := loadClaudeBackoffState(fresh)
	if !ok {
		t.Fatal("loadClaudeBackoffState after a rate-limited batch = inactive, want a recorded deadline: the runtime's rate-limit branch must call saveClaudeBackoffState alongside the plan carryover")
	}
	if !until.After(time.Now()) {
		t.Fatalf("recorded backoff deadline %v is not in the future: the window must cover upcoming ticks", until)
	}
	// Deadline shape: the fake's error says "resets at 3pm", so the stamp must
	// be that CLI-reported reset plus the boundary margin — not
	// now+claudeBackoffWindow(). The fixed 6h window is what kept the
	// loop-runner asleep ~3h past the real reset on 2026-07-14/15. Both the
	// same-day and rolled-over reset are accepted so a run straddling 3pm (or
	// midnight) cannot flake the gate.
	nowRef := time.Now()
	wantSame := time.Date(nowRef.Year(), nowRef.Month(), nowRef.Day(), 15, 0, 0, 0, time.Local).Add(claudeResetMargin)
	wantNext := wantSame.Add(24 * time.Hour)
	if !until.Equal(wantSame) && !until.Equal(wantNext) {
		t.Fatalf("recorded backoff deadline = %v, want CLI-reported reset+margin (%v or %v): the runtime rate-limit branch must honor the reset hint over the fixed window", until, wantSame, wantNext)
	}

	// The durable plan carryover must survive alongside the backoff.
	gotPath, gotPlan := loadSuperpowersPlanState(fresh)
	if gotPath != planPath || gotPlan != activePlan {
		t.Fatalf("after a rate-limit failure, durable plan state = (%q, %q), want (%q, %q): the carryover must be preserved for the resume after the backoff expires", gotPath, gotPlan, planPath, activePlan)
	}
}

// TestRunSuperpowersRuntime_ActiveBackoffShortCircuits proves the honoring half
// of the contract: with a live backoff window persisted by an earlier
// rate-limited tick and a saved plan carryover,
// runSuperpowersRuntimeFromExistingPlanAction must return -1 with outcome
// goap_fusion_rate_limited and goap_fusion_goals_unchanged set — BEFORE
// creating a worktree or spending the 45-minute batch execution — so the
// ExecutionRouter degrades to the deterministic ScheduledAnalysisPath in
// milliseconds. The exit must reuse the exact rate-limited Result/Outcome shape
// so the existing deferred clearSuperpowersPlanState guard preserves the plan
// carryover for the tick after the window expires.
func TestRunSuperpowersRuntime_ActiveBackoffShortCircuits(t *testing.T) {
	isolateClaudeBackoffStore(t)
	t.Chdir(t.TempDir())

	prevRunner, prevClaude := defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner
	t.Cleanup(func() {
		defaultSuperpowersCommandRunner = prevRunner
		defaultSuperpowersClaudeRunner = prevClaude
	})
	events := []string{}
	runner := &scriptedSuperpowersRunner{
		t:      t,
		events: &events,
		// One passing test result so that, should execution wrongly proceed
		// past the guard, the batch fails deterministically ("RED unexpectedly
		// passed") instead of tripping the runner's unexpected-command Fatalf.
		testResults: []CommandResult{{Output: "ok  \tgithub.com/nico/go-bt-evolve/internal/engine\t0.01s\n"}},
	}
	claude := &scriptedClaudeRunner{events: &events}
	defaultSuperpowersCommandRunner = runner
	defaultSuperpowersClaudeRunner = claude

	planPath := filepath.Join(t.TempDir(), "plan.md")
	activePlan := buildDeterministicImplementationPlan("improve the platform")
	if err := os.WriteFile(planPath, []byte(activePlan), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{
		ChainState: map[string]any{},
		BB:         blackboard.NewHandle(mgr, "run-backoff-honor", "", "goap-loop"),
	}
	saveSuperpowersPlanState(bb, planPath, activePlan)
	// A previous tick's rate-limited outcome left a live backoff window.
	saveClaudeBackoffState(bb, time.Now().Add(time.Hour))

	run := &SuperpowersRun{
		ID:           "run-backoff-honor",
		Task:         "improve the platform",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: t.TempDir(),
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
	}
	setSuperpowersRun(bb, run)

	fn := GetAction("RunSuperpowersClaudeImplementation")
	if fn == nil {
		t.Fatal("RunSuperpowersClaudeImplementation not registered")
	}
	result := fn(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if result != -1 {
		t.Fatalf("expected FAILURE (-1) so the Selector advances to ScheduledAnalysisPath, got %d: %s", result, bb.Result)
	}
	if bb.Outcome != "goap_fusion_rate_limited" {
		t.Fatalf("expected outcome goap_fusion_rate_limited from the backoff entry guard, got %q (bb.Result=%s): an active backoff must short-circuit with the exact rate-limited shape so the plan carryover is preserved", bb.Outcome, bb.Result)
	}
	if got, _ := bb.ChainState["goap_fusion_goals_unchanged"].(string); got != "true" {
		t.Fatalf("goap_fusion_goals_unchanged = %q, want \"true\": the guard must let the Selector fall through to ScheduledAnalysisPath", got)
	}
	if !strings.Contains(strings.ToLower(bb.Result), "backoff") {
		t.Fatalf("bb.Result must narrate the active backoff and plan carryover, got:\n%s", bb.Result)
	}
	// The whole point of the guard: NO Claude invocation and NO worktree/batch
	// commands while the quota is known to be closed.
	if len(claude.prompts) != 0 {
		t.Fatalf("Claude was invoked %d time(s) during an active backoff window; the guard must short-circuit before ExecuteSuperpowersTaskBatchRuntime", len(claude.prompts))
	}
	if len(events) != 0 {
		t.Fatalf("commands ran during an active backoff window (%v); the guard must sit before worktree creation and the batch execution", events)
	}

	// The deferred non-rate-limit clear must NOT fire: the carryover survives
	// for the tick after the window expires.
	fresh := &Blackboard{BB: blackboard.NewHandle(mgr, "run-next", "", "goap-loop")}
	gotPath, gotPlan := loadSuperpowersPlanState(fresh)
	if gotPath != planPath || gotPlan != activePlan {
		t.Fatalf("after the backoff short-circuit, durable plan state = (%q, %q), want (%q, %q): the guard exit must preserve the carryover", gotPath, gotPlan, planPath, activePlan)
	}
}

// TestRunSuperpowersRuntime_ExpiredBackoffExecutes proves the mechanism is
// self-clearing rather than a permanent wedge (the runaway-backstop lesson): a
// backoff deadline already in the past must NOT short-circuit — the runtime
// proceeds into normal execution and the stale timestamp is cleared from the
// durable store so it cannot confuse later ticks.
func TestRunSuperpowersRuntime_ExpiredBackoffExecutes(t *testing.T) {
	isolateClaudeBackoffStore(t)
	t.Chdir(t.TempDir())

	prevRunner, prevClaude := defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner
	t.Cleanup(func() {
		defaultSuperpowersCommandRunner = prevRunner
		defaultSuperpowersClaudeRunner = prevClaude
	})
	// One passing test result → the batch fails deterministically with "RED
	// unexpectedly passed" (a non-rate-limit failure). The point here is only
	// that execution PROCEEDED despite the stale backoff.
	defaultSuperpowersCommandRunner = &scriptedSuperpowersRunner{
		t:           t,
		testResults: []CommandResult{{Output: "ok  \tgithub.com/nico/go-bt-evolve/internal/engine\t0.01s\n"}},
	}
	claude := &scriptedClaudeRunner{}
	defaultSuperpowersClaudeRunner = claude

	planPath := filepath.Join(t.TempDir(), "plan.md")
	activePlan := buildDeterministicImplementationPlan("improve the platform")
	if err := os.WriteFile(planPath, []byte(activePlan), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{
		ChainState: map[string]any{},
		BB:         blackboard.NewHandle(mgr, "run-backoff-expired", "", "goap-loop"),
	}
	saveSuperpowersPlanState(bb, planPath, activePlan)
	// A stale window left by a rate-limited tick an hour ago — long expired.
	saveClaudeBackoffState(bb, time.Now().Add(-time.Hour))

	run := &SuperpowersRun{
		ID:           "run-backoff-expired",
		Task:         "improve the platform",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: t.TempDir(),
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
	}
	setSuperpowersRun(bb, run)

	fn := GetAction("RunSuperpowersClaudeImplementation")
	if fn == nil {
		t.Fatal("RunSuperpowersClaudeImplementation not registered")
	}
	result := fn(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if result != -1 {
		t.Fatalf("expected FAILURE (-1) from the deterministic batch failure, got %d: %s", result, bb.Result)
	}
	if bb.Outcome == "goap_fusion_rate_limited" {
		t.Fatalf("an EXPIRED backoff must not short-circuit as rate-limited; got outcome %q (bb.Result=%s)", bb.Outcome, bb.Result)
	}
	if len(claude.prompts) == 0 {
		t.Fatal("Claude was never invoked: an expired backoff must let normal execution proceed instead of skipping the tick")
	}

	// The stale timestamp must be gone from the durable store so later ticks
	// never re-evaluate it.
	fresh := &Blackboard{BB: blackboard.NewHandle(mgr, "run-next", "", "goap-loop")}
	if until, ok := loadClaudeBackoffState(fresh); ok {
		t.Fatalf("stale backoff deadline %v still present after an expired-backoff run: the entry guard must clear expired state (half-open), not leave it to be re-parsed forever", until)
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

// TestGoapFusionNoopPatchStreak_IncrementsOnNoopResetsOnChange proves the
// producer half of this task's contract: the consecutive no-op-patch streak the
// CIRCUITPOLICY loop runner reads via goapFusionNoopPatchStreak actually has a
// production producer. When runSuperpowersRuntimeFromExistingPlanAction applies a
// run that changed no tracked files (empty ChangedFiles AND no AppliedCommit),
// recordGoapFusionPatchApply must INCREMENT the durable streak; an apply that
// genuinely changed files (or recorded an AppliedCommit) must RESET it to 0.
// Before this task no code ever wrote goap_fusion_noop_patch_streak, so
// goapFusionNoopPatchStreak saw a permanent 0 in production and the no-op tail of
// Activity-Progress Confusion could never be detected.
func TestGoapFusionNoopPatchStreak_IncrementsOnNoopResetsOnChange(t *testing.T) {
	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{
		ChainState: map[string]any{},
		BB:         blackboard.NewHandle(mgr, "run-noop", "", "goap-loop"),
	}

	if got := goapFusionNoopPatchStreak(bb); got != 0 {
		t.Fatalf("initial streak = %d, want 0", got)
	}

	// A no-op apply: no tracked files changed, no commit recorded.
	recordGoapFusionPatchApply(bb, &SuperpowersRun{ID: "noop-1"})
	if got := goapFusionNoopPatchStreak(bb); got != 1 {
		t.Fatalf("after 1 no-op apply streak = %d, want 1", got)
	}
	recordGoapFusionPatchApply(bb, &SuperpowersRun{ID: "noop-2"})
	if got := goapFusionNoopPatchStreak(bb); got != 2 {
		t.Fatalf("after 2 consecutive no-op applies streak = %d, want 2", got)
	}

	// A genuine change (non-empty ChangedFiles) resets the streak to 0.
	recordGoapFusionPatchApply(bb, &SuperpowersRun{ID: "real", ChangedFiles: []string{"internal/engine/foo.go"}})
	if got := goapFusionNoopPatchStreak(bb); got != 0 {
		t.Fatalf("after a genuine file-changing apply streak = %d, want 0 (reset)", got)
	}

	// An apply that recorded a commit is also a genuine change, even if this run
	// object's ChangedFiles slice was not populated.
	recordGoapFusionPatchApply(bb, &SuperpowersRun{ID: "noop-3"})
	recordGoapFusionPatchApply(bb, &SuperpowersRun{ID: "committed", AppliedCommit: "abc1234"})
	if got := goapFusionNoopPatchStreak(bb); got != 0 {
		t.Fatalf("after an apply with a recorded AppliedCommit streak = %d, want 0 (reset)", got)
	}
}

// TestGoapFusionNoopPatchStreak_DurableAcrossCronTicks proves the durability half
// of this task's contract: each cron tick builds a fresh Blackboard (RunOnce)
// whose ChainState dies with the run, so a streak that only lived in ChainState
// would reset to 0 every tick and goapFusionNoopPatchStreak would never see a
// real value in production. The producer must persist the streak to the
// agent-scope blackboard (mirroring saveGoapFusionStateHashes / saveSuperpowersPlanState)
// so a later tick reading with an empty ChainState still sees — and accumulates
// onto — it.
func TestGoapFusionNoopPatchStreak_DurableAcrossCronTicks(t *testing.T) {
	mgr := blackboard.NewManager(nil)

	tick1 := &Blackboard{
		ChainState: map[string]any{},
		BB:         blackboard.NewHandle(mgr, "run-1", "", "goap-loop"),
	}
	recordGoapFusionPatchApply(tick1, &SuperpowersRun{ID: "noop-1"})

	// A completely fresh Blackboard — the next cron tick — must load the durable
	// streak from the agent-scope store, not start from 0.
	tick2 := &Blackboard{
		ChainState: map[string]any{},
		BB:         blackboard.NewHandle(mgr, "run-2", "", "goap-loop"),
	}
	if got := goapFusionNoopPatchStreak(tick2); got != 1 {
		t.Fatalf("tick 2 loaded streak = %d, want 1 from the durable agent-scope store", got)
	}

	// Another no-op on tick 2 accumulates onto the durable streak across ticks.
	recordGoapFusionPatchApply(tick2, &SuperpowersRun{ID: "noop-2"})
	tick3 := &Blackboard{
		ChainState: map[string]any{},
		BB:         blackboard.NewHandle(mgr, "run-3", "", "goap-loop"),
	}
	if got := goapFusionNoopPatchStreak(tick3); got != 2 {
		t.Fatalf("tick 3 loaded streak = %d, want 2 accumulated across cron ticks", got)
	}

	// A genuine change on a later tick resets the durable streak so a fresh tick
	// reads 0 — the reset must be durable too, not just per-tick.
	recordGoapFusionPatchApply(tick3, &SuperpowersRun{ID: "real", ChangedFiles: []string{"internal/engine/foo.go"}})
	tick4 := &Blackboard{
		ChainState: map[string]any{},
		BB:         blackboard.NewHandle(mgr, "run-4", "", "goap-loop"),
	}
	if got := goapFusionNoopPatchStreak(tick4); got != 0 {
		t.Fatalf("tick 4 loaded streak = %d, want 0 after a genuine change reset the durable streak", got)
	}
}

// TestFusionAnalysis_RateLimitCarryoverNotConflatedAsDegradation proves this
// task's contract: a Claude rate-limit carryover is an expected, healthy pause
// (bb.Outcome == "goap_fusion_rate_limited"), NOT a real ClaudeSuperpowersPath
// degradation. The rate-limit branch of runSuperpowersRuntimeFromExistingPlanAction
// returns -1, which trips the shared defer's markGoapFusionImplDegraded and sets
// goap_fusion_impl_degraded="true". That signal must stay set — the ExecutionRouter
// still needs it so the fallback-eligible NoNewGapsOrImplDegraded guard lets
// ScheduledAnalysisPath catch the cycle. But WriteFusionAnalysis must NOT then
// narrate that healthy carryover in the vault research note as
// "ClaudeSuperpowersPath failed; degraded to deterministic analysis", which
// pollutes the next run's research corpus with a phantom failure. The
// impl-degraded section builder must suppress (or distinctly tag) the failure
// narrative when bb.Outcome is a rate limit, while still reporting genuine
// non-rate-limit degradations verbatim so real breakage stays observable.
func TestFusionAnalysis_RateLimitCarryoverNotConflatedAsDegradation(t *testing.T) {
	const conflation = "ClaudeSuperpowersPath failed; degraded to deterministic analysis"

	// A genuine, non-rate-limit degradation must still narrate the failure so real
	// breakage stays observable in the vault note.
	realFailure := newTestBlackboard()
	realFailure.ChainState["goap_fusion_impl_degraded"] = "true"
	realFailure.ChainState["goap_fusion_impl_degraded_reason"] = "RED command unexpectedly passed"
	if section := goapFusionImplDegradedSection(realFailure); !strings.Contains(section, conflation) {
		t.Fatalf("a genuine non-rate-limit degradation must still narrate %q so real breakage stays observable; got:\n%s", conflation, section)
	}

	// A rate-limit carryover sets the SAME impl_degraded signal (the shared defer
	// stamps it on every -1), but bb.Outcome marks it as an expected pause. The
	// vault note must not conflate it with a real ClaudeSuperpowersPath failure.
	rateLimited := newTestBlackboard()
	rateLimited.ChainState["goap_fusion_impl_degraded"] = "true"
	rateLimited.ChainState["goap_fusion_impl_degraded_reason"] = "session limit reached; resets at 3pm"
	rateLimited.Outcome = "goap_fusion_rate_limited"
	if section := goapFusionImplDegradedSection(rateLimited); strings.Contains(section, conflation) {
		t.Fatalf("a rate-limit carryover (bb.Outcome=goap_fusion_rate_limited) must not be narrated as a ClaudeSuperpowersPath failure; got:\n%s", section)
	}

	// With no impl-degraded signal at all, there is no section to append.
	if section := goapFusionImplDegradedSection(newTestBlackboard()); section != "" {
		t.Fatalf("no impl-degraded signal must yield an empty section; got:\n%s", section)
	}
}

// superpowersRuntimeRunBudget must fit a full goal-driven batch (up to three
// RED→GREEN claude executions plus verification, review, and apply). The
// legacy 45-minute budget fit only the single-task template: on 2026-07-18
// nine consecutive cycles (20260718T164339 … 20260718T232740) finished tasks
// 1-2 green in ~40 minutes and were SIGKILLed mid-task-3 at exactly 45:00,
// landing nothing. 90 minutes matches ExecuteSuperpowersTaskBatch's budget
// for the same batch shape.
func TestSuperpowersRuntimeRunBudgetCoversGoalDrivenBatch(t *testing.T) {
	if superpowersRuntimeRunBudget != 90*time.Minute {
		t.Fatalf("superpowersRuntimeRunBudget = %v, want 90m (a 3-milestone batch needs ~55-70m; 45m treadmilled 2026-07-18)", superpowersRuntimeRunBudget)
	}
}

// recoveryScriptRunner fakes the commands the bounded pending_patch recovery
// pass (recoverGoapFusionPendingPatchesInDir) issues against a parked run:
// a branch-survival check ("git branch --list <branch>") followed by the
// reused reapplyRunBranchOntoMaster sequence (symbolic-ref guard, the
// non-forced "git fetch . <branch>:master" ff, and — only on a refused ff —
// "git rebase master" / "git rebase --abort"). When alwaysFailReapply is set,
// both the ff and the rebase keep failing, modelling a run whose recovery
// keeps failing so the bounded-attempts/abandon contract can be exercised.
type recoveryScriptRunner struct {
	calls             []string
	branchExists      bool
	headBranch        string
	alwaysFailReapply bool
}

func (r *recoveryScriptRunner) Run(_ context.Context, dir string, name string, args ...string) CommandResult {
	cmd := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.calls = append(r.calls, dir+" :: "+cmd)
	res := CommandResult{Command: cmd, Dir: dir, Duration: time.Millisecond}
	switch {
	case name == "git" && len(args) >= 2 && args[0] == "branch" && args[1] == "--list":
		if r.branchExists && len(args) >= 3 {
			res.Output = args[2] + "\n"
		}
	case name == "git" && len(args) >= 2 && args[0] == "fetch" && args[1] == ".":
		if r.alwaysFailReapply {
			res.Err = errors.New("fast-forward refused")
			res.Output = "! [rejected]        master -> master (non-fast-forward)"
		}
	case name == "git" && len(args) >= 1 && args[0] == "symbolic-ref":
		res.Output = r.headBranch + "\n"
	case name == "git" && len(args) >= 2 && args[0] == "rebase" && args[1] == "master":
		if r.alwaysFailReapply {
			res.Err = errors.New("rebase conflict")
			res.Output = "CONFLICT (content): Merge conflict"
		}
	case name == "git" && len(args) >= 2 && args[0] == "rebase" && args[1] == "--abort":
		// no-op success
	}
	return res
}

func (r *recoveryScriptRunner) joined() string { return strings.Join(r.calls, "\n") }

// writeParkedSuperpowersRun writes a pending_patch run.json (plus its plan.md,
// a REUSE-EXISTING pattern mirroring writeSuperpowersRunJSON/ensureSuperpowersRunDirs)
// under runsDir/<id>, the on-disk shape recoverGoapFusionPendingPatchesInDir must
// scan — mirroring how newSuperpowersRunID-based runs already land under
// superpowersRunsDir (superpowers_artifacts.go).
func writeParkedSuperpowersRun(t *testing.T, runsDir, id, planText string) *SuperpowersRun {
	t.Helper()
	dir := filepath.Join(runsDir, id)
	planPath := filepath.Join(dir, "plan.md")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, []byte(planText), 0o644); err != nil {
		t.Fatal(err)
	}
	run := &SuperpowersRun{
		ID:             id,
		Task:           "recover " + id,
		Mode:           SuperpowersModeApply,
		RepoDir:        t.TempDir(),
		WorktreePath:   t.TempDir(),
		WorktreeBranch: "superpowers/" + id,
		ArtifactDir:    dir,
		PlanPath:       planPath,
		ApplyStatus:    "pending_patch",
	}
	if err := writeSuperpowersRunJSON(run); err != nil {
		t.Fatal(err)
	}
	return run
}

// TestRecoverGoapFusionPendingPatches_SkipsSupersededViaKnowledgeStore pins the
// first half of milestone 4/5's contract (Q3 Reliability & Q5 Consistency —
// non-destructive goap-fusion materializer): before attempting any rebase/ff
// recovery on a parked run, the pass must consult superpowersPlanAlreadyImplemented
// against the knowledge store. A parked run whose plan objective is already
// recorded as landed (goap:implemented) is stale/superseded — re-attempting it
// would redo work that landed out-of-band — so it must be skipped with zero
// git rebase/ff commands issued and its ApplyStatus left untouched.
func TestRecoverGoapFusionPendingPatches_SkipsSupersededViaKnowledgeStore(t *testing.T) {
	runsDir := t.TempDir()
	planText := buildDeterministicImplementationPlan("known fix")
	run := writeParkedSuperpowersRun(t, runsDir, "run-known", planText)

	prevKnowledge := btFusionKnowledgePath
	btFusionKnowledgePath = filepath.Join(t.TempDir(), "knowledge.json")
	t.Cleanup(func() { btFusionKnowledgePath = prevKnowledge })

	tasks, err := ParseSuperpowersPlan(planText)
	if err != nil || len(tasks) == 0 {
		t.Fatalf("test setup: could not parse seeded plan: %v", err)
	}
	store, err := research.Open(btFusionKnowledgePath)
	if err != nil {
		t.Fatalf("test setup: open knowledge store: %v", err)
	}
	store.Record("goap:implemented", tasks[0].Title, stripGoapGoalTransientNotes(tasks[0].Objective))
	if err := store.Save(); err != nil {
		t.Fatalf("test setup: save knowledge store: %v", err)
	}

	runner := &recoveryScriptRunner{branchExists: true, headBranch: run.WorktreeBranch}
	recoverGoapFusionPendingPatchesInDir(context.Background(), runner, runsDir)

	joined := runner.joined()
	if strings.Contains(joined, "fetch . "+run.WorktreeBranch+":master") || strings.Contains(joined, "rebase master") {
		t.Fatalf("a parked run whose plan is already recorded as implemented must be skipped entirely, not re-attempted; calls:\n%s", joined)
	}

	reloaded, err := readSuperpowersRunJSON(filepath.Join(runsDir, "run-known", "run.json"))
	if err != nil {
		t.Fatalf("reload run.json: %v", err)
	}
	if reloaded.ApplyStatus != "pending_patch" {
		t.Fatalf("ApplyStatus = %q, want unchanged pending_patch for a skipped superseded run", reloaded.ApplyStatus)
	}
}

// TestRecoverGoapFusionPendingPatches_BoundsAttemptsThenAbandons pins the
// second half of milestone 4/5's contract: a surviving parked run (branch
// intact, plan NOT already recorded as implemented) gets at most ONE bounded
// rebase-onto-master + full re-verify + ff-land attempt (reapplyRunBranchOntoMaster)
// per scheduled cycle. Each failed attempt is recorded in run.json so the
// count survives across cron ticks; after 2 total recorded attempts the run
// is abandoned — a 3rd cycle must skip it (no new commands), never spin
// forever retrying a run that keeps failing to land.
func TestRecoverGoapFusionPendingPatches_BoundsAttemptsThenAbandons(t *testing.T) {
	runsDir := t.TempDir()
	planText := buildDeterministicImplementationPlan("unknown fix")
	run := writeParkedSuperpowersRun(t, runsDir, "run-x", planText)

	// No knowledge-store seeding: this run's plan is not recorded as landed,
	// so it is a genuine surviving parked run eligible for recovery attempts.
	prevKnowledge := btFusionKnowledgePath
	btFusionKnowledgePath = filepath.Join(t.TempDir(), "knowledge.json")
	t.Cleanup(func() { btFusionKnowledgePath = prevKnowledge })

	runner := &recoveryScriptRunner{branchExists: true, headBranch: run.WorktreeBranch, alwaysFailReapply: true}
	runJSONPath := filepath.Join(runsDir, "run-x", "run.json")

	// pendingPatchRecoveryCheckName is the VerificationCheck.Name a recorded
	// recovery attempt must use — reusing run.Verification (rather than a new
	// SuperpowersRun field) as the "record the attempt in run.json" ledger, the
	// same durable-artifact shape every other apply-time check already uses
	// (superpowers_apply.go's verifySuperpowersRuntimeInDir).
	const pendingPatchRecoveryCheckName = "pending-patch-recovery"
	countAttempts := func() int {
		reloaded, err := readSuperpowersRunJSON(runJSONPath)
		if err != nil {
			t.Fatalf("reload run.json: %v", err)
		}
		if reloaded.ApplyStatus != "pending_patch" {
			t.Fatalf("ApplyStatus = %q, want pending_patch (recovery kept failing, run stays parked)", reloaded.ApplyStatus)
		}
		n := 0
		for _, v := range reloaded.Verification {
			if v.Name == pendingPatchRecoveryCheckName {
				n++
			}
		}
		return n
	}

	// Cycle 1: one bounded attempt, recorded, run stays parked.
	recoverGoapFusionPendingPatchesInDir(context.Background(), runner, runsDir)
	if got := countAttempts(); got != 1 {
		t.Fatalf("attempts recorded after cycle 1 = %d, want 1", got)
	}
	callsAfterCycle1 := len(runner.calls)
	if callsAfterCycle1 == 0 {
		t.Fatalf("expected the surviving parked run to be attempted on cycle 1, but no commands ran")
	}

	// Cycle 2: second (and final, per the 2-total-attempts budget) attempt.
	recoverGoapFusionPendingPatchesInDir(context.Background(), runner, runsDir)
	if got := countAttempts(); got != 2 {
		t.Fatalf("attempts recorded after cycle 2 = %d, want 2", got)
	}
	callsAfterCycle2 := len(runner.calls)
	if callsAfterCycle2 <= callsAfterCycle1 {
		t.Fatalf("expected a second recovery attempt to issue new commands; calls stayed at %d", callsAfterCycle2)
	}

	// Cycle 3: 2 total attempts already recorded — the run must be abandoned,
	// i.e. skipped with NO new commands issued and no 3rd attempt recorded.
	recoverGoapFusionPendingPatchesInDir(context.Background(), runner, runsDir)
	if got := countAttempts(); got != 2 {
		t.Fatalf("attempts recorded after cycle 3 = %d, want still 2 (abandoned, no 3rd attempt)", got)
	}
	if len(runner.calls) != callsAfterCycle2 {
		t.Fatalf("a run with 2 recorded attempts must be abandoned: no new commands may run on cycle 3; calls before=%d after=%d", callsAfterCycle2, len(runner.calls))
	}
}
