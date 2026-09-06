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
