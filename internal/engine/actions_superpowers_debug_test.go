package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	btcore "github.com/rvitorper/go-bt/core"
)

// debugTestRunner records every command it is asked to run and never errors,
// so tests can assert on the exact command list without scripted per-call
// responses (unlike scriptedSuperpowersRunner, which exists for the
// RED/GREEN test-loop's git-status/test-command sequencing).
type debugTestRunner struct {
	calls []string
}

func (r *debugTestRunner) Run(_ context.Context, dir string, name string, args ...string) CommandResult {
	cmd := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.calls = append(r.calls, dir+" :: "+cmd)
	return CommandResult{Command: cmd, Dir: dir, Duration: time.Millisecond}
}

// debugTestClaude returns a fixed canned response per call and records every
// prompt it was given.
type debugTestClaude struct {
	prompts []string
	output  string
}

func (c *debugTestClaude) RunClaude(_ context.Context, repoDir string, prompt string) CommandResult {
	c.prompts = append(c.prompts, prompt)
	return CommandResult{Command: "claude <prompt>", Dir: repoDir, Output: c.output, Duration: time.Millisecond}
}

// withSwappedSuperpowersRunners swaps the package-level default
// runner/claude globals the RegisterAction closures call through, restoring
// them on cleanup. Passing nil for either leaves that global untouched.
func withSwappedSuperpowersRunners(t *testing.T, runner CommandRunner, claude ClaudeRunner) {
	t.Helper()
	prevRunner, prevClaude := defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner
	t.Cleanup(func() {
		defaultSuperpowersCommandRunner = prevRunner
		defaultSuperpowersClaudeRunner = prevClaude
	})
	if runner != nil {
		defaultSuperpowersCommandRunner = runner
	}
	if claude != nil {
		defaultSuperpowersClaudeRunner = claude
	}
}

