package knowledge

import (
	"fmt"
	"sort"
	"strings"
)

// Analytics holds computed graph analytics.
type Analytics struct {
	Centrality        []CentralityEntry
	ToolContention    []ContentionEntry
	CoverageGaps      []string
	Bottlenecks       []BottleneckEntry
	SelectionPressure []SelectionPressureEntry
	SuggestedActions  []string
}

// Selection-pressure thresholds: a proven tree that nobody is exercising.
const (
	// provenFitnessThreshold is the fitness at or above which a tree counts as
	// "proven" — worth breeding from rather than leaving idle.
	provenFitnessThreshold = 70.0
	// underbredRunThreshold is the run count below which a proven tree counts as
	// "underbred" — high fitness but starved of selection pressure.
	underbredRunThreshold = 5
)

// CentralityEntry is a tree and how many others depend on it.
type CentralityEntry struct {
	TreeID     string
	Dependents int
}

// ContentionEntry tracks trees sharing a tool.
type ContentionEntry struct {
	ToolID string
	Trees  []string
	Risk   string // "low", "medium", "high"
}

// BottleneckEntry is a tree with low success rate.
type BottleneckEntry struct {
	TreeID      string
	SuccessRate float64
	Runs        int
	// LastFailureTask and LastFailureOutcome carry the most recent failing
	// trace's task and outcome as structured fields — not only concatenated into
	// the human-readable SuggestedAction string — so bt_evolve_bottlenecks can
	// seed its per-tree evolution context from the actual failing task rather
	// than parse prose. Empty when the tree has no recorded failure.
	LastFailureTask    string
	LastFailureOutcome string
}

// SelectionPressureEntry is a proven tree (high fitness) that is underbred
// (low run count) — a high-fitness asset the loop is failing to exercise.
type SelectionPressureEntry struct {
	TreeID   string
	Fitness  float64
	RunCount int
}

// ComputeAnalytics runs all analytics on the knowledge graph.
func (kg *KnowledgeGraph) ComputeAnalytics() Analytics {
	kg.mu.RLock()
	defer kg.mu.RUnlock()
	a := Analytics{}

	// 1. Centrality: count incoming edges per tree
	dependents := map[string]int{}
	for _, edge := range kg.Edges {
		if edge.Type == "depends_on" || edge.Type == "extends" || edge.Type == "composes" {
			dependents[edge.To]++
		}
	}
	for id, count := range dependents {
		a.Centrality = append(a.Centrality, CentralityEntry{TreeID: id, Dependents: count})
	}
	sort.Slice(a.Centrality, func(i, j int) bool {
		return a.Centrality[i].Dependents > a.Centrality[j].Dependents
	})

	// 2. Tool contention: trees sharing tools
	toolUsers := map[string][]string{}
	for _, edge := range kg.Edges {
		if edge.Type == "uses_tool" && strings.HasPrefix(edge.To, "tool:") {
			toolID := strings.TrimPrefix(edge.To, "tool:")
			toolUsers[toolID] = append(toolUsers[toolID], edge.From)
		}
	}
	for tool, users := range toolUsers {
		risk := "low"
		if len(users) >= 3 {
			risk = "high"
		} else if len(users) >= 2 {
			risk = "medium"
		}
		a.ToolContention = append(a.ToolContention, ContentionEntry{
			ToolID: tool,
			Trees:  users,
			Risk:   risk,
		})
	}
	sort.Slice(a.ToolContention, func(i, j int) bool {
		return len(a.ToolContention[i].Trees) > len(a.ToolContention[j].Trees)
	})

	// 3. Coverage gaps: expected domain trees not registered in the graph. The
	// expected set is injected via ExpectedDomains (populated by the daemon from
	// the live domain registry) so this stays registry-accurate instead of tied
	// to a stale hardcoded slice. Fall back to defaultExpectedDomains when unset.
	// Also audit resolverSpecialCaseTreeIDs — the bare, non-"domain:"-prefixed
	// IDs that tree_resolver.go special-cases outside AllDomainTrees() — so a
	// resolver ID added without a matching registry.go Register call surfaces
	// here automatically instead of depending on periodic manual review of
	// tree_resolver.go against registry.go to rediscover it.
	expectedDomains := kg.ExpectedDomains
	if len(expectedDomains) == 0 {
		expectedDomains = defaultExpectedDomains
	}
	for _, id := range expectedDomains {
		if _, ok := kg.Trees[id]; !ok {
			a.CoverageGaps = append(a.CoverageGaps, id)
		}
	}
	for _, id := range resolverSpecialCaseTreeIDs {
		if _, ok := kg.Trees[id]; !ok {
			a.CoverageGaps = append(a.CoverageGaps, id)
		}
	}

	// 4. Bottlenecks: trees with low success rate. Capture the most recent
	// failing trace's task/outcome as structured fields so downstream evolution
	// (bt_evolve_bottlenecks) can seed per-tree context from the actual failing
	// task instead of re-parsing the human-readable SuggestedAction string.
	for id, tree := range kg.Trees {
		if tree.RunCount >= 3 && tree.Fitness < 30 {
			entry := BottleneckEntry{
				TreeID:      id,
				SuccessRate: tree.Fitness,
				Runs:        tree.RunCount,
			}
			if trace := GlobalTraceStore.LastFailure(id); trace != nil {
				entry.LastFailureTask = trace.Task
				entry.LastFailureOutcome = trace.Outcome
			}
			a.Bottlenecks = append(a.Bottlenecks, entry)
		}
	}
	sort.Slice(a.Bottlenecks, func(i, j int) bool {
		return a.Bottlenecks[i].SuccessRate < a.Bottlenecks[j].SuccessRate
	})

	// 5. Selection pressure: proven trees (high fitness) that are underbred
	// (low run count). These are winners the loop keeps on the shelf — surfacing
	// them lets breeding and deterministic discovery become fitness-driven.
	for id, tree := range kg.Trees {
		if tree.Fitness >= provenFitnessThreshold && tree.RunCount < underbredRunThreshold {
			a.SelectionPressure = append(a.SelectionPressure, SelectionPressureEntry{
				TreeID:   id,
				Fitness:  tree.Fitness,
				RunCount: tree.RunCount,
			})
		}
	}
	sort.Slice(a.SelectionPressure, func(i, j int) bool {
		return a.SelectionPressure[i].Fitness > a.SelectionPressure[j].Fitness
	})

	// 6. Suggested actions
	for _, gap := range a.CoverageGaps {
		a.SuggestedActions = append(a.SuggestedActions,
			fmt.Sprintf("Register %s as a KG tree (skill exists)", gap))
	}
	for _, c := range a.ToolContention {
		if c.Risk == "high" {
			a.SuggestedActions = append(a.SuggestedActions,
				fmt.Sprintf("Stagger cron for trees sharing %s: %v (contention risk)", c.ToolID, c.Trees))
		}
	}
	for _, b := range a.Bottlenecks {
		action := fmt.Sprintf("Investigate %s: %.0f%% success (%d runs)", b.TreeID, b.SuccessRate, b.Runs)
		if b.LastFailureOutcome != "" {
			action += fmt.Sprintf(" — last failure: %s (%s)", b.LastFailureOutcome, b.LastFailureTask)
		}
		a.SuggestedActions = append(a.SuggestedActions, action)
	}
	for _, sp := range a.SelectionPressure {
		a.SuggestedActions = append(a.SuggestedActions,
			fmt.Sprintf("Breed/exercise %s: proven (%.0f%% fitness) but underbred (%d runs) — apply selection pressure",
				sp.TreeID, sp.Fitness, sp.RunCount))
	}

	return a
}

