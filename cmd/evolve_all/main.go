package main

import (
	"fmt"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
)

func main() {
	client, err := llm.NewClient(llm.DefaultConfig())
	if err != nil { fmt.Println("Ollama unavailable"); return }

	fmt.Println("Phase 1: Decision Tree Optimization")
	opt := evolution.NewBTOptimizer()
	trees := []struct{ name string; tree *evolution.SerializableNode }{
		{"godev", evolution.GoDeveloperTree()},
		{"default", evolution.DefaultTree()},
		{"stockfish_evolve", evolution.StockfishEvolutionTree()},
		{"hermes_evolve", evolution.HermesSelfEvolutionTree()},
	}
	for _, t := range trees {
		report := opt.AnalyzeTree(t.tree, t.name)
		fmt.Printf("  %s: entropy=%.2f gini=%.2f score=%.1f/10 changes=%d\n",
			t.name, report.Entropy, report.Gini, report.OverallScore, report.ReorderChanges)
	}

	fmt.Println("\nPhase 2: Genetic Algorithm")
	pop := evolution.NewPopulation(10, evolution.DefaultTree())
	best := pop.Evolve(5, func(t *evolution.SerializableNode) float64 {
		return float64(evolution.CountNodes(t)) * 2.0
	})
	fmt.Printf("  Pop:10 Gen:5 Best:%.1f Diversity:%.2f\n", pop.BestFitness, pop.Diversity())
	_ = best

	fmt.Println("\nPhase 3: Stockfish Evolution")
	tree := evolution.StockfishEvolutionTree()
	bb := &engine.Blackboard{
		Task: "Evolve all behavior trees using Stockfish algorithms. Focus on trees with lowest fitness.",
		LLM:  client,
	}
	cmd := engine.BuildTree(tree, bb)
	engine.RunTask(bb, cmd)
	fmt.Printf("Stockfish: %s\n", bb.Outcome)
	if bb.Result != "" { fmt.Println(bb.Result[:min(200, len(bb.Result))]) }

	fmt.Println("\nEvolution complete.")
}

func min(a, b int) int { if a<b {return a}; return b }
