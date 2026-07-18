package domains

import (
	"strings"
	"testing"
)

// TestKanbanAndHermesTreesPassValidate guards against undersized max_tokens
// metadata on llm_call ChainAction nodes across kanban.go, hermes_evolve.go,
// and hermes_obsidian.go. Now that max_tokens actually governs LLM output
// budgets (see internal/engine/chains.go), a value below the
// minPlausibleMaxTokens floor (internal/evolution/node_types.go) silently
// truncates every response from that node instead of being an inert field.
func TestKanbanAndHermesTreesPassValidate(t *testing.T) {
	trees := map[string]func() interface{ Validate() []string }{
		"KanbanTaskCreatorTree":       func() interface{ Validate() []string } { return KanbanTaskCreatorTree() },
		"KanbanRefinerTree":           func() interface{ Validate() []string } { return KanbanRefinerTree() },
		"KanbanQATree":                func() interface{ Validate() []string } { return KanbanQATree() },
		"KanbanBoardMonitorTree":      func() interface{ Validate() []string } { return KanbanBoardMonitorTree() },
		"KanbanWorkflowTree":          func() interface{ Validate() []string } { return KanbanWorkflowTree() },
		"KanbanAutoPilotTree":         func() interface{ Validate() []string } { return KanbanAutoPilotTree() },
		"HermesSelfEvolutionTree":     func() interface{ Validate() []string } { return HermesSelfEvolutionTree() },
		"HermesObsidianOptimizerTree": func() interface{ Validate() []string } { return HermesObsidianOptimizerTree() },
	}

	for name, build := range trees {
		t.Run(name, func(t *testing.T) {
			errs := build().Validate()
			var tokenErrs []string
			for _, e := range errs {
				if strings.Contains(e, "max_tokens") && strings.Contains(e, "implausibly small") {
					tokenErrs = append(tokenErrs, e)
				}
			}
			if len(tokenErrs) > 0 {
				t.Errorf("%s: Validate() reported %d implausibly-small max_tokens error(s):\n%s",
					name, len(tokenErrs), strings.Join(tokenErrs, "\n"))
			}
		})
	}
}
