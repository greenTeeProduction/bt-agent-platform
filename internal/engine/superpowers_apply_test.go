package engine

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	for _, want := range []string{"git apply --check", "git apply ", "go test", "go build", "graphify update .", "git commit -m"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected command containing %q; calls:\n%s", want, joined)
		}
	}
}
