package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
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
	server.RegisterBlackboardTool("bt_program_reconcile", "Back up the framework program backlog and move unsupported historical RED-pass completions to review. Runs no implementation and grants no completion credit.", map[string]engine.Property{}, nil, func(_ json.RawMessage) *engine.ToolResult {
		user := ""
		if deps.bb != nil {
			user = deps.bb.User
		}
		result, err := engine.ReviewResearchPrograms(user)
		if err != nil {
			return mcpErr(err)
		}
		data, _ := json.Marshal(result)
		return textToolResult(string(data))
	})

	server.RegisterBlackboardTool("bt_program_review", "Revise a framework milestone held for review. Requires the exact current goal and a changed goal/test requirement; preserves evidence and reopens pending work without completion credit.",
		map[string]engine.Property{
			"program_id":      {Type: "string", Description: "Program ID from bt_research_status"},
			"milestone_index": {Type: "integer", Description: "Zero-based milestone index"},
			"expected_goal":   {Type: "string", Description: "Exact current goal; stale revisions are refused"},
			"revised_goal":    {Type: "string", Description: "Revised goal and meaningful regression requirement"},
		}, []string{"program_id", "milestone_index", "expected_goal", "revised_goal"}, func(args json.RawMessage) *engine.ToolResult {
			var p struct {
				Program  string `json:"program_id"`
				Index    *int   `json:"milestone_index"`
				Expected string `json:"expected_goal"`
				Revised  string `json:"revised_goal"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return mcpErr(err)
			}
			if p.Index == nil {
				return mcpErr(fmt.Errorf("milestone_index is required"))
			}
			user := ""
			if deps.bb != nil {
				user = deps.bb.User
			}
			if err := engine.ReviseResearchProgramMilestone(user, p.Program, *p.Index, p.Expected, p.Revised); err != nil {
				return mcpErr(err)
			}
			return textToolResult(`{"status":"pending","completion_credit":false}`)
		})
	server.RegisterBlackboardTool("bt_research_status", "Report this user's research sources, verified code deliveries, observed runtime builds and independently checked results. Observed adoption does not prove causal research impact.",
		map[string]engine.Property{}, nil, func(_ json.RawMessage) *engine.ToolResult {
			user := ""
			if deps.bb != nil {
				user = deps.bb.User
			}
			var releases *evolution.RuntimeReleaseStore
			if deps.treeStore != nil {
				releases = evolution.NewRuntimeReleaseStore(filepath.Join(deps.treeStore.Dir(), "runtime-versions"))
			}
			result, err := engine.ResearchRuntimeStatus(context.Background(), user, deps.refStore, releases)
			if err != nil {
				result = map[string]any{"error": err.Error()}
			}
			data, _ := json.Marshal(result)
			return textToolResult(string(data))
		})
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
