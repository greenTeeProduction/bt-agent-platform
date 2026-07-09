package agentexec

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/domains"
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
