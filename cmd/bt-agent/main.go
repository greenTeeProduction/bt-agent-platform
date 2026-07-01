package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	a2a_mod "github.com/nico/go-bt-evolve/internal/a2a"
	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/audit"
	"github.com/nico/go-bt-evolve/internal/blocks"
	"github.com/nico/go-bt-evolve/internal/config"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/factory"
	"github.com/nico/go-bt-evolve/internal/hitl"
	"github.com/nico/go-bt-evolve/internal/knowledge"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/tracing"
)

func resolveTree(id string) *evolution.SerializableNode {
	return domains.ResolveTreeID(id)
}

func main() {
	engine.Init()
	engine.Info("bt-agent starting", "version", "1.0.0", "binary", "go-bt-agent")

	// ── Configuration ─────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		engine.Warn("config validation warning, using defaults", "error", err)
		cfg, _ = config.Load()
		if cfg == nil {
			fmt.Fprintf(os.Stderr, "fatal: config load failed\n")
			os.Exit(1)
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		engine.Error("failed to get home directory", "error", err)
		os.Exit(1)
	}
	engine.AgentMemoryBaseDir = home
	engine.DelegateToTreeFn = func(treeID string, bb *engine.Blackboard) (string, error) {
		ser := resolveTree(treeID)
		if ser == nil {
			return "", fmt.Errorf("unknown tree: %s", treeID)
		}
		cmd, err := engine.BuildAndValidate(ser, bb)
		if err != nil {
			return "", err
		}
		return engine.RunTask(bb, cmd), nil
	}
	platformHome := agent.HomeDir()
	if _, err := hitl.InitStore(platformHome); err != nil {
		engine.Warn("hitl store init failed", "error", err)
	}
	config.ApplyHITLPolicy(cfg)
	audit.Init(platformHome)
	sloEvidencePath := filepath.Join(platformHome, "slo", "slo-metrics.json")

	// ── Persistence ────────────────────────────────────────────────────────
	refStore, err := evolution.NewStore(filepath.Join(home, ".go-bt-reflections"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
	blocks.InitRegistry(filepath.Join(home, ".go-bt-reflections"))

	treeStore, err := evolution.NewTreeStore(filepath.Join(home, ".go-bt-reflections"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}

	// ── LLM Provider ───────────────────────────────────────────────────────
	llmClient, err := llm.NewProvider(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: llm provider: %v\n", err)
		os.Exit(1)
	}
	engine.Info("llm provider initialized", "provider", cfg.LLMProvider)

	// Graceful Degradation: LLM health monitor
	llmHealth := llm.NewHealthMonitor(cfg.OllamaHost, 30*time.Second)
	llmHealth.Start()

	// ── Agent Factory ──────────────────────────────────────────────────────
	agentFactory, err := factory.NewAgentFactory(llmClient, home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: factory: %v\n", err)
		os.Exit(1)
	}

	// ── Knowledge Graph ────────────────────────────────────────────────────
	kg := knowledge.GlobalGraph
	if kg == nil {
		kg = knowledge.BuildKnowledgeGraph()
	}
	go func() {
		if err := kg.BuildIndex(); err != nil {
			fmt.Fprintf(os.Stderr, "KG: embedding build skipped: %v\n", err)
		}
	}()

	// ── Behavior Tree ──────────────────────────────────────────────────────
	tree, err := treeStore.Load()
	if err != nil || tree == nil {
		tree = evolution.DefaultTree()
		_ = treeStore.Save(tree)
	}

	bb := &engine.Blackboard{
		Reflections: refStore,
		TreeStore:   treeStore,
		LLM:         llmClient,
	}

	bt := engine.BuildTree(tree, bb)

	// ── Agent Platform ─────────────────────────────────────────────────────
	agentReg, _ := agent.NewRegistry(agent.RegistryDir())
	agentHist, _ := agent.NewHistory(agent.HistoryDir())
	agentLocalMem := agent.MemoryDir()
	dlq := reliability.NewDeadLetterQueue(agent.DLQFile())
	engine.TaskDLQ = dlq

	jobStoreDir := agent.JobsDir()
	_ = os.MkdirAll(jobStoreDir, 0755)

	// Persistent agent scheduler (with FileJobStore for durability across restarts)
	globalSched := agent.NewScheduler(agent.SchedulerConfig{
		Registry: agentReg,
		History:  agentHist,
		JobStore: agent.NewFileJobStore(agent.SchedulerJobsFile()),
		CBStore: agent.NewAgentCircuitBreakerStore(agent.CircuitBreakerOptions{
			Threshold: cfg.CBThreshold,
			Cooldown:  time.Duration(cfg.CBCooldownSecs) * time.Second,
		}),
	})

	agentRunner := &agent.RunDeps{
		Registry:    agentReg,
		History:     agentHist,
		LLM:         llmClient,
		RefStore:    refStore,
		TreeStore:   treeStore,
		ResolveTree: resolveTree,
	}

	go globalSched.Start(func(ctx agent.RunContext) (outcome, output string, res *agent.RunResult, err error) {
		task := ctx.Task
		if task == "" {
			task = ctx.AgentName
		}

		treeName := ctx.AgentName
		if inst, getErr := agentReg.Get(ctx.AgentName); getErr == nil {
			treeName = inst.Definition.Tree
		}

		policy := reliability.RetryPolicy{
			MaxRetries:   cfg.RetryMaxRetries,
			Base:         time.Duration(cfg.RetryBaseDelayMs) * time.Millisecond,
			MaxDelay:     time.Duration(cfg.RetryMaxDelayMs) * time.Millisecond,
			LLMBase:      time.Duration(cfg.RetryLLMBaseMs) * time.Millisecond,
			RetryUnknown: true, // retry unknown errors to match legacy behavior
		}
		switch cfg.RetryJitter {
		case "no_jitter":
			policy.Jitter = reliability.NoJitter
		case "full_jitter":
			policy.Jitter = reliability.FullJitterStrategy
		case "equal_jitter":
			policy.Jitter = reliability.EqualJitterStrategy
		case "decorrelated_jitter":
			policy.Jitter = reliability.DecorrelatedJitterStrategy
		default:
			policy.Jitter = reliability.FullJitterStrategy
		}
		// SLO evidence (B1): record per-attempt outcomes so the gardener's
		// validation gate has real execution data to judge deployments by.
		slo := engine.GetSLOMetrics(ctx.AgentName, treeName)
		attempts := 0
		err = policy.ExecuteContext(ctx.Context, func() error {
			attempts++
			attemptStart := time.Now()
			attemptRes, runErr := agentRunner.RunOnce(ctx.Context, ctx.AgentName, task, agent.RunOptions{
				InjectMemory:   true,
				EnforceQuality: true,
			})
			if attemptRes != nil {
				outcome = attemptRes.Outcome
				output = attemptRes.Output
				res = attemptRes
			}
			if runErr == nil && attemptRes != nil && attemptRes.Outcome == "success" {
				slo.RecordSuccess(time.Since(attemptStart))
				if attempts > 1 {
					slo.RecordRecovery(0)
				}
				return nil
			}
			slo.RecordFailure(time.Since(attemptStart))
			if runErr != nil {
				return runErr
			}
			return fmt.Errorf("agent outcome: %s", outcome)
		})

		if saveErr := engine.SaveSLOMetrics(sloEvidencePath); saveErr != nil {
			engine.Error("failed to persist SLO evidence", "error", saveErr)
		}

		if err != nil {
			dlq.Push(reliability.DeadLetterEntry{
				ID:       fmt.Sprintf("%s-%d", ctx.AgentName, time.Now().UnixNano()),
				Task:     task,
				Agent:    ctx.AgentName,
				Error:    err.Error(),
				Attempts: 3,
				FailedAt: time.Now(),
				Circuit:  "scheduler",
			})
		}

		return outcome, output, res, err
	})

	// Auto-load agent schedules on startup
	for _, inst := range agentReg.List() {
		sched := inst.Definition.Schedule
		if sched != "" && sched != "on_demand" {
			if _, err := globalSched.Schedule(inst.Definition.Name, sched, "2h", 3); err != nil {
				engine.Info("auto-schedule failed", "agent", inst.Definition.Name, "error", err)
			} else {
				engine.Info("auto-scheduled agent", "agent", inst.Definition.Name, "schedule", sched)
			}
		}
	}

	// ── MCP Server ─────────────────────────────────────────────────────────
	server := engine.NewServer("go-bt-agent")

	// Create a shared memory store for MCP tools (stores per-agent memory)
	sharedMem, _ := agent.NewMemoryStore(agentLocalMem, "_global", 200)

	// Register all MCP tools via the extracted handler function.
	registerMCPTools(server, &mcpDeps{
		bb:           bb,
		bt:           &bt,
		treeStore:    treeStore,
		refStore:     refStore,
		agentFactory: agentFactory,
		kg:           kg,
		llmClient:    llmClient,
		llmHealth:    llmHealth,
		cfg:          cfg,
		agentReg:     agentReg,
		agentHist:    agentHist,
		agentMem:     sharedMem,
		globalSched:  globalSched,
		dlq:          dlq,
		agentRunner:  agentRunner,
	})

	server.SetSecurity(true, os.Getenv("BT_API_KEY"))
	server.SetRateLimit(2, 5)
	server.SetMaxMessageSize(1 << 20)

	// ── Tracing (OTel SDK; no-op unless OTEL_EXPORTER_OTLP_ENDPOINT/BT_OTLP_ENDPOINT set) ──
	tracingShutdown := tracing.InitFromEnv("bt-agent")
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tracingShutdown(ctx)
	}()

	// ── A2A Server ──────────────────────────────────────────────────────────
	a2aPort := 8686
	if p := os.Getenv("BT_A2A_PORT"); p != "" {
		_, _ = fmt.Sscanf(p, "%d", &a2aPort)
	}
	a2aBaseURL := fmt.Sprintf("http://localhost:%d", a2aPort)
	if u := os.Getenv("BT_A2A_BASE_URL"); u != "" {
		a2aBaseURL = u
	}

	a2aSrv, a2aErr := a2a_mod.NewServer(agentReg, llmClient, a2aPort, a2aBaseURL)
	if a2aErr != nil {
		engine.Warn("a2a server init failed, continuing without A2A", "error", a2aErr)
	}

	// ── Agent Event Bus ─────────────────────────────────────────────────────
	agent.InitAgentBus(200)
	engine.Info("agent event bus initialized", "max_history", 200)

	// ── Hermes Webhook Bridge (AgentBus → Hermes events) ─────────────────────
	whPublisher := agent.NewWebhookPublisher("http://localhost:8644", agent.DefaultWebhookSecrets())
	whPublisher.Attach(agent.GlobalAgentBus)
	engine.Info("hermes webhook bridge attached")

	if a2aErr == nil {
		// Inject tree resolver and pre-resolve trees for all agents
		a2a_mod.SetTreeResolver(resolveTree)
		a2a_mod.InitEngineDelegate()
		a2aSrv.Executor.TreeMap = make(map[string]*evolution.SerializableNode)
		for _, inst := range agentReg.List() {
			if t := resolveTree(inst.Definition.Tree); t != nil {
				a2aSrv.Executor.TreeMap[inst.Definition.Name] = t
			}
		}
		go func() {
			if err := a2aSrv.Start(); err != nil {
				engine.Error("a2a server failed", "error", err)
			}
		}()
		engine.Info("a2a server started", "port", a2aPort, "agents", len(a2aSrv.CardCache))
	}

	if noMCPMode() {
		engine.Info("MCP server disabled (--no-mcp), A2A + scheduler running")
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		engine.Info("bt-agent shutdown signal received")
		return
	}

	if err := server.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

func noMCPMode() bool {
	for _, arg := range os.Args[1:] {
		if arg == "--no-mcp" || arg == "no-mcp" {
			return true
		}
	}
	return false
}
