package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

// HumanGateBlock returns a HumanApprovalGate subtree for composition.
func HumanGateBlock(name, prompt string) evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "HumanApprovalGate",
		Name:        name,
		Description: prompt,
		Metadata: map[string]any{
			"prompt": prompt,
		},
		Children: []evolution.SerializableNode{},
	}
}

// DefaultTaskBlocksWithHITL is the standard pipeline with a human checkpoint before tool execution.
var DefaultTaskBlocksWithHITL = PipelineWithToolsProfile([]string{
	"core:pre_gate",
	"core:human_gate",
	"core:tool_execution",
	"core:error_handling",
}, "default")

// ComposeTaskTreeWithHITL composes the task pipeline with human approval before execution.
func ComposeTaskTreeWithHITL(reg *Registry, name string, strategy *evolution.SerializableNode) (*evolution.SerializableNode, error) {
	if reg == nil {
		reg = DefaultRegistry
	}
	spec := ComposeSpec{Name: name, Blocks: append([]string{}, DefaultTaskBlocksWithHITL...), Middle: strategy}
	return composeWithMiddle(reg, spec, false)
}
