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

// RunOnce must also flush the run's Selector-attributed child ticks into the
// sibling DTAnalyzer telemetry file (Q2 Evolvability milestone 1): the
// entropy/Gini-based decision-tree engine's producer, wired the same way as
// the SelectorOptimizer stats file above. The probe tree's Selector child is
// a Sequence guarded by a Condition node so extractCondition has a real
// condition name to attribute to the path.
func TestRunOnceFlushesDecisionTreeTelemetry(t *testing.T) {
	prevDir := selectorStatsDir
	selectorStatsDir = t.TempDir()
	t.Cleanup(func() { selectorStatsDir = prevDir })

	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "FlushDTRoot",
		Children: []evolution.SerializableNode{
			{
				Type: "Selector", Name: "FlushDTRouter",
				Children: []evolution.SerializableNode{
					{
						Type: "Sequence", Name: "FlushDTChildA",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "CheckConfidence"},
							{Type: "AlwaysSucceed", Name: "FlushDTLeaf"},
						},
					},
				},
			},
		},
	}
	deps := &RunDeps{ResolveTree: func(id string) *evolution.SerializableNode {
		if id == "tree:flush-dt-probe" {
			return tree
		}
		return nil
	}}

	if _, err := deps.RunOnce(context.Background(), "tree:flush-dt-probe", "probe task", RunOptions{DisableBlackboard: true}); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	da := evolution.NewDTAnalyzer()
	if err := da.Load(DecisionTreeStatsFile("tree:flush-dt-probe")); err != nil {
		t.Fatalf("load decision-tree telemetry: %v", err)
	}
	ss := da.Stats["FlushDTRouter"]
	if ss == nil {
		t.Fatalf("decision-tree telemetry not flushed for FlushDTRouter: %+v", da.Stats)
	}
	var path *evolution.PathStats
	for i := range ss.Paths {
		if ss.Paths[i].PathName == "FlushDTChildA" {
			path = &ss.Paths[i]
		}
	}
	if path == nil || path.HitCount != 1 || path.SuccessCount != 1 {
		t.Fatalf("FlushDTChildA path = %+v, want HitCount/SuccessCount 1/1", path)
	}
	if path.Condition != "CheckConfidence" {
		t.Fatalf("FlushDTChildA condition = %q, want CheckConfidence", path.Condition)
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
