package benchmark

import (
	"fmt"
	"testing"

	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/finance"
	"github.com/nico/go-bt-evolve/internal/research"
)

func TestBFCLV3_MultiTurn_Basic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Ollama-dependent test in short mode")
	}
	// Use 2 base entries from BuiltinBFCLV3
	all := BuiltinBFCLV3()
	var baseEntries []BFCLV3Entry
	for _, e := range all {
		if e.Category == "multi_turn_base" {
			baseEntries = append(baseEntries, e)
		}
	}
	if len(baseEntries) < 2 {
		t.Fatalf("expected at least 2 base entries, got %d", len(baseEntries))
	}
	baseEntries = baseEntries[:2]

	// Run against GoDeveloperTree
	tree := evolution.GoDeveloperTree()
	llmClient := DefaultLLM()
	metrics := EvaluateBFCLV3(tree, baseEntries, llmClient)

	fmt.Printf("\nBFCL V3 Multi-Turn Basic: %d/%d correct turns (%.0f%%), %d/%d fully correct (%.0f%%)\n",
		metrics.CorrectTurns, metrics.TotalTurns, metrics.TurnAccuracy*100,
		metrics.FullyCorrect, metrics.TotalEntries, metrics.MultiStepSuccessRate*100)

	for _, r := range metrics.Results {
		s := "✗"
		if r.AllCorrect {
			s = "✓"
		}
		fmt.Printf("  %s %-20s [%s]: %d/%d turns correct\n",
			s, r.EntryID, r.Category, r.CorrectInTurns, r.NumTurns)
	}

	if metrics.TurnAccuracy < 0.3 {
		t.Errorf("BFCL V3 multi-turn base accuracy too low: %.0f%% (expected > 30%%)", metrics.TurnAccuracy*100)
	}
}

func TestBFCLV3_MultiTurn_Composite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Ollama-dependent test in short mode")
	}
	// Use 2 composite entries
	all := BuiltinBFCLV3()
	var compEntries []BFCLV3Entry
	for _, e := range all {
		if e.Category == "multi_turn_composite" {
			compEntries = append(compEntries, e)
		}
	}
	if len(compEntries) < 2 {
		t.Fatalf("expected at least 2 composite entries, got %d", len(compEntries))
	}
	compEntries = compEntries[:2]

	// Run against CodeReviewTree for first entry, Finance tree for second
	// Composite-001 uses SecurityReview+StyleReview → CodeReviewTree
	// Composite-002 uses ReconPath → use a general tree
	var allResults []BFCLV3Result
	totalCorrect := 0
	totalTurns := 0
	totalFullyCorrect := 0

	// First entry: code review domain
	{
		tree := domains.CodeReviewTree()
		llmClient := DefaultLLM()
		metrics := EvaluateBFCLV3(tree, compEntries[:1], llmClient)
		allResults = append(allResults, metrics.Results...)
		totalCorrect += metrics.CorrectTurns
		totalTurns += metrics.TotalTurns
		totalFullyCorrect += metrics.FullyCorrect
	}

	// Second entry: finance domain
	{
		tree := finance.PitchAgentTree()
		llmClient := DefaultLLM()
		metrics := EvaluateBFCLV3(tree, compEntries[1:], llmClient)
		allResults = append(allResults, metrics.Results...)
		totalCorrect += metrics.CorrectTurns
		totalTurns += metrics.TotalTurns
		totalFullyCorrect += metrics.FullyCorrect
	}

	turnAcc := 0.0
	if totalTurns > 0 {
		turnAcc = float64(totalCorrect) / float64(totalTurns)
	}

	fmt.Printf("\nBFCL V3 Multi-Turn Composite: %d/%d correct turns (%.0f%%), %d/%d fully correct\n",
		totalCorrect, totalTurns, turnAcc*100,
		totalFullyCorrect, len(compEntries))

	for _, r := range allResults {
		s := "✗"
		if r.AllCorrect {
			s = "✓"
		}
		fmt.Printf("  %s %-20s: %d/%d turns correct\n",
			s, r.EntryID, r.CorrectInTurns, r.NumTurns)
	}

	// Composite tasks are harder; just verify the evaluation runs
	if turnAcc == 0 && totalTurns > 0 {
		t.Log("composite multi-turn accuracy is 0% — expected for complex tasks with real LLM")
	}
}

func TestBFCLV3_LongContext(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Ollama-dependent test in short mode")
	}
	// Use long_context entries to verify tree handles long conversations
	all := BuiltinBFCLV3()
	var longCtxEntries []BFCLV3Entry
	for _, e := range all {
		if e.Category == "multi_turn_long_context" {
			longCtxEntries = append(longCtxEntries, e)
		}
	}
	if len(longCtxEntries) == 0 {
		t.Fatal("expected at least 1 long_context entry")
	}

	// Run against DeepResearchTree (handles research-type long context)
	tree := research.DeepResearchTree()
	llmClient := DefaultLLM()
	metrics := EvaluateBFCLV3(tree, longCtxEntries, llmClient)

	fmt.Printf("\nBFCL V3 Long Context: %d/%d correct turns (%.0f%%), %d/%d fully correct (%.0f%%)\n",
		metrics.CorrectTurns, metrics.TotalTurns, metrics.TurnAccuracy*100,
		metrics.FullyCorrect, metrics.TotalEntries, metrics.MultiStepSuccessRate*100)

	for _, r := range metrics.Results {
		fmt.Printf("  %-20s: %d/%d turns correct, all=%v\n",
			r.EntryID, r.CorrectInTurns, r.NumTurns, r.AllCorrect)
	}

	// Verify that the tree didn't crash on long multi-turn input
	if metrics.TotalTurns == 0 {
		t.Error("no turns were processed for long context entries")
	}
	// All entries should have been processed
	if metrics.TotalEntries != len(longCtxEntries) {
		t.Errorf("expected %d entries processed, got %d", len(longCtxEntries), metrics.TotalEntries)
	}
}

func TestSWEVerified_Evaluation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Ollama-dependent test in short mode")
	}
	// Use a sample of 5 entries from BuiltinSWEVerifiedSample
	all := BuiltinSWEVerifiedSample()
	entries := all[:minInt(5, len(all))]

	tree := evolution.GoDeveloperTree()
	llmClient := DefaultLLM()
	metrics := EvaluateSWEVerified(tree, entries, llmClient)

	fmt.Printf("\nSWE-bench Verified: %d/%d resolved (%.0f%% resolve rate)\n",
		metrics.Resolved, metrics.TotalEntries, metrics.ResolveRate*100)

	for _, r := range metrics.Results {
		s := "✗"
		if r.Resolved {
			s = "✓"
		}
		fmt.Printf("  %s %-35s [%s]: outcome=%s, output=%d chars\n",
			s, r.Entry.InstanceID, r.Entry.Repo, r.Outcome, len(r.Output))
	}

	// No hard threshold — success depends on LLM quality
	// Just verify the evaluation runs without error and processes all entries
	if metrics.TotalEntries != len(entries) {
		t.Errorf("expected %d entries processed, got %d", len(entries), metrics.TotalEntries)
	}

	t.Logf("resolve rate: %.0f%% (depends on LLM, no hard threshold enforced)", metrics.ResolveRate*100)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