// TestDiscardWorktree_RefusesMainRepoPath proves the hard guard: a run whose
// WorktreePath equals its RepoDir (main repo checkout) must be refused
// outright — FAILURE, zero commands executed — never a silent no-op, since
// discard is destructive (branch deletion).
func TestDiscardWorktree_RefusesMainRepoPath(t *testing.T) {
	t.Chdir(t.TempDir())
	runner := &debugTestRunner{}
	withSwappedSuperpowersRunners(t, runner, nil)

	repo := t.TempDir()
	run := &SuperpowersRun{
		ID:           "run-discard-main",
		Mode:         SuperpowersModeApply,
		RepoDir:      repo,
		WorktreePath: repo, // same as RepoDir — must refuse
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
	}
	bb := newTestBlackboard()
	setSuperpowersRun(bb, run)

	act := GetAction("DiscardSuperpowersWorktree")
	if act == nil {
		t.Fatal("DiscardSuperpowersWorktree not registered")
	}
	result := act(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if result != -1 {
		t.Fatalf("result = %d, want -1 (refuse)", result)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("expected zero commands executed, got %v", runner.calls)
	}
	if strings.TrimSpace(bb.Result) == "" {
		t.Fatal("expected a refusal message in bb.Result")
	}
}

// TestDiscardWorktree_RefusesEmptyPath is the hard guard's other half: an
// empty WorktreePath must also be refused, not treated as "nothing to do".
func TestDiscardWorktree_RefusesEmptyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	runner := &debugTestRunner{}
	withSwappedSuperpowersRunners(t, runner, nil)

	run := &SuperpowersRun{
		ID:           "run-discard-empty",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: "", // empty — must refuse
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
	}
	bb := newTestBlackboard()
	setSuperpowersRun(bb, run)

	act := GetAction("DiscardSuperpowersWorktree")
	if act == nil {
		t.Fatal("DiscardSuperpowersWorktree not registered")
	}
	result := act(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if result != -1 {
		t.Fatalf("result = %d, want -1 (refuse)", result)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("expected zero commands executed, got %v", runner.calls)
	}
}

// TestDiscardWorktree_RemovesWorktreeAndDeletesBranch proves the happy path:
// a legitimate (non-main-repo) worktree is removed and its branch deleted,
// both from RepoDir.
func TestDiscardWorktree_RemovesWorktreeAndDeletesBranch(t *testing.T) {
	t.Chdir(t.TempDir())
	runner := &debugTestRunner{}
	withSwappedSuperpowersRunners(t, runner, nil)

	run := &SuperpowersRun{
		ID:             "run-discard-ok",
		Mode:           SuperpowersModeApply,
		RepoDir:        t.TempDir(),
		WorktreePath:   t.TempDir(),
		WorktreeBranch: "superpowers/run-discard-ok",
		ArtifactDir:    filepath.Join(t.TempDir(), "artifacts"),
	}
	bb := newTestBlackboard()
	setSuperpowersRun(bb, run)

	act := GetAction("DiscardSuperpowersWorktree")
	if act == nil {
		t.Fatal("DiscardSuperpowersWorktree not registered")
	}
	if result := act(&btcore.BTContext[Blackboard]{Blackboard: bb}); result != 1 {
		t.Fatalf("result = %d, want 1 (success); bb.Result=%s", result, bb.Result)
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, "git worktree remove --force "+run.WorktreePath) {
		t.Fatalf("expected worktree remove call, got:\n%s", joined)
	}
	if !strings.Contains(joined, "git branch -D "+run.WorktreeBranch) {
		t.Fatalf("expected branch delete call, got:\n%s", joined)
	}
	for _, c := range runner.calls {
		if !strings.HasPrefix(c, run.RepoDir+" :: ") {
			t.Fatalf("expected all commands to run from RepoDir, got %v", runner.calls)
		}
	}
}

// TestPushBranchAndCreatePR_InvokesGitPushAndGhPr proves the action pushes
// the worktree branch then opens a PR via `gh pr create --fill`, both from
// the worktree directory, push before create.
func TestPushBranchAndCreatePR_InvokesGitPushAndGhPr(t *testing.T) {
	t.Chdir(t.TempDir())
	runner := &debugTestRunner{}
	withSwappedSuperpowersRunners(t, runner, nil)

	run := &SuperpowersRun{
		ID:             "run-pr",
		Mode:           SuperpowersModeApply,
		RepoDir:        t.TempDir(),
		WorktreePath:   t.TempDir(),
		WorktreeBranch: "superpowers/run-pr",
		ArtifactDir:    filepath.Join(t.TempDir(), "artifacts"),
	}
	bb := newTestBlackboard()
	setSuperpowersRun(bb, run)

	act := GetAction("PushBranchAndCreatePR")
	if act == nil {
		t.Fatal("PushBranchAndCreatePR not registered")
	}
	if result := act(&btcore.BTContext[Blackboard]{Blackboard: bb}); result != 1 {
		t.Fatalf("result = %d, want 1 (success); bb.Result=%s", result, bb.Result)
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, "git push -u origin "+run.WorktreeBranch) {
		t.Fatalf("expected git push call, got:\n%s", joined)
	}
	if !strings.Contains(joined, "gh pr create --fill") {
		t.Fatalf("expected gh pr create --fill call, got:\n%s", joined)
	}
	pushIdx := strings.Index(joined, "git push")
	prIdx := strings.Index(joined, "gh pr create")
	if pushIdx < 0 || prIdx < 0 || pushIdx >= prIdx {
		t.Fatalf("expected push before PR create, got:\n%s", joined)
	}
	for _, c := range runner.calls {
		if !strings.HasPrefix(c, run.WorktreePath+" :: ") {
			t.Fatalf("expected all commands to run in the worktree dir, got %v", runner.calls)
		}
	}
}

// TestDebugPhases_WriteEvidenceFiles drives DebugRootCauseInvestigation then
// DebugPatternAnalysis against a fake Claude output, proving each phase
// writes its own debug-phase-N.md evidence file under run.ArtifactDir and
// that ChainState["debug_findings"] accumulates (string concat) across
// phases rather than being overwritten.
func TestDebugPhases_WriteEvidenceFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	runner := &debugTestRunner{}
	claude := &debugTestClaude{output: "root cause is X because Y"}
	withSwappedSuperpowersRunners(t, runner, claude)

	run := &SuperpowersRun{
		ID:           "run-debug",
		Task:         "fix the flaky test",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: t.TempDir(),
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
	}
	bb := newTestBlackboard()
	setSuperpowersRun(bb, run)

	act := GetAction("DebugRootCauseInvestigation")
	if act == nil {
		t.Fatal("DebugRootCauseInvestigation not registered")
	}
	if result := act(&btcore.BTContext[Blackboard]{Blackboard: bb}); result != 1 {
		t.Fatalf("result = %d, want 1 (success); bb.Result=%s", result, bb.Result)
	}

	evidencePath := filepath.Join(run.ArtifactDir, "debug-phase-1.md")
	data, err := os.ReadFile(evidencePath)
	if err != nil {
		t.Fatalf("expected evidence file %s: %v", evidencePath, err)
	}
	if !strings.Contains(string(data), "root cause is X because Y") {
		t.Fatalf("evidence file missing Claude output: %s", data)
	}
	findings, _ := bb.ChainState["debug_findings"].(string)
	if !strings.Contains(findings, "root cause is X because Y") {
		t.Fatalf("debug_findings not appended: %q", findings)
	}
	if len(claude.prompts) != 1 {
		t.Fatalf("expected exactly one Claude call, got %d", len(claude.prompts))
	}
	if !strings.Contains(claude.prompts[0], "Phase 1") {
		t.Fatalf("prompt did not identify phase 1: %s", claude.prompts[0])
	}

	// Chain the next phase and confirm findings accumulate via string concat.
	actPattern := GetAction("DebugPatternAnalysis")
	if actPattern == nil {
		t.Fatal("DebugPatternAnalysis not registered")
	}
	if result := actPattern(&btcore.BTContext[Blackboard]{Blackboard: bb}); result != 1 {
		t.Fatalf("DebugPatternAnalysis result = %d, want 1; bb.Result=%s", result, bb.Result)
	}
	evidence2 := filepath.Join(run.ArtifactDir, "debug-phase-2.md")
	if _, err := os.Stat(evidence2); err != nil {
		t.Fatalf("expected phase 2 evidence file: %v", err)
	}
	findings2, _ := bb.ChainState["debug_findings"].(string)
	if len(findings2) <= len(findings) || !strings.HasPrefix(findings2, findings) {
		t.Fatalf("expected debug_findings to grow (concat) after phase 2, before=%q after=%q", findings, findings2)
	}
}

