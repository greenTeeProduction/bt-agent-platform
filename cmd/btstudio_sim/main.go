package main

import (
	"fmt"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/startup"
	"github.com/nico/go-bt-evolve/internal/startup/companies"
)

func main() {
	client, err := llm.NewClient(llm.DefaultConfig())
	if err != nil {
		fmt.Println("Ollama unavailable")
		return
	}

	company := companies.BTStudioCompany()
	orch := startup.NewOrchestrator(company, client)

	fmt.Println("═══════════════════════════════════════════")
	fmt.Printf("  %s — Building Flutter BT Editor\n", company.Name)
	fmt.Println("═══════════════════════════════════════════")
	fmt.Printf("  Stage: %s | Round: %s | Team: %d | Runway: %dmo\n",
		company.ProductStage, company.FundingRound, company.TeamSize, company.Runway)
	fmt.Printf("  Stack: %v\n", company.TechStack)
	fmt.Println()

	// Run Sprint 1: Flutter visual editor MVP
	sprint := orch.RunSprint()
	fmt.Printf("=== Sprint %d: %s ===\n", sprint.SprintNum, sprint.Goal)
	fmt.Printf("  Completed: %v\n", sprint.Completed)
	fmt.Printf("  Velocity: %.1f\n", sprint.Velocity)
	fmt.Printf("  Tech Debt: %.0f\n", company.TechnicalDebt)
	fmt.Println()

	// Run Sprint 2: Drag-drop + WebSocket
	company.CurrentSprint = 2
	company.SprintGoal = "Add drag-drop canvas, WebSocket real-time collaboration"
	sprint2 := orch.RunSprint()
	fmt.Printf("=== Sprint %d: %s ===\n", sprint2.SprintNum, sprint2.Goal)
	fmt.Printf("  Completed: %v\n", sprint2.Completed)
	fmt.Printf("  Velocity: %.1f\n", sprint2.Velocity)
	fmt.Println()

	fmt.Printf("After 2 sprints: Features=%d, Users=%d, MRR=$%.0f, Cash=$%.0f\n",
		len(company.Features), company.Users, company.MRR, company.CashInBank)
}
