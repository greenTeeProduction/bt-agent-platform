package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

// MemoryLoadBlock loads agent memory into ChainState["agent_memory"].
func MemoryLoadBlock() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "Sequence",
		Name:        "MemoryLoad",
		Description: "Load persistent agent memory into chain state",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "LoadAgentMemory", Description: "Read agent memory context block"},
		},
	}
}

// MemoryWriteBlock persists run summary to agent memory.
func MemoryWriteBlock() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "Sequence",
		Name:        "MemoryWrite",
		Description: "Persist task result summary to agent memory",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "WriteAgentMemory", Description: "Write last_run_summary"},
		},
	}
}
