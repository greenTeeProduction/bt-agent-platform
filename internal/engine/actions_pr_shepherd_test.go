package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// captureFleetNotifications stubs NotifyFleetEventFn for a test and returns
// the recorded calls; restored on cleanup.
func captureFleetNotifications(t *testing.T) *[][3]string {
	t.Helper()
	var calls [][3]string
	prev := NotifyFleetEventFn
	t.Cleanup(func() { NotifyFleetEventFn = prev })
	NotifyFleetEventFn = func(source, outcome, summary string) {
		calls = append(calls, [3]string{source, outcome, summary})
	}
	return &calls
}

// TestGithubTokenFromCredentialStore pins the credential-store token fallback:
// the host authenticates git pushes via credential.helper=store, so the
// github.com PAT there must be usable for the API without duplicating the
// secret into an env file.
func TestGithubTokenFromCredentialStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "git-credentials")
	content := "https://other.example.com/x\nhttp://insecure:nope@github.com\nhttps://nico:ghp_testtoken123@github.com\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := githubTokenFromCredentialStore(path); got != "ghp_testtoken123" {
		t.Fatalf("token = %q, want ghp_testtoken123", got)
	}
	if got := githubTokenFromCredentialStore(filepath.Join(dir, "missing")); got != "" {
		t.Fatalf("missing file must yield empty token, got %q", got)
	}
}

// prShepherdScriptRunner scripts git command results by substring match and
// records every call, mirroring applyScriptRunner/commitScopeFakeRunner.
type prShepherdScriptRunner struct {
	calls  []string
	script func(dir, cmd string) (CommandResult, bool)
}

func (r *prShepherdScriptRunner) Run(_ context.Context, dir string, name string, args ...string) CommandResult {
	cmd := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.calls = append(r.calls, dir+" :: "+cmd)
	if r.script != nil {
		if res, ok := r.script(dir, cmd); ok {
			res.Command = cmd
			return res
		}
	}
	return CommandResult{Command: cmd, Dir: dir}
}

func (r *prShepherdScriptRunner) called(sub string) bool {
	for _, c := range r.calls {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

type fakeClaudeFixRunner struct {
	prompts []string
	out     string
	err     error
}

func (f *fakeClaudeFixRunner) RunClaude(_ context.Context, _ string, prompt string) CommandResult {
	f.prompts = append(f.prompts, prompt)
	return CommandResult{Output: f.out, Err: f.err}
}

// fakeGitHub is a minimal scripted GitHub API server covering the endpoints
// the shepherd uses. Every request path is recorded for assertions.
type fakeGitHub struct {
	t           *testing.T
	openPRs     []map[string]any
	checkRuns   map[string][]map[string]any // head sha -> runs
	annotations map[int64][]map[string]any  // check-run id -> annotations
	mergeCode   int                         // 0 => 200 merged
	mergeMsg    string
	requests    []string
}

func (g *fakeGitHub) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		g.requests = append(g.requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/pulls"):
			_ = json.NewEncoder(w).Encode(g.openPRs)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/pulls"):
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 77,
				"head":   map[string]any{"sha": "localsha", "ref": "fleet/landing"},
			})
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/commits/") && strings.HasSuffix(r.URL.Path, "/check-runs"):
			parts := strings.Split(r.URL.Path, "/")
			sha := parts[len(parts)-2]
			runs := g.checkRuns[sha]
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": len(runs), "check_runs": runs})
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/check-runs/") && strings.HasSuffix(r.URL.Path, "/annotations"):
			var id int64
			_, _ = fmt.Sscanf(r.URL.Path[strings.Index(r.URL.Path, "/check-runs/")+len("/check-runs/"):], "%d", &id)
			_ = json.NewEncoder(w).Encode(g.annotations[id])
		case r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/merge"):
			if g.mergeCode != 0 {
				w.WriteHeader(g.mergeCode)
				_ = json.NewEncoder(w).Encode(map[string]any{"message": g.mergeMsg})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"merged": true})
		case r.Method == "DELETE" && strings.Contains(r.URL.Path, "/git/refs/"):
			w.WriteHeader(http.StatusNoContent)
		default:
			g.t.Errorf("fakeGitHub: unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return httptest.NewServer(mux)
}

