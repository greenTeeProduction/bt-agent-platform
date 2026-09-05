package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

// HITLTieredBlock gates only when side_effect_class is external or destroy.
// Set bb.ChainState["side_effect_class"] before this block runs.
func HITLTieredBlock() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "Selector",
		Name:        "HITLTiered",
		Description: "Human approval only for risky side effects",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence",
				Name: "RiskyPath",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "RequiresExternalApproval", Description: "destroy or external side effect"},
					{
						Type:        "HumanApprovalGate",
						Name:        "RiskyApproval",
						Description: "Approve risky operation",
						Metadata: map[string]any{
							"prompt": "Approve this operation with external or destructive side effects.",
						},
						Children: []evolution.SerializableNode{
							{Type: "Action", Name: "ProceedDirectly"},
						},
					},
				},
			},
			{
				Type: "Sequence",
				Name: "SafePath",
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "ProceedDirectly", Description: "No human gate for local effects"},
				},
			},
		},
	}
}
