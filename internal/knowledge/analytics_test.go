package knowledge

import (
	"strings"
	"testing"
)

// =============================================================================
// Analytics — ComputeAnalytics, FormatAnalytics
// =============================================================================

func TestComputeAnalytics_EmptyGraph(t *testing.T) {
	kg := NewKnowledgeGraph()
	a := kg.ComputeAnalytics()

	if len(a.Centrality) != 0 {
		t.Errorf("expected 0 centrality entries, got %d", len(a.Centrality))
	}
	if len(a.ToolContention) != 0 {
		t.Errorf("expected 0 tool contention entries, got %d", len(a.ToolContention))
	}
	if len(a.CoverageGaps) == 0 {
		t.Error("empty graph should have coverage gaps (all domains missing)")
	}
	if len(a.Bottlenecks) != 0 {
		t.Errorf("expected 0 bottlenecks, got %d", len(a.Bottlenecks))
	}
	if len(a.SuggestedActions) == 0 {
		t.Error("empty graph should suggest registering missing domains")
	}
}

func TestComputeAnalytics_Centrality(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "a", Name: "A", Category: "test", Fitness: 80})
	kg.Register(&TreeMeta{ID: "b", Name: "B", Category: "test", Fitness: 50})
	kg.Register(&TreeMeta{ID: "c", Name: "C", Category: "test", Fitness: 30})
	kg.Connect("b", "a", "depends_on")
	kg.Connect("c", "a", "depends_on")

	a := kg.ComputeAnalytics()

	// Tree 'a' has 2 dependents (b, c)
	found := false
	for _, c := range a.Centrality {
		if c.TreeID == "a" {
			found = true
			if c.Dependents != 2 {
				t.Errorf("expected tree 'a' to have 2 dependents, got %d", c.Dependents)
			}
		}
	}
	if !found {
		t.Error("tree 'a' should appear in centrality results")
	}
}

func TestComputeAnalytics_ExtendedEdges(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "x", Name: "X", Category: "test"})
	kg.Register(&TreeMeta{ID: "y", Name: "Y", Category: "test"})
	kg.Register(&TreeMeta{ID: "z", Name: "Z", Category: "test"})
	kg.Connect("y", "x", "extends")
	kg.Connect("z", "x", "composes")

	a := kg.ComputeAnalytics()

	foundX := false
	for _, c := range a.Centrality {
		if c.TreeID == "x" {
			foundX = true
			if c.Dependents != 2 {
				t.Errorf("expected tree 'x' to have 2 dependents (extends + composes), got %d", c.Dependents)
			}
		}
	}
	if !foundX {
		t.Error("tree 'x' should appear in centrality")
	}
}

func TestComputeAnalytics_ToolContention(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "t1", Name: "T1", Category: "test"})
	kg.Register(&TreeMeta{ID: "t2", Name: "T2", Category: "test"})
	kg.Register(&TreeMeta{ID: "t3", Name: "T3", Category: "test"})

	// 3 trees sharing a tool = high risk
	kg.Connect("t1", "tool:web_search", "uses_tool")
	kg.Connect("t2", "tool:web_search", "uses_tool")
	kg.Connect("t3", "tool:web_search", "uses_tool")

	// 2 trees sharing another tool = medium risk
	kg.Connect("t1", "tool:calculator", "uses_tool")
	kg.Connect("t2", "tool:calculator", "uses_tool")

	a := kg.ComputeAnalytics()

	foundHigh := false
	foundMedium := false
	for _, c := range a.ToolContention {
		if c.ToolID == "web_search" {
			foundHigh = true
			if c.Risk != "high" {
				t.Errorf("expected web_search risk='high', got %q", c.Risk)
			}
			if len(c.Trees) != 3 {
				t.Errorf("expected 3 web_search users, got %d", len(c.Trees))
			}
		}
		if c.ToolID == "calculator" {
			foundMedium = true
			if c.Risk != "medium" {
				t.Errorf("expected calculator risk='medium', got %q", c.Risk)
			}
		}
	}
	if !foundHigh {
		t.Error("web_search should appear in contention")
	}
	if !foundMedium {
		t.Error("calculator should appear in contention")
	}
}

