package engine

import "github.com/nico/go-bt-evolve/internal/evolution"

// ValidateTree checks that all Action and Condition nodes in the tree
// have registered handlers. Returns a list of missing node names.
func ValidateTree(tree *evolution.SerializableNode) []string {
	var missing []string
	validateNode(tree, &missing)
	return missing
}

func validateNode(node *evolution.SerializableNode, missing *[]string) {
	switch node.Type {
	case "Action":
		if actionRegistry[node.Name] == nil {
			*missing = append(*missing, node.Name)
		}
	case "Condition":
		if conditionRegistry[node.Name] == nil {
			*missing = append(*missing, node.Name)
		}
	}
	for i := range node.Children {
		validateNode(&node.Children[i], missing)
	}
}
