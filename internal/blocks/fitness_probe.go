package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

// FitnessProbeBlock records block-level fitness into ChainState for evolution trees.
func FitnessProbeBlock() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "Sequence",
		Name:        "FitnessProbe",
		Description: "Probe execution fitness for block-level evolution",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "FitnessProbe", Description: "Write block_fitness to ChainState"},
		},
	}
}
