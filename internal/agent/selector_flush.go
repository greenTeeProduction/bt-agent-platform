package agent

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/knowledge"
)

// selectorStatsDir overrides the durable selector-telemetry directory (tests);
// empty means <platform home>/selector-stats.
var selectorStatsDir string

// SelectorStatsFile returns the durable per-tree Selector telemetry path for
// treeID. Telemetry is keyed per tree — a single shared file would let equal
// selector NAMES in unrelated trees pollute each other's learned ordering
// (the cross-tree collision hazard flagged in the 2026-07-09 review).
func SelectorStatsFile(treeID string) string {
	return selectorTelemetryPath(treeID, ".json")
}

// DecisionTreeStatsFile returns the durable per-tree DTAnalyzer telemetry
// path for treeID — a sidecar living alongside SelectorStatsFile's file in
// the same directory, distinguished by a "-dt" suffix.
func DecisionTreeStatsFile(treeID string) string {
	return selectorTelemetryPath(treeID, "-dt.json")
}

func selectorTelemetryPath(treeID, suffix string) string {
	dir := selectorStatsDir
	if dir == "" {
		dir = filepath.Join(HomeDir(), "selector-stats")
	}
	sanitized := strings.NewReplacer(":", "_", "/", "_", "\\", "_").Replace(treeID)
	return filepath.Join(dir, sanitized+suffix)
}

// flushSelectorTelemetry merges the run's Selector-attributed terminal child
// ticks into the tree's durable telemetry — the writer half of learned
// Selector ordering. Ticks under non-Selector composites are filtered out by
// walking the run's own tree definition. Best-effort: a telemetry write
// failure never fails the run.
func (d *RunDeps) flushSelectorTelemetry(tree *evolution.SerializableNode, treeID string, bb *engine.Blackboard) {
	if tree == nil || bb == nil || strings.TrimSpace(treeID) == "" {
		return
	}
	ticks := bb.ChildTicks()
	if len(ticks) == 0 {
		return
	}
	selectors := make(map[string]bool)
	collectSelectorNames(tree, selectors)
	if len(selectors) == 0 {
		return
	}
	conditions := evolution.SelectorChildConditions(tree)
	outcomes := make([]knowledge.SelectorChildOutcome, 0, len(ticks))
	dtOutcomes := make([]knowledge.DecisionTreeChildOutcome, 0, len(ticks))
	for _, tk := range ticks {
		if !selectors[tk.Parent] {
			continue
		}
		outcomes = append(outcomes, knowledge.SelectorChildOutcome{
			Selector: tk.Parent,
			Child:    tk.Child,
			Status:   tk.Status,
		})
		dtOutcomes = append(dtOutcomes, knowledge.DecisionTreeChildOutcome{
			Selector:  tk.Parent,
			Child:     tk.Child,
			Condition: conditions[tk.Parent][tk.Child],
			Status:    tk.Status,
		})
	}
	if len(outcomes) == 0 {
		return
	}
	path := SelectorStatsFile(treeID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		slog.Warn("selector telemetry flush failed", "tree", treeID, "error", err)
		return
	}
	if err := knowledge.RecordSelectorChildOutcomes(path, outcomes); err != nil {
		slog.Warn("selector telemetry flush failed", "tree", treeID, "error", err)
	}

	// Decision-tree telemetry sidecar: feeds evolution.DTAnalyzer/BTOptimizer,
	// the entropy/Gini-based sibling of the SelectorOptimizer ordering above.
	dtPath := DecisionTreeStatsFile(treeID)
	if err := knowledge.RecordDecisionTreeChildOutcomes(dtPath, dtOutcomes); err != nil {
		slog.Warn("decision-tree telemetry flush failed", "tree", treeID, "error", err)
	}
}

// collectSelectorNames gathers the names of every Selector node in the tree.
func collectSelectorNames(n *evolution.SerializableNode, out map[string]bool) {
	if n == nil {
		return
	}
	if n.Type == "Selector" && n.Name != "" {
		out[n.Name] = true
	}
	for i := range n.Children {
		collectSelectorNames(&n.Children[i], out)
	}
}
