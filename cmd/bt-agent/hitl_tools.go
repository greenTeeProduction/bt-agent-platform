package main

import (
	"encoding/json"
	"fmt"

	"github.com/nico/go-bt-evolve/internal/blocks"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/hitl"
)

func registerHITLTools(server *engine.Server, deps *mcpDeps) {
	server.RegisterTool("bt_hitl_list", "List human-in-the-loop approval requests",
		map[string]engine.Property{
			"pending_only": {Type: "boolean", Description: "If true, only pending requests"},
		},
		[]string{},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				PendingOnly bool `json:"pending_only"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return mcpErr(err)
			}
			if hitl.DefaultStore == nil {
				return mcpErr(fmt.Errorf("HITL store not initialized"))
			}
			var list []*hitl.Request
			if params.PendingOnly {
				list = hitl.DefaultStore.ListPending()
			} else {
				list = hitl.DefaultStore.ListAll()
			}
			data, _ := json.Marshal(map[string]any{"requests": list, "count": len(list)})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_hitl_get", "Get a single approval request by ID",
		map[string]engine.Property{
			"request_id": {Type: "string", Description: "HITL request id (hitl-xxxxxxxx)"},
		},
		[]string{"request_id"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				RequestID string `json:"request_id"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return mcpErr(err)
			}
			if hitl.DefaultStore == nil {
				return mcpErr(fmt.Errorf("HITL store not initialized"))
			}
			req, ok := hitl.DefaultStore.Get(params.RequestID)
			if !ok {
				return mcpErr(fmt.Errorf("request %q not found", params.RequestID))
			}
			data, _ := json.Marshal(req)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_hitl_approve", "Approve a pending human-in-the-loop request",
		map[string]engine.Property{
			"request_id": {Type: "string", Description: "HITL request id"},
			"reviewer":   {Type: "string", Description: "Reviewer name or id"},
			"comment":    {Type: "string", Description: "Optional approval comment"},
		},
		[]string{"request_id"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				RequestID string `json:"request_id"`
				Reviewer  string `json:"reviewer"`
				Comment   string `json:"comment"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return mcpErr(err)
			}
			if params.Reviewer == "" {
				params.Reviewer = "human"
			}
			req, err := hitl.DefaultStore.Approve(params.RequestID, params.Reviewer, params.Comment)
			if err != nil {
				return mcpErr(err)
			}
			data, _ := json.Marshal(req)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_hitl_reject", "Reject a pending human-in-the-loop request",
		map[string]engine.Property{
			"request_id": {Type: "string", Description: "HITL request id"},
			"reviewer":   {Type: "string", Description: "Reviewer name or id"},
			"reason":     {Type: "string", Description: "Rejection reason"},
		},
		[]string{"request_id", "reason"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				RequestID string `json:"request_id"`
				Reviewer  string `json:"reviewer"`
				Reason    string `json:"reason"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return mcpErr(err)
			}
			if params.Reviewer == "" {
				params.Reviewer = "human"
			}
			req, err := hitl.DefaultStore.Reject(params.RequestID, params.Reviewer, params.Reason)
			if err != nil {
				return mcpErr(err)
			}
			data, _ := json.Marshal(req)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_hitl_compose_task", "Compose a task tree with human approval before tool execution",
		map[string]engine.Property{
			"name":     {Type: "string", Description: "Root tree name"},
			"strategy": {Type: "string", Description: "Optional strategy tree id for middle section"},
			"save":     {Type: "boolean", Description: "Save as active agent tree"},
		},
		[]string{},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Name     string `json:"name"`
				Strategy string `json:"strategy"`
				Save     bool   `json:"save"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return mcpErr(err)
			}
			var strategy *evolution.SerializableNode
			if params.Strategy != "" {
				strategy = resolveTree(params.Strategy)
			}
			tree, err := blocks.ComposeTaskTreeWithHITL(blocks.DefaultRegistry, params.Name, strategy)
			if err != nil {
				return mcpErr(err)
			}
			if params.Save && deps.treeStore != nil {
				_ = deps.treeStore.Save(tree)
				*deps.bt = engine.BuildTree(tree, deps.bb)
			}
			data, _ := json.Marshal(map[string]any{"tree": tree, "blocks": blocks.DefaultTaskBlocksWithHITL})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})
}
