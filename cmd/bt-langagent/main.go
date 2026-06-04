package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/factory"
	"github.com/nico/go-bt-evolve/internal/langagent"
	"github.com/nico/go-bt-evolve/internal/llm"
	btlog "github.com/nico/go-bt-evolve/internal/log"
	"github.com/nico/go-bt-evolve/internal/mcp"
	"github.com/nico/go-bt-evolve/internal/reflection"
	"github.com/nico/go-bt-evolve/internal/tracing"

	"github.com/tmc/langchaingo/llms/ollama"
)

func main() {
	btlog.Init()
	btlog.Info("bt-langagent starting", "version", "1.0.0", "binary", "go-bt-langagent")

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		btlog.Error("failed to get home directory", "error", err)
		os.Exit(1)
	}

	// Stores
	refStore, _ := reflection.NewStore(filepath.Join(home, ".go-bt-reflections"))
	treeStore, _ := evolution.NewTreeStore(filepath.Join(home, ".go-bt-reflections"))

	// LLM clients (both our wrapper and langchaingo's native)
	llmCfg := llm.DefaultConfig()
	llmClient, err := llm.NewClient(llmCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: llm: %v\n", err)
		os.Exit(1)
	}

	langLLM, err := ollama.New(
		ollama.WithModel(llmCfg.Model),
		ollama.WithServerURL(llmCfg.ServerURL),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: langchain llm: %v\n", err)
		os.Exit(1)
	}

	// Factory
	agentFactory, _ := factory.NewAgentFactory(llmClient, home)

	// Load or create tree
	tree, err := treeStore.Load()
	if err != nil || tree == nil {
		tree = evolution.DefaultTree()
		_ = treeStore.Save(tree)
	}

	// Blackboard
	bb := &engine.Blackboard{
		Reflections: refStore,
		TreeStore:   treeStore,
		LLM:         llmClient,
	}

	// Build BT
	bt := engine.BuildTree(tree, bb)

	// Run function closure
	runTaskFn := func(task string) string {
		bb.Task = task
		bb.Complexity = ""
		bb.Plan = ""
		bb.Result = ""
		bb.Outcome = ""
		bb.KgResults = ""
		bb.CachedResult = ""
		return engine.RunTask(bb, bt)
	}

	// Create evolved agent
	evolved, err := langagent.NewEvolvedAgent(langagent.Config{
		LLMClient:    llmClient,
		LangLLM:      langLLM,
		RefStore:     refStore,
		TreeStore:    treeStore,
		AgentFactory: agentFactory,
		RunTaskFn:    runTaskFn,
		BB:           bb,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: agent: %v\n", err)
		os.Exit(1)
	}

	// MCP server
	server := mcp.NewServer("go-bt-langagent")

	server.RegisterTool("la_run", "Run a task through the evolved langchain agent (ReAct loop with BT tools)",
		map[string]mcp.Property{
			"task": {Type: "string", Description: "The task to execute"},
		},
		[]string{"task"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				Task string `json:"task"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return &mcp.ToolResult{
					Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}},
				}
			}

			result, err := evolved.Run(context.Background(), params.Task)
			if err != nil {
				return &mcp.ToolResult{
					Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}},
				}
			}

			response := map[string]interface{}{
				"result":  result,
				"outcome": bb.Outcome,
			}
			data, _ := json.Marshal(response)
			return &mcp.ToolResult{
				Content: []mcp.ContentItem{{Type: "text", Text: string(data)}},
			}
		})

	server.RegisterTool("la_fitness", "Get evolved agent fitness and tree stats",
		map[string]mcp.Property{},
		nil,
		func(_ json.RawMessage) *mcp.ToolResult {
			tree, _ := treeStore.Load()
			records, _ := refStore.LoadAll()
			failures := refStore.CountFailures()
			successes := len(records) - failures
			rate := 0.0
			if len(records) > 0 {
				rate = float64(successes) / float64(len(records))
			}
			nodeCount := 0
			if tree != nil {
				nodeCount = evolution.CountNodes(tree)
			}
			result := map[string]interface{}{
				"total_tasks":  len(records),
				"successes":    successes,
				"failures":     failures,
				"success_rate": fmt.Sprintf("%.1f%%", rate*100),
				"node_count":   nodeCount,
				"tools":        len(evolved.Tools),
			}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{
				Content: []mcp.ContentItem{{Type: "text", Text: string(data)}},
			}
		})

	server.RegisterTool("la_evolve", "Force evolution of the behavior tree",
		map[string]mcp.Property{},
		nil,
		func(_ json.RawMessage) *mcp.ToolResult {
			tree, err := treeStore.Load()
			if err != nil || tree == nil {
				return &mcp.ToolResult{
					Content: []mcp.ContentItem{{Type: "text", Text: `{"error": "no tree"}`}},
				}
			}
			ops := []evolution.MutationOp{
				{Operation: "wrap_retry", Target: "AnalyzeTask"},
				{Operation: "increase_retries", Target: "RetrySelfCorrect"},
			}
			before := evolution.CountNodes(tree)
			applied := evolution.ApplyMutations(tree, ops)
			after := evolution.CountNodes(tree)
			if applied > 0 {
				_ = treeStore.Save(tree)
			}
			result := map[string]interface{}{
				"evolved":      applied > 0,
				"mutations":    applied,
				"nodes_before": before,
				"nodes_after":  after,
			}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{
				Content: []mcp.ContentItem{{Type: "text", Text: string(data)}},
			}
		})

	btlog.Info("bt-langagent: 3 MCP tools ready, listening on stdin")
	server.SetSecurity(true, os.Getenv("BT_API_KEY"))
	server.SetRateLimit(2, 5)         // 2 req/s, burst 5
	server.SetMaxMessageSize(1 << 20) // 1 MB message size limit

	// ── Tracing: initialize global tracer ──
	tracingLogPath := filepath.Join(home, ".go-bt-evolve", "logs", "traces.log")
	if f, err := os.OpenFile(tracingLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		tracer := tracing.NewConsoleTracer("bt-langagent", f)
		tracing.ConfigureOTLPFromEnv(tracer)
		tracing.SetGlobalTracer(tracer)
	}

	if err := server.Run(); err != nil {
		btlog.Error("bt-langagent: server error", "error", err)
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
