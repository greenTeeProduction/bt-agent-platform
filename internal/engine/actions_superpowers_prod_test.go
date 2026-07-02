package engine

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
