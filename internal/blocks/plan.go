package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

// PlanBlock returns a planning subtree (complexity + plan generation).
func PlanBlock() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "Sequence",
		Name:        "Plan",
		Description: "Assess complexity and generate an execution plan",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "ValidateInput", Description: "Non-empty task"},
			{Type: "Action", Name: "AssessComplexity", Description: "Set bb.Complexity from LLM"},
			{
				Type:      "Timeout",
				Name:      "GeneratePlan_Timeout",
				TimeoutMs: 60_000,
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "GeneratePlan", Description: "Populate bb.Plan"},
				},
			},
			{Type: "Condition", Name: "HasPlan", Description: "Plan was generated"},
		},
	}
}
