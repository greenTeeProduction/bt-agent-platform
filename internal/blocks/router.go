package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

// StrategyRouterBlock is an empty StrategyRouter selector for middle insertion via ComposeSpec.Middle.
func StrategyRouterBlock() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "Selector",
		Name:        "StrategyRouter",
		Description: "Intent routing — add strategy paths as children or via ComposeSpec.Middle",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence",
				Name: "DefaultPath",
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "MarkSuccessful", Description: "Default route when no strategy matched"},
				},
			},
		},
	}
}
