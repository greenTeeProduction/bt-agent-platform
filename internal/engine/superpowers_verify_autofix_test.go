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

// When --fix cannot clear the findings, the retry's failure is final — the
// remediation is one-shot, not a loop.
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
	}}
	withAutofixVerifyEnv(t, runner)

	run := autofixVerifyRun(t)
	err := VerifySuperpowersRunRuntime(context.Background(), run)
	if err == nil || !strings.Contains(err.Error(), "changed-packages-lint") {
		t.Fatalf("unfixable lint finding must still fail verification, got %v", err)
	}
	if !strings.Contains(verificationTrail(run), "changed-packages-lint-retry:false") {
		t.Fatalf("failed retry must be recorded, trail: %s", verificationTrail(run))
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