func (g *fakeGitHub) requested(sub string) bool {
	for _, r := range g.requests {
		if strings.Contains(r, sub) {
			return true
		}
	}
	return false
}

// prTestDeps builds deps wired to the fakes with a scripted git state:
// localSHA/originSHA control the sync comparison; localAhead/originAhead
// control the merge-base ancestry answers.
func prTestDeps(t *testing.T, gh *fakeGitHub, runner *prShepherdScriptRunner, claude ClaudeRunner) prShepherdDeps {
	t.Helper()
	srv := gh.server()
	t.Cleanup(srv.Close)
	return prShepherdDeps{
		runner:   runner,
		claude:   claude,
		repoDir:  t.TempDir(),
		stateDir: t.TempDir(),
		api: &githubPRClient{
			base: srv.URL, owner: "greenTeeProduction", repo: "bt-agent-platform",
			token: "test-token", hc: srv.Client(),
		},
		now:           func() time.Time { return time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC) },
		claudeTimeout: time.Minute,
	}
}

// gitAncestryScript answers the shepherd's git probes for a repo where local
// master is at localSHA, origin/master at originSHA, with the given ancestry.
func gitAncestryScript(localSHA, originSHA string, localIsAncestorOfOrigin, originIsAncestorOfLocal bool) func(dir, cmd string) (CommandResult, bool) {
	return func(_ string, cmd string) (CommandResult, bool) {
		switch {
		case strings.Contains(cmd, "rev-parse refs/heads/master"):
			return CommandResult{Output: localSHA + "\n"}, true
		case strings.Contains(cmd, "rev-parse refs/remotes/origin/master"):
			return CommandResult{Output: originSHA + "\n"}, true
		case strings.Contains(cmd, "merge-base --is-ancestor refs/heads/master refs/remotes/origin/master"):
			if localIsAncestorOfOrigin {
				return CommandResult{}, true
			}
			return CommandResult{Err: fmt.Errorf("exit status 1")}, true
		case strings.Contains(cmd, "merge-base --is-ancestor refs/remotes/origin/master refs/heads/master"):
			if originIsAncestorOfLocal {
				return CommandResult{}, true
			}
			return CommandResult{Err: fmt.Errorf("exit status 1")}, true
		case strings.Contains(cmd, "log -1 --format=%s"):
			return CommandResult{Output: "engine: some subject\n"}, true
		case strings.Contains(cmd, "rev-list --count"):
			return CommandResult{Output: "3\n"}, true
		}
		return CommandResult{}, false
	}
}

func TestPRShepherd_DisabledSkips(t *testing.T) {
	t.Setenv("BT_PR_SHEPHERD", "off")
	runner := &prShepherdScriptRunner{}
	bb := newTestBlackboard()
	if got := runPRShepherd(bb, prShepherdDeps{runner: runner, stateDir: t.TempDir()}); got != 1 {
		t.Fatalf("result = %d, want 1", got)
	}
	if bb.Outcome != "pr_shepherd_disabled" {
		t.Fatalf("Outcome = %q", bb.Outcome)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("expected no git calls, got %v", runner.calls)
	}
}

func TestPRShepherd_NoTokenSkips(t *testing.T) {
	for _, k := range []string{"BT_GITHUB_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"} {
		t.Setenv(k, "")
	}
	prevCreds := prShepherdGitCredentialsPath
	t.Cleanup(func() { prShepherdGitCredentialsPath = prevCreds })
	prShepherdGitCredentialsPath = func() string { return filepath.Join(t.TempDir(), "absent-credentials") }
	runner := &prShepherdScriptRunner{}
	bb := newTestBlackboard()
	deps := prShepherdDeps{runner: runner, repoDir: t.TempDir(), stateDir: t.TempDir()}
	if got := runPRShepherd(bb, deps); got != 1 {
		t.Fatalf("result = %d, want 1", got)
	}
	if bb.Outcome != "pr_shepherd_no_token" {
		t.Fatalf("Outcome = %q", bb.Outcome)
	}
	if !strings.Contains(bb.Result, "BT_GITHUB_TOKEN") {
		t.Fatalf("Result should name the token env vars: %s", bb.Result)
	}
}

