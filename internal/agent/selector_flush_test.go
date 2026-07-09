package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// RunOnce must flush the run's Selector-attributed child ticks into the
// per-tree durable telemetry — the writer the 2026-07-09 selector-ordering
// landings shipped without. The probe tree's Selector child is an
// AlwaysSucceed leaf (registry-free, side-effect-free), so the run yields one
// success tick under "FlushRouter" that must land on disk.
func TestRunOnceFlushesSelectorTelemetry(t *testing.T) {
	prevDir := selectorStatsDir
	selectorStatsDir = t.TempDir()
	t.Cleanup(func() { selectorStatsDir = prevDir })

	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "FlushRoot",
		Children: []evolution.SerializableNode{
			{
				Type: "Selector", Name: "FlushRouter",
				Children: []evolution.SerializableNode{
					{Type: "AlwaysSucceed", Name: "FlushChildA"},
				},
			},
		},
	}
	deps := &RunDeps{ResolveTree: func(id string) *evolution.SerializableNode {
		if id == "tree:flush-probe" {
			return tree
		}
		return nil
	}}

	if _, err := deps.RunOnce(context.Background(), "tree:flush-probe", "probe task", RunOptions{DisableBlackboard: true}); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	so := evolution.NewSelectorOptimizer(evolution.OrderBySuccessRate)
	if err := so.LoadSelectorStats(SelectorStatsFile("tree:flush-probe")); err != nil {
		t.Fatalf("load telemetry: %v", err)
	}
	rs := so.Stats["FlushRouter"]
	if rs == nil || rs.Children["FlushChildA"] == nil || rs.Children["FlushChildA"].Successes != 1 {
		t.Fatalf("selector telemetry not flushed for FlushRouter/FlushChildA: %+v", so.Stats)
	}
	// Only Selector parents belong in selector telemetry: the Sequence root's
	// child tick (FlushRoot → FlushRouter) must have been filtered out.
	if _, polluted := so.Stats["FlushRoot"]; polluted {
		t.Fatalf("non-Selector parent leaked into selector telemetry: %+v", so.Stats)
	}
}

// Per-tree stats files must never carry path-hostile characters from tree ids.
func TestSelectorStatsFileSanitizesTreeID(t *testing.T) {
	prevDir := selectorStatsDir
	selectorStatsDir = "/stats-root"
	t.Cleanup(func() { selectorStatsDir = prevDir })

	got := SelectorStatsFile("domain:goap/fusion")
	if got != "/stats-root/domain_goap_fusion.json" {
		t.Fatalf("SelectorStatsFile = %q, want /stats-root/domain_goap_fusion.json", got)
	}
	if strings.ContainsAny(got[len("/stats-root/"):], ":") {
		t.Fatalf("unsanitized id in %q", got)
	}
}
