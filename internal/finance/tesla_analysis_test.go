package finance

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
)

func TestTeslaFullAnalysis(t *testing.T) {
	if testing.Short() { t.Skip("skipping Ollama-dependent test in short mode") }
	client, err := llm.NewClient(llm.DefaultConfig())
	if err != nil {
		t.Skipf("Ollama unavailable: %v", err)
		return
	}

	outPath := filepath.Join(t.TempDir(), "tesla_analysis.txt")
	outFile, err := os.Create(outPath)
	if err != nil {
		t.Fatalf("failed to create output file: %v", err)
	}
	defer outFile.Close()

	task := "Analyze Tesla (TSLA) comprehensively: current stock price and market cap, last 4 quarters earnings (revenue, EPS, margins), key growth drivers (EV deliveries, energy storage, FSD/AI), competitive position vs BYD/Ford/GM/Rivian, balance sheet (cash, debt, FCF), valuation (P/E, EV/EBITDA vs auto industry), risks (China, regulatory, competition, Musk concentration), bull/bear cases with price targets, and investment recommendation with conviction level."

	type treeRun struct {
		Name string
		Desc string
	}

	runs := []treeRun{
		{"pitch_agent", "Investment Pitch"},
		{"earnings_reviewer", "Earnings Review"},
		{"market_researcher", "Market & Competition"},
		{"model_builder", "Financial Model"},
		{"valuation_reviewer", "Valuation Analysis"},
	}

	for _, r := range runs {
		tree, ok := AllFinanceTrees()[r.Name]
		if !ok {
			continue
		}
		fmt.Fprintf(outFile, "\n═══════════════════════════════════════\n")
		fmt.Fprintf(outFile, "  %s: %s\n", r.Name, r.Desc)
		fmt.Fprintf(outFile, "═══════════════════════════════════════\n")

		bb := &engine.Blackboard{
			Task: task,
			LLM:  client,
		}
		bt := engine.BuildTree(tree, bb)
		output := engine.RunTask(bb, bt)

		fmt.Fprintf(outFile, "Outcome: %s\n\n", bb.Outcome)
		fmt.Fprintf(outFile, "%s\n", output)
	}
	fmt.Fprintf(outFile, "\n--- END ---\n")
	t.Logf("Results written to %s", outPath)
}

func trunc(s string, n int) string {
	if len(s) <= n { return s }
	return s[:n] + "..."
}

func TestTeslaPitchAgent(t *testing.T) {
	if testing.Short() { t.Skip("skipping Ollama-dependent test in short mode") }
	client, err := llm.NewClient(llm.DefaultConfig())
	if err != nil {
		t.Skipf("Ollama unavailable: %v", err)
	}
	tree := PitchAgentTree()
	bb := &engine.Blackboard{
		Task: "Create an investment pitch for Tesla (TSLA). Include: business overview, market opportunity in EVs/energy/AI, financial highlights, competitive moat, key catalysts for 2025-2026, risks, valuation multiples vs peers, and a 12-month price target with conviction level (high/medium/low).",
		LLM:  client,
	}
	bt := engine.BuildTree(tree, bb)
	output := engine.RunTask(bb, bt)
	t.Logf("Outcome: %s | Plan: %s", bb.Outcome, trunc(bb.Plan, 150))
	t.Logf("Result (%d chars):\n%s", len(output), output)
}

func TestTeslaEarnings(t *testing.T) {
	if testing.Short() { t.Skip("skipping Ollama-dependent test in short mode") }
	runTeslaTree(t, "earnings_reviewer", EarningsReviewerTree())
}
func TestTeslaMarket(t *testing.T) {
	if testing.Short() { t.Skip("skipping Ollama-dependent test in short mode") }
	runTeslaTree(t, "market_researcher", MarketResearcherTree())
}
func TestTeslaModel(t *testing.T) {
	if testing.Short() { t.Skip("skipping Ollama-dependent test in short mode") }
	runTeslaTree(t, "model_builder", ModelBuilderTree())
}
func TestTeslaValuation(t *testing.T) {
	if testing.Short() { t.Skip("skipping Ollama-dependent test in short mode") }
	runTeslaTree(t, "valuation_reviewer", ValuationReviewerTree())
}

func runTeslaTree(t *testing.T, name string, tree *evolution.SerializableNode) {
	t.Helper()
	client, err := llm.NewClient(llm.DefaultConfig())
	if err != nil { t.Skipf("Ollama unavailable: %v", err); return }
	bb := &engine.Blackboard{
		Task: "Analyze Tesla (TSLA): current stock price, market cap, last 4 quarters earnings (revenue, EPS, margins), EV deliveries growth, energy storage, FSD/AI progress, competitive position vs BYD/Ford/GM/Rivian, balance sheet (cash, debt, FCF), valuation (P/E, EV/EBITDA vs auto), risks (China, regulatory, competition, Musk concentration), bull/bear cases with price targets, investment recommendation.",
		LLM:  client,
	}
	bt := engine.BuildTree(tree, bb)
	output := engine.RunTask(bb, bt)
	t.Logf("%s | Outcome: %s\n%s", name, bb.Outcome, output)
}