func TestPRShepherd_IdleWhenInSync(t *testing.T) {
	gh := &fakeGitHub{t: t}
	runner := &prShepherdScriptRunner{script: gitAncestryScript("samesha", "samesha", true, true)}
	bb := newTestBlackboard()
	deps := prTestDeps(t, gh, runner, nil)
	if got := runPRShepherd(bb, deps); got != 1 {
		t.Fatalf("result = %d, want 1", got)
	}
	if bb.Outcome != "pr_shepherd_idle" || bb.OutcomeRefinement != "no_change" {
		t.Fatalf("Outcome = %q refinement = %q", bb.Outcome, bb.OutcomeRefinement)
	}
	if runner.called("push") {
		t.Fatalf("in-sync pass must not push: %v", runner.calls)
	}
	// Every pass must leave a durable trace: bb.Outcome is overwritten by
	// later tree nodes, so state.LastOutcome is the only reliable record.
	if st := loadPRShepherdState(deps.stateDir); st.LastOutcome != "pr_shepherd_idle" || st.LastPassAt == "" {
		t.Fatalf("durable pass record missing: %+v", st)
	}
}

func TestPRShepherd_SyncsMasterWhenOriginAhead(t *testing.T) {
	gh := &fakeGitHub{t: t}
	runner := &prShepherdScriptRunner{script: gitAncestryScript("localsha", "originsha", true, false)}
	bb := newTestBlackboard()
	if got := runPRShepherd(bb, prTestDeps(t, gh, runner, nil)); got != 1 {
		t.Fatalf("result = %d, want 1", got)
	}
	if bb.Outcome != "pr_shepherd_synced_master" {
		t.Fatalf("Outcome = %q; result=%s", bb.Outcome, bb.Result)
	}
	if !runner.called("fetch . refs/remotes/origin/master:master") {
		t.Fatalf("expected non-forced ff of local master, calls: %v", runner.calls)
	}
}

func TestPRShepherd_DivergedSkips(t *testing.T) {
	gh := &fakeGitHub{t: t}
	runner := &prShepherdScriptRunner{script: gitAncestryScript("localsha", "originsha", false, false)}
	bb := newTestBlackboard()
	if got := runPRShepherd(bb, prTestDeps(t, gh, runner, nil)); got != 1 {
		t.Fatalf("result = %d, want 1", got)
	}
	if bb.Outcome != "pr_shepherd_diverged" {
		t.Fatalf("Outcome = %q", bb.Outcome)
	}
	if runner.called("push") || gh.requested("PUT") {
		t.Fatalf("diverged pass must not push or merge")
	}
}

func TestPRShepherd_AheadOpensPR(t *testing.T) {
	notes := captureFleetNotifications(t)
	gh := &fakeGitHub{t: t} // no open PRs
	runner := &prShepherdScriptRunner{script: gitAncestryScript("localsha", "originsha", false, true)}
	bb := newTestBlackboard()
	deps := prTestDeps(t, gh, runner, nil)
	if got := runPRShepherd(bb, deps); got != 1 {
		t.Fatalf("result = %d, want 1; result=%s", got, bb.Result)
	}
	if bb.Outcome != "pr_shepherd_pr_opened" {
		t.Fatalf("Outcome = %q; result=%s", bb.Outcome, bb.Result)
	}
	if !runner.called("push --force-with-lease origin refs/heads/master:refs/heads/fleet/landing") {
		t.Fatalf("expected branch push from master, calls: %v", runner.calls)
	}
	if !gh.requested("POST") {
		t.Fatalf("expected PR creation, requests: %v", gh.requests)
	}
	if st := loadPRShepherdState(deps.stateDir); st.PRNumber != 77 {
		t.Fatalf("state PRNumber = %d, want 77", st.PRNumber)
	}
	if len(*notes) != 1 || (*notes)[0][1] != "pr_opened" {
		t.Fatalf("expected one pr_opened fleet notification, got %v", *notes)
	}
}

