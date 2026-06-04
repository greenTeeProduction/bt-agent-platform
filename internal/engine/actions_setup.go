// Package engine — Setup tool actions extracted from tree.go actionForName switch.
// Registers tool-setup actions. (SetupDefaultTools already in registry.go)
package engine

import (
	"strings"

	btcore "github.com/rvitorper/go-bt/core"
)

func init() {
	registerSetupActions()
}

func registerSetupActions() {
	RegisterAction("SetupDevTools", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.ChainTools = buildRealTools("go_build", "go_test", "go_vet", "web_search")
		return 1
	})

	RegisterAction("SetupUniversalTools", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.ChainTools = buildRealTools("shell_exec", "file_read", "file_write", "web_search", "calculator")
		return 1
	})

	RegisterAction("SetupDataPipelineTools", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.ChainTools = buildRealTools("file_read", "file_write", "shell_exec", "calculator")
		return 1
	})

	RegisterAction("DiscoverAvailableTools", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.ChainState == nil {
			bb.ChainState = map[string]any{}
		}
		available := availableToolNames(bb)
		if available == "(none)" {
			available = strings.Join(allRealToolNames(), ", ")
		}
		bb.ChainState["available_tools"] = available
		return 1
	})

	RegisterAction("EnsureTaskTools", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.ChainState == nil {
			bb.ChainState = map[string]any{}
		}
		requested := inferToolsForTask(bb.Task)
		added := appendMissingRealTools(bb, requested...)
		bb.ChainState["requested_tools"] = strings.Join(requested, ", ")
		bb.ChainState["created_tools"] = strings.Join(added, ", ")
		bb.ChainState["available_tools"] = availableToolNames(bb)
		return 1
	})

	RegisterAction("SetupNotebookLMTools", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.ChainTools = buildRealTools(
			"notebooklm_server_info",
			"notebooklm_list",
			"notebooklm_notebook_get",
			"notebooklm_research_start",
			"notebooklm_research_status",
			"notebooklm_research_import",
			"notebooklm_notebook_query",
			"notebooklm_refresh_auth",
			"shell_exec",
			"file_read",
			"file_write",
			"web_search",
		)
		return 1
	})

	RegisterAction("SetupResearchTools", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.ChainTools = buildRealTools("web_search", "http_get", "file_read", "shell_exec", "graphify", "calculator")
		return 1
	})

	RegisterAction("SetupStartupTools", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.ChainTools = buildRealTools("web_search", "calculator")
		return 1
	})
}

func appendMissingRealTools(bb *Blackboard, names ...string) []string {
	if bb == nil {
		return nil
	}
	existing := map[string]bool{}
	type named interface{ Name() string }
	for _, tool := range bb.ChainTools {
		if n, ok := tool.(named); ok {
			existing[n.Name()] = true
		}
	}
	added := []string{}
	for _, name := range names {
		if name == "" || existing[name] {
			continue
		}
		tool, ok := buildRealTool(name)
		if !ok {
			continue
		}
		bb.ChainTools = append(bb.ChainTools, tool)
		existing[name] = true
		added = append(added, name)
	}
	return added
}

func inferToolsForTask(task string) []string {
	lower := strings.ToLower(task)
	tools := []string{}
	add := func(names ...string) { tools = append(tools, names...) }

	if strings.Contains(lower, "notebooklm") || strings.Contains(lower, "notebook lm") || strings.Contains(lower, "notebook") {
		add("notebooklm_server_info", "notebooklm_list", "notebooklm_notebook_get", "notebooklm_notebook_query", "notebooklm_research_start", "notebooklm_research_status", "notebooklm_research_import", "notebooklm_refresh_auth")
	}
	if strings.Contains(lower, "code") || strings.Contains(lower, "review") || strings.Contains(lower, "bug") || strings.Contains(lower, "file") || strings.Contains(lower, ".go") || strings.Contains(lower, ".py") {
		add("file_read", "shell_exec")
	}
	if strings.Contains(lower, "build") || strings.Contains(lower, "test") || strings.Contains(lower, "ci") || strings.Contains(lower, "go ") {
		add("go_build", "go_test", "go_vet", "shell_exec")
	}
	if strings.Contains(lower, "research") || strings.Contains(lower, "web") || strings.Contains(lower, "http") || strings.Contains(lower, "url") {
		add("web_search", "http_get")
	}
	if strings.Contains(lower, "data") || strings.Contains(lower, "csv") || strings.Contains(lower, "json") || strings.Contains(lower, "extract") || strings.Contains(lower, "pipeline") {
		add("file_read", "file_write", "shell_exec", "calculator")
	}
	if strings.Contains(lower, "graph") || strings.Contains(lower, "graphify") || strings.Contains(lower, "architecture") {
		add("graphify", "file_read")
	}
	if len(tools) == 0 {
		add("shell_exec", "file_read", "web_search", "calculator")
	}
	return uniqueStrings(tools)
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}
	return unique
}
