package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// fakeCommitRunner scripts git/shell results for the commit auto-fix loop. Every
// commit invocation consumes the next entry of commitResults (the last entry
// repeats once exhausted). The staged-check always reports "changes present" so
// the loop proceeds to commit. Deterministic fixers and any other command
// succeed and are recorded.
type fakeCommitRunner struct {
	commitResults []CommandResult
	commitCalls   int
	stageErr      error
	ran           map[string]int
}

func newFakeCommitRunner(results ...CommandResult) *fakeCommitRunner {
	return &fakeCommitRunner{commitResults: results, ran: map[string]int{}}
}

func (f *fakeCommitRunner) Run(ctx context.Context, dir, name string, args ...string) CommandResult {
	full := name + " " + strings.Join(args, " ")
	if name == "git" && len(args) > 0 && args[0] == "add" {
		f.ran["git-add"]++
		return CommandResult{Err: f.stageErr}
	}
	if name == "bash" && len(args) >= 2 && args[0] == "-c" {
		cmd := args[1]
		switch {
		case strings.Contains(cmd, "git diff --cached --quiet"):
			// Non-nil error => there ARE staged changes => proceed to commit.
			return CommandResult{Err: fmt.Errorf("staged changes present")}
		case strings.Contains(cmd, "git commit"):
			i := f.commitCalls
			f.commitCalls++
			if i < len(f.commitResults) {
				return f.commitResults[i]
			}
			if len(f.commitResults) > 0 {
				return f.commitResults[len(f.commitResults)-1]
			}
			return CommandResult{}
		case strings.Contains(cmd, "gofmt -w"):
			f.ran["gofmt"]++
			return CommandResult{}
		case strings.Contains(cmd, "mod tidy"):
			f.ran["mod-tidy"]++
			return CommandResult{}
		case strings.Contains(cmd, "golangci-lint"):
			f.ran["lint-fix"]++
			return CommandResult{}
		}
	}
	_ = full
	return CommandResult{}
}

type fakeClaude struct {
	calls  int
	output string
}

func (f *fakeClaude) RunClaude(ctx context.Context, dir, prompt string) CommandResult {
	f.calls++
	return CommandResult{Output: f.output}
}

func fail(output string) CommandResult {
	return CommandResult{Err: fmt.Errorf("hook rejected"), Output: output}
}

func newAutofixRun(t *testing.T) *SuperpowersRun {
	t.Helper()
	return &SuperpowersRun{ID: "20260713T000000-test", ArtifactDir: t.TempDir()}
}

// TestCommitAutoFix_DeterministicGofmt: a gofmt-only rejection is fixed by
// gofmt -w with no Claude call, and the retry commit lands.
func TestCommitAutoFix_DeterministicGofmt(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_COMMIT_FIX_ATTEMPTS", "10")
	// commitResults are the RETRY commits only; the first (rejected) commit is
	// passed in as firstFailure, as stageAndCommit does in production.
	runner := newFakeCommitRunner(CommandResult{})
	claude := &fakeClaude{}
	run := newAutofixRun(t)
	first := fail("gofmt check: internal/x/y.go is not formatted")
	committed, err := commitWithAutoFix(context.Background(), runner, claude, run, "/wt", "git commit -m x", first)
	if !committed || err != nil {
		t.Fatalf("committed=%v err=%v, want committed nil", committed, err)
	}
	if claude.calls != 0 {
		t.Fatalf("gofmt-only fix must not call Claude, got %d calls", claude.calls)
	}
	if runner.ran["gofmt"] == 0 {
		t.Fatalf("gofmt fixer was not run")
	}
}

// TestCommitAutoFix_ClaudeFixesTest: a failing-test rejection triggers a Claude
// repair pass, and the retry commit lands.
func TestCommitAutoFix_ClaudeFixesTest(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_COMMIT_FIX_ATTEMPTS", "10")
	runner := newFakeCommitRunner(CommandResult{})
	claude := &fakeClaude{}
	run := newAutofixRun(t)
	first := fail("--- FAIL: TestThing (0.01s)\nFAIL")
	committed, err := commitWithAutoFix(context.Background(), runner, claude, run, "/wt", "git commit -m x", first)
	if !committed || err != nil {
		t.Fatalf("committed=%v err=%v, want committed nil", committed, err)
	}
	if claude.calls != 1 {
		t.Fatalf("test failure must trigger exactly one Claude repair, got %d", claude.calls)
	}
}

// TestCommitAutoFix_ExhaustsAttempts: a persistent failure loops the configured
// number of attempts, then abandons as applied_uncommitted.
func TestCommitAutoFix_ExhaustsAttempts(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_COMMIT_FIX_ATTEMPTS", "3")
	runner := newFakeCommitRunner(fail("--- FAIL: TestThing")) // every retry also fails
	claude := &fakeClaude{}
	run := newAutofixRun(t)
	first := fail("--- FAIL: TestThing")
	committed, err := commitWithAutoFix(context.Background(), runner, claude, run, "/wt", "git commit -m x", first)
	if committed || err == nil {
		t.Fatalf("committed=%v err=%v, want not committed with error", committed, err)
	}
	if run.ApplyStatus != "applied_uncommitted" {
		t.Fatalf("ApplyStatus = %q, want applied_uncommitted", run.ApplyStatus)
	}
	if claude.calls != 3 {
		t.Fatalf("Claude repair calls = %d, want 3 (one per attempt)", claude.calls)
	}
	if !strings.Contains(err.Error(), "after 3 auto-fix attempt") {
		t.Fatalf("error missing attempt count: %v", err)
	}
}

// TestCommitAutoFix_RateLimitedSkipsClaude: when the Claude repair pass reports a
// rate limit, further Claude calls are skipped, the loop exits early, and the
// evidence names the rate-limit skip.
func TestCommitAutoFix_RateLimitedSkipsClaude(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_COMMIT_FIX_ATTEMPTS", "10")
	runner := newFakeCommitRunner(fail("--- FAIL: TestThing"))
	claude := &fakeClaude{output: "You've hit your usage limit; resets in 3 days"}
	run := newAutofixRun(t)
	first := fail("--- FAIL: TestThing")
	committed, err := commitWithAutoFix(context.Background(), runner, claude, run, "/wt", "git commit -m x", first)
	if committed || err == nil {
		t.Fatalf("committed=%v err=%v, want not committed with error", committed, err)
	}
	if claude.calls != 1 {
		t.Fatalf("rate-limited Claude must be called once then skipped, got %d", claude.calls)
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("error must note the rate-limit skip: %v", err)
	}
}

// TestCommitAutoFix_Disabled: attempts=0 keeps the old give-up behavior at the
// stage/commit boundary (verified via the wrapper on a bare-style dir path).
func TestClassifyHookFailure(t *testing.T) {
	c := classifyHookFailure("gofmt check: x.go is not formatted")
	if !c.Gofmt || c.Test {
		t.Fatalf("gofmt classification wrong: %+v", c)
	}
	c = classifyHookFailure("--- FAIL: TestX\nFAIL\tpkg 0.1s")
	if !c.Test || !c.needsCodeFix() {
		t.Fatalf("test classification wrong: %+v", c)
	}
	c = classifyHookFailure("=== Doc Drift Validation ===\ndocumentation drift detected")
	if !c.DocDrift || !c.needsCodeFix() {
		t.Fatalf("doc-drift classification wrong: %+v", c)
	}
}
