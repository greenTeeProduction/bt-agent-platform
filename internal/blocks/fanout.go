package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

// ParallelFanoutBlock runs map_reduce over the plan, then merges results.
func ParallelFanoutBlock() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "Sequence",
		Name:        "ParallelFanout",
		Description: "Decompose plan into subtasks and merge outputs",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "HasPlan", Description: "Plan available for fan-out"},
			{
				Type:        "Parallel",
				Name:        "FanoutParallel",
				Description: "Execute plan steps in parallel (all must succeed)",
				Metadata:    map[string]any{"success_policy": "all"},
				Children: []evolution.SerializableNode{
					{
						Type:        "ChainAction",
						Name:        "map_reduce:Execute subtasks from the plan.\n\nTask: {{.Task}}\nPlan: {{.Plan}}",
						Description: "Map-reduce over plan steps",
						Metadata: map[string]any{
							"max_tokens": float64(2048),
						},
					},
				},
			},
			{Type: "Action", Name: "MergeResults", Description: "Combine bb.Results into bb.Result"},
		},
	}
}

// MergeResultsBlock merges accumulated chain results.
func MergeResultsBlock() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type: "Sequence",
		Name: "MergeResults",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MergeResults", Description: "Merge bb.Results"},
		},
	}
}
