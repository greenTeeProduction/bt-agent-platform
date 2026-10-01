package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/knowledge"
)

func factoryTaskProperties() map[string]engine.Property {
	return map[string]engine.Property{
		"task":            {Type: "string", Description: "Full task for a response workflow; this factory does not execute external tools"},
		"user":            {Type: "string", Description: "Optional owner; requires a personal workspace"},
		"category":        {Type: "string", Description: "Optional category for the new tree"},
		"result_contract": {Type: "object", Description: "Required JSON result checks: json_fields maps keys to expected values; required_keys lists mandatory fields; optional min_length"},
		"steps":           {Type: "array", Description: "Optional preceding steps, each with instruction and result_contract; at most eight"},
		"max_tokens":      {Type: "integer", Description: "Per-call output budget, 64 to 8192; default 2048"},
		"parent_a":        {Type: "string", Description: "Optional resolvable parent recorded as design lineage"},
		"parent_b":        {Type: "string", Description: "Optional resolvable parent recorded as design lineage"},
	}
}

func createFactoryTask(deps *mcpDeps, args json.RawMessage) *engine.ToolResult {
	result := map[string]any{"persisted": false, "registered": false, "qualified": false}
	finish := func(err error) *engine.ToolResult {
		if err != nil {
			result["error"] = err.Error()
		}
		data, _ := json.Marshal(result)
		return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
	}
	var params struct {
		knowledge.TaskRequest
		ParentA string `json:"parent_a"`
		ParentB string `json:"parent_b"`
	}
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()
	if !json.Valid(args) {
		return finish(fmt.Errorf("invalid JSON arguments"))
	}
	if err := decoder.Decode(&params); err != nil {
		return finish(err)
	}
	params.User = strings.TrimSpace(params.User)
	if params.User != "" && deps.personaStore == nil {
		return finish(fmt.Errorf("personal tree store not configured"))
	}
	for _, parent := range []string{params.ParentA, params.ParentB} {
		if parent != "" {
			params.Parents = append(params.Parents, parent)
		}
	}
	factory := newTreeFactory(deps)
	tree, id, err := factory.BuildTask(params.TaskRequest)
	if err != nil {
		return finish(err)
	}
	version, err := evolution.TreeVersion(tree)
	if err != nil {
		return finish(err)
	}
	result["tree_id"], result["tree_version"] = id, version
	result["node_count"] = evolution.CountNodes(tree)
	result["kind"], result["owner"] = "response", params.User
	result["qualification"] = "unexecuted; creation and validation are not task-performance evidence"
	persistGeneratedTreeForUser(deps, params.User, id, tree, result)
	if persisted, _ := result["persisted"].(bool); !persisted {
		return finish(fmt.Errorf("tree persistence failed: %v", result))
	}
	// Personal task text must not enter the shared discovery index. Owned
	// trees resolve by ID through the requesting user's workspace.
	if params.User == "" && deps.kg != nil {
		category, _ := tree.Metadata["category"].(string)
		meta := &knowledge.TreeMeta{ID: id, Name: tree.Name, Category: category, Description: params.Task, NodeCount: evolution.CountNodes(tree), Tags: []string{"factory_response", "unqualified"}, Capabilities: []knowledge.Capability{{Action: "generate_response", Domain: category}}}
		for _, parent := range params.Parents {
			meta.Relations = append(meta.Relations, knowledge.Relation{Target: parent, Type: "derived_from"})
		}
		deps.kg.Register(meta)
		result["registered"] = true
	}
	result["action"] = "created"
	return finish(nil)
}
