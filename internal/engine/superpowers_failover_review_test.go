package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRateLimitFailoverReviewCancellationNotQuota(t *testing.T) {
	isolateBackoffStores(t)
	t.Setenv("BT_SUPERPOWERS_RATE_LIMIT_FAILOVER", "true")
	t.Setenv("BT_SUPERPOWERS_PROVIDER", "claude")
	repo, _ := newReviewTestRepo(t)
	d := delegatingRunner{claude: failoverRunnerFunc(func(context.Context, string, string) CommandResult {
		return CommandResult{Err: context.Canceled, Output: "usage limit reached"}
	})}
	bb := &Blackboard{Task: "review"}
	runClaudeCodeReviewResearch(bb, reviewTestDeps(t, repo, d))
	if strings.Contains(bb.Outcome, "rate_limited") {
		t.Fatalf("cancellation misclassified: %s", bb.Result)
	}
}

func TestRateLimitFailoverReviewKeepsPrimaryCooldown(t *testing.T) {
	isolateBackoffStores(t)
	t.Setenv("BT_SUPERPOWERS_RATE_LIMIT_FAILOVER", "true")
	t.Setenv("BT_SUPERPOWERS_PROVIDER", "codex")
	repo, _ := newReviewTestRepo(t)
	until := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	writeSharedBackoff(backoffPathFor(DelegationProviderCodex), until, "test")
	d := delegatingRunner{codex: failoverRunnerFunc(func(context.Context, string, string) CommandResult {
		t.Error("closed primary called")
		return CommandResult{}
	}), claude: failoverRunnerFunc(func(context.Context, string, string) CommandResult {
		return CommandResult{Err: errors.New("usage limit reached")}
	})}
	bb := &Blackboard{Task: "review"}
	runClaudeCodeReviewResearch(bb, reviewTestDeps(t, repo, d))
	if !strings.Contains(bb.Outcome, "rate_limited") {
		t.Fatalf("missing rate outcome: %s", bb.Result)
	}
	if got, _ := readSharedBackoff(backoffPathFor(DelegationProviderCodex)); !got.Equal(until) {
		t.Fatalf("primary cooldown changed: %v", got)
	}
	if !strings.Contains(bb.Result, until.Format(time.RFC3339)) {
		t.Fatalf("earliest retry missing: %s", bb.Result)
	}
}
