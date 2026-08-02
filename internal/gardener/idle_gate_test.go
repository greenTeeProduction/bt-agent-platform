package gardener

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 2026-08-01: gardener-metrics.json showed total_cycles 9829, total_improvements
// 0, total_rollbacks 0, and every per-tree record with mutations_applied 0 and
// 1-2 rejections. The acceptance path is CORRECT — candidates are rejected as
// no-ops or regressions against fitness values (46-88) well above the quality
// floor. The population is simply converged: further gains need fresh
// reflection records, which only arrive when agents actually execute.
//
// So 247 cycles/day on a Jetson were pure CPU burn competing with the goap
// cycles that produce the reflection data evolution is waiting for. The gate
// skips a cycle when no new reflection data has arrived since the last one,
// with a forced run after ForceAfter so a stalled reflection writer can never
// freeze evolution permanently.

func writeReflection(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(`{"tree":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIdleGate_FirstCycleAlwaysRuns(t *testing.T) {
	dir := t.TempDir()
	writeReflection(t, dir, "tree-a.json")
	g := &IdleGate{Dir: dir, ForceAfter: time.Hour}

	if run, _ := g.ShouldRunCycle(time.Now()); !run {
		t.Fatal("the first cycle must always run — there is no prior watermark to compare against")
	}
}

func TestIdleGate_SkipsWhenNoNewReflectionData(t *testing.T) {
	dir := t.TempDir()
	writeReflection(t, dir, "tree-a.json")
	now := time.Now()
	g := &IdleGate{Dir: dir, ForceAfter: time.Hour}

	if run, _ := g.ShouldRunCycle(now); !run {
		t.Fatal("first cycle must run")
	}
	run, reason := g.ShouldRunCycle(now.Add(5 * time.Minute))
	if run {
		t.Fatal("a cycle with no new reflection data since the last one must be skipped: " +
			"evolution has nothing new to learn from and the CPU is needed by the agents that produce it")
	}
	if reason == "" {
		t.Fatal("a skip must carry a reason an operator can read")
	}
}

func TestIdleGate_RunsAgainWhenNewReflectionDataArrives(t *testing.T) {
	dir := t.TempDir()
	writeReflection(t, dir, "tree-a.json")
	now := time.Now()
	g := &IdleGate{Dir: dir, ForceAfter: time.Hour}
	g.ShouldRunCycle(now)

	writeReflection(t, dir, "tree-b.json")

	if run, _ := g.ShouldRunCycle(now.Add(5 * time.Minute)); !run {
		t.Fatal("new reflection data must re-open the gate — that is the whole signal evolution waits for")
	}
}

// Safety valve: a stalled reflection writer must not freeze evolution forever.
func TestIdleGate_ForcesACycleAfterTheIdleCeiling(t *testing.T) {
	dir := t.TempDir()
	writeReflection(t, dir, "tree-a.json")
	now := time.Now()
	g := &IdleGate{Dir: dir, ForceAfter: 6 * time.Hour}
	g.ShouldRunCycle(now)

	if run, _ := g.ShouldRunCycle(now.Add(5 * time.Hour)); run {
		t.Fatal("still inside the ceiling with unchanged data — must skip")
	}
	run, reason := g.ShouldRunCycle(now.Add(7 * time.Hour))
	if !run {
		t.Fatal("past ForceAfter the gate must run anyway, so a stalled reflection writer " +
			"cannot silently freeze evolution")
	}
	if reason == "" {
		t.Fatal("the forced run must say why")
	}
}

// An unreadable/missing reflection dir must not wedge the gate shut.
func TestIdleGate_UnreadableDirStillRuns(t *testing.T) {
	g := &IdleGate{Dir: filepath.Join(t.TempDir(), "does-not-exist"), ForceAfter: time.Hour}
	now := time.Now()
	if run, _ := g.ShouldRunCycle(now); !run {
		t.Fatal("first cycle must run")
	}
	if run, _ := g.ShouldRunCycle(now.Add(time.Minute)); run {
		t.Fatal("a stable (even if absent) reflection dir is still 'no new data'")
	}
}
