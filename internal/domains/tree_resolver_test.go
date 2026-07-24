package domains

import (
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// writeSelectorStats persists a durable Selector telemetry file describing one
// Selector ("TriageSel") whose "FastPath" child succeeds far more often than its
// authored-first "SlowPath" child, using the same Record→SaveSelectorStats path
// production writers use. successCounts maps child name → (successes, failures);
// the resulting per-child SuccessRate drives OrderBySuccessRate reordering.
func writeSelectorStats(t *testing.T, counts map[string][2]int) string {
	t.Helper()
	so := evolution.NewSelectorOptimizer(evolution.OrderBySuccessRate)
	for name, sf := range counts {
		for i := 0; i < sf[0]; i++ {
			so.Record("TriageSel", evolution.NodeExecutionRecord{NodeName: name, Outcome: "success"})
		}
		for i := 0; i < sf[1]; i++ {
			so.Record("TriageSel", evolution.NodeExecutionRecord{NodeName: name, Outcome: "failure"})
		}
	}
	path := filepath.Join(t.TempDir(), "selector_stats.json")
	if err := so.SaveSelectorStats(path); err != nil {
		t.Fatalf("SaveSelectorStats: %v", err)
	}
	return path
}

// triageSelectorTree returns a fresh Selector "TriageSel" whose two children are
// in authored order [SlowPath, FastPath]. Neither child name matches the
// default-path/AlwaysSucceed fallback guard, so both are eligible for reordering.
func triageSelectorTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Selector",
		Name: "TriageSel",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "SlowPath"},
			{Type: "Condition", Name: "FastPath"},
		},
	}
}

// TestResolveTreeID_AppliesLearnedSelectorOrdering pins the accumulated-telemetry
// consumer: when a durable Selector stats file is wired via SelectorStatsPath and
// a resolved tree contains a Selector with enough samples, ResolveTreeID must
// reorder that Selector's children by learned success rate (via
// evolution.SelectorOptimizer) so the higher-yield child is tried first.
//
// FastPath (0.9 success) is authored second but must be promoted ahead of
// SlowPath (0.2 success). Without the consumer wired at tree resolution, the
// authored order [SlowPath, FastPath] survives and this fails.
func TestResolveTreeID_AppliesLearnedSelectorOrdering(t *testing.T) {
	origFn := DynamicResolveFn
	defer func() { DynamicResolveFn = origFn }()
	origPath := SelectorStatsPath
	defer func() { SelectorStatsPath = origPath }()

	// 20 total samples for TriageSel — comfortably above the optimizer's
	// MinSamples (10) threshold, so cold-start suppression does not apply.
	SelectorStatsPath = writeSelectorStats(t, map[string][2]int{
		"FastPath": {9, 1}, // success rate 0.9
		"SlowPath": {2, 8}, // success rate 0.2
	})

	tree := triageSelectorTree()
	DynamicResolveFn = func(id string) *evolution.SerializableNode {
		if id == "core:triage" {
			return tree
		}
		return nil
	}

	got := ResolveTreeID("core:triage")
	if got == nil {
		t.Fatal("ResolveTreeID returned nil for core:triage")
	}
	if len(got.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(got.Children))
	}
	if got.Children[0].Name != "FastPath" || got.Children[1].Name != "SlowPath" {
		t.Fatalf("learned ordering not applied: children = [%s, %s], want [FastPath, SlowPath]",
			got.Children[0].Name, got.Children[1].Name)
	}
}

// TestResolveTreeID_ColdSelectorKeepsAuthoredOrder guards the minimum-sample
// threshold: a Selector with too few recorded samples must retain its authored
// child order, so undertrained Selectors are not reshuffled by noise.
func TestResolveTreeID_ColdSelectorKeepsAuthoredOrder(t *testing.T) {
	origFn := DynamicResolveFn
	defer func() { DynamicResolveFn = origFn }()
	origPath := SelectorStatsPath
	defer func() { SelectorStatsPath = origPath }()

	// Only 4 total samples — below MinSamples (10) — so OrderChildren declines to
	// reorder even though FastPath has the better rate.
	SelectorStatsPath = writeSelectorStats(t, map[string][2]int{
		"FastPath": {1, 1},
		"SlowPath": {1, 1},
	})

	tree := triageSelectorTree()
	DynamicResolveFn = func(id string) *evolution.SerializableNode {
		if id == "core:triage" {
			return tree
		}
		return nil
	}

	got := ResolveTreeID("core:triage")
	if got == nil {
		t.Fatal("ResolveTreeID returned nil for core:triage")
	}
	if got.Children[0].Name != "SlowPath" || got.Children[1].Name != "FastPath" {
		t.Fatalf("cold Selector was reordered: children = [%s, %s], want authored [SlowPath, FastPath]",
			got.Children[0].Name, got.Children[1].Name)
	}
}

