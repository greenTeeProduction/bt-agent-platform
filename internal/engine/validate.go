package engine

import (
	"fmt"
	"strings"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// ValidateTree checks that all Action and Condition nodes in the tree have
// registered handlers, that memory nodes (MemSelector,
// PersistentMemSequence, ForEachTask) have unique, non-empty names — their
// ChainState cursor keys are derived from Name, so a missing or duplicate
// name causes cursor collisions across resumed runs — and that
// CachedCondition nodes never memoize an approval/HITL condition, since a
// stale cached "approved" result would bypass a human gate. Returns a flat
// list of validation messages.
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
		if actionRegistry[node.Name] == nil {
			*msgs = append(*msgs, node.Name)
		}
	case "Condition":
		if conditionRegistry[node.Name] == nil {
			*msgs = append(*msgs, node.Name)
		}
	case "MemSelector", "PersistentMemSequence", "ForEachTask":
		if strings.TrimSpace(node.Name) == "" {
			*msgs = append(*msgs,
				fmt.Sprintf("%s: memory node requires unique non-empty name", node.Type))
		} else {
			nameCounts[node.Name]++
		}
	case "CachedCondition":
		for i := range node.Children {
			child := &node.Children[i]
			if child.Type == "Condition" &&
				(strings.Contains(child.Name, "HITL") || strings.Contains(child.Name, "Approved")) {
				*msgs = append(*msgs,
					fmt.Sprintf("CachedCondition %s: must not wrap approval/HITL condition %s",
						node.Name, child.Name))
			}
		}
	}
	for i := range node.Children {
		validateNode(&node.Children[i], msgs, nameCounts)
	}
}
