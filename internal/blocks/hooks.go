package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

func init() {
	evolution.RegisterBlockMutations(func(tree *evolution.SerializableNode, op evolution.MutationOp) bool {
		return ApplyBlockMutations(DefaultRegistry, tree, op)
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
