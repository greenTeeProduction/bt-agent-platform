package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

// HumanReviewBlock is a post-execution approval gate (child runs before human review).
func HumanReviewBlock(name, prompt string) evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "HumanApprovalGate",
		Name:        name,
		Description: prompt,
		Metadata: map[string]any{
			"phase":  "post",
			"prompt": prompt,
		},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MarkSuccessful", Description: "Finalize after approval"},
		},
	}
}
