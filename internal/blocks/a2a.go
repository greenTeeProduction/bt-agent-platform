package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

// A2AHandoffBlock sends the task to an external A2A agent.
// Set bb.ChainState["a2a_url"] or ["a2a_target_url"] before running.
func A2AHandoffBlock() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "Sequence",
		Name:        "A2AHandoff",
		Description: "Delegate task to external A2A agent",
		Metadata: map[string]any{
			"side_effect_class": "external",
		},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "PrepareA2AHandoff", Description: "Map a2a_url to a2a_target_url"},
			{Type: "Condition", Name: "HasA2ATarget", Description: "A2A target configured"},
			{
				Type:        "HumanApprovalGate",
				Name:        "A2AApproval",
				Description: "Approve external A2A handoff",
				Metadata: map[string]any{
					"prompt": "Approve sending this task to the external A2A agent?",
				},
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "DelegateToA2A", Description: "Execute A2A delegation"},
				},
			},
		},
	}
}
