package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type autofixScriptStep struct {
	match string
	res   CommandResult
}

// autofixScriptRunner replays an ordered command script, failing the test on
// any out-of-order or unexpected command.
type autofixScriptRunner struct {
	t     *testing.T
	steps []autofixScriptStep
	i     int
	calls []string
}

func (r *autofixScriptRunner) Run(_ context.Context, dir, name string, args ...string) CommandResult {
	cmd := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, cmd)
	if r.i >= len(r.steps) {
		r.t.Fatalf("unexpected command #%d: %q", r.i+1, cmd)
	}
	step := r.steps[r.i]
	r.i++
	if !strings.Contains(cmd, step.match) {
		r.t.Fatalf("command #%d = %q, want match %q", r.i, cmd, step.match)
	}
	res := step.res
	res.Command = cmd
	res.Dir = dir
	if res.Duration == 0 {
		res.Duration = time.Millisecond
	}
	return res
}

func withAutofixVerifyEnv(t *testing.T, runner *autofixScriptRunner) {
	t.Helper()
	oldRunner := defaultSuperpowersCommandRunner
	oldLint := superpowersLintBin
	defaultSuperpowersCommandRunner = runner
	superpowersLintBin = "golangci-lint"
	t.Cleanup(func() {
		defaultSuperpowersCommandRunner = oldRunner
		superpowersLintBin = oldLint
	})
}

// withAutofixClaude overrides defaultSuperpowersClaudeRunner with a fake for the
// duration of the test, so the verification-lint Claude self-correct pass can be
// observed without executing the real claude binary. Reuses the fakeClaude type
// from superpowers_commit_autofix_test.go (same package).
func withAutofixClaude(t *testing.T, claude ClaudeRunner) {
	t.Helper()
	old := defaultSuperpowersClaudeRunner
	defaultSuperpowersClaudeRunner = claude
	t.Cleanup(func() { defaultSuperpowersClaudeRunner = old })
}

func autofixVerifyRun(t *testing.T) *SuperpowersRun {
	t.Helper()
	return &SuperpowersRun{
		RepoDir:      t.TempDir(),
		ArtifactDir:  t.TempDir(),
		ChangedFiles: []string{"internal/evolution/map_elites.go"},
	}
}

func verificationTrail(run *SuperpowersRun) string {
	names := make([]string, 0, len(run.Verification))
	for _, vc := range run.Verification {
		names = append(names, fmt.Sprintf("%s:%v", vc.Name, vc.Passed))
	}
	return strings.Join(names, ",")
}

// A verification failure on changed-packages-lint alone must not kill the run:
// staticcheck's QF class ships machine-applicable fixes, so the runtime runs
// one bounded `golangci-lint run --fix` pass and re-checks. The 2026-07-15
// 22:29 cycle lost 42 minutes of landable work (all three self-healing-
// envelope milestones) to a single auto-fixable QF1008 selector.
func TestVerifySuperpowersRunAutofixesLintOnlyFailure(t *testing.T) {
	runner := &autofixScriptRunner{t: t, steps: []autofixScriptStep{
		{match: "go test ./internal/domains", res: CommandResult{}},
		{match: "go build", res: CommandResult{}},
		{match: "go test ./internal/evolution", res: CommandResult{}},
		{match: "golangci-lint run ./internal/evolution/...", res: CommandResult{
			Err:    errors.New("exit status 1"),
			Output: "map_elites_test.go:368:13: QF1008: could remove embedded field \"Population\" from selector (staticcheck)",
		}},
		{match: "golangci-lint run --fix ./internal/evolution/...", res: CommandResult{}},
		{match: "golangci-lint run ./internal/evolution/...", res: CommandResult{Output: "0 issues."}},
	}}
	withAutofixVerifyEnv(t, runner)

	run := autofixVerifyRun(t)
	if err := VerifySuperpowersRunRuntime(context.Background(), run); err != nil {
		t.Fatalf("verification with auto-fixable lint finding = %v, want nil", err)
	}
	trail := verificationTrail(run)
	for _, want := range []string{
		"changed-packages-lint:false",
		"changed-packages-lint-autofix:true",
		"changed-packages-lint-retry:true",
	} {
		if !strings.Contains(trail, want) {
			t.Fatalf("verification trail %q missing %q", trail, want)
		}
	}
}

