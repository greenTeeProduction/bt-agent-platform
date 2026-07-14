package engine

import (
	"path/filepath"
	"testing"
	"time"

	btcore "github.com/rvitorper/go-bt/core"

	"github.com/nico/go-bt-evolve/internal/research"
)

// The milestone-abandon budget (goapProgramMaxMilestoneAttempts) exists to stop
// fabricated/unbuildable milestones from freezing a program. It must be
// consumed ONLY by genuine implementation failures — when a cycle dies for
// infrastructure reasons (Claude rate limit, commit gate wedged by an external
// landing, apply/sync refusal, worktree creation failure), the attempt charged
// at queue time has to be refunded, or three cycles of external outage wrongly
// block the milestone. That is exactly what happened on 2026-07-09 (doc-drift
// wedge blocked 2 programs ×3 attempts) and 2026-07-08 (rate-limit window
// blocked all 5 of a69ef9d1's milestones).

// seedRefundProgram points goapProgramsPath at a temp store holding one program
// whose head milestone has the given attempts/status, and returns its ID.
func seedRefundProgram(t *testing.T, attempts int, status string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "programs.json")
	ps, err := research.OpenPrograms(path)
	if err != nil {
		t.Fatal(err)
	}
	p := ps.Add("Refund probe program", "test", []string{"head milestone", "tail milestone"})
	ps.Programs[0].Milestones[0].Attempts = attempts
	ps.Programs[0].Milestones[0].Status = status
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}
	prev := goapProgramsPath
	goapProgramsPath = path
	t.Cleanup(func() { goapProgramsPath = prev })
	return p.ID
}

// refundProbeHeadMilestone reloads the seeded store and returns its head milestone.
func refundProbeHeadMilestone(t *testing.T) research.Milestone {
	t.Helper()
	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		t.Fatal(err)
	}
	return ps.Programs[0].Milestones[0]
}

// Infra-class failures refund; implementation failures and verification
// failures keep consuming the abandon budget.
func TestIsGoapInfraCycleFailure(t *testing.T) {
	cases := []struct {
		name    string
		outcome string
		result  string
		want    bool
	}{
		{"rate limit outcome", "goap_fusion_rate_limited", "", true},
		{"pending patch outcome", "pending_patch", "", true},
		{"commit gate wedge in result", "failure", "## GOAP Superpowers Pending Patch\n\napplied_uncommitted: git commit failed (pre-commit hook?)", true},
		{"worktree creation failure", "failure", "## GOAP Superpowers Worktree Failed\n\nfatal: could not create work tree", true},
		{"agent declined milestone", "failure", "## GOAP Superpowers Execution Failed\n\nthe implementation agent declined the fabricated milestone", false},
		{"verification failure is genuine", "failure", "## GOAP Superpowers Verification Failed\n\nchanged-packages-tests: FAIL", false},
		{"claude usage limit is infra (quota outage carries over, not an unbuildable milestone)", "failure", "## GOAP Superpowers Execution Failed\n\nred-phase claude failed: exit status 1\nYou've reached your Fable 5 limit. Run /usage-credits to continue or switch models with /model.", true},
		{"claude usage limit is model-agnostic", "failure", "## GOAP Superpowers Execution Failed\n\nred-phase claude failed: exit status 1\nClaude usage limit reached — run /usage-credits to continue.", true},
		{"plain success", "success", "## GOAP Superpowers Runtime Complete", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isGoapInfraCycleFailure(c.outcome, c.result); got != c.want {
				t.Fatalf("isGoapInfraCycleFailure(%q, %q) = %v, want %v", c.outcome, c.result, got, c.want)
			}
		})
	}
}

