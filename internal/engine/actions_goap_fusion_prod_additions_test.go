package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	btcore "github.com/rvitorper/go-bt/core"
)

// writeGoapFusionCycleReport builds a `## GOAP Fusion Cycle Complete` report
// whose Analysis path exists on disk (so the os.Stat guard passes) and whose
// Verification block carries the supplied verify/graphify lines. It mirrors the
// shape ReportFusionCycle emits in production, including the normalized
// `Build/tests: DELEGATED ...` line that ReportFusionCycle appends whenever the
// verify_result carries VerifyGoapBuild's bare-repo delegation marker.
func writeGoapFusionCycleReport(t *testing.T, verify, graphify string) string {
	t.Helper()
	analysisPath := filepath.Join(t.TempDir(), "analysis.md")
	if err := os.WriteFile(analysisPath, []byte("# analysis\n"), 0o644); err != nil {
		t.Fatalf("write analysis artifact: %v", err)
	}
	report := fmt.Sprintf("## GOAP Fusion Cycle Complete\n\nAnalysis: `%s`\n\nVerification:\n```\n%s\n%s\n```", analysisPath, verify, graphify)
	if strings.Contains(verify, "delegated to apply-stage worktree verification") {
		report += "\n\nBuild/tests: DELEGATED (bare main repo, verified in apply worktree)"
	}
	return report
}

// TestVerifyGoapFusionEvidenceAcceptsDelegatedVerification mirrors
// TestVerifyGoapBuildDelegatesOnBareRepo at the evidence-gate layer: on the
// bare main repo VerifyGoapBuild passes through with the delegation note
// "delegated to apply-stage worktree verification" instead of the two build/
// test PASSED strings. The deterministic evidence gate must accept that
// delegation marker as valid build/test evidence — otherwise every
// ScheduledAnalysisPath cycle dead-letters after a successful apply.
func TestVerifyGoapFusionEvidenceAcceptsDelegatedVerification(t *testing.T) {
	fn := GetAction("VerifyGoapFusionEvidence")
	if fn == nil {
		t.Fatal("VerifyGoapFusionEvidence not registered")
	}
	report := writeGoapFusionCycleReport(t,
		"delegated to apply-stage worktree verification (bare main repo)",
		"graphify update .: PASSED")
	bb := &Blackboard{Result: report}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code != 1 {
		t.Fatalf("VerifyGoapFusionEvidence with delegated verification = %d, want 1; result: %s",
			code, bb.Result[:min(len(bb.Result), 400)])
	}
}

// TestReportFusionCycleAppendsNormalizedDelegationLine pins the new contract:
// when goap_fusion_verify_result carries VerifyGoapBuild's bare-repo delegation
// marker, ReportFusionCycle must append an explicit, self-describing
// `Build/tests: DELEGATED (bare main repo, verified in apply worktree)` line to
// the Verification block instead of leaving the raw internal note as the only
// evidence. This makes the report self-describing and gives GOAL1's gate a
// stable token to key on.
func TestReportFusionCycleAppendsNormalizedDelegationLine(t *testing.T) {
	fn := GetAction("ReportFusionCycle")
	if fn == nil {
		t.Fatal("ReportFusionCycle not registered")
	}
	bb := &Blackboard{ChainState: map[string]any{
		"goap_fusion_fusion_analysis_path":   "/tmp/analysis.md",
		"goap_fusion_verify_result":          "delegated to apply-stage worktree verification (bare main repo)",
		"goap_fusion_graphify_update_result": "graphify update .: PASSED",
	}}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code != 1 {
		t.Fatalf("ReportFusionCycle = %d, want 1", code)
	}
	want := "Build/tests: DELEGATED (bare main repo, verified in apply worktree)"
	if !strings.Contains(bb.Result, want) {
		t.Fatalf("ReportFusionCycle output missing normalized delegation line %q; got:\n%s", want, bb.Result)
	}
}

// TestReportFusionCycleOmitsDelegationLineWhenGenuinelyVerified is the negative
// side: a cycle that produced real build/test PASSED evidence (no delegation
// marker) must NOT gain a spurious DELEGATED line.
func TestReportFusionCycleOmitsDelegationLineWhenGenuinelyVerified(t *testing.T) {
	fn := GetAction("ReportFusionCycle")
	if fn == nil {
		t.Fatal("ReportFusionCycle not registered")
	}
	bb := &Blackboard{ChainState: map[string]any{
		"goap_fusion_fusion_analysis_path":   "/tmp/analysis.md",
		"goap_fusion_verify_result":          "go build ./cmd/bt-agent ./cmd/bt-agent-cli: PASSED\nfocused go tests: PASSED",
		"goap_fusion_graphify_update_result": "graphify update .: PASSED",
	}}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code != 1 {
		t.Fatalf("ReportFusionCycle = %d, want 1", code)
	}
	if strings.Contains(bb.Result, "Build/tests: DELEGATED") {
		t.Fatalf("ReportFusionCycle appended a spurious DELEGATED line for a genuinely verified cycle; got:\n%s", bb.Result)
	}
}