func openPR(headSHA string) []map[string]any {
	return []map[string]any{{
		"number": 77,
		"head":   map[string]any{"sha": headSHA, "ref": "fleet/landing"},
	}}
}

func TestPRShepherd_PendingCISkips(t *testing.T) {
	notes := captureFleetNotifications(t)
	gh := &fakeGitHub{t: t, openPRs: openPR("localsha"), checkRuns: map[string][]map[string]any{
		"localsha": {{"id": int64(1), "name": "Lint", "status": "in_progress", "conclusion": ""}},
	}}
	claude := &fakeClaudeFixRunner{}
	runner := &prShepherdScriptRunner{script: gitAncestryScript("localsha", "originsha", false, true)}
	bb := newTestBlackboard()
	if got := runPRShepherd(bb, prTestDeps(t, gh, runner, claude)); got != 1 {
		t.Fatalf("result = %d, want 1", got)
	}
	if bb.Outcome != "pr_shepherd_ci_pending" {
		t.Fatalf("Outcome = %q", bb.Outcome)
	}
	if gh.requested("PUT") || len(claude.prompts) != 0 {
		t.Fatalf("pending CI must neither merge nor invoke Claude")
	}
	if len(*notes) != 0 {
		t.Fatalf("routine ci_pending pass must not notify, got %v", *notes)
	}
}

func TestPRShepherd_GreenMergesAndSyncs(t *testing.T) {
	notes := captureFleetNotifications(t)
	gh := &fakeGitHub{t: t, openPRs: openPR("localsha"), checkRuns: map[string][]map[string]any{
		"localsha": {
			{"id": int64(1), "name": "Lint", "status": "completed", "conclusion": "success"},
			{"id": int64(2), "name": "Test", "status": "completed", "conclusion": "success"},
		},
	}}
	runner := &prShepherdScriptRunner{script: gitAncestryScript("localsha", "originsha", false, true)}
	bb := newTestBlackboard()
	deps := prTestDeps(t, gh, runner, nil)
	if got := runPRShepherd(bb, deps); got != 1 {
		t.Fatalf("result = %d, want 1; result=%s", got, bb.Result)
	}
	if bb.Outcome != "pr_shepherd_merged" {
		t.Fatalf("Outcome = %q; result=%s", bb.Outcome, bb.Result)
	}
	if !gh.requested("PUT /repos/greenTeeProduction/bt-agent-platform/pulls/77/merge") {
		t.Fatalf("expected merge call, requests: %v", gh.requests)
	}
	if !runner.called("fetch . refs/remotes/origin/master:master") {
		t.Fatalf("expected local master ff after merge, calls: %v", runner.calls)
	}
	if !gh.requested("DELETE") {
		t.Fatalf("expected branch cleanup, requests: %v", gh.requests)
	}
	// The API branch deletion leaves the LOCAL remote-tracking ref behind;
	// only pruning fetches keep the next push's --force-with-lease from
	// failing with "stale info" (live 2026-07-22 16:52).
	if !runner.called("fetch origin --prune") {
		t.Fatalf("fetches must prune stale remote-tracking refs, calls: %v", runner.calls)
	}
	if st := loadPRShepherdState(deps.stateDir); st.PRNumber != 0 || len(st.FixAttempts) != 0 {
		t.Fatalf("state should be cleared after merge: %+v", st)
	}
	// The merge is operator-facing news: it must go out as its own Telegram
	// notification instead of dying with the cycle's routine outcome.
	if len(*notes) != 1 || (*notes)[0][0] != "pr-shepherd" || (*notes)[0][1] != "merged" {
		t.Fatalf("expected one pr-shepherd 'merged' fleet notification, got %v", *notes)
	}
}

