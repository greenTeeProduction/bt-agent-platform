package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/research"

	btcore "github.com/rvitorper/go-bt/core"
)

// --- Finding 1: resume must not re-execute an already-implemented plan ---

func TestResumeSkipsAlreadyImplementedPlan(t *testing.T) {
	kpath := filepath.Join(t.TempDir(), "knowledge.json")
	old := btFusionKnowledgePath
	btFusionKnowledgePath = kpath
	t.Cleanup(func() { btFusionKnowledgePath = old })

	plan := buildDeterministicImplementationPlan("some already-landed change")
	tasks, err := ParseSuperpowersPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	store, _ := research.Open(kpath)
	for _, task := range tasks {
		store.Record("goap:implemented", task.Title, task.Objective)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	bb := &Blackboard{Task: "improve", ChainState: map[string]any{
		"goap_fusion_superpowers_plan_path":   "/tmp/some-plan.md",
		"goap_fusion_superpowers_active_plan": plan,
	}}
	fn := GetAction("RunScheduledGoapFusionCycle")
	if got := fn(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("resume of a fully implemented plan must no-op succeed, got %d: %s", got, bb.Result)
	}
	if !strings.Contains(bb.Result, "already implemented") {
		t.Fatalf("result must say the plan was already implemented: %s", bb.Result)
	}
	if p, _ := bb.ChainState["goap_fusion_superpowers_plan_path"].(string); p != "" {
		t.Fatal("stale plan state must be cleared so the cycle re-plans from fresh research")
	}
}

func TestSuperpowersPlanAlreadyImplementedRequiresAllTasks(t *testing.T) {
	kpath := filepath.Join(t.TempDir(), "knowledge.json")
	old := btFusionKnowledgePath
	btFusionKnowledgePath = kpath
	t.Cleanup(func() { btFusionKnowledgePath = old })

	plan := buildDeterministicImplementationPlan("half-landed change")
	tasks, _ := ParseSuperpowersPlan(plan)
	store, _ := research.Open(kpath)
	store.Record("goap:implemented", tasks[0].Title, tasks[0].Objective)
	_ = store.Save()
	// Single-task legacy plan: with its only task recorded, it counts as done.
	if !superpowersPlanAlreadyImplemented(plan) {
		t.Fatal("plan whose every task objective is recorded must count as implemented")
	}
	if superpowersPlanAlreadyImplemented("### Task 1: brand new\n\n**Objective:** something never recorded\n\n**Files:**\n- Modify: a.go\n\nRun: go test ./...\nRED GREEN\n") {
		t.Fatal("plan with unrecorded objectives must not count as implemented")
	}
}

// --- Finding 2: lint parity — verification must include the hook's linter ---

func TestChangedPackagesLintCommandMirrorsHookGate(t *testing.T) {
	oldBin := superpowersLintBin
	superpowersLintBin = "/usr/bin/golangci-lint-stub"
	t.Cleanup(func() { superpowersLintBin = oldBin })

	cmd := changedPackagesLintCommand([]string{"internal/engine/a.go", "docs/x.md"})
	if !strings.Contains(cmd, "golangci-lint-stub run") || !strings.Contains(cmd, "./internal/engine/...") {
		t.Fatalf("lint command must target changed packages: %q", cmd)
	}
	if changedPackagesLintCommand([]string{"docs/x.md"}) != "" {
		t.Fatal("no Go changes must skip the lint check")
	}
	superpowersLintBin = ""
	if changedPackagesLintCommand([]string{"internal/engine/a.go"}) != "" {
		t.Fatal("missing linter binary must skip the check, not fail it")
	}
}

// --- Finding 3b: apply commit failures must leave evidence ---

func TestApplyCommitFailureWritesEvidenceArtifact(t *testing.T) {
	run := bareTestRun(t)
	runner := &bareApplyScriptRunner{
		patch:  "diff --git a/internal/engine/actions_superpowers.go b/x\n",
		failOn: "git commit",
	}
	err := applySuperpowersRunToMainRepo(context.Background(), runner, run)
	if err == nil {
		t.Fatal("commit failure must fail the apply")
	}
	if !strings.Contains(err.Error(), "applied_uncommitted") {
		t.Fatalf("unexpected error: %v", err)
	}
	b, rerr := os.ReadFile(filepath.Join(run.ArtifactDir, "verification", "apply-commit.txt"))
	if rerr != nil {
		t.Fatalf("apply-commit.txt evidence must be written: %v", rerr)
	}
	if !strings.Contains(string(b), "forced failure") {
		t.Fatalf("evidence must carry the git output: %s", b)
	}
}

func TestApplyCommitSkipsHookLintWhenPreverified(t *testing.T) {
	run := bareTestRun(t)
	run.Verification = []VerificationCheck{{Name: "changed-packages-lint", Passed: true}}
	runner := &bareApplyScriptRunner{patch: "diff --git a/internal/engine/actions_superpowers.go b/x\n"}
	if err := applySuperpowersRunToMainRepo(context.Background(), runner, run); err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if !strings.Contains(runner.joined(), "BT_SUPERPOWERS_PREVERIFIED=1 git commit") {
		t.Fatalf("lint-preverified runs must commit with the hook-skip flag; calls:\n%s", runner.joined())
	}
}

func TestApplyCommitKeepsHookLintWithoutPreverification(t *testing.T) {
	run := bareTestRun(t)
	runner := &bareApplyScriptRunner{patch: "diff --git a/internal/engine/actions_superpowers.go b/x\n"}
	if err := applySuperpowersRunToMainRepo(context.Background(), runner, run); err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if strings.Contains(runner.joined(), "BT_SUPERPOWERS_PREVERIFIED") {
		t.Fatalf("without a passed lint check the hook must run its own lint; calls:\n%s", runner.joined())
	}
}

func TestReportSuperpowersImplementationCarriesContinueMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "programs.json")
	oldP := goapProgramsPath
	goapProgramsPath = path
	t.Cleanup(func() { goapProgramsPath = oldP })
	ps, _ := research.OpenPrograms(path)
	ps.Add("Pending program", "test", []string{"Milestone in internal/a2a/a.go"})
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}

	bb := &Blackboard{Task: "improve", ChainState: map[string]any{}}
	setSuperpowersRun(bb, &SuperpowersRun{ID: "run-1", ArtifactDir: t.TempDir(), ApplyStatus: "committed"})
	fn := GetAction("ReportSuperpowersImplementation")
	if got := fn(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("status = %d: %s", got, bb.Result)
	}
	if !strings.Contains(bb.Result, "PROGRAM-CONTINUE") {
		t.Fatalf("the cycle's final result rewrite must carry the continue marker:\n%s", bb.Result)
	}
}
