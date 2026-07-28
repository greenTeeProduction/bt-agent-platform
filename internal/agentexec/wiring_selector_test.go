package agentexec

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// Learned Selector reordering at resolve time is STRICTLY opt-in: success-rate
// ordering inverts cost-first routers (the nlm-before-Claude quota economy),
// so it must never become an ambient default. Opt-in wires the per-tree stats
// resolver so unrelated trees can never pollute each other's ordering.
func TestWireSelectorReorderIsOptIn(t *testing.T) {
	prev := domains.SelectorStatsPathFn
	t.Cleanup(func() { domains.SelectorStatsPathFn = prev })

	domains.SelectorStatsPathFn = nil
	t.Setenv("BT_SELECTOR_REORDER", "")
	wireSelectorReorder()
	if domains.SelectorStatsPathFn != nil {
		t.Fatal("without BT_SELECTOR_REORDER=1 the resolve-time reorder must stay unwired")
	}

	t.Setenv("BT_SELECTOR_REORDER", "1")
	wireSelectorReorder()
	if domains.SelectorStatsPathFn == nil {
		t.Fatal("BT_SELECTOR_REORDER=1 must wire the per-tree stats resolver")
	}
	if got := domains.SelectorStatsPathFn("domain:goap_fusion"); !strings.HasSuffix(got, "selector-stats/domain_goap_fusion.json") {
		t.Fatalf("wired resolver must yield the per-tree stats path, got %q", got)
	}
}

// TestWireSelectorReorderWiresDTStatsPathFn pins the DTAnalyzer/BTOptimizer
// sibling of TestWireSelectorReorderIsOptIn above: domains.DTStatsPath has no
// production writer wired at resolve time (ADR-191's activation was inert
// without agent.DecisionTreeStatsFile — see gardener's dtStatsPathFor, which
// already got this per-tree-first fix for the evolution-time path). This pins
// the same fix for the resolve-time path, mirroring how
// domains.SelectorStatsPathFn is wired under the same BT_SELECTOR_REORDER=1
// gate: opt-in only, and per-tree so unrelated trees' DT telemetry can never
// pollute each other's learned ordering.
func TestWireSelectorReorderWiresDTStatsPathFn(t *testing.T) {
	prev := domains.DTStatsPathFn
	t.Cleanup(func() { domains.DTStatsPathFn = prev })

	domains.DTStatsPathFn = nil
	t.Setenv("BT_SELECTOR_REORDER", "")
	wireSelectorReorder()
	if domains.DTStatsPathFn != nil {
		t.Fatal("without BT_SELECTOR_REORDER=1 the resolve-time DT reorder must stay unwired")
	}

	t.Setenv("BT_SELECTOR_REORDER", "1")
	wireSelectorReorder()
	if domains.DTStatsPathFn == nil {
		t.Fatal("BT_SELECTOR_REORDER=1 must wire the per-tree DT stats resolver")
	}
	if got := domains.DTStatsPathFn("domain:goap_fusion"); !strings.HasSuffix(got, "selector-stats/domain_goap_fusion-dt.json") {
		t.Fatalf("wired DT resolver must yield the per-tree DT stats path, got %q", got)
	}
}

// TestWireSelectorReorder_SelectsStrategyFromEnv pins milestone 4/5 of the
// Selector-reordering consolidation program: evolution.OrderByIG/OrderByGini/
// OrderByHybrid have zero production callers because
// internal/domains/tree_resolver.go's applyLearnedSelectorOrdering hardcodes
// evolution.OrderBySuccessRate. wireSelectorReorder must read
// BT_SELECTOR_ORDERING_STRATEGY and set domains.SelectorOrderingStrategy so
// the resolve-time reorder pass can actually use IG/Gini/Hybrid instead of
// only ever exercising OrderBySuccessRate. Unset (or unrecognized) must keep
// today's OrderBySuccessRate behavior — this pass is already live for every
// BT_SELECTOR_REORDER=1 deployment (TestWireSelectorReorderIsOptIn above), so
// silently changing its ranking would change production behavior for
// existing opt-ins, not just activate dead code.
func TestWireSelectorReorder_SelectsStrategyFromEnv(t *testing.T) {
	prevPath := domains.SelectorStatsPathFn
	prevStrategy := domains.SelectorOrderingStrategy
	t.Cleanup(func() {
		domains.SelectorStatsPathFn = prevPath
		domains.SelectorOrderingStrategy = prevStrategy
	})
	t.Setenv("BT_SELECTOR_REORDER", "1")

	t.Setenv("BT_SELECTOR_ORDERING_STRATEGY", "")
	wireSelectorReorder()
	if domains.SelectorOrderingStrategy != evolution.OrderBySuccessRate {
		t.Errorf("default SelectorOrderingStrategy = %q, want %q (unset env must not change existing behavior)",
			domains.SelectorOrderingStrategy, evolution.OrderBySuccessRate)
	}

	t.Setenv("BT_SELECTOR_ORDERING_STRATEGY", "information_gain")
	wireSelectorReorder()
	if domains.SelectorOrderingStrategy != evolution.OrderByIG {
		t.Errorf("SelectorOrderingStrategy = %q, want %q when BT_SELECTOR_ORDERING_STRATEGY=information_gain — OrderByIG is otherwise unreachable in production",
			domains.SelectorOrderingStrategy, evolution.OrderByIG)
	}
}
