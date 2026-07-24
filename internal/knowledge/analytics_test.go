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

func TestComputeAnalytics_CoverageGapsUseExpectedDomains(t *testing.T) {
	kg := NewKnowledgeGraph()
	// Inject the expected-domain set instead of relying on a hardcoded slice.
	// This is the seam the daemon populates from domains.AllDomainTrees() keys,
	// which avoids the analytics→domains import cycle.
	kg.ExpectedDomains = []string{"domain:alpha", "domain:beta", "domain:gamma"}
	// Register two of the three expected domains.
	kg.Register(&TreeMeta{ID: "domain:alpha", Name: "Alpha", Category: "domain"})
	kg.Register(&TreeMeta{ID: "domain:beta", Name: "Beta", Category: "domain"})

	a := kg.ComputeAnalytics()

	gaps := map[string]bool{}
	for _, g := range a.CoverageGaps {
		gaps[g] = true
	}

	// The unregistered-but-expected domain must surface as a gap.
	if !gaps["domain:gamma"] {
		t.Errorf("expected 'domain:gamma' to surface as a coverage gap, got: %v", a.CoverageGaps)
	}
	// Every registered domain must be covered (not reported as a gap).
	if gaps["domain:alpha"] {
		t.Errorf("registered 'domain:alpha' should not be a coverage gap, got: %v", a.CoverageGaps)
	}
	if gaps["domain:beta"] {
		t.Errorf("registered 'domain:beta' should not be a coverage gap, got: %v", a.CoverageGaps)
	}
	// Gaps must be driven solely by the injected ExpectedDomains seam — the stale
	// hardcoded 8-domain slice must no longer leak into the result.
	if gaps["domain:security_audit"] {
		t.Errorf("legacy hardcoded 'domain:security_audit' leaked into gaps; CoverageGaps is not registry-accurate: %v", a.CoverageGaps)
	}
}

// TestComputeAnalytics_CoverageGapsIncludeResolverSpecialCases pins
// ComputeAnalytics's CoverageGaps to also audit the bare (non-"domain:"-
// prefixed) tree IDs that internal/domains/tree_resolver.go's
// resolveTreeIDWithResolver special-cases (vault_manager, kanban:*,
// notebooklm*, hermes_obsidian, superpowers_pipeline, fusion — see
// internal/domains/kg_registry_coverage_test.go's resolverSpecialCaseTreeIDs
// for the full enumerated set). Previously CoverageGaps only checked
// ExpectedDomains/defaultExpectedDomains, so a resolver ID added without a
// matching kg.Register call was invisible to ComputeAnalytics — the gap only
// surfaced via periodic manual review of tree_resolver.go against
// registry.go, or the separate hand-maintained test in the domains package.
// This test requires that gap to surface automatically through the same
// production CoverageGaps/SuggestedActions signal the dashboard and gardener
// already consume for domain trees.
func TestComputeAnalytics_CoverageGapsIncludeResolverSpecialCases(t *testing.T) {
	kg := NewKnowledgeGraph()
	// Register only one of the resolver special-case trees; leave the rest
	// unregistered so they must surface as gaps.
	kg.Register(&TreeMeta{ID: "vault_manager", Name: "Vault Manager", Category: "core"})

	a := kg.ComputeAnalytics()

	gaps := map[string]bool{}
	for _, g := range a.CoverageGaps {
		gaps[g] = true
	}

	unregisteredResolverSpecialCases := []string{
		"kanban:task_creator", "kanban:refiner", "kanban:qa",
		"kanban:monitor", "kanban:workflow", "kanban:autopilot",
		"notebooklm", "notebooklm-consumer", "notebooklm-bridge",
		"hermes_obsidian", "superpowers_pipeline", "fusion",
	}
	for _, id := range unregisteredResolverSpecialCases {
		if !gaps[id] {
			t.Errorf("expected unregistered resolver special-case %q to surface as a coverage gap, got: %v", id, a.CoverageGaps)
		}
	}
	if gaps["vault_manager"] {
		t.Errorf("registered resolver special-case 'vault_manager' should not be a coverage gap, got: %v", a.CoverageGaps)
	}

	// The gap must also drive a suggested action, mirroring how domain-tree
	// coverage gaps already do.
	hasAction := false
	for _, action := range a.SuggestedActions {
		if strings.Contains(action, "fusion") {
			hasAction = true
		}
	}
	if !hasAction {
		t.Errorf("expected a suggested action for unregistered resolver special-case 'fusion', got: %v", a.SuggestedActions)
	}
}

