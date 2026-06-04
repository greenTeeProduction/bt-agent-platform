package engine

import (
	"fmt"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// expandTreeFn is set by internal/blocks via RegisterTreeExpander at init.
var expandTreeFn func(*evolution.SerializableNode) (*evolution.SerializableNode, error)

// RegisterTreeExpander wires block SubTreeRef expansion into BuildAndValidate.
// Called from internal/blocks/hooks.go init to avoid an import cycle.
func RegisterTreeExpander(fn func(*evolution.SerializableNode) (*evolution.SerializableNode, error)) {
	expandTreeFn = fn
}

func prepareTreeForBuild(serTree *evolution.SerializableNode) (*evolution.SerializableNode, error) {
	if serTree == nil {
		return nil, fmt.Errorf("tree is nil")
	}
	if !treeHasSubTreeRefs(serTree) {
		return serTree, nil
	}
	if expandTreeFn == nil {
		return nil, fmt.Errorf("tree contains SubTreeRef nodes but no expander is registered (ensure internal/blocks is imported)")
	}
	expanded, err := expandTreeFn(serTree)
	if err != nil {
		return nil, fmt.Errorf("expand: %w", err)
	}
	if treeHasSubTreeRefs(expanded) {
		return nil, fmt.Errorf("expand: unresolved SubTreeRef nodes remain")
	}
	return expanded, nil
}

func treeHasSubTreeRefs(node *evolution.SerializableNode) bool {
	if node == nil {
		return false
	}
	if node.Type == "SubTreeRef" {
		return true
	}
	for i := range node.Children {
		if treeHasSubTreeRefs(&node.Children[i]) {
			return true
		}
	}
	return false
}
