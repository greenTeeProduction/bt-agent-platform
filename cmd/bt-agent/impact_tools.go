package main

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/knowledge"
)

// impactTests wraps knowledge.ImpactedTests as the pure handler bt_impact_tests
// registers (NotebookLM research: the impact graph had zero production
// consumers), so a caller can gate a commit on a change-scoped test list
// instead of always running the full suite.
func impactTests(root, source string) map[string]any {
	if strings.TrimSpace(source) == "" {
		return map[string]any{"error": "source is required"}
	}
	rel, err := knowledge.NormalizeImpactSource(root, source)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	tests, err := knowledge.ImpactedTests(root, rel)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"tests": tests, "source": source}
}

// registerImpactTools registers the change-impact-analysis MCP surface.
func registerImpactTools(server *engine.Server, deps *mcpDeps) {
	server.RegisterTool("bt_impact_tests", "Compute the change-impact test list for a changed source file: tests affected via import edges or directory proximity, so a commit can gate on a scoped test list instead of always running the full suite",
		map[string]engine.Property{
			"root":   {Type: "string", Description: "Module root directory (contains go.mod); defaults to the current working directory"},
			"source": {Type: "string", Description: "Module-relative path to the changed source file (e.g. \"internal/knowledge/impact.go\")"},
		},
		[]string{"source"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Root   string `json:"root"`
				Source string `json:"source"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}
			root := params.Root
			if root == "" {
				if wd, err := os.Getwd(); err == nil {
					root = wd
				}
			}
			result := impactTests(root, params.Source)
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})
}