// TestDebugPhase_DryRunSkipsClaudeAndWritesMarker proves the sibling
// dry-run convention: dry-run mode skips Claude entirely and writes a
// dry-run marker evidence file, returning SUCCESS.
func TestDebugPhase_DryRunSkipsClaudeAndWritesMarker(t *testing.T) {
	t.Chdir(t.TempDir())
	claude := &debugTestClaude{output: "should never be produced"}
	withSwappedSuperpowersRunners(t, &debugTestRunner{}, claude)

	run := &SuperpowersRun{
		ID:          "run-debug-dry",
		Mode:        SuperpowersModeDryRun,
		RepoDir:     t.TempDir(),
		ArtifactDir: filepath.Join(t.TempDir(), "artifacts"),
	}
	bb := newTestBlackboard()
	setSuperpowersRun(bb, run)

	act := GetAction("DebugHypothesisTest")
	if act == nil {
		t.Fatal("DebugHypothesisTest not registered")
	}
	if result := act(&btcore.BTContext[Blackboard]{Blackboard: bb}); result != 1 {
		t.Fatalf("result = %d, want 1 (dry-run skip is a sibling-convention success)", result)
	}
	if len(claude.prompts) != 0 {
		t.Fatal("Claude must not be invoked in dry-run mode")
	}
	data, err := os.ReadFile(filepath.Join(run.ArtifactDir, "debug-phase-3.md"))
	if err != nil {
		t.Fatalf("expected dry-run marker file: %v", err)
	}
	if !strings.Contains(strings.ToUpper(string(data)), "DRY RUN") {
		t.Fatalf("expected DRY RUN marker, got: %s", data)
	}
}
