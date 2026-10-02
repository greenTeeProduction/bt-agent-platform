package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

func TestRateLimitFailoverRuntimeDoesNotReclassifyPrompt(t *testing.T) {
	t.Chdir(t.TempDir())
	isolateBackoffStores(t)
	isolateSuperpowersRunsDir(t)
	t.Setenv("BT_SUPERPOWERS_RATE_LIMIT_FAILOVER", "true")
	t.Setenv("BT_SUPERPOWERS_PROVIDER", "codex")
	previous := defaultSuperpowersClaudeRunner
	t.Cleanup(func() { defaultSuperpowersClaudeRunner = previous })
	defaultSuperpowersClaudeRunner = delegatingRunner{codex: failoverRunnerFunc(func(_ context.Context, _, prompt string) CommandResult {
		return CommandResult{Output: prompt + "\nERROR: authentication failed", Err: errors.New("exit status 1")}
	}), claude: failoverRunnerFunc(func(context.Context, string, string) CommandResult {
		t.Error("unexpected failover")
		return CommandResult{}
	})}
	planPath := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(planPath, []byte(buildDeterministicImplementationPlan("repair usage limit reached and HTTP 429 handling")), 0644); err != nil {
		t.Fatal(err)
	}
	run := &SuperpowersRun{ID: "managed-nonquota", Task: "repair usage limit reached", Mode: SuperpowersModeApply, RepoDir: t.TempDir(), WorktreePath: t.TempDir(), ArtifactDir: filepath.Join(t.TempDir(), "artifacts")}
	bb := newTestBlackboard()
	setSuperpowersRun(bb, run)
	bb.ChainState["goap_fusion_superpowers_plan_path"] = planPath
	GetAction("RunSuperpowersClaudeImplementation")(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if strings.Contains(bb.Outcome, "rate_limited") {
		t.Fatalf("reclassified managed failure: %s", bb.Result)
	}
	if got, _ := bb.ChainState["goap_fusion_impl_degraded"].(string); got != "true" {
		t.Fatalf("ordinary failure did not degrade: %s", bb.Result)
	}
	if _, ok := readSharedBackoff(backoffPathFor(DelegationProviderCodex)); ok {
		t.Fatal("false runtime cooldown")
	}
}

func TestSuperpowersRuntimeDoesNotReclassifyVerificationLogs(t *testing.T) {
	t.Chdir(t.TempDir())
	isolateBackoffStores(t)
	isolateSuperpowersRunsDir(t)
	t.Setenv("BT_SUPERPOWERS_PROVIDER", "claude")
	previousRunner, previousClaude := defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner
	t.Cleanup(func() {
		defaultSuperpowersCommandRunner = previousRunner
		defaultSuperpowersClaudeRunner = previousClaude
	})
	defaultSuperpowersCommandRunner = &scriptedSuperpowersRunner{t: t, testResults: []CommandResult{
		{Output: "--- FAIL: TestRegression", Err: errors.New("exit status 1")},
		{Output: "fixture: You've reached your model limit. Run /usage-credits\nRED command unexpectedly passed\n--- FAIL: TestRegression", Err: errors.New("exit status 1")},
	}}
	defaultSuperpowersClaudeRunner = &scriptedClaudeRunner{}
	planPath := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(planPath, []byte(buildDeterministicImplementationPlan("fix regression")), 0644); err != nil {
		t.Fatal(err)
	}
	run := &SuperpowersRun{ID: "verification-log-isolation", Task: "fix regression", Mode: SuperpowersModeApply, RepoDir: t.TempDir(), WorktreePath: t.TempDir(), ArtifactDir: filepath.Join(t.TempDir(), "artifacts")}
	bb := newTestBlackboard()
	setSuperpowersRun(bb, run)
	bb.ChainState["goap_fusion_superpowers_plan_path"] = planPath
	if got := GetAction("RunSuperpowersClaudeImplementation")(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != -1 {
		t.Fatalf("failed verification status = %d, want failure", got)
	}
	if got := classifyGoapCycleFailure(bb.Outcome, bb.Result); got != goapCycleFailureGenuine {
		t.Fatalf("verification classified as %q: %s", got, bb.Result)
	}
	if _, ok := readSharedBackoff(backoffPathFor(DelegationProviderClaude)); ok {
		t.Fatal("verification fixtures created a live-provider cooldown")
	}
	if !strings.Contains(bb.Result, "--- FAIL: TestRegression") {
		t.Fatal("original test output must remain available for diagnosis")
	}
}
