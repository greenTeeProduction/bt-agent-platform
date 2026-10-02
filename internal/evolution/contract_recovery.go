package evolution

import (
	"encoding/json"
	"slices"
)

// ContractRecoveryTargets returns gateways whose failed results cannot yet be
// repaired. Each mutation adds exactly one result-checked recovery call.
func ContractRecoveryTargets(tree *SerializableNode) []string {
	if tree == nil {
		return nil
	}
	var targets []string
	if tree.Type == "QualityGate" && len(tree.Children) == 1 {
		if contract, err := ParseResultContract(tree); err == nil && contract != nil && (len(contract.JSONFields) > 0 || len(contract.RequiredKeys) > 0) {
			targets = append(targets, tree.Name)
		}
	}
	for i := range tree.Children {
		targets = append(targets, ContractRecoveryTargets(&tree.Children[i])...)
	}
	return targets
}

func addContractRecovery(node *SerializableNode, target, task string) bool {
	if node.Name == target {
		if node.Type != "QualityGate" || len(node.Children) != 1 {
			return false
		}
		contract, err := ParseResultContract(node)
		if err != nil || contract == nil || len(contract.JSONFields)+len(contract.RequiredKeys) == 0 {
			return false
		}
		keys := append([]string(nil), contract.RequiredKeys...)
		for key := range contract.JSONFields {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		encoded, _ := json.Marshal(slices.Compact(keys))
		if task == "" {
			task = "{{.Task}}"
		}
		// Recompute from the original task without anchoring on a failed value.
		// The gateway retains the rejected output; its expected values are never
		// supplied to the worker as an answer.
		prompt := "Solve this task independently and check your calculation: " + task + "\nReturn only a JSON object with these required field names: " + string(encoded) + ". Compute every value from the task. A previous attempt failed validation.\nValidation failure: {{.ChainState.result_contract_error}}\nReturn complete JSON."
		node.Children = append(node.Children, SerializableNode{Type: "ChainAction", Name: "llm_call:" + prompt, Metadata: map[string]any{"max_tokens": 512, "system_msg": "Independently recompute and verify the requested task result. Return JSON only."}})
		return true
	}
	for i := range node.Children {
		if addContractRecovery(&node.Children[i], target, task) {
			return true
		}
	}
	return false
}