// TestVerifyGoapFusionEvidenceKeysOnNormalizedDelegationToken proves the
// decoupling: the evidence gate must accept a report whose Verification block
// carries the normalized `Build/tests: DELEGATED ...` token even when
// VerifyGoapBuild's raw internal note has been reworded away. Keying on the
// normalized token (not the exact wording of VerifyGoapBuild's note) means a
// future reword of one doesn't silently re-break the other.
func TestVerifyGoapFusionEvidenceKeysOnNormalizedDelegationToken(t *testing.T) {
	fn := GetAction("VerifyGoapFusionEvidence")
	if fn == nil {
		t.Fatal("VerifyGoapFusionEvidence not registered")
	}
	// The verify line here deliberately does NOT contain the legacy
	// "delegated to apply-stage worktree verification" wording — only the
	// normalized token appears, standing in for a future reword of
	// VerifyGoapBuild's internal note.
	report := writeGoapFusionCycleReport(t,
		"Build/tests: DELEGATED (bare main repo, verified in apply worktree)\n(verification handed to the apply-stage worktree)",
		"graphify update .: PASSED")
	if strings.Contains(report, "delegated to apply-stage worktree verification") {
		t.Fatalf("test setup leaked the legacy marker; report:\n%s", report)
	}
	bb := &Blackboard{Result: report}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code != 1 {
		t.Fatalf("VerifyGoapFusionEvidence keyed on normalized token = %d, want 1; result: %s",
			code, bb.Result[:min(len(bb.Result), 400)])
	}
}

// TestVerifyGoapFusionEvidenceRejectsBogusVerification pins the negative side of
// the either/or check: a `## GOAP Fusion Cycle Complete` report whose
// Verification block carries neither the two build/test PASSED strings nor the
// delegation marker must still fail the gate.
func TestVerifyGoapFusionEvidenceRejectsBogusVerification(t *testing.T) {
	fn := GetAction("VerifyGoapFusionEvidence")
	if fn == nil {
		t.Fatal("VerifyGoapFusionEvidence not registered")
	}
	report := writeGoapFusionCycleReport(t,
		"nothing was actually verified here",
		"graphify update .: PASSED")
	bb := &Blackboard{Result: report}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code == 1 {
		t.Fatalf("VerifyGoapFusionEvidence with bogus verification = 1, want failure (-1)")
	}
	if !strings.Contains(bb.Result, "GOAP Fusion Evidence Failed") {
		t.Fatalf("expected evidence-failed report, got: %s", bb.Result[:min(len(bb.Result), 400)])
	}
}

// TestVerifyGoapFusionEvidenceAcceptsCommittedPROpened pins the fix for a false
// negative in the evidence gate for fleet-PR successes: ApplyStatus
// "committed_pr_opened" (set by pushLandingMasterToOrigin when a bare-repo
// landing pushes to a fleet PR branch instead of main) must be treated as a
// completed run by both ReportSuperpowersImplementation (which currently only
// emits the "## Superpowers Implementation Complete" heading for a fixed list
// of statuses that omits committed_pr_opened) and VerifyGoapFusionEvidence's
// Complete-branch check (which currently only accepts the exact string
// "Apply status: `committed`" — a false negative for
// "Apply status: `committed_pr_opened`", since there is no backtick directly
// after "committed").
func TestVerifyGoapFusionEvidenceAcceptsCommittedPROpened(t *testing.T) {
	reportFn := GetAction("ReportSuperpowersImplementation")
	if reportFn == nil {
		t.Fatal("ReportSuperpowersImplementation not registered")
	}
	verifyFn := GetAction("VerifyGoapFusionEvidence")
	if verifyFn == nil {
		t.Fatal("VerifyGoapFusionEvidence not registered")
	}

	artifactDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(artifactDir, "run.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write run.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "finish.md"), []byte("# finish\n"), 0o644); err != nil {
		t.Fatalf("write finish.md: %v", err)
	}

	run := &SuperpowersRun{
		ID:          "20260723T000000",
		ArtifactDir: artifactDir,
		ApplyStatus: "committed_pr_opened",
	}
	bb := &Blackboard{ChainState: map[string]any{}}
	setSuperpowersRun(bb, run)

	if code := reportFn(btcore.NewBTContext(context.Background(), bb)); code != 1 {
		t.Fatalf("ReportSuperpowersImplementation = %d, want 1", code)
	}
	if !strings.Contains(bb.Result, "## Superpowers Implementation Complete") {
		t.Fatalf("ReportSuperpowersImplementation with ApplyStatus=committed_pr_opened did not emit the Complete heading; got:\n%s", bb.Result)
	}

	code := verifyFn(btcore.NewBTContext(context.Background(), bb))
	if code != 1 {
		t.Fatalf("VerifyGoapFusionEvidence for committed_pr_opened = %d, want 1; result: %s",
			code, bb.Result[:min(len(bb.Result), 400)])
	}
	if !bb.QualityAuthoritative {
		t.Fatal("VerifyGoapFusionEvidence did not set QualityAuthoritative for committed_pr_opened")
	}
}

