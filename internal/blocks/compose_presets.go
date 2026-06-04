package blocks

import (
	"fmt"
	"strings"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// Pipeline presets (block order). Tool profiles are inserted via PipelineWithToolsProfile.
var (
	DefaultTaskBlocksAgentic = PipelineWithToolsProfile([]string{
		"core:pre_gate",
		"core:plan",
		"core:tool_execution",
		"core:quality_gate",
		"core:error_handling",
	}, "default")

	DefaultTaskBlocksFull = PipelineWithToolsProfile([]string{
		"core:pre_gate",
		"core:plan",
		"core:memory_load",
		"core:rag_gate",
		"core:clarify_gate",
		"core:human_gate",
		"core:tool_execution",
		"core:quality_gate",
		"core:memory_write",
		"core:error_handling",
	}, "default")
)

// PipelineWithToolsProfile inserts a tool-setup block after core:plan, or after pre_gate if plan is absent.
func PipelineWithToolsProfile(blocks []string, toolsProfile string) []string {
	toolsID := ToolProfileBlockID(toolsProfile)
	out := make([]string, 0, len(blocks)+1)
	inserted := false
	for _, id := range blocks {
		out = append(out, id)
		if !inserted && (id == "core:plan" || (id == "core:pre_gate" && !sliceContains(blocks, "core:plan"))) {
			out = append(out, toolsID)
			inserted = true
		}
	}
	if !inserted {
		out = append([]string{toolsID}, out...)
	}
	return out
}

func sliceContains(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}

// ComposePreset builds a tree from a named preset.
// Supports tools suffix: "agentic:dev", "full:research", etc.
func ComposePreset(reg *Registry, preset, name string, strategy *evolution.SerializableNode) (*evolution.SerializableNode, error) {
	return ComposePresetWithTools(reg, preset, "", name, strategy)
}

// ComposePresetWithTools builds a tree from preset and optional tools profile (dev, research, startup, universal, default).
func ComposePresetWithTools(reg *Registry, preset, toolsProfile, name string, strategy *evolution.SerializableNode) (*evolution.SerializableNode, error) {
	if reg == nil {
		reg = DefaultRegistry
	}
	base := preset
	profile := toolsProfile
	if i := strings.Index(preset, ":"); i > 0 {
		base = preset[:i]
		if profile == "" {
			profile = preset[i+1:]
		}
	}
	switch base {
	case "default", "":
		blocks := PipelineWithToolsProfile(append([]string{}, DefaultTaskBlocks...), profileOrDefault(profile))
		if strategy != nil {
			return composeOrderedWithMiddle(reg, name, blocks, "core:pre_gate", strategy, false)
		}
		return Compose(reg, ComposeSpec{Name: name, Blocks: blocks}, false)
	case "agentic":
		blocks := PipelineWithToolsProfile([]string{
			"core:pre_gate", "core:plan", "core:tool_execution", "core:quality_gate", "core:error_handling",
		}, profileOrDefault(profile))
		if strategy == nil {
			return Compose(reg, ComposeSpec{Name: name, Blocks: blocks}, false)
		}
		return composeOrderedWithMiddle(reg, name, blocks, "core:plan", strategy, false)
	case "hitl":
		blocks := PipelineWithToolsProfile(append([]string{}, DefaultTaskBlocksWithHITL...), profileOrDefault(profile))
		if strategy == nil {
			return Compose(reg, ComposeSpec{Name: name, Blocks: blocks}, false)
		}
		return composeOrderedWithMiddle(reg, name, blocks, "core:human_gate", strategy, false)
	case "full":
		blocks := PipelineWithToolsProfile([]string{
			"core:pre_gate", "core:plan", "core:memory_load", "core:rag_gate", "core:clarify_gate",
			"core:human_gate", "core:tool_execution", "core:quality_gate", "core:memory_write", "core:error_handling",
		}, profileOrDefault(profile))
		if strategy == nil {
			return Compose(reg, ComposeSpec{Name: name, Blocks: blocks}, false)
		}
		return composeOrderedWithMiddle(reg, name, blocks, "core:plan", strategy, false)
	case "delegate":
		return Compose(reg, ComposeSpec{Name: name, Blocks: []string{"core:pre_gate", "core:delegate"}}, false)
	default:
		return nil, fmt.Errorf("unknown compose preset %q", preset)
	}
}

func profileOrDefault(p string) string {
	if strings.TrimSpace(p) == "" {
		return "default"
	}
	return strings.TrimSpace(p)
}

// ComposeTaskTreeAgentic composes pre → plan → tools → [strategy] → tool → quality → error.
func ComposeTaskTreeAgentic(reg *Registry, name string, strategy *evolution.SerializableNode) (*evolution.SerializableNode, error) {
	return ComposePreset(reg, "agentic", name, strategy)
}

// ComposeTaskTreeFull composes the full agentic pipeline including RAG, clarify, and HITL gates.
func ComposeTaskTreeFull(reg *Registry, name string, strategy *evolution.SerializableNode) (*evolution.SerializableNode, error) {
	return ComposePreset(reg, "full", name, strategy)
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

// ListToolProfileBlocks returns block ids for tool profile blocks.
func ListToolProfileBlocks() []string {
	return []string{
		"core:tools_default",
		"core:tools_dev",
		"core:tools_research",
		"core:tools_startup",
		"core:tools_universal",
	}
}
