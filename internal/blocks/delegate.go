package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

// DelegateBlock dispatches the task to another tree via engine.DelegateToTreeFn.
// Set bb.ChainState["delegate_tree_id"] and optionally ["delegate_task"] before running.
func DelegateBlock() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "Sequence",
		Name:        "Delegate",
		Description: "Run task through another behavior tree",
		Metadata: map[string]any{
			"side_effect_class": "external",
		},
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "HasDelegateTarget", Description: "delegate_tree_id in chain state"},
			{
				Type:        "HumanApprovalGate",
				Name:        "DelegateApproval",
				Description: "Approve delegation to external tree",
				Metadata: map[string]any{
					"prompt": "Approve running this task on a delegated behavior tree?",
				},
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "DelegateToTree", Description: "Execute delegated tree"},
				},
			},
		},
	}
}
