package gardener

// idle_gate.go — don't evolve when there is nothing new to learn from.
//
// 2026-08-01, from ~/.go-bt-gardener/gardener-metrics.json: total_cycles 9829,
// total_improvements 0, total_rollbacks 0, and every per-tree record carrying
// mutations_applied 0 with 1-2 rejections. The acceptance path is CORRECT —
// candidates are rejected as no-ops (ApplyMutations returned 0) or as
// regressions against composite fitness values of 46-88, all far above the
// quality floor. The population is converged: RunCycleV2 evaluates candidates
// against reflection records, so further gains need FRESH records, and those
// only arrive when agents actually execute.
//
// Until then a cycle every ~5 minutes (247/day observed) is pure CPU burn on a
// Jetson, competing with the very goap cycles that produce the reflection data
// evolution is waiting for. The gate skips a cycle when no new reflection data
// has landed since the last one — and forces one anyway after ForceAfter, so a
// stalled reflection writer can never silently freeze evolution.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// IdleGate decides whether an evolution cycle is worth running.
type IdleGate struct {
	// Dir is the reflection store directory (~/.go-bt-reflections).
	Dir string
	// ForceAfter runs a cycle even with unchanged data once this long has
	// passed since the last run. Zero disables the safety valve.
	ForceAfter time.Duration

	watermark string
	lastRun   time.Time
	started   bool
}

// ShouldRunCycle reports whether to run now, recording the decision's state when
// it answers yes. The reason is operator-facing and always non-empty.
func (g *IdleGate) ShouldRunCycle(now time.Time) (bool, string) {
	current := reflectionWatermark(g.Dir)

	if !g.started {
		g.started, g.watermark, g.lastRun = true, current, now
		return true, "first cycle since start"
	}
	if current != g.watermark {
		g.watermark, g.lastRun = current, now
		return true, "new reflection data since the last cycle"
	}
	if idle := now.Sub(g.lastRun); g.ForceAfter > 0 && idle >= g.ForceAfter {
		// Compute the idle span BEFORE moving lastRun, or the message always
		// reads "0s".
		g.lastRun = now
		return true, fmt.Sprintf("no new reflection data for %s — forcing a cycle so a stalled reflection writer cannot freeze evolution", idle.Truncate(time.Minute))
	}
	return false, fmt.Sprintf("no new reflection data since %s — nothing for evolution to learn from", g.lastRun.Format(time.RFC3339))
}

// reflectionWatermark is a cheap value that changes whenever the reflection
// store does: every file's name, size and modification time. An unreadable or
// absent directory yields a stable empty watermark — "no new data", never a
// spurious change that would defeat the gate on every tick.
func reflectionWatermark(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var parts []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%d:%d", filepath.Base(e.Name()), info.Size(), info.ModTime().UnixNano()))
	}
	sort.Strings(parts) // ReadDir order is not guaranteed stable across platforms
	return fmt.Sprint(parts)
}