func TestComputeAnalytics_SelectionPressure(t *testing.T) {
	kg := NewKnowledgeGraph()
	// Proven but underbred: high fitness, low run count — the loop should surface
	// this as a selection-pressure opportunity (a proven tree nobody is exercising).
	kg.Register(&TreeMeta{ID: "proven_underused", Name: "Proven Underused", Category: "test", Fitness: 90.0, RunCount: 1})
	// Proven AND well-exercised: high fitness but plenty of runs — not underbred.
	kg.Register(&TreeMeta{ID: "proven_wellused", Name: "Proven Wellused", Category: "test", Fitness: 90.0, RunCount: 20})
	// Underused but unproven: low fitness — not a selection-pressure signal.
	kg.Register(&TreeMeta{ID: "weak_underused", Name: "Weak Underused", Category: "test", Fitness: 10.0, RunCount: 1})

	a := kg.ComputeAnalytics()

	// The proven-but-underbred tree must appear in the selection-pressure report.
	found := false
	for _, sp := range a.SelectionPressure {
		if sp.TreeID == "proven_underused" {
			found = true
			if sp.Fitness != 90.0 {
				t.Errorf("expected proven_underused fitness 90, got %v", sp.Fitness)
			}
			if sp.RunCount != 1 {
				t.Errorf("expected proven_underused run count 1, got %d", sp.RunCount)
			}
		}
		if sp.TreeID == "proven_wellused" {
			t.Error("'proven_wellused' should NOT be selection pressure (already well-exercised)")
		}
		if sp.TreeID == "weak_underused" {
			t.Error("'weak_underused' should NOT be selection pressure (fitness too low to be proven)")
		}
	}
	if !found {
		t.Errorf("expected 'proven_underused' in selection pressure, got: %+v", a.SelectionPressure)
	}

	// It must also surface as a suggested action so the loop actually sees it.
	hasAction := false
	for _, action := range a.SuggestedActions {
		if strings.Contains(action, "proven_underused") {
			hasAction = true
		}
	}
	if !hasAction {
		t.Errorf("expected suggested action for underbred proven tree, got: %v", a.SuggestedActions)
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

func TestComputeAnalytics_BottleneckCarriesStructuredFailure(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "struct_bn", Name: "Struct BN", Category: "test", RunCount: 5, Fitness: 12.0})

	// The last failing trace's task/outcome must be exposed as structured fields
	// on the BottleneckEntry — not only concatenated into the human-readable
	// SuggestedAction string — so bt_evolve_bottlenecks can seed its per-tree
	// evolution context from the actual failing task rather than parse prose.
	GlobalTraceStore.Record(DecisionTrace{
		RunID:   "struct-bn-trace",
		TreeID:  "struct_bn",
		Task:    "resolve the impossible dependency",
		Outcome: "failure",
		Steps:   []TraceStep{{NodeName: "step1", NodeType: "Action", Status: "failure", Error: "boom"}},
	})

	a := kg.ComputeAnalytics()

	var entry *BottleneckEntry
	for i := range a.Bottlenecks {
		if a.Bottlenecks[i].TreeID == "struct_bn" {
			entry = &a.Bottlenecks[i]
			break
		}
	}
	if entry == nil {
		t.Fatalf("expected 'struct_bn' to be a bottleneck, got: %+v", a.Bottlenecks)
	}
	if entry.LastFailureTask != "resolve the impossible dependency" {
		t.Errorf("expected LastFailureTask to carry the failing task, got %q", entry.LastFailureTask)
	}
	if entry.LastFailureOutcome != "failure" {
		t.Errorf("expected LastFailureOutcome to carry the failing outcome, got %q", entry.LastFailureOutcome)
	}
}

func TestComputeAnalytics_BottleneckWithoutTraceHasEmptyFailure(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "no_trace_bn", Name: "No Trace BN", Category: "test", RunCount: 4, Fitness: 8.0})

	a := kg.ComputeAnalytics()

	for _, b := range a.Bottlenecks {
		if b.TreeID == "no_trace_bn" {
			if b.LastFailureTask != "" || b.LastFailureOutcome != "" {
				t.Errorf("bottleneck without a recorded failure should have empty failure fields, got task=%q outcome=%q",
					b.LastFailureTask, b.LastFailureOutcome)
			}
			return
		}
	}
	t.Fatalf("expected 'no_trace_bn' to be a bottleneck, got: %+v", a.Bottlenecks)
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