// A commit-gate rejection caused by the cycle's OWN staged code failing a
// deterministic quality check (gofmt / go vet / golangci-lint / go mod tidy /
// short tests) reproduces every cycle until the code is fixed, so it MUST charge
// the milestone-abandon budget — NOT refund — even though the failing step
// reports the generic applied_uncommitted / pending_patch result. On 2026-07-12
// program 94b0b31's milestone treadmilled ~15 cycles over 12h (each cycle's
// generated code tripped a different linter — redefines-builtin `min`, unchecked
// errcheck, gocritic appendCombine) and refunded every time, landing nothing.
// An OOM kill ("signal: killed", which prints no ✗ marker) or an external
// doc-drift wedge stays infra (refund) — those do not reproduce from the code.
func TestIsGoapInfraCycleFailure_OwnCodeGateFailureCharges(t *testing.T) {
	cases := []struct {
		name    string
		outcome string
		result  string
		want    bool
	}{
		{
			"golangci-lint redefines-builtin charges even with pending_patch outcome",
			"pending_patch",
			"## GOAP Superpowers Pending Patch\n\napplied_uncommitted: git commit failed: exit status 1\ncmd/bt-agent/main.go:84:2: redefines-builtin-id: redefinition of the built-in function min (revive)\n✗ golangci-lint found issues — the CI Lint job runs the same linters repo-wide.",
			false,
		},
		{
			"golangci-lint errcheck charges",
			"failure",
			"applied_uncommitted: git commit failed: exit status 1\ninternal/agent/rebuild.go:50:24: Error return value is not checked (errcheck)\n✗ golangci-lint found issues — the CI Lint job runs the same linters repo-wide.",
			false,
		},
		{
			"go vet failure charges",
			"pending_patch",
			"applied_uncommitted: git commit failed: exit status 1\n✗ go vet found issues. Fix before committing.",
			false,
		},
		{
			"gofmt failure charges",
			"pending_patch",
			"applied_uncommitted: git commit failed: exit status 1\n✗ The following staged files need formatting:\ncmd/bt-agent/main.go",
			false,
		},
		{
			"short-test failure charges",
			"pending_patch",
			"applied_uncommitted: git commit failed: exit status 1\n✗ Tests failed. Fix before committing.",
			false,
		},
		{
			"go mod tidy drift charges",
			"pending_patch",
			"applied_uncommitted: git commit failed: exit status 1\n✗ go.mod/go.sum out of sync — go mod tidy changed them; stage and retry.",
			false,
		},
		{
			"OOM kill mid-gate stays infra (no ✗ own-code marker)",
			"pending_patch",
			"## GOAP Superpowers Pending Patch\n\napplied_uncommitted: git commit failed: signal: killed\n→ golangci-lint (staged packages)...\n0 issues.\n  ✓ passed\n→ go test -short...",
			true,
		},
		{
			"external doc-drift wedge stays infra (self-heals; preserves 2026-07-09 protection)",
			"pending_patch",
			"applied_uncommitted: git commit failed: exit status 1\n✗ Documentation drift detected. Update docs to match codebase.",
			true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isGoapInfraCycleFailure(c.outcome, c.result); got != c.want {
				t.Fatalf("isGoapInfraCycleFailure(%q, ...) = %v, want %v", c.outcome, got, c.want)
			}
		})
	}
}

// The refund targets the milestone PrioritizeGoapGoals charged this cycle (the
// "program_milestone_charged" stamp), and is idempotent per cycle so a doubled
// failure path can never refund twice.
func TestRefundGoapMilestoneAttempt_UsesChargedStampAndIsIdempotent(t *testing.T) {
	id := seedRefundProgram(t, 1, "pending")
	bb := &Blackboard{ChainState: map[string]any{
		"goap_fusion_program_milestone_charged": id + ":0",
	}}

	if !refundGoapMilestoneAttemptForInfraFailure(bb) {
		t.Fatal("refund must fire when a charged stamp exists")
	}
	if m := refundProbeHeadMilestone(t); m.Attempts != 0 || m.Status != "pending" {
		t.Fatalf("store after refund: attempts=%d status=%q, want 0/pending", m.Attempts, m.Status)
	}

	// Second invocation in the same cycle: no-op (per-cycle idempotence flag).
	if refundGoapMilestoneAttemptForInfraFailure(bb) {
		t.Fatal("refund must be idempotent within one cycle")
	}
	if m := refundProbeHeadMilestone(t); m.Attempts != 0 {
		t.Fatalf("second refund changed the store: attempts=%d, want 0", m.Attempts)
	}
}