// reportFusionCycleBaseChainState returns the minimal ChainState ReportFusionCycle
// needs for a genuinely-verified, non-degraded cycle, shared by the
// materializer-wipe and parked-branch visibility tests below.
func reportFusionCycleBaseChainState() map[string]any {
	return map[string]any{
		"goap_fusion_fusion_analysis_path":   "/tmp/analysis.md",
		"goap_fusion_verify_result":          "go build ./cmd/bt-agent ./cmd/bt-agent-cli: PASSED\nfocused go tests: PASSED",
		"goap_fusion_graphify_update_result": "graphify update .: PASSED",
	}
}

// TestReportFusionCycleListsMaterializerSnapshotsFilenameSizeAndChangedCount pins
// milestone 2/5 of the non-destructive materializer program: every
// materializer-snapshot patch (written by writeGoapFusionMaterializerSnapshot to
// ~/.go-bt-evolve/materializer-snapshots before a bare-repo wipe) must be
// surfaced in the cycle report with its filename, byte size, and changed-file
// count — otherwise a silent wipe on the bare main repo leaves no trace in
// cycle history or on the dashboard.
func TestReportFusionCycleListsMaterializerSnapshotsFilenameSizeAndChangedCount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	snapDir := filepath.Join(home, ".go-bt-evolve", "materializer-snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatalf("mkdir snapshot dir: %v", err)
	}
	patch := "diff --git a/foo.go b/foo.go\n" +
		"index 1111111..2222222 100644\n" +
		"--- a/foo.go\n" +
		"+++ b/foo.go\n" +
		"@@ -1 +1 @@\n" +
		"-old\n" +
		"+new\n" +
		"diff --git a/bar.go b/bar.go\n" +
		"index 3333333..4444444 100644\n" +
		"--- a/bar.go\n" +
		"+++ b/bar.go\n" +
		"@@ -1 +1 @@\n" +
		"-old2\n" +
		"+new2\n"
	name := "20260716T120000.patch"
	if err := os.WriteFile(filepath.Join(snapDir, name), []byte(patch), 0o644); err != nil {
		t.Fatalf("write snapshot patch: %v", err)
	}

	fn := GetAction("ReportFusionCycle")
	if fn == nil {
		t.Fatal("ReportFusionCycle not registered")
	}
	bb := &Blackboard{ChainState: reportFusionCycleBaseChainState()}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code != 1 {
		t.Fatalf("ReportFusionCycle = %d, want 1", code)
	}

	if !strings.Contains(bb.Result, "Materializer Snapshot") {
		t.Fatalf("ReportFusionCycle report missing a Materializer Snapshots section; got:\n%s", bb.Result)
	}
	if !strings.Contains(bb.Result, name) {
		t.Fatalf("ReportFusionCycle report missing snapshot filename %q; got:\n%s", name, bb.Result)
	}
	wantSize := fmt.Sprintf("%d bytes", len(patch))
	if !strings.Contains(bb.Result, wantSize) {
		t.Fatalf("ReportFusionCycle report missing snapshot byte size %q; got:\n%s", wantSize, bb.Result)
	}
	if !strings.Contains(bb.Result, "2 file") {
		t.Fatalf("ReportFusionCycle report missing snapshot changed-file count (want a %q substring); got:\n%s", "2 file", bb.Result)
	}
}