// When --fix cannot clear the findings, exactly ONE bounded Claude self-correct
// pass is attempted before the failure is final — not a loop. Here Claude's
// edits also fail to clear the findings, so the run still fails, and the fake
// Claude is invoked exactly once.
func TestVerifySuperpowersRunLintAutofixFailureIsFinal(t *testing.T) {
	runner := &autofixScriptRunner{t: t, steps: []autofixScriptStep{
		{match: "go test ./internal/domains", res: CommandResult{}},
		{match: "go build", res: CommandResult{}},
		{match: "go test ./internal/evolution", res: CommandResult{}},
		{match: "golangci-lint run ./internal/evolution/...", res: CommandResult{
			Err: errors.New("exit status 1"), Output: "foo.go:1:1: SA4006: unused value (staticcheck)"}},
		{match: "golangci-lint run --fix ./internal/evolution/...", res: CommandResult{}},
		{match: "golangci-lint run ./internal/evolution/...", res: CommandResult{
			Err: errors.New("exit status 1"), Output: "foo.go:1:1: SA4006: unused value (staticcheck)"}},
		// Claude self-correct pass (fakeClaude, no command-runner step), then
		// re-fix + re-lint, still failing.
		{match: "golangci-lint run --fix ./internal/evolution/...", res: CommandResult{}},
		{match: "golangci-lint run ./internal/evolution/...", res: CommandResult{
			Err: errors.New("exit status 1"), Output: "foo.go:1:1: SA4006: unused value (staticcheck)"}},
	}}
	withAutofixVerifyEnv(t, runner)
	claude := &fakeClaude{output: "edited the code but the finding remains"}
	withAutofixClaude(t, claude)

	run := autofixVerifyRun(t)
	err := VerifySuperpowersRunRuntime(context.Background(), run)
	if err == nil || !strings.Contains(err.Error(), "changed-packages-lint") {
		t.Fatalf("unfixable lint finding must still fail verification, got %v", err)
	}
	if claude.calls != 1 {
		t.Fatalf("exactly one Claude self-correct pass expected, got %d", claude.calls)
	}
	if !strings.Contains(verificationTrail(run), "changed-packages-lint-claude-retry:false") {
		t.Fatalf("failed post-Claude retry must be recorded, trail: %s", verificationTrail(run))
	}
}

// When deterministic --fix leaves findings golangci-lint cannot repair
// (errcheck here), ONE bounded Claude self-correct pass fixes the root cause;
// after Claude's edits the re-fix + re-lint pass clean, so the fully-tested
// cycle lands instead of degrading. The Claude pass is recorded in the trail.
func TestVerifySuperpowersRunClaudeRepairsUnfixableLint(t *testing.T) {
	runner := &autofixScriptRunner{t: t, steps: []autofixScriptStep{
		{match: "go test ./internal/domains", res: CommandResult{}},
		{match: "go build", res: CommandResult{}},
		{match: "go test ./internal/evolution", res: CommandResult{}},
		{match: "golangci-lint run ./internal/evolution/...", res: CommandResult{
			Err:    errors.New("exit status 1"),
			Output: "map_elites.go:12:2: Error return value is not checked (errcheck)"}},
		{match: "golangci-lint run --fix ./internal/evolution/...", res: CommandResult{}},
		{match: "golangci-lint run ./internal/evolution/...", res: CommandResult{
			Err:    errors.New("exit status 1"),
			Output: "map_elites.go:12:2: Error return value is not checked (errcheck)"}},
		// Claude self-correct pass (fakeClaude, no command-runner step), then
		// re-fix + re-lint, now clean.
		{match: "golangci-lint run --fix ./internal/evolution/...", res: CommandResult{}},
		{match: "golangci-lint run ./internal/evolution/...", res: CommandResult{Output: "0 issues."}},
	}}
	withAutofixVerifyEnv(t, runner)
	claude := &fakeClaude{output: "handled the unchecked error"}
	withAutofixClaude(t, claude)

	run := autofixVerifyRun(t)
	if err := VerifySuperpowersRunRuntime(context.Background(), run); err != nil {
		t.Fatalf("Claude-repaired lint must pass verification, got %v", err)
	}
	if claude.calls != 1 {
		t.Fatalf("exactly one Claude self-correct pass expected, got %d", claude.calls)
	}
	trail := verificationTrail(run)
	for _, want := range []string{
		"changed-packages-lint-claude-fix:true",
		"changed-packages-lint-claude-refix:true",
		"changed-packages-lint-claude-retry:true",
	} {
		if !strings.Contains(trail, want) {
			t.Fatalf("verification trail %q missing %q", trail, want)
		}
	}
}

