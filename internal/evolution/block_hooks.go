package evolution

// Block mutation hooks are registered by internal/blocks from init() to avoid an import cycle.

var applyBlockMutationsFn func(tree *SerializableNode, op MutationOp) bool

var blockRandomMutatorFn func(tree *SerializableNode) []MutationOp

// RegisterBlockMutations wires block-aware mutation operations from the blocks package.
func RegisterBlockMutations(fn func(tree *SerializableNode, op MutationOp) bool) {
	applyBlockMutationsFn = fn
}

// RegisterBlockRandomMutator wires random block-insertion mutations for the evolver.
func RegisterBlockRandomMutator(fn func(tree *SerializableNode) []MutationOp) {
	blockRandomMutatorFn = fn
}

func tryApplyBlockMutations(tree *SerializableNode, op MutationOp) bool {
	if applyBlockMutationsFn == nil {
		return false
	}
	return applyBlockMutationsFn(tree, op)
}

func tryBlockRandomMutation(tree *SerializableNode) []MutationOp {
	if blockRandomMutatorFn == nil {
		return nil
	}
	return blockRandomMutatorFn(tree)
}
