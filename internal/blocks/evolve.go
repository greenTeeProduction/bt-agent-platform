package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

// EvolveOnFailureBlock triggers tree update only after persistent failures pass quality gate.
func EvolveOnFailureBlock() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "Sequence",
		Name:        "EvolveOnFailure",
		Description: "Evolve behavior tree when failures persist and quality allows",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "PersistentFailures", Description: "Failure threshold met"},
			{
				Type:        "QualityGate",
				Name:        "EvolutionQualityGate",
				Description: "Ensure output quality before applying evolution",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateOutput", Description: "Output meets quality bar"},
					{Type: "Action", Name: "UpdateBehaviorTree", Description: "Apply evolved tree changes"},
				},
			},
		},
	}
}
