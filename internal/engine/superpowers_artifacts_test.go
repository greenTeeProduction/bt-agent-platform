package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteArtifactOnce_Idempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "plan.md")
	written, err := writeArtifactOnce(p, []byte("one"))
	if err != nil || !written {
		t.Fatalf("first write = %v, %v; want written nil", written, err)
	}
	written, err = writeArtifactOnce(p, []byte("two"))
	if err != nil || written {
		t.Fatalf("second write = %v, %v; want reused nil", written, err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "one" {
		t.Fatalf("artifact overwritten: %q", got)
	}
}

func TestSafeSlug(t *testing.T) {
	if got := safeSlug("Hello, BT Fusion!!!"); got != "hello-bt-fusion" {
		t.Fatalf("safeSlug = %q", got)
	}
}

func TestSuperpowersPlanAttemptSaturatedInDir(t *testing.T) {
	dir := t.TempDir()
	task := "repeat me"
	suffix := superpowersTaskHashSuffix(task)
	for _, name := range []string{"20260625T050617-" + suffix, "20260625T053048-" + suffix} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	saturated, matches := superpowersPlanAttemptSaturatedInDir(dir, task, 2)
	if !saturated {
		t.Fatalf("expected task hash %s to be saturated; matches=%v", suffix, matches)
	}
	if len(matches) != 2 {
		t.Fatalf("matches = %d, want 2 (%v)", len(matches), matches)
	}

	saturated, _ = superpowersPlanAttemptSaturatedInDir(dir, "different task", 2)
	if saturated {
		t.Fatal("different task should not be saturated")
	}
}

// isolateSuperpowersRunsDir points superpowersRunsDir at a private temp
// directory. Tests that create runs (currentSuperpowersRun), scan attempt
// saturation via the package default, or execute actions embedding the
// pending-patch recovery / orphaned-branch reap passes MUST call this
// (mirroring isolateClaudeBackoffStore / isolateGoapProgramStore): the default
// is the operator's REAL docs/superpowers/runs — without isolation, tests scan
// hundreds of live run artifacts (inflating parked/force-reap/abandonment
// telemetry 4–1000×, see the 2026-07-23 fleet review) and can even feed live
// pending-patch runs into recovery git operations.
func isolateSuperpowersRunsDir(t *testing.T) string {
	t.Helper()
	prev := superpowersRunsDir
	dir := t.TempDir()
	superpowersRunsDir = dir
	t.Cleanup(func() { superpowersRunsDir = prev })
	return dir
}

// TestSuperpowersRunsDir_OverrideScopesRunCreationAndSaturationScan pins the
// runs-directory seam: the package default must be overridable so the whole
// engine test binary (TestMain) and individual tests can scope run-artifact
// reads and writes away from the operator's live docs/superpowers/runs.
func TestSuperpowersRunsDir_OverrideScopesRunCreationAndSaturationScan(t *testing.T) {
	dir := isolateSuperpowersRunsDir(t)

	bb := &Blackboard{Task: "runs-dir seam probe task"}
	run, err := currentSuperpowersRun(bb)
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Dir(run.ArtifactDir); got != dir {
		t.Fatalf("new run rooted at %q, want under override %q", run.ArtifactDir, dir)
	}

	// The package-default saturation scan must flow through the same
	// override: plant saturating sibling run dirs for the task and expect the
	// default-path scan to see them.
	suffix := superpowersTaskHashSuffix(bb.Task)
	for _, name := range []string{"20260723T000001-" + suffix, "20260723T000002-" + suffix} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	saturated, matches := superpowersPlanAttemptSaturated(bb.Task, 2)
	if !saturated || len(matches) != 2 {
		t.Fatalf("default-path saturation scan did not observe planted run dirs under the override (saturated=%v matches=%v)", saturated, matches)
	}
}