// TestResolveTreeID_PerTreeStatsFn pins the per-tree telemetry consumer: when
// SelectorStatsPathFn is wired (agentexec, behind BT_SELECTOR_REORDER=1), each
// resolved tree reorders from its OWN stats file. A single shared file would
// let equal selector NAMES in unrelated trees pollute each other's learned
// ordering — tree A's telemetry must reorder only tree A.
func TestResolveTreeID_PerTreeStatsFn(t *testing.T) {
	origFn := DynamicResolveFn
	defer func() { DynamicResolveFn = origFn }()
	origPathFn := SelectorStatsPathFn
	defer func() { SelectorStatsPathFn = origPathFn }()
	origPath := SelectorStatsPath
	defer func() { SelectorStatsPath = origPath }()
	SelectorStatsPath = ""

	// Telemetry exists ONLY for core:a — same "TriageSel" selector name in
	// both trees.
	statsA := writeSelectorStats(t, map[string][2]int{
		"FastPath": {9, 1},
		"SlowPath": {2, 8},
	})
	SelectorStatsPathFn = func(treeID string) string {
		if treeID == "core:a" {
			return statsA
		}
		return ""
	}

	trees := map[string]*evolution.SerializableNode{
		"core:a": triageSelectorTree(),
		"core:b": triageSelectorTree(),
	}
	DynamicResolveFn = func(id string) *evolution.SerializableNode { return trees[id] }

	a := ResolveTreeID("core:a")
	if a == nil || a.Children[0].Name != "FastPath" {
		t.Fatalf("core:a must reorder from its own telemetry; children[0] = %v", a)
	}
	b := ResolveTreeID("core:b")
	if b == nil || b.Children[0].Name != "SlowPath" || b.Children[1].Name != "FastPath" {
		t.Fatalf("core:b has no telemetry and must keep authored order; got [%s, %s]",
			b.Children[0].Name, b.Children[1].Name)
	}
}

// dtRouterTree returns a fresh Selector "DTRouter" whose two Condition
// children have deliberately overlapping condition strings: "TypeA" is a
// substring of "TypeAExtra". That overlap is what makes
// evolution.BTOptimizer.OptimizeSelectors see differential information gain
// between the two children — a Selector whose paths have non-overlapping,
// mutually-exclusive single-path conditions always ties at parentEntropy,
// because either split perfectly isolates one path (see
// evolution.DTAnalyzer.InformationGain). Authored order is [TypeA,
// TypeAExtra]; genuine DT telemetry recorded on "TypeAExtra" must promote it
// ahead of "TypeA".
func dtRouterTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Selector",
		Name: "DTRouter",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "TypeA"},
			{Type: "Condition", Name: "TypeAExtra"},
		},
	}
}

// writeDTStats persists a durable DTAnalyzer telemetry file for "DTRouter",
// recording hitCounts[name] hits under each named child using its own name as
// both path and condition — the same shape production telemetry takes via
// knowledge.RecordDecisionTreeChildOutcomes (Condition comes from
// evolution.SelectorChildConditions, which for a leaf Condition node is the
// node's own Name).
func writeDTStats(t *testing.T, hitCounts map[string]int) string {
	t.Helper()
	da := evolution.NewDTAnalyzer()
	for name, n := range hitCounts {
		for i := 0; i < n; i++ {
			da.RecordHit("DTRouter", name, name, true)
		}
	}
	path := filepath.Join(t.TempDir(), "dt_stats.json")
	if err := da.Save(path); err != nil {
		t.Fatalf("DTAnalyzer.Save: %v", err)
	}
	return path
}

