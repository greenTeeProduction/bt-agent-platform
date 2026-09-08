package engine

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestPRShepherd_MergeResetsBudgetEvenWhenLocalSyncFails(t *testing.T) {
	gh := &fakeGitHub{t: t, openPRs: openPR("localsha"), checkRuns: map[string][]map[string]any{
		"localsha": {{"id": int64(1), "name": "Test", "status": "completed", "conclusion": "success"}},
	}}
	base := gitAncestryScript("localsha", "originsha", false, true)
	runner := &prShepherdScriptRunner{script: func(dir, cmd string) (CommandResult, bool) {
		if strings.Contains(cmd, "fetch . refs/remotes/origin/master:master") {
			return CommandResult{Err: fmt.Errorf("ff refused")}, true
		}
		return base(dir, cmd)
	}}
	deps := prTestDeps(t, gh, runner, nil)
	if err := savePRShepherdState(deps.stateDir, prShepherdState{PRNumber: 77, FixAttempts: map[string]int{"localsha": 3}, TotalFixAttempts: 6}); err != nil {
		t.Fatal(err)
	}
	bb := newTestBlackboard()
	runPRShepherd(bb, deps)
	st := loadPRShepherdState(deps.stateDir)
	if bb.Outcome != "pr_shepherd_merged" || st.TotalFixAttempts != 0 || len(st.FixAttempts) != 0 {
		t.Fatalf("merged PR must reset budget despite sync failure: outcome=%s state=%+v", bb.Outcome, st)
	}
}

func TestPRShepherd_PreviousMergeLookupFailsClosed(t *testing.T) {
	gh := &fakeGitHub{t: t}
	runner := &prShepherdScriptRunner{script: gitAncestryScript("localsha", "originsha", false, true)}
	deps := prTestDeps(t, gh, runner, nil)
	// A failing HTTP transport proves the old budget cannot be cleared from
	// missing evidence, nor can another PR overwrite its durable identity.
	deps.api.hc = &http.Client{Transport: budgetFailTransport{}}
	if err := savePRShepherdState(deps.stateDir, prShepherdState{PRNumber: 76, TotalFixAttempts: 6}); err != nil {
		t.Fatal(err)
	}
	bb := newTestBlackboard()
	runPRShepherd(bb, deps)
	st := loadPRShepherdState(deps.stateDir)
	if bb.Outcome != "pr_shepherd_api_error" || st.PRNumber != 76 || st.TotalFixAttempts != 6 || runner.called("push") {
		t.Fatalf("lookup error must preserve budget: outcome=%s state=%+v calls=%v", bb.Outcome, st, runner.calls)
	}
}

type budgetFailTransport struct{}

func (budgetFailTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.HasSuffix(r.URL.Path, "/pulls/76") {
		return nil, fmt.Errorf("lookup unavailable")
	}
	return http.DefaultTransport.RoundTrip(r)
}

func TestPRShepherd_ExternalMergeResetsBudgetForAlreadyOpenNextPR(t *testing.T) {
	gh := &fakeGitHub{t: t, previousMerged: true, openPRs: openPR("newhead")}
	runner := &prShepherdScriptRunner{script: gitAncestryScript("localsha", "originsha", false, true)}
	deps := prTestDeps(t, gh, runner, nil)
	if err := savePRShepherdState(deps.stateDir, prShepherdState{PRNumber: 76, FixAttempts: map[string]int{"oldhead": 3}, TotalFixAttempts: 6}); err != nil {
		t.Fatal(err)
	}
	bb := newTestBlackboard()
	runPRShepherd(bb, deps)
	st := loadPRShepherdState(deps.stateDir)
	if bb.Outcome != "pr_shepherd_ci_pending" || st.PRNumber != 77 || st.TotalFixAttempts != 0 || len(st.FixAttempts) != 0 {
		t.Fatalf("next PR inherited budget: outcome=%s state=%+v", bb.Outcome, st)
	}
}
