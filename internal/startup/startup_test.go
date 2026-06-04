package startup

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// ---------------------------------------------------------------------------
// Helper function tests
// ---------------------------------------------------------------------------

func TestClamp(t *testing.T) {
	tests := []struct {
		name string
		val  float64
		min  float64
		max  float64
		want float64
	}{
		{"value below min", -5.0, 0, 100, 0},
		{"value above max", 150.0, 0, 100, 100},
		{"value in range", 50.0, 0, 100, 50},
		{"exactly min", 0, 0, 100, 0},
		{"exactly max", 100, 0, 100, 100},
		{"negative range below", -20, -10, 10, -10},
		{"negative range in", -5, -10, 10, -5},
		{"zero width", 42, 42, 42, 42},
		{"negative range above", 20, -10, 10, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clamp(tt.val, tt.min, tt.max); got != tt.want {
				t.Errorf("clamp(%v, %v, %v) = %v, want %v", tt.val, tt.min, tt.max, got, tt.want)
			}
		})
	}
}

func TestSafeDiv(t *testing.T) {
	tests := []struct {
		name string
		a, b float64
		want float64
	}{
		{"normal division", 10, 2, 5},
		{"zero numerator", 0, 5, 0},
		{"zero denominator", 10, 0, 0},
		{"negative numerator", -10, 2, -5},
		{"negative denominator", 10, -2, -5},
		{"fractional", 3, 2, 1.5},
		{"both zero", 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := safeDiv(tt.a, tt.b); got != tt.want {
				t.Errorf("safeDiv(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tree structure tests — validate that role trees have the right structure
// without needing an LLM.
// ---------------------------------------------------------------------------

// checkTreeBasics validates common tree properties: non-nil, Sequence root,
// non-empty children, PreGate present.
func checkTreeBasics(t *testing.T, tree *evolution.SerializableNode, name string) {
	t.Helper()
	if tree == nil {
		t.Fatalf("%s tree is nil", name)
	}
	if tree.Type != "Sequence" {
		t.Errorf("%s tree root type = %q, want %q", name, tree.Type, "Sequence")
	}
	if len(tree.Children) == 0 {
		t.Errorf("%s tree has no children", name)
	}

	// First child should be PreGate (Sequence)
	hasUpdateBT := false
	if len(tree.Children) > 0 {
		preGate := tree.Children[0]
		if preGate.Type != "Sequence" || preGate.Name != "PreGate" {
			t.Errorf("%s tree first child = %s/%s, want Sequence/PreGate", name, preGate.Type, preGate.Name)
		}
		// PreGate should have condition + action children
		if len(preGate.Children) < 2 {
			t.Errorf("%s PreGate has only %d children, want >= 2", name, len(preGate.Children))
		}

		// Check if last child is UpdateBehaviorTree
		last := tree.Children[len(tree.Children)-1]
		if last.Type == "Action" && last.Name == "UpdateBehaviorTree" {
			hasUpdateBT = true
		}
	}

	// CEO/CTO/PM trees end with Reflect (a Sequence with no OutcomeSelector/UpdateBehaviorTree pattern)
	// They use Reflect action directly as the last work node.
	if !hasUpdateBT {
		// Check for Reflect as final work node
		workChildren := 0
		for _, c := range tree.Children {
			if c.Type != "Sequence" || c.Name != "PreGate" {
				workChildren++
			}
		}
		if workChildren < 2 {
			t.Errorf("%s tree has only %d non-PreGate children, want at least 2", name, workChildren)
		}
	}
}

func TestCEOTree_Structure(t *testing.T) {
	tree := CEOTree()
	checkTreeBasics(t, tree, "CEO")

	// CEO tree has exactly 5 children beyond PreGate + UpdateBehaviorTree
	// PreGate, ReviewMetrics, StrategicDecisions, QuarterGoals, Vision, Reflect, UpdateBehaviorTree
	if len(tree.Children) < 6 {
		t.Errorf("CEOTree has %d children, expected at least 6 (PreGate + 4 chain + Reflect + UpdateBehaviorTree)", len(tree.Children))
	}

	// Check for chain action nodes (llm_call type)
	foundChain := false
	for _, c := range tree.Children {
		if c.Type == "ChainAction" {
			foundChain = true
			break
		}
	}
	if !foundChain {
		t.Error("CEOTree should have at least one ChainAction node")
	}
}

func TestCTOTree_Structure(t *testing.T) {
	tree := CTOTree()
	checkTreeBasics(t, tree, "CTO")

	foundChain := false
	foundReflect := false
	for _, c := range tree.Children {
		if c.Type == "ChainAction" {
			foundChain = true
		}
		if c.Type == "Action" && c.Name == "Reflect" {
			foundReflect = true
		}
	}
	if !foundChain {
		t.Error("CTOTree should have at least one ChainAction node")
	}
	if !foundReflect {
		t.Error("CTOTree should have an Action/Reflect node")
	}
}

func TestPMTree_Structure(t *testing.T) {
	tree := PMTree()
	checkTreeBasics(t, tree, "PM")

	foundChain := false
	foundCompetitive := false
	for _, c := range tree.Children {
		if c.Type == "ChainAction" {
			foundChain = true
			if strings.Contains(c.Name, "competitive") {
				foundCompetitive = true
			}
		}
	}
	if !foundChain {
		t.Error("PMTree should have at least one ChainAction node")
	}
	if !foundCompetitive {
		t.Error("PMTree should have a competitive analysis chain action")
	}
}

func TestEngineerTree_Structure(t *testing.T) {
	tree := EngineerTree()
	checkTreeBasics(t, tree, "Engineer")

	// Engineer tree has OutcomeSelector between Reflect and UpdateBehaviorTree
	foundSelector := false
	for _, c := range tree.Children {
		if c.Type == "Selector" {
			foundSelector = true
			break
		}
	}
	if !foundSelector {
		t.Error("EngineerTree should have a Selector (OutcomeSelector)")
	}
}

func TestMarketingTree_Structure(t *testing.T) {
	tree := MarketingTree()
	checkTreeBasics(t, tree, "Marketing")

	foundChain := false
	for _, c := range tree.Children {
		if c.Type == "ChainAction" {
			foundChain = true
			break
		}
	}
	if !foundChain {
		t.Error("MarketingTree should have at least one ChainAction node")
	}
}

func TestSalesTree_Structure(t *testing.T) {
	tree := SalesTree()
	checkTreeBasics(t, tree, "Sales")

	foundChains := 0
	for _, c := range tree.Children {
		if c.Type == "ChainAction" {
			foundChains++
		}
	}
	if foundChains < 3 {
		t.Errorf("SalesTree has %d ChainAction nodes, expected at least 3", foundChains)
	}
}

// ---------------------------------------------------------------------------
// Registry tests
// ---------------------------------------------------------------------------

func TestRoles(t *testing.T) {
	roles := Roles()
	if roles == nil {
		t.Fatal("Roles() returned nil")
	}
	expected := []string{"ceo", "cto", "pm"}
	for _, name := range expected {
		tree, ok := roles[name]
		if !ok {
			t.Errorf("Roles() missing key %q", name)
			continue
		}
		if tree == nil {
			t.Errorf("Roles()[%q] is nil", name)
		}
	}
	if len(roles) != len(expected) {
		t.Errorf("Roles() has %d entries, want %d", len(roles), len(expected))
	}
}

func TestStartupTrees(t *testing.T) {
	trees := StartupTrees()
	if trees == nil {
		t.Fatal("StartupTrees() returned nil")
	}
	expected := []string{"engineer", "marketing", "sales"}
	for _, name := range expected {
		tree, ok := trees[name]
		if !ok {
			t.Errorf("StartupTrees() missing key %q", name)
			continue
		}
		if tree == nil {
			t.Errorf("StartupTrees()[%q] is nil", name)
		}
	}
	if len(trees) != len(expected) {
		t.Errorf("StartupTrees() has %d entries, want %d", len(trees), len(expected))
	}
}

// ---------------------------------------------------------------------------
// Summary test
// ---------------------------------------------------------------------------

func TestSummary(t *testing.T) {
	company := NewDefaultCompany()
	orch := NewOrchestrator(company, engine.NewMockLLM())
	summary := orch.Summary()
	if summary == "" {
		t.Fatal("Summary() returned empty string")
	}

	// Should mention company name
	if !strings.Contains(summary, "HermesAI") {
		t.Errorf("Summary should mention company name 'HermesAI', got: %q[:50]", summary)
	}

	// Should include key metrics
	shouldContain := []string{"MRR", "ARR", "Cash", "Burn", "Runway", "Users", "Tech Debt"}
	for _, s := range shouldContain {
		if !strings.Contains(summary, s) {
			t.Errorf("Summary should contain %q", s)
		}
	}
}

// ---------------------------------------------------------------------------
// RunYear test — full year simulation
// ---------------------------------------------------------------------------

func TestRunYear(t *testing.T) {
	company := NewDefaultCompany()
	orch := NewOrchestrator(company, engine.NewMockLLM())

	results := orch.RunYear()
	if results == nil {
		t.Fatal("RunYear() returned nil")
	}
	if len(results) != 4 {
		t.Errorf("RunYear() returned %d quarters, want 4", len(results))
	}

	// Each quarter should be non-nil
	for i, qr := range results {
		if qr.Revenue <= 0 {
			t.Errorf("Quarter %d revenue = $%.0f, want > 0", i+1, qr.Revenue)
		}
		if qr.Quarter != i+1 {
			t.Errorf("Quarter %d .Quarter = %d, want %d", i, qr.Quarter, i+1)
		}
	}

	// After a full year, sprints should have advanced
	if company.CurrentSprint <= 12 {
		t.Errorf("After a year, current sprint should be > 12, got %d", company.CurrentSprint)
	}

	// Company state should have evolved
	if company.MRR <= 18000 {
		t.Errorf("MRR should have grown from $18000, got $%.0f", company.MRR)
	}
}

// ---------------------------------------------------------------------------
// RunSprint edge cases
// ---------------------------------------------------------------------------

func TestRunSprint_ProducesValidResult(t *testing.T) {
	company := NewDefaultCompany()
	orch := NewOrchestrator(company, engine.NewMockLLM())

	result := orch.RunSprint()
	if result == nil {
		t.Fatal("RunSprint returned nil")
	}
	if result.SprintNum != company.CurrentSprint {
		t.Errorf("Sprint number = %d, want %d", result.SprintNum, company.CurrentSprint)
	}
	if result.Goal == "" {
		t.Error("Sprint goal should not be empty")
	}
	// Sprinthistory should have been appended
	if len(orch.SprintHistory) != 1 {
		t.Errorf("SprintHistory length = %d, want 1", len(orch.SprintHistory))
	}
}

func TestRunSprint_CompanyMetricsUpdated(t *testing.T) {
	company := NewDefaultCompany()
	initialMRR := company.MRR
	initialUsers := company.Users
	initialCash := company.CashInBank

	orch := NewOrchestrator(company, engine.NewMockLLM())
	_ = orch.RunSprint()

	// MRR should increase from sales
	if company.MRR <= initialMRR {
		t.Errorf("MRR should increase after sprint: was $%.0f, now $%.0f", initialMRR, company.MRR)
	}
	// Users should increase from marketing
	if company.Users <= initialUsers {
		t.Errorf("Users should increase after sprint: was %d, now %d", initialUsers, company.Users)
	}
	// Cash should decrease (burn)
	if company.CashInBank >= initialCash {
		t.Errorf("Cash should decrease after sprint: was $%.0f, now $%.0f", initialCash, company.CashInBank)
	}
}

// ---------------------------------------------------------------------------
// RunSprint with zero-staff edge cases
// ---------------------------------------------------------------------------

func TestNewDefaultCompany_Values(t *testing.T) {
	c := NewDefaultCompany()
	if c.Name != "HermesAI" {
		t.Errorf("Company name = %q, want %q", c.Name, "HermesAI")
	}
	if c.MRR != 18000 {
		t.Errorf("MRR = $%.0f, want $%.0f", c.MRR, float64(18000))
	}
	if c.Users != 1200 {
		t.Errorf("Users = %d, want %d", c.Users, 1200)
	}
	if c.Runway != 14 {
		t.Errorf("Runway = %d months, want %d", c.Runway, 14)
	}
	if c.Engineers != 4 {
		t.Errorf("Engineers = %d, want %d", c.Engineers, 4)
	}
	if c.SalesPeople != 1 {
		t.Errorf("SalesPeople = %d, want %d", c.SalesPeople, 1)
	}
	if c.MarketingStaff != 1 {
		t.Errorf("MarketingStaff = %d, want %d", c.MarketingStaff, 1)
	}
}

// ---------------------------------------------------------------------------
// History tracking
// ---------------------------------------------------------------------------

func TestSprintAndQuarterHistory(t *testing.T) {
	company := NewDefaultCompany()
	orch := NewOrchestrator(company, engine.NewMockLLM())

	// Run one quarter (12 sprints)
	_ = orch.RunQuarter()

	if len(orch.SprintHistory) != 12 {
		t.Errorf("Expected 12 sprints in history, got %d", len(orch.SprintHistory))
	}
	if len(orch.QuarterHistory) != 1 {
		t.Errorf("Expected 1 quarter in history, got %d", len(orch.QuarterHistory))
	}

	// Verify sprint numbers are valid
	for i, sprint := range orch.SprintHistory {
		if sprint.SprintNum == 0 {
			t.Errorf("Sprint %d has SprintNum = 0", i)
		}
	}
}

func TestRunQuarter_MetricsValid(t *testing.T) {
	company := NewDefaultCompany()
	orch := NewOrchestrator(company, engine.NewMockLLM())

	qr := orch.RunQuarter()
	if qr.Revenue <= 0 {
		t.Errorf("Quarter revenue = $%.0f, want > 0", qr.Revenue)
	}
	// Growth can be negative if mock LLM causes tree failures, but revenue should always be positive
	if qr.Quarter != 1 {
		t.Errorf("Quarter number = %d, want 1", qr.Quarter)
	}
}

// ---------------------------------------------------------------------------
// Orchard value semantics — no nil panics
// ---------------------------------------------------------------------------

func TestNilSafeDiv_NoPanic(t *testing.T) {
	// safeDiv should never panic
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("safeDiv panicked: %v", r)
		}
	}()
	_ = safeDiv(1, 0)
	_ = safeDiv(0, 1)
	_ = safeDiv(0, 0)
}

func TestNilClamp_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("clamp panicked: %v", r)
		}
	}()
	_ = clamp(0, 0, 0)
	_ = clamp(-1e10, -1e10, 1e10)
	_ = clamp(1e10, -1e10, 1e10)
}
