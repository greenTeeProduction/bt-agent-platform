package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nico/go-bt-evolve/internal/blocks"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/mcp"
	"github.com/nico/go-bt-evolve/internal/metrics"
)

// registerBlockTools registers MCP tools for reusable tree building blocks.
func registerBlockTools(server *mcp.Server, deps *mcpDeps) {
	server.RegisterTool("bt_blocks_list_profiles", "List tool-profile building blocks (core:tools_*)",
		map[string]mcp.Property{}, []string{},
		func(_ json.RawMessage) *mcp.ToolResult {
			ids := blocks.ListToolProfileBlocks()
			data, _ := json.Marshal(map[string]any{"profiles": ids})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_blocks_list", "List reusable behavior-tree building blocks",
		map[string]mcp.Property{},
		[]string{},
		func(_ json.RawMessage) *mcp.ToolResult {
			list := blocks.DefaultRegistry.List()
			data, _ := json.Marshal(map[string]any{"blocks": list, "count": len(list)})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_blocks_get", "Get a building block definition by id",
		map[string]mcp.Property{
			"block_id": {Type: "string", Description: "Block id, e.g. core:tool_execution"},
		},
		[]string{"block_id"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				BlockID string `json:"block_id"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return mcpErr(err)
			}
			b := blocks.DefaultRegistry.Get(params.BlockID)
			if b == nil {
				return mcpErr(fmt.Errorf("unknown block %q", params.BlockID))
			}
			data, _ := json.Marshal(b)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_blocks_compose", "Compose a task/action tree from building blocks on demand",
		map[string]mcp.Property{
			"name":          {Type: "string", Description: "Root sequence name"},
			"block_ids":     {Type: "string", Description: "Comma-separated block ids"},
			"tools_profile": {Type: "string", Description: "Tool profile: default, dev, research, startup, universal"},
			"preset":        {Type: "string", Description: "Compose preset: default, agentic, hitl, full (overrides block_ids when set)"},
			"task_tree":     {Type: "boolean", Description: "If true, use default task layout: pre_gate + strategy + tool_execution + error_handling"},
			"strategy":      {Type: "string", Description: "Optional tree id for middle StrategyRouter (domain:code_review, etc.)"},
			"save":          {Type: "boolean", Description: "Save composed tree as active agent tree"},
			"inline":        {Type: "boolean", Description: "Inline blocks instead of SubTreeRef"},
		},
		[]string{"block_ids"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				Name         string `json:"name"`
				BlockIDs     string `json:"block_ids"`
				Preset       string `json:"preset"`
				ToolsProfile string `json:"tools_profile"`
				TaskTree     bool   `json:"task_tree"`
				Strategy     string `json:"strategy"`
				Save         bool   `json:"save"`
				Inline       bool   `json:"inline"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return mcpErr(err)
			}
			reg := blocks.DefaultRegistry
			var tree *evolution.SerializableNode
			var err error
			switch {
			case params.Preset != "":
				var strategy *evolution.SerializableNode
				if params.Strategy != "" {
					strategy = resolveTree(params.Strategy)
				}
				tree, err = blocks.ComposePresetWithTools(reg, params.Preset, params.ToolsProfile, params.Name, strategy)
			case params.TaskTree:
				var strategy *evolution.SerializableNode
				if params.Strategy != "" {
					strategy = resolveTree(params.Strategy)
				}
				tree, err = blocks.ComposeTaskTree(reg, params.Name, strategy)
			default:
				ids := strings.Split(params.BlockIDs, ",")
				for i := range ids {
					ids[i] = strings.TrimSpace(ids[i])
				}
				spec := blocks.ComposeSpec{Name: params.Name, Blocks: ids}
				if params.Strategy != "" {
					spec.Middle = resolveTree(params.Strategy)
				}
				tree, err = blocks.Compose(reg, spec, params.Inline)
			}
			if err != nil {
				return mcpErr(err)
			}
			if params.Save && deps.treeStore != nil {
				_ = deps.treeStore.Save(tree)
				deps.bb.TreeStore = deps.treeStore
				*deps.bt = engine.BuildTree(tree, deps.bb)
			}
			data, _ := json.Marshal(map[string]any{
				"composed": true,
				"name":     tree.Name,
				"refs":     blocks.HasSubTreeRefs(tree),
				"tree":     tree,
			})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_blocks_register", "Register a custom building block from the current tree subtree",
		map[string]mcp.Property{
			"block_id":    {Type: "string", Description: "New block id (e.g. custom:my_block)"},
			"node_name":   {Type: "string", Description: "Node name in active tree to promote"},
			"description": {Type: "string", Description: "Optional description"},
		},
		[]string{"block_id", "node_name"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				BlockID     string `json:"block_id"`
				NodeName    string `json:"node_name"`
				Description string `json:"description"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return mcpErr(err)
			}
			tree, _ := deps.treeStore.Load()
			if tree == nil {
				return mcpErr(fmt.Errorf("no active tree loaded"))
			}
			b, err := blocks.PromoteSubtree(blocks.DefaultRegistry, tree, params.NodeName, params.BlockID)
			if err != nil {
				return mcpErr(err)
			}
			if params.Description != "" {
				b.Description = params.Description
				_ = blocks.DefaultRegistry.Register(*b)
			}
			data, _ := json.Marshal(b)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_blocks_compose_evolve", "Compose from blocks then run one evolution mutation pass",
		map[string]mcp.Property{
			"block_ids": {Type: "string", Description: "Comma-separated block ids"},
			"name":      {Type: "string", Description: "Root name"},
		},
		[]string{"block_ids"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				BlockIDs string `json:"block_ids"`
				Name     string `json:"name"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return mcpErr(err)
			}
			ids := strings.Split(params.BlockIDs, ",")
			for i := range ids {
				ids[i] = strings.TrimSpace(ids[i])
			}
			tree, err := blocks.Compose(blocks.DefaultRegistry, blocks.ComposeSpec{
				Name: params.Name, Blocks: ids,
			}, false)
			if err != nil {
				return mcpErr(err)
			}
			ops := blocks.RandomBlockMutation(blocks.DefaultRegistry, tree)
			ops = blocks.DefaultRegistry.FilterEvolutionMutations(ops)
			applied := evolution.ApplyMutations(tree, ops)
			data, _ := json.Marshal(map[string]any{
				"mutations_applied": applied,
				"operations":        ops,
				"tree":              tree,
			})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})
	server.RegisterTool("bt_blocks_fitness", "Return per-block fitness scores from metrics",
		map[string]mcp.Property{}, []string{},
		func(_ json.RawMessage) *mcp.ToolResult {
			snap := metrics.BlockFitnessSnapshot()
			rank := blocks.FitnessRanking()
			data, _ := json.Marshal(map[string]any{"fitness": snap, "ranking": rank})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_blocks_freeze", "Freeze a block (set mutable=false)",
		map[string]mcp.Property{
			"block_id": {Type: "string", Description: "Block id to freeze"},
		},
		[]string{"block_id"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				BlockID string `json:"block_id"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return mcpErr(err)
			}
			if err := blocks.DefaultRegistry.Freeze(params.BlockID); err != nil {
				return mcpErr(err)
			}
			data, _ := json.Marshal(map[string]string{"status": "frozen", "block_id": params.BlockID})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_blocks_promote", "Promote a block to a new custom version",
		map[string]mcp.Property{
			"source_id": {Type: "string", Description: "Source block id (e.g. core:pre_gate)"},
			"dest_id":   {Type: "string", Description: "Optional destination id (custom:pre_gate_v2)"},
		},
		[]string{"source_id"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				SourceID string `json:"source_id"`
				DestID   string `json:"dest_id"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return mcpErr(err)
			}
			b, err := blocks.DefaultRegistry.PromoteVersion(params.SourceID, params.DestID)
			if err != nil {
				return mcpErr(err)
			}
			data, _ := json.Marshal(b)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

}

func mcpErr(err error) *mcp.ToolResult {
	data, _ := json.Marshal(map[string]string{"error": err.Error()})
	return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
}