// A refund that undoes the charge which had just blocked the milestone also
// restores it to pending — the block was an infrastructure artifact.
func TestRefundGoapMilestoneAttempt_UnblocksJustBlockedMilestone(t *testing.T) {
	id := seedRefundProgram(t, goapProgramMaxMilestoneAttempts, "blocked")
	bb := &Blackboard{ChainState: map[string]any{
		"goap_fusion_program_milestone_charged": id + ":0",
	}}

	if !refundGoapMilestoneAttemptForInfraFailure(bb) {
		t.Fatal("refund must fire for the just-blocked milestone")
	}
	m := refundProbeHeadMilestone(t)
	if m.Status != "pending" || m.Attempts != goapProgramMaxMilestoneAttempts-1 {
		t.Fatalf("after refund: status=%q attempts=%d, want pending/%d",
			m.Status, m.Attempts, goapProgramMaxMilestoneAttempts-1)
	}
}

// Older cycles' state (no charged stamp) still refunds via the head queued ref,
// and a cycle with no program stamps at all is a no-op.
func TestRefundGoapMilestoneAttempt_HeadRefFallbackAndNoop(t *testing.T) {
	id := seedRefundProgram(t, 2, "pending")

	fallback := &Blackboard{ChainState: map[string]any{
		"goap_fusion_program_milestone": id + ":0," + id + ":1",
	}}
	if !refundGoapMilestoneAttemptForInfraFailure(fallback) {
		t.Fatal("refund must fall back to the head queued ref when no charged stamp exists")
	}
	if m := refundProbeHeadMilestone(t); m.Attempts != 1 {
		t.Fatalf("fallback refund: attempts=%d, want 1", m.Attempts)
	}

	if refundGoapMilestoneAttemptForInfraFailure(&Blackboard{ChainState: map[string]any{}}) {
		t.Fatal("a cycle without program stamps must not refund anything")
	}
	if m := refundProbeHeadMilestone(t); m.Attempts != 1 {
		t.Fatalf("no-op path changed the store: attempts=%d, want 1", m.Attempts)
	}
}

// PrioritizeGoapGoals must stamp WHICH milestone it charged: when the charge
// blocks the milestone, the queued refs (re-read after re-opening the store)
// start at the NEXT pending milestone, so the head queued ref no longer names
// the charged one — the refund needs its own stamp to undo the right charge.
func TestPrioritizeGoapGoals_StampsChargedMilestone(t *testing.T) {
	id := seedRefundProgram(t, 0, "pending")

	prioritize := GetAction("PrioritizeGoapGoals")
	if prioritize == nil {
		t.Fatal("action \"PrioritizeGoapGoals\" not registered")
	}
	bb := &Blackboard{ChainState: map[string]any{}}
	if got := prioritize(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("PrioritizeGoapGoals status = %d, want 1; result: %s", got, bb.Result)
	}

	charged, _ := bb.ChainState["goap_fusion_program_milestone_charged"].(string)
	if charged != id+":0" {
		t.Fatalf("charged stamp = %q, want %q", charged, id+":0")
	}
	if m := refundProbeHeadMilestone(t); m.Attempts != 1 {
		t.Fatalf("charge not recorded: attempts=%d, want 1", m.Attempts)
	}
}

// End-to-end wiring: a scheduled runtime cycle that dies on the rate-limit
// entry guard (durable Claude backoff active) must refund the attempt charged
// earlier in the same cycle — external quota windows cannot consume the
// milestone-abandon budget.
func TestScheduledRuntime_RefundsChargeOnRateLimitBackoff(t *testing.T) {
	isolateClaudeBackoffStore(t)
	id := seedRefundProgram(t, 1, "pending")

	bb := &Blackboard{ChainState: map[string]any{
		"plan_path":                             "/nonexistent/superpowers-plan.md",
		"goap_fusion_program_milestone_charged": id + ":0",
	}}
	saveClaudeBackoffState(bb, time.Now().Add(time.Hour))

	got := runSuperpowersRuntimeFromExistingPlanAction(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if got != -1 {
		t.Fatalf("backoff entry guard must degrade the cycle: status = %d, want -1", got)
	}
	if bb.Outcome != "goap_fusion_rate_limited" {
		t.Fatalf("outcome = %q, want goap_fusion_rate_limited", bb.Outcome)
	}
	if m := refundProbeHeadMilestone(t); m.Attempts != 0 {
		t.Fatalf("rate-limited cycle did not refund the charge: attempts=%d, want 0", m.Attempts)
	}
}
