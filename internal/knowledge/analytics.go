package knowledge

import (
	"fmt"
	"sort"
	"strings"
)

// Analytics holds computed graph analytics.
type Analytics struct {
	Centrality       []CentralityEntry
	ToolContention   []ContentionEntry
	CoverageGaps     []string
	Bottlenecks      []BottleneckEntry
	SuggestedActions []string
}

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

	// 3. Coverage gaps: domain trees that might be missing
	knownDomains := []string{
		"domain:security_audit", "domain:crash_investigator",
		"domain:data_pipeline", "domain:meeting_notes",
		"domain:refactoring", "domain:devops_ci",
		"domain:trading_signal", "domain:game_ai",
	}
	for _, id := range knownDomains {
		if _, ok := kg.Trees[id]; !ok {
			a.CoverageGaps = append(a.CoverageGaps, id)
		}
	}

	// 4. Bottlenecks: trees with low success rate
	for id, tree := range kg.Trees {
		if tree.RunCount >= 3 && tree.Fitness < 30 {
			a.Bottlenecks = append(a.Bottlenecks, BottleneckEntry{
				TreeID:      id,
				SuccessRate: tree.Fitness,
				Runs:        tree.RunCount,
			})
		}
	}
	sort.Slice(a.Bottlenecks, func(i, j int) bool {
		return a.Bottlenecks[i].SuccessRate < a.Bottlenecks[j].SuccessRate
	})

	// 5. Suggested actions
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
		trace := GlobalTraceStore.LastFailure(b.TreeID)
		action := fmt.Sprintf("Investigate %s: %.0f%% success (%d runs)", b.TreeID, b.SuccessRate, b.Runs)
		if trace != nil {
			action += fmt.Sprintf(" — last failure: %s (%s)", trace.Outcome, trace.Task)
		}
		a.SuggestedActions = append(a.SuggestedActions, action)
	}

	return a
}

// FormatAnalytics returns a human-readable analytics report.
func (a Analytics) FormatAnalytics() string {
	var s strings.Builder

	s.WriteString("=== BT Platform Graph Analytics ===\n\n")

	if len(a.Centrality) > 0 {
		s.WriteString("Centrality (most depended-on trees):\n")
		for _, c := range a.Centrality[:min(5, len(a.Centrality))] {
			s.WriteString(fmt.Sprintf("  %-35s %d dependents\n", c.TreeID, c.Dependents))
		}
		s.WriteString("\n")
	}

	if len(a.ToolContention) > 0 {
		s.WriteString("Tool Contention:\n")
		for _, c := range a.ToolContention {
			riskIcon := "\u2705" // ✅
			if c.Risk == "high" {
				riskIcon = "\U0001F534" // 🔴
			} else if c.Risk == "medium" {
				riskIcon = "\U0001F7E1" // 🟡
			}
			s.WriteString(fmt.Sprintf("  %s %s: %v\n", riskIcon, c.ToolID, c.Trees))
		}
		s.WriteString("\n")
	}

	if len(a.CoverageGaps) > 0 {
		s.WriteString("Coverage Gaps (skills without KG trees):\n")
		for _, gap := range a.CoverageGaps {
			s.WriteString(fmt.Sprintf("  - %s\n", gap))
		}
		s.WriteString("\n")
	}

	if len(a.Bottlenecks) > 0 {
		s.WriteString("Bottlenecks (low success rate):\n")
		for _, b := range a.Bottlenecks {
			s.WriteString(fmt.Sprintf("  %-35s %.0f%% success (%d runs)\n", b.TreeID, b.SuccessRate, b.Runs))
		}
		s.WriteString("\n")
	}

	if len(a.SuggestedActions) > 0 {
		s.WriteString("Suggested Actions:\n")
		for i, action := range a.SuggestedActions {
			s.WriteString(fmt.Sprintf("  %d. %s\n", i+1, action))
		}
	}

	return s.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
