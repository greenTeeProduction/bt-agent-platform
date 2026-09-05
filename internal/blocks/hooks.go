package blocks

import (
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

func init() {
	evolution.RegisterBlockMutations(func(tree *evolution.SerializableNode, op evolution.MutationOp) bool {
		return ApplyBlockMutations(DefaultRegistry, tree, op)
	})
	engine.RegisterTreeExpander(func(tree *evolution.SerializableNode) (*evolution.SerializableNode, error) {
		return Expand(DefaultRegistry, tree)
	})
	evolution.RegisterBlockRandomMutator(func(tree *evolution.SerializableNode) []evolution.MutationOp {
		return RandomBlockMutation(DefaultRegistry, tree)
	})
}

// InitRegistry replaces DefaultRegistry with a disk-backed registry (call from main).
func InitRegistry(homeDir string) *Registry {
	if homeDir != "" {
		DefaultRegistry = NewRegistry(homeDir)
	}
	return DefaultRegistry
}
