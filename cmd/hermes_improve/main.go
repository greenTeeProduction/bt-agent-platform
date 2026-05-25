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

	tree := evolution.HermesSelfEvolutionTree()
	bb := &engine.Blackboard{Task: "Evolve yourself: Review ALL behavior trees, the dashboard UI, the sprint execution, the thinktank pipeline, and the knowledge graph. Identify weaknesses and improvements.", LLM: client}

	fmt.Println("═══ HERMES SELF-EVOLUTION ═══")
	cmd := engine.BuildTree(tree, bb)
	result := engine.RunTask(bb, cmd)
	fmt.Printf("Outcome: %s\n", bb.Outcome)
	fmt.Println("─── Report ───")
	fmt.Println(bb.Result)
	if result != "" { fmt.Println(result) }
}
