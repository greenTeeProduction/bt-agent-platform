package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	btcore "github.com/rvitorper/go-bt/core"
)

type applyScriptRunner struct {
	t      *testing.T
	calls  []string
	status string
	patch  string
}

func (r *applyScriptRunner) Run(_ context.Context, dir string, name string, args ...string) CommandResult {
	cmd := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.calls = append(r.calls, dir+" :: "+cmd)
	res := CommandResult{Command: cmd, Dir: dir, Duration: time.Millisecond}
	if name == "git" && len(args) >= 1 && args[0] == "status" {
		res.Output = r.status
		return res
	}
	if name == "bash" && len(args) >= 2 && args[0] == "-c" {
		script := args[1]
		switch {
		case strings.Contains(script, "git add -N"):
			return res
		case strings.Contains(script, "git diff --binary"):
			res.Output = r.patch
			return res
		}
	}
	return res
}

func TestHasBlockingMainRepoDirtyIgnoresGeneratedArtifacts(t *testing.T) {
	safe := strings.Join([]string{
		" M graphify-out/GRAPH_REPORT.md",
		" M graphify-out/graph.json",
		"?? docs/superpowers/runs/20260625T083031/run.json",
		"?? docs/superpowers/plans/goap-fusion-20260625T083031.md",
		"",
	}, "\n")
	if hasBlockingMainRepoDirty(safe) {
		t.Fatalf("generated graph/superpowers artifacts should not block apply:\n%s", safe)
	}

	blocking := safe + " M internal/engine/actions_superpowers.go\n"
	if !hasBlockingMainRepoDirty(blocking) {
		t.Fatalf("source edits must block automatic apply:\n%s", blocking)
	}
}

func TestApplySuperpowersRunToMainRepoSavesPendingPatchWhenMainRepoDirty(t *testing.T) {
	artifactDir := filepath.Join(t.TempDir(), "artifacts")
	run := &SuperpowersRun{
		ID:           "run-apply",
		Task:         "implement guard",
		Mode:         SuperpowersModeApply,
		RepoDir:      filepath.Join(t.TempDir(), "repo"),
		WorktreePath: filepath.Join(t.TempDir(), "worktree"),
		ArtifactDir:  artifactDir,
		ChangedFiles: []string{"internal/engine/actions_superpowers.go"},
	}
	runner := &applyScriptRunner{
		t:      t,
		status: " M internal/engine/existing.go\n",
		patch:  "diff --git a/internal/engine/actions_superpowers.go b/internal/engine/actions_superpowers.go\n",
	}

	err := applySuperpowersRunToMainRepo(context.Background(), runner, run)
	if err == nil || !strings.Contains(err.Error(), "pending_patch") {
		t.Fatalf("apply error = %v, want pending_patch", err)
	}
	if run.ApplyStatus != "pending_patch" {
		t.Fatalf("ApplyStatus = %q, want pending_patch", run.ApplyStatus)
	}
	if run.PatchPath == "" {
		t.Fatal("PatchPath not recorded")
	}
	patchBytes, readErr := readFileForTest(run.PatchPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(patchBytes, "diff --git") {
		t.Fatalf("patch artifact missing diff content:\n%s", patchBytes)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "git apply") {
			t.Fatalf("git apply must not run when main repo has blocking dirty files; calls=%v", runner.calls)
		}
	}
}

type cleanApplyScriptRunner struct {
	calls []string
	patch string
}

func (r *cleanApplyScriptRunner) Run(_ context.Context, dir string, name string, args ...string) CommandResult {
	cmd := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.calls = append(r.calls, dir+" :: "+cmd)
	res := CommandResult{Command: cmd, Dir: dir, Duration: time.Millisecond}
	switch {
	case name == "git" && len(args) >= 1 && args[0] == "status":
		return res
	case name == "git" && len(args) >= 1 && args[0] == "apply":
		return res
	case name == "git" && len(args) >= 1 && args[0] == "add":
		return res
	case name == "git" && len(args) >= 1 && args[0] == "commit":
		return res
	case name == "git" && len(args) >= 1 && args[0] == "push":
		return res
	case name == "git" && len(args) >= 2 && args[0] == "rev-parse":
		res.Output = "abc1234\n"
		return res
	case name == "bash" && len(args) >= 2 && args[0] == "-c":
		script := args[1]
		switch {
		case strings.Contains(script, "git add -N"):
			return res
		case strings.Contains(script, "git diff --binary"):
			res.Output = r.patch
			return res
		case strings.Contains(script, "go test") || strings.Contains(script, "go build") || strings.Contains(script, "graphify update"):
			res.Output = "ok\n"
			return res
		case strings.Contains(script, "git diff --cached --quiet"):
			res.Err = errors.New("exit status 1") // staged changes exist
			return res
		}
	}
	return res
}

