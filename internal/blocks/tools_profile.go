package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

func toolsProfileBlock(id, name, setupAction string) Block {
	return Block{
		ID:          id,
		Name:        name,
		Description: "Tool profile: " + setupAction,
		Category:    CategoryTool,
		Mutable:     false,
		Version:     1,
		Tree: &evolution.SerializableNode{
			Type: "Sequence",
			Name: name,
			Children: []evolution.SerializableNode{
				{Type: "Action", Name: setupAction, Description: "Populate bb.ChainTools for " + name},
			},
		},
	}
}

// ToolProfileBlocks returns all swappable tool-setup blocks.
func ToolProfileBlocks() []Block {
	return []Block{
		toolsProfileBlock("core:tools_default", "ToolsDefault", "SetupDefaultTools"),
		toolsProfileBlock("core:tools_dev", "ToolsDev", "SetupDevTools"),
		toolsProfileBlock("core:tools_research", "ToolsResearch", "SetupResearchTools"),
		toolsProfileBlock("core:tools_startup", "ToolsStartup", "SetupStartupTools"),
		toolsProfileBlock("core:tools_universal", "ToolsUniversal", "SetupUniversalTools"),
	}
}

// PreGateValidationOnly returns pre_gate without tool setup (validation only).
func PreGateValidationOnly() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type: "Sequence",
		Name: "PreGate",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "ValidateInput", Description: "Non-empty task"},
			{Type: "Condition", Name: "HasClearTask", Description: "Task has clear goal"},
		},
	}
}

// ToolProfileBlockID maps profile name to block id.
func ToolProfileBlockID(profile string) string {
	switch profile {
	case "dev":
		return "core:tools_dev"
	case "research":
		return "core:tools_research"
	case "startup":
		return "core:tools_startup"
	case "universal":
		return "core:tools_universal"
	default:
		return "core:tools_default"
	}
}
