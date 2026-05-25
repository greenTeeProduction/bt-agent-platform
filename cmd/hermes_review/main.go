package main

import (
	"fmt"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/thinktank"
)

func main() {
	client, err := llm.NewClient(llm.DefaultConfig())
	if err != nil { fmt.Println("Ollama unavailable"); return }

	tt := thinktank.NewThinkTank("Hermes Review Council", 
		"Review the current Hermes Agent setup and recommend improvements. Analyze: architecture, 24 MCP tools, 38 behavior trees, 10 chain types, knowledge graph, gardener evolution, Stockfish algorithms, thinktank/startup simulations, qwen3.6 model performance on Jetson ARM64. Identify strengths, weaknesses, risks, and top 5 actionable improvements.")

	orch := thinktank.NewOrchestrator(tt, client)
	
	fmt.Println("═══════════════════════════════════════════")
	fmt.Println("  HERMES AGENT — THINK TANK REVIEW")
	fmt.Println("  5 Fellows: Bull, Bear, Technical, Macro, Contrarian")
	fmt.Println("═══════════════════════════════════════════")
	fmt.Println()
	
	// Phase 1: Research
	fmt.Println("Phase 1: Independent Fellow Research...")
	orch.RunResearchRound()
	for _, f := range tt.ResearchFindings {
		fmt.Printf("  %s (%s): %d insights, confidence %.0f%%\n",
			f.FellowName, f.Role, len(f.KeyInsights), f.ConfidenceScore*100)
	}
	fmt.Println()
	
	// Phase 2: Debate
	fmt.Println("Phase 2: Structured Dialectic Debate...")
	orch.RunDebate()
	fmt.Printf("  %d debate turns across %d rounds\n", len(tt.DebateTranscript), tt.DelphiRounds)
	fmt.Println()
	
	// Phase 3: Synthesis
	fmt.Println("Phase 3: Hegelian Synthesis...")
	orch.RunSynthesis()
	if tt.Synthesis != nil {
		fmt.Printf("  Thesis: %s\n", tt.Synthesis.Thesis)
		fmt.Printf("  Antithesis: %s\n", tt.Synthesis.Antithesis)
		fmt.Printf("  Synthesis: %s\n", tt.Synthesis.Synthesis)
		fmt.Printf("  Agreement points: %d\n", len(tt.Synthesis.PointsOfAgreement))
		fmt.Printf("  Disagreement points: %d\n", len(tt.Synthesis.PointsOfDisagreement))
	}
	fmt.Println()
	
	// Phase 4: Peer Review
	fmt.Println("Phase 4: Peer Review...")
	orch.RunPeerReview()
	critical := 0
	for _, r := range tt.PeerReview {
		if r.Severity == "critical" { critical++ }
	}
	fmt.Printf("  %d review comments (%d critical)\n", len(tt.PeerReview), critical)
	fmt.Println()
	
	// Phase 5: Report
	fmt.Println("Phase 5: Final Report...")
	orch.RunReportGeneration()
	if tt.FinalReport != nil {
		fmt.Printf("  Title: %s\n", tt.FinalReport.Title)
		fmt.Printf("  Recommendation: %s\n", tt.FinalReport.Recommendation)
		fmt.Printf("  Confidence: %s\n", tt.FinalReport.ConfidenceLevel)
		fmt.Printf("  Scenarios: %d\n", len(tt.FinalReport.Scenarios))
		fmt.Printf("  Risks: %d\n", len(tt.FinalReport.RisksAndCaveats))
		fmt.Println()
		fmt.Println("═══════════════════════════════════════════")
		fmt.Println("  EXECUTIVE SUMMARY")
		fmt.Println("═══════════════════════════════════════════")
		fmt.Println(tt.FinalReport.ExecutiveSummary)
	}
}
