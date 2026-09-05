package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/factory"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/tracing"

	btcore "github.com/rvitorper/go-bt/core"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/ollama"
)

// langAgentServer holds the stores and evolved agent backing the la_* tool
// handlers so they can be exercised directly in tests without going through
// the MCP transport.
type langAgentServer struct {
	refStore  *evolution.Store
	treeStore *evolution.TreeStore
	bb        *engine.Blackboard
	bt        btcore.Command[engine.Blackboard]
	evolved   *agent.EvolvedAgent
}

// newLangAgentServer wires up the reflection store, tree store, blackboard,
// behavior tree, and evolved langchain agent rooted at home. It loads the
// existing tree if one is persisted, otherwise seeds and persists the
// default tree.
func newLangAgentServer(home string, llmClient llm.LLM, langLLM llms.Model) (*langAgentServer, error) {
	refStore, err := evolution.NewStore(filepath.Join(home, ".go-bt-reflections"))
	if err != nil {
		return nil, err
	}
	treeStore, err := evolution.NewTreeStore(filepath.Join(home, ".go-bt-reflections"))
	if err != nil {
		return nil, err
	}

	agentFactory, _ := factory.NewAgentFactory(llmClient, home)

	tree, err := treeStore.Load()
	if err != nil || tree == nil {
		tree = evolution.DefaultTree()
		if err := treeStore.Save(tree); err != nil {
			return nil, err
		}
	}

	bb := &engine.Blackboard{
		Reflections: refStore,
		TreeStore:   treeStore,
		LLM:         llmClient,
	}

	bt := engine.BuildTree(tree, bb)

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

	evolved, err := agent.NewEvolvedAgent(agent.Config{
		LLMClient:    llmClient,
		LangLLM:      langLLM,
		RefStore:     refStore,
		TreeStore:    treeStore,
		AgentFactory: agentFactory,
		RunTaskFn:    runTaskFn,
		BB:           bb,
	})
	if err != nil {
		return nil, err
	}

	return &langAgentServer{
		refStore:  refStore,
		treeStore: treeStore,
		bb:        bb,
		bt:        bt,
		evolved:   evolved,
	}, nil
}

// handleRun implements la_run: run a task through the evolved langchain agent.
func (s *langAgentServer) handleRun(args json.RawMessage) *engine.ToolResult {
	var params struct {
		Task string `json:"task"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return &engine.ToolResult{
			Content: []engine.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}},
		}
	}

	result, err := s.evolved.Run(context.Background(), params.Task)
	if err != nil {
		return &engine.ToolResult{
			Content: []engine.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}},
		}
	}

	response := map[string]any{
		"result":  result,
		"outcome": s.bb.Outcome,
	}
	data, _ := json.Marshal(response)
	return &engine.ToolResult{
		Content: []engine.ContentItem{{Type: "text", Text: string(data)}},
	}
}

// handleFitness implements la_fitness: evolved agent fitness and tree stats.
func (s *langAgentServer) handleFitness(args json.RawMessage) *engine.ToolResult {
	tree, _ := s.treeStore.Load()
	records, _ := s.refStore.LoadAll()
	failures := s.refStore.CountFailures()
	successes := len(records) - failures
	rate := 0.0
	if len(records) > 0 {
		rate = float64(successes) / float64(len(records))
	}
	nodeCount := 0
	if tree != nil {
		nodeCount = evolution.CountNodes(tree)
	}
	result := map[string]any{
		"total_tasks":  len(records),
		"successes":    successes,
		"failures":     failures,
		"success_rate": fmt.Sprintf("%.1f%%", rate*100),
		"node_count":   nodeCount,
		"tools":        len(s.evolved.Tools),
	}
	data, _ := json.Marshal(result)
	return &engine.ToolResult{
		Content: []engine.ContentItem{{Type: "text", Text: string(data)}},
	}
}

// handleEvolve implements la_evolve: force evolution of the behavior tree.
func (s *langAgentServer) handleEvolve(args json.RawMessage) *engine.ToolResult {
	tree, err := s.treeStore.Load()
	if err != nil || tree == nil {
		return &engine.ToolResult{
			Content: []engine.ContentItem{{Type: "text", Text: `{"error": "no tree"}`}},
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
		_ = s.treeStore.Save(tree)
	}
	result := map[string]any{
		"evolved":      applied > 0,
		"mutations":    applied,
		"nodes_before": before,
		"nodes_after":  after,
	}
	data, _ := json.Marshal(result)
	return &engine.ToolResult{
		Content: []engine.ContentItem{{Type: "text", Text: string(data)}},
	}
}

func main() {
	engine.Init()
	engine.SetAsDefault()
	engine.Info("bt-langagent starting", "version", "1.0.0", "binary", "go-bt-langagent")

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		engine.Error("failed to get home directory", "error", err)
		os.Exit(1)
	}

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

	s, err := newLangAgentServer(home, llmClient, langLLM)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		engine.Error("bt-langagent: failed to initialize", "error", err)
		os.Exit(1)
	}

	server := engine.NewServer("go-bt-langagent")

	server.RegisterTool("la_run", "Run a task through the evolved langchain agent (ReAct loop with BT tools)",
		map[string]engine.Property{
			"task": {Type: "string", Description: "The task to execute"},
		},
		[]string{"task"},
		s.handleRun)

	server.RegisterTool("la_fitness", "Get evolved agent fitness and tree stats",
		map[string]engine.Property{},
		nil,
		s.handleFitness)

	server.RegisterTool("la_evolve", "Force evolution of the behavior tree",
		map[string]engine.Property{},
		nil,
		s.handleEvolve)

	engine.Info("bt-langagent: 3 MCP tools ready, listening on stdin")
	server.SetSecurity(true, os.Getenv("BT_API_KEY"))
	server.SetRateLimit(2, 5)         // 2 req/s, burst 5
	server.SetMaxMessageSize(1 << 20) // 1 MB message size limit

	// ── Tracing (OTel SDK; no-op unless OTEL_EXPORTER_OTLP_ENDPOINT/BT_OTLP_ENDPOINT set) ──
	tracingShutdown := tracing.InitFromEnv("bt-langagent")
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tracingShutdown(ctx)
	}()

	logShutdown := engine.InitLogExport("bt-langagent")
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = logShutdown(ctx)
	}()

	if err := server.Run(); err != nil {
		engine.Error("bt-langagent: server error", "error", err)
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