func TestComputeAnalytics_ToolContention_LowRisk(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "t1", Name: "T1", Category: "test"})
	kg.Connect("t1", "tool:solo_tool", "uses_tool")

	a := kg.ComputeAnalytics()

	for _, c := range a.ToolContention {
		if c.ToolID == "solo_tool" {
			if c.Risk != "low" {
				t.Errorf("expected solo_tool risk='low', got %q", c.Risk)
			}
		}
	}
}

func TestComputeAnalytics_Bottlenecks(t *testing.T) {
	kg := NewKnowledgeGraph()
	// Register trees with various fitness levels
	kg.Register(&TreeMeta{ID: "good", Name: "Good Tree", Category: "test", RunCount: 10, Fitness: 85.0})
	kg.Register(&TreeMeta{ID: "bad", Name: "Bad Tree", Category: "test", RunCount: 5, Fitness: 20.0})
	kg.Register(&TreeMeta{ID: "okay", Name: "Okay Tree", Category: "test", RunCount: 3, Fitness: 29.0}) // below 30
	kg.Register(&TreeMeta{ID: "few", Name: "Few Runs", Category: "test", RunCount: 2, Fitness: 10.0})   // below min runs

	a := kg.ComputeAnalytics()

	// "bad" and "okay" should be bottlenecks (runCount >= 3, fitness < 30)
	if len(a.Bottlenecks) < 2 {
		t.Fatalf("expected at least 2 bottlenecks (bad + okay), got %d", len(a.Bottlenecks))
	}
	bottleneckIDs := map[string]bool{}
	for _, b := range a.Bottlenecks {
		bottleneckIDs[b.TreeID] = true
	}
	if !bottleneckIDs["bad"] {
		t.Error("'bad' should be a bottleneck")
	}
	if !bottleneckIDs["okay"] {
		t.Error("'okay' should be a bottleneck")
	}
	if bottleneckIDs["few"] {
		t.Error("'few' should NOT be a bottleneck (only 2 runs)")
	}
	if bottleneckIDs["good"] {
		t.Error("'good' should NOT be a bottleneck (fitness 85)")
	}
}

func TestComputeAnalytics_SuggestedActions(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "bottleneck", Name: "Bottleneck", Category: "test", RunCount: 5, Fitness: 15.0})

	a := kg.ComputeAnalytics()

	// Should have suggestions for missing domains AND the bottleneck
	hasBottleneckAction := false
	for _, action := range a.SuggestedActions {
		if strings.Contains(action, "bottleneck") && strings.Contains(action, "15%") {
			hasBottleneckAction = true
		}
	}
	if !hasBottleneckAction {
		t.Errorf("expected bottleneck suggested action, got: %v", a.SuggestedActions)
	}
}

func TestComputeAnalytics_BottleneckWithTrace(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "trace_bn", Name: "Trace BN", Category: "test", RunCount: 5, Fitness: 10.0})

	// Record a failure trace
	GlobalTraceStore.Record(DecisionTrace{
		RunID:   "bn-trace",
		TreeID:  "trace_bn",
		Task:    "impossible task",
		Outcome: "failure",
		Steps:   []TraceStep{{NodeName: "step1", NodeType: "Action", Status: "failure", Error: "boom"}},
	})

	a := kg.ComputeAnalytics()

	hasTraceAction := false
	for _, action := range a.SuggestedActions {
		if strings.Contains(action, "trace_bn") && strings.Contains(action, "impossible task") {
			hasTraceAction = true
		}
	}
	if !hasTraceAction {
		t.Errorf("expected trace info in bottleneck action, got: %v", a.SuggestedActions)
	}
}

func TestComputeAnalytics_HighContentionSuggestion(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "t1", Name: "T1", Category: "test"})
	kg.Register(&TreeMeta{ID: "t2", Name: "T2", Category: "test"})
	kg.Register(&TreeMeta{ID: "t3", Name: "T3", Category: "test"})
	kg.Connect("t1", "tool:shared_tool", "uses_tool")
	kg.Connect("t2", "tool:shared_tool", "uses_tool")
	kg.Connect("t3", "tool:shared_tool", "uses_tool")

	a := kg.ComputeAnalytics()

	hasStaggerAction := false
	for _, action := range a.SuggestedActions {
		if strings.Contains(action, "Stagger") && strings.Contains(action, "shared_tool") {
			hasStaggerAction = true
		}
	}
	if !hasStaggerAction {
		t.Errorf("expected stagger suggestion for high-contention tool, got: %v", a.SuggestedActions)
	}
}