// When the finding survives even the Claude pass, verification still fails and
// the attempted Claude repair is recorded (distinct from the deterministic-only
// path in TestVerifySuperpowersRunLintAutofixFailureIsFinal).
func TestVerifySuperpowersRunClaudeRepairStillFailsIsFinal(t *testing.T) {
	runner := &autofixScriptRunner{t: t, steps: []autofixScriptStep{
		{match: "go test ./internal/domains", res: CommandResult{}},
		{match: "go build", res: CommandResult{}},
		{match: "go test ./internal/evolution", res: CommandResult{}},
		{match: "golangci-lint run ./internal/evolution/...", res: CommandResult{
			Err:    errors.New("exit status 1"),
			Output: "map_elites.go:12:2: Error return value is not checked (errcheck)"}},
		{match: "golangci-lint run --fix ./internal/evolution/...", res: CommandResult{}},
		{match: "golangci-lint run ./internal/evolution/...", res: CommandResult{
			Err:    errors.New("exit status 1"),
			Output: "map_elites.go:12:2: Error return value is not checked (errcheck)"}},
		{match: "golangci-lint run --fix ./internal/evolution/...", res: CommandResult{}},
		{match: "golangci-lint run ./internal/evolution/...", res: CommandResult{
			Err:    errors.New("exit status 1"),
			Output: "map_elites.go:12:2: Error return value is not checked (errcheck)"}},
	}}
	withAutofixVerifyEnv(t, runner)
	claude := &fakeClaude{output: "attempted a fix"}
	withAutofixClaude(t, claude)

	run := autofixVerifyRun(t)
	err := VerifySuperpowersRunRuntime(context.Background(), run)
	if err == nil || !strings.Contains(err.Error(), "changed-packages-lint") {
		t.Fatalf("lint that survives the Claude pass must fail verification, got %v", err)
	}
	if !strings.Contains(verificationTrail(run), "changed-packages-lint-claude-fix:true") {
		t.Fatalf("the attempted Claude repair must be recorded, trail: %s", verificationTrail(run))
	}
}

// A rate-limited Claude pass must bail cleanly: Claude is invoked once, detects
// the rate limit, and the run degrades to today's failure WITHOUT running the
// post-Claude re-fix / re-lint (those steps are absent from the script, so the
// ordered runner would fatal if they ran).
func TestVerifySuperpowersRunLintRepairSkippedWhenRateLimited(t *testing.T) {
	runner := &autofixScriptRunner{t: t, steps: []autofixScriptStep{
		{match: "go test ./internal/domains", res: CommandResult{}},
		{match: "go build", res: CommandResult{}},
		{match: "go test ./internal/evolution", res: CommandResult{}},
		{match: "golangci-lint run ./internal/evolution/...", res: CommandResult{
			Err:    errors.New("exit status 1"),
			Output: "map_elites.go:12:2: Error return value is not checked (errcheck)"}},
		{match: "golangci-lint run --fix ./internal/evolution/...", res: CommandResult{}},
		{match: "golangci-lint run ./internal/evolution/...", res: CommandResult{
			Err:    errors.New("exit status 1"),
			Output: "map_elites.go:12:2: Error return value is not checked (errcheck)"}},
	}}
	withAutofixVerifyEnv(t, runner)
	claude := &fakeClaude{output: "You've hit your usage limit; resets in 3 days"}
	withAutofixClaude(t, claude)

	run := autofixVerifyRun(t)
	err := VerifySuperpowersRunRuntime(context.Background(), run)
	if err == nil || !strings.Contains(err.Error(), "changed-packages-lint") {
		t.Fatalf("rate-limited repair must fall through to failure, got %v", err)
	}
	if claude.calls != 1 {
		t.Fatalf("Claude attempted once then skipped on rate limit, got %d calls", claude.calls)
	}
	if strings.Contains(verificationTrail(run), "changed-packages-lint-claude-refix") {
		t.Fatalf("rate-limited pass must not run refix/retry, trail: %s", verificationTrail(run))
	}
}