// TestReportFusionCycleMaterializerSnapshotsOmitPriorlyReportedOnNextCycle pins
// the "since the prior cycle" half of the contract: a materializer snapshot
// already surfaced in one cycle's report must not be repeated forever in every
// later cycle's report once no new snapshot has been written. Two Blackboards
// sharing the same durable agent-scope manager stand in for two successive
// scheduled ticks (each RunOnce tick builds a fresh Blackboard/ChainState, but
// the agent-scope store persists across ticks).
func TestReportFusionCycleMaterializerSnapshotsOmitPriorlyReportedOnNextCycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	snapDir := filepath.Join(home, ".go-bt-evolve", "materializer-snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatalf("mkdir snapshot dir: %v", err)
	}
	name := "20260716T120000.patch"
	patch := "diff --git a/foo.go b/foo.go\nindex 1..2 100644\n--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n-old\n+new\n"
	if err := os.WriteFile(filepath.Join(snapDir, name), []byte(patch), 0o644); err != nil {
		t.Fatalf("write snapshot patch: %v", err)
	}

	fn := GetAction("ReportFusionCycle")
	if fn == nil {
		t.Fatal("ReportFusionCycle not registered")
	}
	mgr := blackboard.NewManager(nil)
	newTickBlackboard := func() *Blackboard {
		return &Blackboard{
			BB:         blackboard.NewHandle(mgr, "run-x", "", "goap-loop"),
			ChainState: reportFusionCycleBaseChainState(),
		}
	}

	first := newTickBlackboard()
	if code := fn(btcore.NewBTContext(context.Background(), first)); code != 1 {
		t.Fatalf("first ReportFusionCycle = %d, want 1", code)
	}
	if !strings.Contains(first.Result, name) {
		t.Fatalf("first cycle report missing newly-written snapshot %q; got:\n%s", name, first.Result)
	}

	second := newTickBlackboard()
	if code := fn(btcore.NewBTContext(context.Background(), second)); code != 1 {
		t.Fatalf("second ReportFusionCycle = %d, want 1", code)
	}
	if strings.Contains(second.Result, name) {
		t.Fatalf("second cycle re-reported an already-seen snapshot %q; got:\n%s", name, second.Result)
	}
}

// TestReportFusionCycleListsAgedUnmergedSuperpowersBranchesAsPendingPatch pins
// the parked-run-triage half of milestone 2/5: every superpowers/* branch that
// is (a) not merged into master and (b) whose tip commit is older than 24h
// must appear in the cycle report tagged pending_patch — a branch survives
// past reapOrphanedSuperpowersBranches' `git branch -d` sweep only when it is
// genuinely unmerged, which is exactly the parked-run triage backlog this
// milestone must make visible. A fresh (<24h) unmerged branch, and a branch
// that has already landed (merged), must NOT appear.
func TestReportFusionCycleListsAgedUnmergedSuperpowersBranchesAsPendingPatch(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runInDir(t, dir, "git add -A && git commit -qm base")

	oldDate := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	freshDate := time.Now().UTC().Format(time.RFC3339)

	// Old and unmerged: must be reported as pending_patch.
	runInDir(t, dir, "git checkout -q -b superpowers/old-parked")
	if err := os.WriteFile(filepath.Join(dir, "old.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runInDir(t, dir, fmt.Sprintf(`git add -A && GIT_AUTHOR_DATE=%q GIT_COMMITTER_DATE=%q git commit -qm old-parked`, oldDate, oldDate))
	runInDir(t, dir, "git checkout -q master")

	// Fresh and unmerged: too young to surface yet.
	runInDir(t, dir, "git checkout -q -b superpowers/fresh-parked")
	if err := os.WriteFile(filepath.Join(dir, "fresh.txt"), []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runInDir(t, dir, fmt.Sprintf(`git add -A && GIT_AUTHOR_DATE=%q GIT_COMMITTER_DATE=%q git commit -qm fresh-parked`, freshDate, freshDate))
	runInDir(t, dir, "git checkout -q master")

	// Old but already merged: already landed, must not be reported as parked.
	runInDir(t, dir, "git checkout -q -b superpowers/old-merged")
	if err := os.WriteFile(filepath.Join(dir, "merged.txt"), []byte("merged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runInDir(t, dir, fmt.Sprintf(`git add -A && GIT_AUTHOR_DATE=%q GIT_COMMITTER_DATE=%q git commit -qm old-merged`, oldDate, oldDate))
	runInDir(t, dir, "git checkout -q master")
	runInDir(t, dir, "git merge -q --no-ff -m merge-it superpowers/old-merged")

	prev := goapFusionRepo
	goapFusionRepo = dir
	t.Cleanup(func() { goapFusionRepo = prev })

	fn := GetAction("ReportFusionCycle")
	if fn == nil {
		t.Fatal("ReportFusionCycle not registered")
	}
	bb := &Blackboard{ChainState: reportFusionCycleBaseChainState()}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code != 1 {
		t.Fatalf("ReportFusionCycle = %d, want 1", code)
	}

	if !strings.Contains(bb.Result, "superpowers/old-parked") {
		t.Fatalf("report missing aged unmerged branch superpowers/old-parked; got:\n%s", bb.Result)
	}
	if !strings.Contains(bb.Result, "pending_patch") {
		t.Fatalf("report missing pending_patch status for the parked branch; got:\n%s", bb.Result)
	}
	if strings.Contains(bb.Result, "superpowers/fresh-parked") {
		t.Fatalf("report must not list a branch younger than 24h; got:\n%s", bb.Result)
	}
	if strings.Contains(bb.Result, "superpowers/old-merged") {
		t.Fatalf("report must not list an already-merged branch; got:\n%s", bb.Result)
	}
}
