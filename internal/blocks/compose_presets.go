package blocks

import (
	"fmt"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// Pipeline presets (block order).
var (
	// DefaultTaskBlocksAgentic adds plan + quality validation around tool execution.
	DefaultTaskBlocksAgentic = []string{
		"core:pre_gate",
		"core:plan",
		"core:tool_execution",
		"core:quality_gate",
		"core:error_handling",
	}

	// DefaultTaskBlocksFull is the maximal agentic pipeline with optional gates.
	DefaultTaskBlocksFull = []string{
		"core:pre_gate",
		"core:plan",
		"core:rag_gate",
		"core:clarify_gate",
		"core:human_gate",
		"core:tool_execution",
		"core:quality_gate",
		"core:error_handling",
	}
)

// ComposePreset builds a tree from a named preset.
func ComposePreset(reg *Registry, preset, name string, strategy *evolution.SerializableNode) (*evolution.SerializableNode, error) {
	if reg == nil {
		reg = DefaultRegistry
	}
	switch preset {
	case "default", "":
		return ComposeTaskTree(reg, name, strategy)
	case "agentic":
		return ComposeTaskTreeAgentic(reg, name, strategy)
	case "hitl":
		return ComposeTaskTreeWithHITL(reg, name, strategy)
	case "full":
		return ComposeTaskTreeFull(reg, name, strategy)
	default:
		return nil, fmt.Errorf("unknown compose preset %q", preset)
	}
}

// ComposeTaskTreeAgentic composes pre → plan → [strategy] → tool → quality → error.
func ComposeTaskTreeAgentic(reg *Registry, name string, strategy *evolution.SerializableNode) (*evolution.SerializableNode, error) {
	if strategy == nil {
		return Compose(reg, ComposeSpec{Name: name, Blocks: append([]string{}, DefaultTaskBlocksAgentic...)}, false)
	}
	return composeOrderedWithMiddle(reg, name, DefaultTaskBlocksAgentic, "core:plan", strategy, false)
}

// ComposeTaskTreeFull composes the full agentic pipeline including RAG, clarify, and HITL gates.
func ComposeTaskTreeFull(reg *Registry, name string, strategy *evolution.SerializableNode) (*evolution.SerializableNode, error) {
	if strategy == nil {
		return Compose(reg, ComposeSpec{Name: name, Blocks: append([]string{}, DefaultTaskBlocksFull...)}, false)
	}
	return composeOrderedWithMiddle(reg, name, DefaultTaskBlocksFull, "core:plan", strategy, false)
}

// composeOrderedWithMiddle inserts middle immediately after afterBlockID in the block order.
func composeOrderedWithMiddle(reg *Registry, name string, blockIDs []string, afterBlockID string, middle *evolution.SerializableNode, inline bool) (*evolution.SerializableNode, error) {
	if reg == nil {
		reg = DefaultRegistry
	}
	var children []evolution.SerializableNode
	for _, id := range blockIDs {
		if reg.Get(id) == nil {
			return nil, fmt.Errorf("compose: unknown block %q", id)
		}
		if inline {
			children = append(children, *cloneTree(reg.Get(id).Tree))
		} else {
			children = append(children, SubTreeRefNode(id))
		}
		if id == afterBlockID && middle != nil {
			children = append(children, *cloneTree(middle))
		}
	}
	if name == "" {
		name = "Composed_Main"
	}
	root := &evolution.SerializableNode{Type: "Sequence", Name: name, Children: children}
	if errs := root.Validate(); len(errs) > 0 {
		return nil, fmt.Errorf("compose: %v", errs)
	}
	return root, nil
}