// BT_SUPERPOWERS_VERIFY_LINT_FIX_ATTEMPTS=0 is the escape hatch back to today's
// deterministic --fix-only behavior: NO Claude call is made.
func TestVerifyLintFixMaxAttemptsZeroDisablesClaude(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_VERIFY_LINT_FIX_ATTEMPTS", "0")
	runner := &autofixScriptRunner{t: t, steps: []autofixScriptStep{
		{match: "go test ./internal/domains", res: CommandResult{}},
		{match: "go build", res: CommandResult{}},
		{match: "go test ./internal/evolution", res: CommandResult{}},
		{match: "golangci-lint run ./internal/evolution/...", res: CommandResult{
			Err:    errors.New("exit status 1"),
			Output: "map_elites.go:12:2: Error return value is not checked (errcheck)"}},
		{match: "golangci-lint run --fix ./internal/evolution/...", res: CommandResult{}},
		{match: "golangci-lint run ./internal/evolution/...", res: CommandResult{
			Err:    errors.New("exit status 1"),
			Output: "map_elites.go:12:2: Error return value is not checked (errcheck)"}},
	}}
	withAutofixVerifyEnv(t, runner)
	claude := &fakeClaude{output: "should never be called"}
	withAutofixClaude(t, claude)

	run := autofixVerifyRun(t)
	err := VerifySuperpowersRunRuntime(context.Background(), run)
	if err == nil || !strings.Contains(err.Error(), "changed-packages-lint") {
		t.Fatalf("disabled Claude pass must degrade to today's one-shot --fix, got %v", err)
	}
	if claude.calls != 0 {
		t.Fatalf("attempts=0 must not call Claude, got %d calls", claude.calls)
	}
}

// The remediation is lint-specific: build/test failures fail immediately and
// never trigger a --fix pass.
func TestVerifySuperpowersRunDoesNotAutofixNonLintFailures(t *testing.T) {
	runner := &autofixScriptRunner{t: t, steps: []autofixScriptStep{
		{match: "go test ./internal/domains", res: CommandResult{}},
		{match: "go build", res: CommandResult{Err: errors.New("exit status 2"), Output: "syntax error"}},
	}}
	withAutofixVerifyEnv(t, runner)

	run := autofixVerifyRun(t)
	if err := VerifySuperpowersRunRuntime(context.Background(), run); err == nil {
		t.Fatal("build failure must fail verification")
	}
	for _, c := range runner.calls {
		if strings.Contains(c, "--fix") {
			t.Fatalf("build failure must not trigger lint autofix, ran: %q", c)
		}
	}
}

func TestChangedPackagesLintFixCommandInsertsFixFlag(t *testing.T) {
	oldLint := superpowersLintBin
	superpowersLintBin = "golangci-lint"
	t.Cleanup(func() { superpowersLintBin = oldLint })

	cmd := changedPackagesLintFixCommand([]string{"internal/evolution/map_elites.go"})
	if !strings.Contains(cmd, "golangci-lint run --fix ./internal/evolution/...") {
		t.Fatalf("fix command = %q, want run --fix on the changed packages", cmd)
	}
}