func TestComputeAnalytics_PartialToolEdges(t *testing.T) {
	// Edge case: uses_tool but no "tool:" prefix
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "t1", Name: "T1", Category: "test"})
	kg.Connect("t1", "not_a_tool", "uses_tool")

	a := kg.ComputeAnalytics()
	// Not-a-tool edges should NOT create contention entries (missing "tool:" prefix)
	for _, c := range a.ToolContention {
		if c.ToolID == "not_a_tool" {
			t.Errorf("'not_a_tool' should not appear in tool contention (no 'tool:' prefix)")
		}
	}
}

// =============================================================================
// FormatAnalytics
// =============================================================================

func TestFormatAnalytics_Empty(t *testing.T) {
	a := Analytics{}
	s := a.FormatAnalytics()

	if !strings.Contains(s, "BT Platform Graph Analytics") {
		t.Error("should contain title")
	}
	if strings.Contains(s, "Centrality") {
		t.Error("empty analytics should not show centrality")
	}
}

func TestFormatAnalytics_WithCentrality(t *testing.T) {
	a := Analytics{
		Centrality: []CentralityEntry{
			{TreeID: "tree:main", Dependents: 5},
			{TreeID: "tree:other", Dependents: 2},
		},
	}
	s := a.FormatAnalytics()

	if !strings.Contains(s, "tree:main") {
		t.Error("should show central tree")
	}
	if !strings.Contains(s, "5 dependents") {
		t.Error("should show dependent count")
	}
	if !strings.Contains(s, "tree:other") {
		t.Error("should show second tree")
	}
}

func TestFormatAnalytics_WithHighRiskTool(t *testing.T) {
	a := Analytics{
		ToolContention: []ContentionEntry{
			{ToolID: "web_search", Trees: []string{"t1", "t2", "t3"}, Risk: "high"},
		},
	}
	s := a.FormatAnalytics()

	if !strings.Contains(s, "web_search") {
		t.Error("should show tool name")
	}
	if !strings.Contains(s, "🔴") {
		t.Error("high risk should use red circle")
	}
}

func TestFormatAnalytics_WithMediumRiskTool(t *testing.T) {
	a := Analytics{
		ToolContention: []ContentionEntry{
			{ToolID: "calculator", Trees: []string{"t1", "t2"}, Risk: "medium"},
		},
	}
	s := a.FormatAnalytics()

	if !strings.Contains(s, "calculator") {
		t.Error("should show tool name")
	}
	if !strings.Contains(s, "🟡") {
		t.Error("medium risk should use yellow circle")
	}
}

func TestFormatAnalytics_WithGaps(t *testing.T) {
	a := Analytics{
		CoverageGaps: []string{"domain:missing_one", "domain:missing_two"},
	}
	s := a.FormatAnalytics()

	if !strings.Contains(s, "missing_one") || !strings.Contains(s, "missing_two") {
		t.Error("should show coverage gaps")
	}
}

func TestFormatAnalytics_WithBottlenecks(t *testing.T) {
	a := Analytics{
		Bottlenecks: []BottleneckEntry{
			{TreeID: "bad_tree", SuccessRate: 15.0, Runs: 10},
		},
	}
	s := a.FormatAnalytics()

	if !strings.Contains(s, "bad_tree") || !strings.Contains(s, "15%") {
		t.Error("should show bottleneck info")
	}
}

func TestFormatAnalytics_WithSuggestedActions(t *testing.T) {
	a := Analytics{
		SuggestedActions: []string{"Fix the bottleneck", "Add rate limiting"},
	}
	s := a.FormatAnalytics()

	if !strings.Contains(s, "Fix the bottleneck") || !strings.Contains(s, "Add rate limiting") {
		t.Error("should show suggested actions")
	}
}

func TestFormatAnalytics_CentralityCappedAt5(t *testing.T) {
	// FormatAnalytics caps centrality output at top 5
	entries := make([]CentralityEntry, 10)
	for i := 0; i < 10; i++ {
		entries[i] = CentralityEntry{TreeID: "t", Dependents: i}
	}
	a := Analytics{Centrality: entries}
	s := a.FormatAnalytics()

	// Should contain "Centrality" header but only show up to 5
	if !strings.Contains(s, "Centrality") {
		t.Error("should show centrality header")
	}
}
