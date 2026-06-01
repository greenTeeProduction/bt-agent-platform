package startup

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/llm"
)

// mockLLM returns a mock that produces fast deterministic output for tests.
// Real Ollama on Jetson takes 2-4 min per sprint call — use mock in tests.
func testLLM() llm.LLM {
	return &mockLLM{}
}

type mockLLM struct{}

func (m *mockLLM) GenerateCtx(ctx context.Context, prompt string) (string, error) {
	return m.Generate(prompt)
}
func (m *mockLLM) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) {
	return m.Generate(prompt)
}

func (m *mockLLM) Generate(prompt string) (string, error) {
	return fmt.Sprintf("Mock response for: %.50s...", prompt), nil
}
func (m *mockLLM) AnalyzeComplexity(task string) string { return "medium" }
func (m *mockLLM) GeneratePlan(task, complexity string) string {
	return "Execute the plan step by step"
}
func (m *mockLLM) Reflect(task, outcome, plan string) (string, string) {
	return "completed task", "nothing to improve"
}

func TestCompanySimulation_Sprint(t *testing.T) {
	company := NewDefaultCompany()
	orch := NewOrchestrator(company, testLLM())

	sprint := orch.RunSprint()
	if sprint == nil {
		t.Fatal("sprint returned nil")
	}

	t.Logf("Sprint %d: %s", sprint.SprintNum, sprint.Goal)
	t.Logf("  Completed: %v", sprint.Completed)
	t.Logf("  Deferred: %v", sprint.Deferred)
	t.Logf("  Bugs Fixed: %d", sprint.BugsFixed)
	t.Logf("  Velocity: %.1f", sprint.Velocity)
	t.Logf("  Tech Debt Delta: %.1f", sprint.TechDebtDelta)
	t.Logf("  After Sprint — MRR: $%.0f, Users: %d, Cash: $%.0f, Runway: %dmo",
		company.MRR, company.Users, company.CashInBank, company.Runway)
}

func TestCompanySimulation_Quarter(t *testing.T) {
	company := NewDefaultCompany()
	orch := NewOrchestrator(company, testLLM())

	quarter := orch.RunQuarter()
	if quarter == nil {
		t.Fatal("quarter returned nil")
	}

	t.Logf("=== Q%d Results ===", quarter.Quarter)
	t.Logf("  Revenue: $%.0f (%.1f%% growth)", quarter.Revenue, quarter.Growth)
	t.Logf("  Users Added: %d, Churn: %.1f%%", quarter.UsersAdded, quarter.Churn*100)
	t.Logf("  Cash Burned: $%.0f", quarter.CashBurned)
	t.Logf("  Highlights: %v", quarter.Highlights)
	t.Logf("  Lowlights: %v", quarter.Lowlights)
	t.Logf("  OKR Progress: %v", quarter.OKRProgress)
}

func TestCompanySimulation_Summary(t *testing.T) {
	company := NewDefaultCompany()
	summary := companySummary(company)
	if len(summary) < 50 {
		t.Error("summary too short")
	}
	t.Logf("\n%s", summary)
}

func companySummary(c *CompanyState) string {
	return fmt.Sprintf("HermesAI — %s | Stage: %s | Round: %s | Team: %d | MRR: $%.0f | Runway: %dmo",
		c.Industry, c.ProductStage, c.FundingRound, c.TeamSize, c.MRR, c.Runway)
}
