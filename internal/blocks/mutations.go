package blocks

import (
	"fmt"
	"math/rand"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// MutationBlockIDKey is stored in MutationOp.Node.Metadata for block insertions.
const MutationBlockIDKey = "block_id"

// ApplyBlockMutations handles block-aware mutation operations on a tree.
// Returns true if the operation was recognized and applied.
func ApplyBlockMutations(reg *Registry, tree *evolution.SerializableNode, op evolution.MutationOp) bool {
	if reg == nil {
		reg = DefaultRegistry
	}
	switch op.Operation {
	case "insert_block_before", "insert_block_after":
		blockID := blockIDFromOp(op)
		if blockID == "" {
			return false
		}
		if reg.Get(blockID) == nil {
			return false
		}
		ref := SubTreeRefNode(blockID)
		if op.Operation == "insert_block_before" {
			return evolutionApplyAddBefore(tree, op.Target, ref)
		}
		return evolutionApplyAddAfter(tree, op.Target, ref)
	case "replace_with_block":
		blockID := blockIDFromOp(op)
		if blockID == "" || reg.Get(blockID) == nil {
			return false
		}
		return replaceNodeWithRef(tree, op.Target, blockID)
	case "compose_blocks":
		if op.Node == nil || op.Node.Metadata == nil {
			return false
		}
		raw, ok := op.Node.Metadata["blocks"].([]any)
		if !ok {
			return false
		}
		var ids []string
		for _, v := range raw {
			if s, ok := v.(string); ok {
				ids = append(ids, s)
			}
		}
		composed, err := Compose(reg, ComposeSpec{Name: tree.Name, Blocks: ids}, false)
		if err != nil || composed == nil {
			return false
		}
		tree.Children = composed.Children
		return true
	}
	return false
}

func blockIDFromOp(op evolution.MutationOp) string {
	if op.Node != nil && op.Node.Metadata != nil {
		if id, ok := op.Node.Metadata[MutationBlockIDKey].(string); ok {
			return id
		}
	}
	return BlockIDFromNode(op.Node)
}

// RandomBlockMutation returns a mutation that inserts a random registered block.
func RandomBlockMutation(reg *Registry, tree *evolution.SerializableNode) []evolution.MutationOp {
	if reg == nil {
		reg = DefaultRegistry
	}
	ids := reg.IDs()
	if len(ids) == 0 {
		return nil
	}
	id := ids[rand.Intn(len(ids))]
	target := randomNamedNode(tree, tree.Name)
	ref := SubTreeRefNode(id)
	return []evolution.MutationOp{{
		Operation: pickInsertOp(),
		Target:    target,
		Node:      &ref,
	}}
}

func pickInsertOp() string {
	if rand.Intn(2) == 0 {
		return "insert_block_before"
	}
	return "insert_block_after"
}

func randomNamedNode(node *evolution.SerializableNode, fallback string) string {
	var names []string
	collectNames(node, &names)
	if len(names) == 0 {
		return fallback
	}
	return names[rand.Intn(len(names))]
}

func collectNames(node *evolution.SerializableNode, names *[]string) {
	if node.Name != "" && node.Type != "SubTreeRef" {
		*names = append(*names, node.Name)
	}
	for i := range node.Children {
		collectNames(&node.Children[i], names)
	}
}

// PromoteSubtree registers a subtree at target path as a new custom block.
func PromoteSubtree(reg *Registry, tree *evolution.SerializableNode, targetName, blockID string) (*Block, error) {
	if reg == nil {
		reg = DefaultRegistry
	}
	sub := extractSubtree(tree, targetName)
	if sub == nil {
		return nil, fmt.Errorf("promote: node %q not found", targetName)
	}
	b := Block{
		ID:          blockID,
		Name:        targetName,
		Description: "Promoted from tree " + tree.Name,
		Category:    CategoryCustom,
		Tree:        sub,
		Mutable:     true,
		Version:     1,
	}
	if err := reg.Register(b); err != nil {
		return nil, err
	}
	return &b, nil
}

func extractSubtree(tree *evolution.SerializableNode, name string) *evolution.SerializableNode {
	if tree == nil {
		return nil
	}
	if tree.Name == name {
		return cloneTree(tree)
	}
	for i := range tree.Children {
		if found := extractSubtree(&tree.Children[i], name); found != nil {
			return found
		}
	}
	return nil
}

func replaceNodeWithRef(tree *evolution.SerializableNode, target, blockID string) bool {
	ref := SubTreeRefNode(blockID)
	return replaceNodeNamed(tree, target, ref)
}

func replaceNodeNamed(tree *evolution.SerializableNode, target string, replacement evolution.SerializableNode) bool {
	if tree.Name == target {
		*tree = replacement
		return true
	}
	for i := range tree.Children {
		if tree.Children[i].Name == target {
			tree.Children[i] = replacement
			return true
		}
		if replaceNodeNamed(&tree.Children[i], target, replacement) {
			return true
		}
	}
	return false
}

func evolutionApplyAddBefore(tree *evolution.SerializableNode, target string, newNode evolution.SerializableNode) bool {
	for i := range tree.Children {
		if tree.Children[i].Name == target {
			tree.Children = append(tree.Children[:i], append([]evolution.SerializableNode{newNode}, tree.Children[i:]...)...)
			return true
		}
	}
	for i := range tree.Children {
		if evolutionApplyAddBefore(&tree.Children[i], target, newNode) {
			return true
		}
	}
	return false
}

func evolutionApplyAddAfter(tree *evolution.SerializableNode, target string, newNode evolution.SerializableNode) bool {
	for i := range tree.Children {
		if tree.Children[i].Name == target {
			tree.Children = append(tree.Children[:i+1], append([]evolution.SerializableNode{newNode}, tree.Children[i+1:]...)...)
			return true
		}
	}
	for i := range tree.Children {
		if evolutionApplyAddAfter(&tree.Children[i], target, newNode) {
			return true
		}
	}
	return false
}