func TestPRShepherd_MergeBlockedSkips(t *testing.T) {
	gh := &fakeGitHub{t: t, openPRs: openPR("localsha"), mergeCode: 405, mergeMsg: "At least 1 approving review is required", checkRuns: map[string][]map[string]any{
		"localsha": {{"id": int64(1), "name": "Lint", "status": "completed", "conclusion": "success"}},
	}}
	runner := &prShepherdScriptRunner{script: gitAncestryScript("localsha", "originsha", false, true)}
	bb := newTestBlackboard()
	if got := runPRShepherd(bb, prTestDeps(t, gh, runner, nil)); got != 1 {
		t.Fatalf("result = %d, want 1", got)
	}
	if bb.Outcome != "pr_shepherd_merge_blocked" {
		t.Fatalf("Outcome = %q", bb.Outcome)
	}
	if !strings.Contains(bb.Result, "approving review") {
		t.Fatalf("Result should carry the API message: %s", bb.Result)
	}
}

func TestPRShepherd_RedRunsOneFixAttempt(t *testing.T) {
	gh := &fakeGitHub{t: t, openPRs: openPR("localsha"),
		checkRuns: map[string][]map[string]any{
			"localsha": {{"id": int64(9), "name": "Lint", "status": "completed", "conclusion": "failure"}},
		},
		annotations: map[int64][]map[string]any{
			9: {{"path": "internal/engine/foo.go", "start_line": 42, "annotation_level": "failure", "message": "undefined: frobnicate"}},
		}}
	claude := &fakeClaudeFixRunner{out: "fixed"}
	fixCommitted := false
	base := gitAncestryScript("localsha", "originsha", false, true)
	runner := &prShepherdScriptRunner{}
	runner.script = func(dir, cmd string) (CommandResult, bool) {
		switch {
		case strings.Contains(cmd, "status --porcelain"):
			return CommandResult{Output: " M internal/engine/foo.go\n"}, true
		case strings.Contains(cmd, "git commit -m"):
			fixCommitted = true
			return CommandResult{}, true
		case strings.Contains(cmd, "rev-parse HEAD"):
			if fixCommitted {
				return CommandResult{Output: "fixsha\n"}, true
			}
			return CommandResult{Output: "localsha\n"}, true
		}
		return base(dir, cmd)
	}
	bb := newTestBlackboard()
	deps := prTestDeps(t, gh, runner, claude)
	if got := runPRShepherd(bb, deps); got != 1 {
		t.Fatalf("result = %d, want 1; result=%s", got, bb.Result)
	}
	if bb.Outcome != "pr_shepherd_fix_pushed" {
		t.Fatalf("Outcome = %q; result=%s", bb.Outcome, bb.Result)
	}
	if len(claude.prompts) != 1 || !strings.Contains(claude.prompts[0], "undefined: frobnicate") {
		t.Fatalf("Claude prompt must carry CI annotations, prompts: %v", claude.prompts)
	}
	if !runner.called("worktree add") || !fixCommitted {
		t.Fatalf("fix must run in a worktree and commit deterministically, calls: %v", runner.calls)
	}
	if !runner.called(":master") {
		t.Fatalf("fix must land on local master via non-forced ff, calls: %v", runner.calls)
	}
	if !runner.called("push --force-with-lease origin refs/heads/master:refs/heads/fleet/landing") {
		t.Fatalf("fixed master must be re-pushed to the fleet branch, calls: %v", runner.calls)
	}
	if st := loadPRShepherdState(deps.stateDir); st.FixAttempts["localsha"] != 1 {
		t.Fatalf("fix attempt not recorded: %+v", st)
	}
}

func TestPRShepherd_FixAttemptsExhausted(t *testing.T) {
	gh := &fakeGitHub{t: t, openPRs: openPR("localsha"), checkRuns: map[string][]map[string]any{
		"localsha": {{"id": int64(9), "name": "Lint", "status": "completed", "conclusion": "failure"}},
	}}
	claude := &fakeClaudeFixRunner{}
	runner := &prShepherdScriptRunner{script: gitAncestryScript("localsha", "originsha", false, true)}
	bb := newTestBlackboard()
	deps := prTestDeps(t, gh, runner, claude)
	if err := savePRShepherdState(deps.stateDir, prShepherdState{PRNumber: 77, FixAttempts: map[string]int{"localsha": 3}}); err != nil {
		t.Fatalf("savePRShepherdState: %v", err)
	}
	if got := runPRShepherd(bb, deps); got != 1 {
		t.Fatalf("result = %d, want 1", got)
	}
	if bb.Outcome != "pr_shepherd_fix_exhausted" {
		t.Fatalf("Outcome = %q", bb.Outcome)
	}
	if len(claude.prompts) != 0 {
		t.Fatalf("exhausted attempts must not invoke Claude")
	}
}

