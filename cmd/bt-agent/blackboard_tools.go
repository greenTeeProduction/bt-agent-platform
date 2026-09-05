package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/engine"
)

func registerBlackboardTools(server *engine.Server, deps *mcpDeps) {
	scopeProps := map[string]engine.Property{
		"scope":    {Type: "string", Description: "Scope kind: run, session, or agent"},
		"scope_id": {Type: "string", Description: "Scope identifier (run_id, session_id, or agent name)"},
		"key":      {Type: "string", Description: "Blackboard key"},
	}

	readHandler := func(args json.RawMessage) *engine.ToolResult {
		var params struct {
			Scope   string `json:"scope"`
			ScopeID string `json:"scope_id"`
			Key     string `json:"key"`
		}
		_ = json.Unmarshal(args, &params)
		mgr, err := bbManager(deps)
		if err != nil {
			return bbError(err)
		}
		scope, err := parseBBScope(params.Scope, params.ScopeID)
		if err != nil {
			return bbError(err)
		}
		e, err := mgr.Get(scope, params.Key)
		if err != nil {
			return bbError(err)
		}
		data, _ := json.Marshal(e)
		return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
	}

	server.RegisterTool("bt_bb_read", "Read a value from the scoped blackboard",
		scopeProps,
		[]string{"scope", "scope_id", "key"},
		readHandler)

	server.RegisterTool("bt_bb_write", "Write a value to the scoped blackboard",
		map[string]engine.Property{
			"scope":        {Type: "string", Description: "Scope kind: run, session, or agent"},
			"scope_id":     {Type: "string", Description: "Scope identifier"},
			"key":          {Type: "string", Description: "Blackboard key"},
			"value":        {Type: "string", Description: "Value to store"},
			"summary":      {Type: "string", Description: "Optional short summary for listings"},
			"content_type": {Type: "string", Description: "Content type (default text)"},
		},
		[]string{"scope", "scope_id", "key", "value"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Scope       string `json:"scope"`
				ScopeID     string `json:"scope_id"`
				Key         string `json:"key"`
				Value       string `json:"value"`
				Summary     string `json:"summary"`
				ContentType string `json:"content_type"`
			}
			_ = json.Unmarshal(args, &params)
			mgr, err := bbManager(deps)
			if err != nil {
				return bbError(err)
			}
			scope, err := parseBBScope(params.Scope, params.ScopeID)
			if err != nil {
				return bbError(err)
			}
			ct := params.ContentType
			if ct == "" {
				ct = "text"
			}
			if err := mgr.Set(scope, params.Key, params.Value, params.Summary, ct); err != nil {
				return bbError(err)
			}
			data, _ := json.Marshal(map[string]any{
				"status": "stored", "scope": params.Scope, "scope_id": params.ScopeID,
				"key": params.Key, "bytes": len(params.Value),
			})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_bb_list", "List keys in a scoped blackboard",
		map[string]engine.Property{
			"scope":    {Type: "string", Description: "Scope kind: run, session, or agent"},
			"scope_id": {Type: "string", Description: "Scope identifier"},
			"prefix":   {Type: "string", Description: "Optional key prefix filter"},
			"limit":    {Type: "integer", Description: "Max entries (default 50)"},
		},
		[]string{"scope", "scope_id"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Scope   string `json:"scope"`
				ScopeID string `json:"scope_id"`
				Prefix  string `json:"prefix"`
				Limit   int    `json:"limit"`
			}
			_ = json.Unmarshal(args, &params)
			if params.Limit <= 0 {
				params.Limit = 50
			}
			mgr, err := bbManager(deps)
			if err != nil {
				return bbError(err)
			}
			scope, err := parseBBScope(params.Scope, params.ScopeID)
			if err != nil {
				return bbError(err)
			}
			entries, err := mgr.List(scope, params.Prefix, params.Limit)
			if err != nil {
				return bbError(err)
			}
			data, _ := json.Marshal(map[string]any{"entries": entries, "count": len(entries)})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_bb_delete", "Delete a key from the scoped blackboard",
		scopeProps,
		[]string{"scope", "scope_id", "key"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Scope   string `json:"scope"`
				ScopeID string `json:"scope_id"`
				Key     string `json:"key"`
			}
			_ = json.Unmarshal(args, &params)
			mgr, err := bbManager(deps)
			if err != nil {
				return bbError(err)
			}
			scope, err := parseBBScope(params.Scope, params.ScopeID)
			if err != nil {
				return bbError(err)
			}
			if err := mgr.Delete(scope, params.Key); err != nil {
				return bbError(err)
			}
			data, _ := json.Marshal(map[string]string{"status": "deleted", "key": params.Key})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})
}

func bbManager(deps *mcpDeps) (*blackboard.Manager, error) {
	if deps == nil || deps.agentRunner == nil {
		return nil, fmt.Errorf("agent runner not configured")
	}
	return deps.agentRunner.BoardManager(), nil
}

func parseBBScope(kind, id string) (blackboard.Scope, error) {
	kind = strings.TrimSpace(strings.ToLower(kind))
	id = strings.TrimSpace(id)
	if id == "" {
		return blackboard.Scope{}, fmt.Errorf("scope_id required")
	}
	switch kind {
	case string(blackboard.ScopeRun):
		return blackboard.Scope{Kind: blackboard.ScopeRun, ID: id}, nil
	case string(blackboard.ScopeSession):
		return blackboard.Scope{Kind: blackboard.ScopeSession, ID: id}, nil
	case string(blackboard.ScopeAgent):
		return blackboard.Scope{Kind: blackboard.ScopeAgent, ID: id}, nil
	default:
		return blackboard.Scope{}, fmt.Errorf("unknown scope %q (use run, session, or agent)", kind)
	}
}

func bbError(err error) *engine.ToolResult {
	data, _ := json.Marshal(map[string]string{"error": err.Error()})
	return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
}
