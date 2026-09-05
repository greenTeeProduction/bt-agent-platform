package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

// QualityGateBlock validates output before marking success.
func QualityGateBlock() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "Selector",
		Name:        "QualityGate",
		Description: "Validate output quality before success",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence",
				Name: "Pass",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateOutput", Description: "Output meets quality bar"},
					{Type: "Action", Name: "MarkSuccessful", Description: "Mark validated success"},
				},
			},
			{
				Type: "Sequence",
				Name: "Fail",
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "ReflectOnOutcome", Description: "Reflect on quality failure"},
					{Type: "Action", Name: "SelfCorrect", Description: "Attempt correction"},
				},
			},
		},
	}
}