func TestApplySuperpowersRunToMainRepoAppliesVerifiesGraphifiesAndCommits(t *testing.T) {
	run := &SuperpowersRun{
		ID:           "run-clean-apply",
		Task:         "implement guard",
		Mode:         SuperpowersModeApply,
		RepoDir:      filepath.Join(t.TempDir(), "repo"),
		WorktreePath: filepath.Join(t.TempDir(), "worktree"),
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
		ChangedFiles: []string{"internal/engine/actions_superpowers.go"},
	}
	runner := &cleanApplyScriptRunner{patch: "diff --git a/internal/engine/actions_superpowers.go b/internal/engine/actions_superpowers.go\n"}

	if err := applySuperpowersRunToMainRepo(context.Background(), runner, run); err != nil {
		t.Fatalf("applySuperpowersRunToMainRepo returned error: %v", err)
	}
	if run.ApplyStatus != "committed" {
		t.Fatalf("ApplyStatus = %q, want committed", run.ApplyStatus)
	}
	if run.AppliedCommit != "abc1234" {
		t.Fatalf("AppliedCommit = %q, want abc1234", run.AppliedCommit)
	}
	joined := strings.Join(runner.calls, "\n")
	for _, want := range []string{"git apply --check", "git apply ", "go test", "go build", "graphify update .", "git commit -m", "git push origin master"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected command containing %q; calls:\n%s", want, joined)
		}
	}
}

// TestGitStageArgsExcludesGeneratedSuperpowersAndGraphifyPaths pins Q5
// milestone 1/5: gitStageArgs (the apply-stage landing commit's `git add -A`
// pathspec, superpowers_commit_autofix.go) must exclude every prefix in
// superpowersGeneratedPathPrefixes, exactly like superpowersGeneratedCommitExclusions
// already does for the per-task commit. Today gitStageArgs only excludes
// docs/superpowers/runs/** and docs/superpowers/plans/** — graphify-out/** is
// missing, so the landing commit stages and commits graphify-out's heavy
// regenerated files (55k-line graph.json diffs) even though
// isGeneratedSuperpowersOrGraphifyPath treats graphify-out as generated and
// excludes it from dirty-repo gating and per-task commits. That contradiction
// is exactly what bloated .git to 781MB.
func TestGitStageArgsExcludesGeneratedSuperpowersAndGraphifyPaths(t *testing.T) {
	args := gitStageArgs()
	joined := strings.Join(args, " ")
	for _, prefix := range superpowersGeneratedPathPrefixes {
		want := ":(exclude)" + strings.TrimSuffix(prefix, "/") + "/**"
		if !strings.Contains(joined, want) {
			t.Fatalf("gitStageArgs() = %v missing exclusion %q for generated path prefix %q; the apply-stage landing commit must exclude the same generated paths as isGeneratedSuperpowersOrGraphifyPath/superpowersGeneratedCommitExclusions or they drift apart", args, want, prefix)
		}
	}
}

// TestCleanupGraphifyOutRemovesUntrackedRegeneratedArtifacts pins Q5
// milestone 1/5: once graphify-out/graph.json, manifest.json, and cache/ are
// untracked (git rm --cached) and gitignored, `git checkout -- graphify-out/`
// — CleanupGraphifyOut's only cleanup step today — is a silent no-op against
// them, since checkout only restores tracked paths. A cycle's `graphify
// update .` regenerates these as untracked, gitignored files that then never
// get cleaned up between cycles. CleanupGraphifyOut must be adjusted to also
// remove the untracked regenerated outputs, while leaving the still-tracked
// GRAPH_REPORT.md alone.
func TestCleanupGraphifyOutRemovesUntrackedRegeneratedArtifacts(t *testing.T) {
	// The pre-commit hook exports GIT_DIR/GIT_INDEX_FILE while running tests;
	// inherited here, git commands would silently operate on the OUTER
	// repository instead of this throwaway one. Scrub for the test's duration.
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_PREFIX", "GIT_COMMON_DIR"} {
		if v, ok := os.LookupEnv(k); ok {
			t.Setenv(k, v)
			os.Unsetenv(k)
		}
	}

	dir := t.TempDir()
	runInDir(t, dir, "git init -q . && git config user.email t@t.local && git config user.name t")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("graphify-out/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "graphify-out"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graphify-out", "GRAPH_REPORT.md"), []byte("# report\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// GRAPH_REPORT.md stays tracked (force-added past the blanket ignore rule),
	// mirroring the milestone's "keep GRAPH_REPORT.md tracked" requirement.
	runInDir(t, dir, "git add -A && git add -f graphify-out/GRAPH_REPORT.md && git commit -qm base")

	// Simulate a cycle's `graphify update .` regenerating outputs that are now
	// untracked+gitignored (post milestone-1 git rm --cached).
	if err := os.WriteFile(filepath.Join(dir, "graphify-out", "graph.json"), []byte(`{"nodes":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graphify-out", "manifest.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "graphify-out", "cache", "ast"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graphify-out", "cache", "ast", "x.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	prev := goapFusionRepo
	goapFusionRepo = dir
	t.Cleanup(func() { goapFusionRepo = prev })

	fn := GetAction("CleanupGraphifyOut")
	if fn == nil {
		t.Fatal("missing action CleanupGraphifyOut")
	}
	bb := &Blackboard{Task: "cleanup graphify-out after cycle"}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code != 1 {
		t.Fatalf("CleanupGraphifyOut = %d, want 1", code)
	}

	for _, stale := range []string{
		filepath.Join(dir, "graphify-out", "graph.json"),
		filepath.Join(dir, "graphify-out", "manifest.json"),
		filepath.Join(dir, "graphify-out", "cache", "ast", "x.json"),
	} {
		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Fatalf("expected untracked regenerated graphify artifact %s to be removed by CleanupGraphifyOut now that graph.json/manifest.json/cache are untracked (git checkout -- only restores tracked paths); stat err=%v", stale, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "graphify-out", "GRAPH_REPORT.md")); err != nil {
		t.Fatalf("tracked GRAPH_REPORT.md must survive cleanup: %v", err)
	}
}