// FormatAnalytics returns a human-readable analytics report.
func (a Analytics) FormatAnalytics() string {
	var s strings.Builder

	s.WriteString("=== BT Platform Graph Analytics ===\n\n")

	if len(a.Centrality) > 0 {
		s.WriteString("Centrality (most depended-on trees):\n")
		for _, c := range a.Centrality[:minInt(5, len(a.Centrality))] {
			fmt.Fprintf(&s, "  %-35s %d dependents\n", c.TreeID, c.Dependents)
		}
		s.WriteString("\n")
	}

	if len(a.ToolContention) > 0 {
		s.WriteString("Tool Contention:\n")
		for _, c := range a.ToolContention {
			riskIcon := "\u2705" // ✅
			switch c.Risk {
			case "high":
				riskIcon = "\U0001F534" // 🔴
			case "medium":
				riskIcon = "\U0001F7E1" // 🟡
			}
			fmt.Fprintf(&s, "  %s %s: %v\n", riskIcon, c.ToolID, c.Trees)
		}
		s.WriteString("\n")
	}

	if len(a.CoverageGaps) > 0 {
		s.WriteString("Coverage Gaps (skills without KG trees):\n")
		for _, gap := range a.CoverageGaps {
			fmt.Fprintf(&s, "  - %s\n", gap)
		}
		s.WriteString("\n")
	}

	if len(a.Bottlenecks) > 0 {
		s.WriteString("Bottlenecks (low success rate):\n")
		for _, b := range a.Bottlenecks {
			fmt.Fprintf(&s, "  %-35s %.0f%% success (%d runs)\n", b.TreeID, b.SuccessRate, b.Runs)
		}
		s.WriteString("\n")
	}

	if len(a.SelectionPressure) > 0 {
		s.WriteString("Selection Pressure (proven but underbred trees):\n")
		for _, sp := range a.SelectionPressure {
			fmt.Fprintf(&s, "  %-35s %.0f%% fitness, %d runs\n", sp.TreeID, sp.Fitness, sp.RunCount)
		}
		s.WriteString("\n")
	}

	if len(a.SuggestedActions) > 0 {
		s.WriteString("Suggested Actions:\n")
		for i, action := range a.SuggestedActions {
			fmt.Fprintf(&s, "  %d. %s\n", i+1, action)
		}
	}

	return s.String()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
