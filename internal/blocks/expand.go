package blocks

import (
	"fmt"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// Expand resolves all SubTreeRef nodes by inlining registered blocks (deep).
func Expand(reg *Registry, tree *evolution.SerializableNode) (*evolution.SerializableNode, error) {
	if reg == nil {
		reg = DefaultRegistry
	}
	if tree == nil {
		return nil, fmt.Errorf("expand: nil tree")
	}
	expanded := expandNode(reg, tree, make(map[string]int))
	if errs := expanded.Validate(); len(errs) > 0 {
		return nil, fmt.Errorf("expand: invalid expanded tree: %v", errs)
	}
	return expanded, nil
}

// expandNode inlines SubTreeRef nodes; depth tracks ref chain to detect cycles.
func expandNode(reg *Registry, node *evolution.SerializableNode, stack map[string]int) *evolution.SerializableNode {
	if node == nil {
		return nil
	}
	if node.Type == "SubTreeRef" {
		id := BlockIDFromNode(node)
		if id == "" {
			return cloneTree(node)
		}
		stack[id]++
		if stack[id] > 4 {
			return &evolution.SerializableNode{
				Type:        "Action",
				Name:        "ReportClarifyViolation",
				Description: "block reference cycle detected: " + id,
			}
		}
		b := reg.Get(id)
		if b == nil || b.Tree == nil {
			return &evolution.SerializableNode{
				Type:        "Action",
				Name:        "ReportClarifyViolation",
				Description: "unknown block reference: " + id,
			}
		}
		out := expandNode(reg, b.Tree, stack)
		stack[id]--
		return out
	}

	out := cloneTree(node)
	out.Children = nil
	for i := range node.Children {
		out.Children = append(out.Children, *expandNode(reg, &node.Children[i], stack))
	}
	return out
}

// HasSubTreeRefs returns true if the tree contains unresolved SubTreeRef nodes.
func HasSubTreeRefs(tree *evolution.SerializableNode) bool {
	if tree == nil {
		return false
	}
	if tree.Type == "SubTreeRef" {
		return true
	}
	for i := range tree.Children {
		if HasSubTreeRefs(&tree.Children[i]) {
			return true
		}
	}
	return false
}
