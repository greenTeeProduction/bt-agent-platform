package engine

import (
	"fmt"
	"strings"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// ValidateTree checks that all Action and Condition nodes in the tree have
// registered handlers, that memory nodes (MemSelector,
// PersistentMemSequence, ForEachTask, BanditSelector) have unique, non-empty
// names — their ChainState cursor keys (or, for BanditSelector, stats file
// path) are derived from Name, so a missing or duplicate name causes cursor
// collisions, or two logically distinct nodes to alias the same stats
// file/cursor key within one tree — and that CachedCondition nodes never
// memoize an approval/HITL condition, since a stale cached "approved" result
// would bypass a human gate. This is a per-tree authoring check only: unique
// names stop two *different* nodes from aliasing each other's state, but
// they say nothing about concurrent access to a shared stats path (e.g. the
// same node name reused across trees, or concurrent goroutines each ticking
// their own instance) — see the locking and caching caveats documented on
// BuildBanditSelector for what protects (and doesn't protect) that case.
// Returns a flat list of validation messages.
func ValidateTree(tree *evolution.SerializableNode) []string {
	var msgs []string
	nameCounts := map[string]int{}
	validateNode(tree, &msgs, nameCounts)
	for name, count := range nameCounts {
		if count > 1 {
			msgs = append(msgs,
				fmt.Sprintf("%s: duplicate memory node name (used %d times)", name, count))
		}
	}
	return msgs
}

func validateNode(node *evolution.SerializableNode, msgs *[]string, nameCounts map[string]int) {
	switch node.Type {
	case "Action":
		if actionRegistry[node.Name] == nil && !isCompiledGoapAction(node.Name) {
			*msgs = append(*msgs, node.Name)
		}
	case "Condition":
		if conditionRegistry[node.Name] == nil && !isCompiledGoapCondition(node.Name) {
			*msgs = append(*msgs, node.Name)
		}
	case "MemSelector", "PersistentMemSequence", "ForEachTask", "BanditSelector":
		if strings.TrimSpace(node.Name) == "" {
			*msgs = append(*msgs,
				fmt.Sprintf("%s: memory node requires unique non-empty name", node.Type))
		} else {
			nameCounts[node.Name]++
		}
	case "CachedCondition":
		for _, guarded := range findGuardedConditions(node) {
			*msgs = append(*msgs,
				fmt.Sprintf("CachedCondition %s: must not wrap approval/HITL condition %s",
					node.Name, guarded))
		}
	}
	// Mirror ValidateTreeFull's leaf-with-children rule (shared leafNodeTypes
	// map so the two validation paths cannot drift): engine.buildNode
	// constructs these types as childless leaves, silently discarding any
	// declared Children, so declaring children is a construction error.
	if leafNodeTypes[node.Type] && len(node.Children) > 0 {
		*msgs = append(*msgs,
			fmt.Sprintf("%s: %s leaf must not declare children", node.Name, node.Type))
	}
	for i := range node.Children {
		validateNode(&node.Children[i], msgs, nameCounts)
	}
}

// findGuardedConditions walks a CachedCondition's entire wrapped subtree —
// not just its direct child — and returns the names of every Condition node
// whose Name contains "HITL" or "Approved". A CachedCondition memoizes
// whatever its subtree returns, so an approval gate buried inside a
// composite (e.g. a Sequence) is just as dangerous to cache as one wired in
// directly.
func findGuardedConditions(node *evolution.SerializableNode) []string {
	var found []string
	for i := range node.Children {
		child := &node.Children[i]
		if child.Type == "Condition" &&
			(strings.Contains(child.Name, "HITL") || strings.Contains(child.Name, "Approved")) {
			found = append(found, child.Name)
		}
		found = append(found, findGuardedConditions(child)...)
	}
	return found
}