// TestPushLandingFallsBackToPROnProtectedBranch pins the landing tail: a
// protected-branch push rejection must ship the landing to the fleet PR
// branch (ApplyStatus committed_pr_opened, nil error) instead of erroring
// committed_unpushed.
func TestPushLandingFallsBackToPROnProtectedBranch(t *testing.T) {
	notes := captureFleetNotifications(t)
	gh := &fakeGitHub{t: t}
	srv := gh.server()
	t.Cleanup(srv.Close)
	prev := landingPRClientFactory
	prevDir := prShepherdDirOverride
	t.Cleanup(func() { landingPRClientFactory = prev; prShepherdDirOverride = prevDir })
	landingPRClientFactory = func(_ context.Context, _ CommandRunner, _ string) (*githubPRClient, error) {
		return &githubPRClient{base: srv.URL, owner: "o", repo: "r", token: "t", hc: srv.Client()}, nil
	}
	prShepherdDirOverride = t.TempDir()

	runner := &prShepherdScriptRunner{}
	runner.script = func(_ string, cmd string) (CommandResult, bool) {
		switch {
		case strings.Contains(cmd, "push origin master"):
			return CommandResult{Err: fmt.Errorf("exit status 1"), Output: "! [remote rejected] master -> master (protected branch hook declined)"}, true
		case strings.Contains(cmd, "log -1 --format=%s"):
			return CommandResult{Output: "engine: subject\n"}, true
		case strings.Contains(cmd, "rev-list --count"):
			return CommandResult{Output: "2\n"}, true
		}
		return CommandResult{}, false
	}
	run := &SuperpowersRun{ID: "run-pr-tail", RepoDir: t.TempDir(), ArtifactDir: t.TempDir()}
	if err := pushLandingMasterToOrigin(context.Background(), runner, run); err != nil {
		t.Fatalf("expected nil error after PR fallback, got %v", err)
	}
	if run.ApplyStatus != "committed_pr_opened" {
		t.Fatalf("ApplyStatus = %q, want committed_pr_opened", run.ApplyStatus)
	}
	if !runner.called("push --force-with-lease origin refs/heads/master:refs/heads/fleet/landing") {
		t.Fatalf("expected fleet branch push, calls: %v", runner.calls)
	}
	if !gh.requested("POST") {
		t.Fatalf("expected PR creation, requests: %v", gh.requests)
	}
	if len(*notes) != 1 || (*notes)[0][0] != "fleet-landing" || (*notes)[0][1] != "landed" || !strings.Contains((*notes)[0][2], "PR #77") {
		t.Fatalf("expected one fleet-landing 'landed' notification naming PR #77, got %v", *notes)
	}
}

// TestPushLandingNotifiesOnDirectPush pins the landed notification for the
// unprotected-master path: a landing that pushes origin/master directly is
// news too.
func TestPushLandingNotifiesOnDirectPush(t *testing.T) {
	notes := captureFleetNotifications(t)
	runner := &prShepherdScriptRunner{}
	run := &SuperpowersRun{ID: "run-direct", RepoDir: t.TempDir(), ArtifactDir: t.TempDir(), AppliedCommit: "abc1234"}
	if err := pushLandingMasterToOrigin(context.Background(), runner, run); err != nil {
		t.Fatalf("direct push should succeed, got %v", err)
	}
	if len(*notes) != 1 || (*notes)[0][0] != "fleet-landing" || (*notes)[0][1] != "landed" || !strings.Contains((*notes)[0][2], "origin/master") {
		t.Fatalf("expected one landed notification naming origin/master, got %v", *notes)
	}
}

func TestShepherdFleetPRActionRegistered(t *testing.T) {
	if GetAction("ShepherdFleetPR") == nil {
		t.Fatal("ShepherdFleetPR is not registered — the goap fusion trees' node would silently no-op")
	}
}
