package domains

import (
	"strings"

	"github.com/nico/go-bt-evolve/internal/blocks"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/startup"
	"github.com/nico/go-bt-evolve/internal/thinktank"
)

// ResolveTreeID maps a tree identifier string to a serializable behavior tree.
// Used by bt-agent, A2A, and template validation tests.
func ResolveTreeID(id string) *evolution.SerializableNode {
	if id == "" {
		return nil
	}
	if id == "hermes_evolve" {
		return HermesSelfEvolutionTree()
	}
	if id == "stockfish_evolve" {
		return evolution.StockfishEvolutionTree()
	}
	if id == "stockfish_loop" {
		return evolution.StockfishEvolutionLoop()
	}
	if id == "vault_manager" {
		return evolution.VaultManagerTree()
	}
	if id == "kanban:task_creator" {
		return KanbanTaskCreatorTree()
	}
	if id == "kanban:refiner" {
		return KanbanRefinerTree()
	}
	if id == "kanban:qa" {
		return KanbanQATree()
	}
	if id == "kanban:monitor" {
		return KanbanBoardMonitorTree()
	}
	if id == "kanban:workflow" {
		return KanbanWorkflowTree()
	}
	if id == "kanban:autopilot" {
		return KanbanAutoPilotTree()
	}
	if id == "notebooklm" {
		return NotebookLMTree()
	}
	if id == "notebooklm-consumer" {
		return NotebookLMConsumerTree()
	}
	if id == "notebooklm-bridge" {
		return evolution.NotebookLMBridgeTree()
	}
	if id == "hermes_obsidian" {
		return HermesObsidianOptimizerTree()
	}
	if id == "superpowers_pipeline" {
		return SuperpowersPipelineTree()
	}
	if id == "godev" {
		return evolution.GoDeveloperTree()
	}
	if id == "fusion" || id == "fusion_deliberation" {
		return evolution.FusionDeliberationTree()
	}
	if len(id) > 8 && id[:8] == "finance:" {
		return evolution.AllFinanceTrees()[id[8:]]
	}
	if len(id) > 9 && id[:9] == "research:" {
		return evolution.ResearchTrees()[id[9:]]
	}
	if len(id) > 7 && id[:7] == "domain:" {
		return AllDomainTrees()[id[7:]]
	}
	if len(id) > 8 && id[:8] == "startup:" {
		role := id[8:]
		trees := startup.StartupTrees()
		if t, ok := trees[role]; ok {
			return t
		}
		return startup.Roles()[role]
	}
	if len(id) > 10 && id[:10] == "thinktank:" {
		switch role := id[10:]; role {
		case "synthesis":
			return thinktank.SynthesisTree()
		case "peer_review":
			return thinktank.PeerReviewTree()
		case "report":
			return thinktank.ReportGenerationTree()
		default:
			return thinktank.SynthesisTree()
		}
	}
	if len(id) > 9 && id[:9] == "composed:" {
		rest := id[9:]
		switch rest {
		case "task":
			if t, err := blocks.ComposeTaskTree(blocks.DefaultRegistry, "ComposedTask", nil); err == nil {
				return t
			}
		case "task:hitl":
			if t, err := blocks.ComposeTaskTreeWithHITL(blocks.DefaultRegistry, "ComposedTaskHITL", nil); err == nil {
				return t
			}
		case "task:agentic":
			if t, err := blocks.ComposeTaskTreeAgentic(blocks.DefaultRegistry, "ComposedTaskAgentic", nil); err == nil {
				return t
			}
		case "task:full":
			if t, err := blocks.ComposeTaskTreeFull(blocks.DefaultRegistry, "ComposedTaskFull", nil); err == nil {
				return t
			}
		default:
			ids := strings.Split(rest, ",")
			if t, err := blocks.Compose(blocks.DefaultRegistry, blocks.ComposeSpec{Name: "Composed_Main", Blocks: ids}, false); err == nil {
				return t
			}
		}
	}
	return evolution.DefaultTree()
}