// TestResolveTreeID_AppliesDTOptimizerOrdering pins the entropy/Gini-based
// sibling consumer: after the existing SelectorOptimizer pass, when a durable
// DTAnalyzer stats file is wired via DTStatsPath and the resolved tree
// contains a Selector with recorded telemetry, ResolveTreeID must reorder
// that Selector's children by evolution.BTOptimizer.OptimizeSelectors
// (information-gain reordering) — the non-destructive sibling of the
// SelectorOptimizer pass above, milestone 2 of the Q2 Evolvability program
// wiring BTOptimizer/DTAnalyzer into the same production path.
//
// "TypeAExtra" has higher information gain than "TypeA" (see dtRouterTree)
// and so must be promoted ahead of it. Without the consumer wired at tree
// resolution, the authored order [TypeA, TypeAExtra] survives and this fails.
func TestResolveTreeID_AppliesDTOptimizerOrdering(t *testing.T) {
	origFn := DynamicResolveFn
	defer func() { DynamicResolveFn = origFn }()
	origDTPath := DTStatsPath
	defer func() { DTStatsPath = origDTPath }()
	origPath := SelectorStatsPath
	defer func() { SelectorStatsPath = origPath }()
	SelectorStatsPath = "" // isolate the DT pass from the SelectorOptimizer pass

	DTStatsPath = writeDTStats(t, map[string]int{
		"TypeA":      6,
		"TypeAExtra": 3,
	})

	tree := dtRouterTree()
	DynamicResolveFn = func(id string) *evolution.SerializableNode {
		if id == "core:dt" {
			return tree
		}
		return nil
	}

	got := ResolveTreeID("core:dt")
	if got == nil {
		t.Fatal("ResolveTreeID returned nil for core:dt")
	}
	if len(got.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(got.Children))
	}
	if got.Children[0].Name != "TypeAExtra" || got.Children[1].Name != "TypeA" {
		t.Fatalf("DT-optimizer ordering not applied: children = [%s, %s], want [TypeAExtra, TypeA]",
			got.Children[0].Name, got.Children[1].Name)
	}
}

// TestResolveTreeID_NoDTStatsLeavesTreeUnchanged guards the no-telemetry
// case: a tree resolved with no DTStatsPath/DTStatsPathFn wired (or one
// pointing at a selector with no recorded stats) must keep its authored
// Selector order untouched, so cold or unwired deployments are unaffected by
// the DT-optimizer pass.
func TestResolveTreeID_NoDTStatsLeavesTreeUnchanged(t *testing.T) {
	origFn := DynamicResolveFn
	defer func() { DynamicResolveFn = origFn }()
	origDTPath := DTStatsPath
	defer func() { DTStatsPath = origDTPath }()
	origPath := SelectorStatsPath
	defer func() { SelectorStatsPath = origPath }()
	SelectorStatsPath = ""
	DTStatsPath = "" // no DT telemetry wired at all

	tree := dtRouterTree()
	DynamicResolveFn = func(id string) *evolution.SerializableNode {
		if id == "core:dt-cold" {
			return tree
		}
		return nil
	}

	got := ResolveTreeID("core:dt-cold")
	if got == nil {
		t.Fatal("ResolveTreeID returned nil for core:dt-cold")
	}
	if got.Children[0].Name != "TypeA" || got.Children[1].Name != "TypeAExtra" {
		t.Fatalf("tree with no DT telemetry was reordered: children = [%s, %s], want authored [TypeA, TypeAExtra]",
			got.Children[0].Name, got.Children[1].Name)
	}
}

// TestResolveTreeID_TelegramClarify pins evolution.TelegramClarifyTree() as
// reachable via ResolveTreeID under the "telegram_clarify" ID, matching how
// its sibling standalone trees (vault_manager, notebooklm-bridge, fusion) are
// wired as bare special-case IDs in resolveTreeIDWithResolver. Without this
// wiring, TelegramClarifyTree is unreachable via bt_delegate_to_tree even
// though its conditions/actions are registered (internal/engine/telegram_init.go).
func TestResolveTreeID_TelegramClarify(t *testing.T) {
	got := ResolveTreeID("telegram_clarify")
	if got == nil {
		t.Fatal(`ResolveTreeID("telegram_clarify") returned nil`)
	}
	if got.Name != "TelegramClarify" {
		t.Fatalf(`ResolveTreeID("telegram_clarify").Name = %q, want "TelegramClarify"`, got.Name)
	}
}
