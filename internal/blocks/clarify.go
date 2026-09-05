package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

// ClarifyGateBlock asks clarifying questions when the task is ambiguous.
func ClarifyGateBlock() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "Selector",
		Name:        "ClarifyGate",
		Description: "Clarify ambiguous tasks before execution",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence",
				Name: "NeedClarify",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "IsAmbiguousQuery", Description: "Task is underspecified"},
					{Type: "Action", Name: "AskClarifyingQuestions", Description: "Emit clarifying questions"},
					{Type: "Action", Name: "MarkSuccessful", Description: "Await user clarification"},
				},
			},
			{
				Type: "Sequence",
				Name: "ClearTask",
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "ProceedDirectly", Description: "Task is clear enough to continue"},
				},
			},
		},
	}
}
