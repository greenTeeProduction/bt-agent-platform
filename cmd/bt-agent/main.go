package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	a2a_mod "github.com/nico/go-bt-evolve/internal/a2a"
	"github.com/nico/go-bt-evolve/internal/config"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/factory"
	"github.com/nico/go-bt-evolve/internal/finance"
	"github.com/nico/go-bt-evolve/internal/knowledge"
	"github.com/nico/go-bt-evolve/internal/llm"
	btlog "github.com/nico/go-bt-evolve/internal/log"
	"github.com/nico/go-bt-evolve/internal/mcp"
	"github.com/nico/go-bt-evolve/internal/reflection"
	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/research"
	"github.com/nico/go-bt-evolve/internal/startup"
	"github.com/nico/go-bt-evolve/internal/thinktank"
	"github.com/nico/go-bt-evolve/internal/tracing"

	btcore "github.com/rvitorper/go-bt/core"
)

func init() {
	// ── Telegram Clarify — quality gate conditions ──
	engine.RegisterCondition("IsTelegram", func(b *engine.Blackboard) bool {
		if platform, ok := b.ChainState["platform"]; ok {
			if p, ok := platform.(string); ok && p == "telegram" {
				return true
			}
		}
		return false
	})

	engine.RegisterCondition("HasQuestion", func(b *engine.Blackboard) bool {
		response := b.Result
		if response == "" {
			response = b.Task
		}
		markers := []string{"?", "should I", "should we",
			"which ", "what ", "how ", "why ", "when ", "where ",
			"do you want", "would you like", "choose ", "pick ", "select "}
		lower := strings.ToLower(response)
		for _, m := range markers {
			if strings.Contains(lower, m) {
				return true
			}
		}
		return false
	})

	engine.RegisterCondition("IsClarifyUsed", func(b *engine.Blackboard) bool {
		if used, ok := b.ChainState["clarify_used"]; ok {
			if v, ok := used.(bool); ok && v {
				return true
			}
		}
		return strings.Contains(strings.ToLower(b.Result), "clarify") ||
			strings.Contains(strings.ToLower(b.Result), "multiple choice")
	})

	// ── Telegram Clarify — quality gate actions ──
	engine.RegisterAction("MarkClarifyOK", func(ctx *btcore.BTContext[engine.Blackboard]) int {
		b := ctx.Blackboard
		b.Outcome = "success"
		b.ChainState["telegram_clarify_ok"] = true
		return 1
	})

	engine.RegisterAction("ReportClarifyViolation", func(ctx *btcore.BTContext[engine.Blackboard]) int {
		b := ctx.Blackboard
		b.ChainState["telegram_clarify_violation"] = true
		b.ChainState["telegram_clarify_fix"] = "Use clarify(question=..., choices=[...]) instead of plain text"
		b.Outcome = "violation"
		return 1
	})

	engine.RegisterAction("SuggestFix", func(ctx *btcore.BTContext[engine.Blackboard]) int {
		b := ctx.Blackboard
		if s, ok := b.ChainState["telegram_clarify_suggestion"]; ok {
			if str, ok := s.(string); ok {
				b.Result = str
				b.Outcome = "success"
				return 1
			}
		}
		b.Result = "Use clarify(question=\"...\", choices=[\"Option A\", \"Option B\"])"
		b.Outcome = "success"
		return 1
	})
}

// resolveTree maps a tree identifier string to the actual tree object.
func resolveTree(id string) *evolution.SerializableNode {
	// hermes self-evolution tree
	if id == "hermes_evolve" {
		return evolution.HermesSelfEvolutionTree()
	}
	// stockfish evolution trees
	if id == "stockfish_evolve" {
		return evolution.StockfishEvolutionTree()
	}
	if id == "stockfish_loop" {
		return evolution.StockfishEvolutionLoop()
	}
	if id == "vault_manager" {
		return evolution.VaultManagerTree()
	}
	// Kanban trees
	if id == "kanban:task_creator" {
		return evolution.KanbanTaskCreatorTree()
	}
	if id == "kanban:refiner" {
		return evolution.KanbanRefinerTree()
	}
	if id == "kanban:qa" {
		return evolution.KanbanQATree()
	}
	if id == "kanban:monitor" {
		return evolution.KanbanBoardMonitorTree()
	}
	if id == "kanban:workflow" {
		return evolution.KanbanWorkflowTree()
	}
	if id == "kanban:autopilot" {
		return evolution.KanbanAutoPilotTree()
	}
	// NotebookLM tree
	if id == "notebooklm" {
		return evolution.NotebookLMTree()
	}
	if id == "hermes_obsidian" {
		return evolution.HermesObsidianOptimizerTree()
	}
	// godev
	if id == "godev" {
		return evolution.GoDeveloperTree()
	}
	// finance:<name>
	if len(id) > 8 && id[:8] == "finance:" {
		name := id[8:]
		trees := finance.AllFinanceTrees()
		return trees[name]
	}
	// research:<name>
	if len(id) > 9 && id[:9] == "research:" {
		name := id[9:]
		trees := research.ResearchTrees()
		return trees[name]
	}
	// domain:<name>
	if len(id) > 7 && id[:7] == "domain:" {
		name := id[7:]
		trees := domains.AllDomainTrees()
		return trees[name]
	}
	// startup:<role>
	if len(id) > 8 && id[:8] == "startup:" {
		role := id[8:]
		trees := startup.StartupTrees()
		if t, ok := trees[role]; ok {
			return t
		}
		roles := startup.Roles()
		return roles[role]
	}
	// thinktank:<role>
	if len(id) > 10 && id[:10] == "thinktank:" {
		switch role := id[10:]; role {
		case "synthesis":
			return thinktank.SynthesisTree()
		case "peer_review":
			return thinktank.PeerReviewTree()
		case "report":
			return thinktank.ReportGenerationTree()
		default:
			return thinktank.SynthesisTree()
		}
	}
	// default: try as direct tree name
	return evolution.DefaultTree()
}

func main() {
	btlog.Init()
	btlog.Info("bt-agent starting", "version", "1.0.0", "binary", "go-bt-agent")

	// ── Configuration ─────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		btlog.Warn("config validation warning, using defaults", "error", err)
		cfg, _ = config.Load()
		if cfg == nil {
			fmt.Fprintf(os.Stderr, "fatal: config load failed\n")
			os.Exit(1)
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		btlog.Error("failed to get home directory", "error", err)
		os.Exit(1)
	}
	agentHome, _ := os.UserHomeDir()

	// ── Persistence ────────────────────────────────────────────────────────
	refStore, err := reflection.NewStore(filepath.Join(home, ".go-bt-reflections"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
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
	btlog.Info("llm provider initialized", "provider", cfg.LLMProvider)

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
	agentReg, _ := agent.NewRegistry(agentHome + "/.go-bt-evolve/agents")
	agentHist, _ := agent.NewHistory(agentHome + "/.go-bt-evolve/history")
	agentLocalMem := agentHome + "/.go-bt-evolve/memory"
	dlq := reliability.NewDeadLetterQueue(agentHome + "/.go-bt-evolve/dead_letter_queue.json")

	// Create jobs directory for scheduler persistence
	jobStoreDir := agentHome + "/.go-bt-evolve/jobs"
	os.MkdirAll(jobStoreDir, 0755)

	// Persistent agent scheduler (with FileJobStore for durability across restarts)
	globalSched := agent.NewScheduler(agent.SchedulerConfig{
		Registry: agentReg,
		History:  agentHist,
		JobStore: agent.NewFileJobStore(jobStoreDir + "/scheduler-jobs.json"),
	})
	go globalSched.Start(func(ctx agent.RunContext) (outcome, output string, err error) {
		// ctx.Task is set by the scheduler from the agent's description.
		// Don't prepend "scheduled run" — that causes self-referential loops
		// when the agent investigates itself instead of its actual purpose.
		task := ctx.Task
		if task == "" {
			task = ctx.AgentName
		}

		// Inject agent memory context into the task
		agentMem, memErr := agent.NewMemoryStore(agentLocalMem, ctx.AgentName, 100)
		if memErr == nil {
			memCtx := agentMem.ContextBlock()
			if memCtx != "" {
				task = task + "\n\n" + memCtx
			}
			prevCtx := agentMem.PreviousRunContext(agentHist, ctx.AgentName, 2)
			if prevCtx != "" {
				task = task + "\n\n" + prevCtx
			}
		}

		// Resolve through agent registry first — agent names are not tree IDs.
		// Only fall back to direct tree resolution if no agent found.
		var tree *evolution.SerializableNode
		inst, getErr := agentReg.Get(ctx.AgentName)
		if getErr == nil {
			tree = resolveTree(inst.Definition.Tree)
		}
		if tree == nil {
			tree = resolveTree(ctx.AgentName)
		}
		if tree == nil {
			return "failure", "", fmt.Errorf("no tree found for agent %s", ctx.AgentName)
		}

		err = reliability.RetryWithBackoff(3, 1*time.Second, 30*time.Second, func() error {
			bb := &engine.Blackboard{Task: task, LLM: llmClient}
			bt := engine.BuildTree(tree, bb)
			_ = engine.RunTask(bb, bt)
			outcome = bb.Outcome
			output = bb.Result
			if bb.Outcome == "success" {
				return nil
			}
			return fmt.Errorf("agent outcome: %s", bb.Outcome)
		})

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

		return outcome, output, err
	})

	// Auto-load agent schedules on startup
	for _, inst := range agentReg.List() {
		sched := inst.Definition.Schedule
		if sched != "" && sched != "on_demand" {
			if _, err := globalSched.Schedule(inst.Definition.Name, sched, "2h", 3); err != nil {
				btlog.Info("auto-schedule failed", "agent", inst.Definition.Name, "error", err)
			} else {
				btlog.Info("auto-scheduled agent", "agent", inst.Definition.Name, "schedule", sched)
			}
		}
	}

	// ── MCP Server ─────────────────────────────────────────────────────────
	server := mcp.NewServer("go-bt-agent")

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
		agentHome:    agentHome,
		agentReg:     agentReg,
		agentHist:    agentHist,
		agentMem:     sharedMem,
		globalSched:  globalSched,
		dlq:          dlq,
	})

	server.SetSecurity(true, os.Getenv("BT_API_KEY"))
	server.SetRateLimit(2, 5)
	server.SetMaxMessageSize(1 << 20)

	// ── Tracing ────────────────────────────────────────────────────────────
	tracingLogPath := filepath.Join(home, ".go-bt-evolve", "logs", "traces.log")
	if f, err := os.OpenFile(tracingLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		tracing.SetGlobalTracer(tracing.NewConsoleTracer("bt-agent", f))
	}

	// ── A2A Server ──────────────────────────────────────────────────────────
	a2aPort := 8686
	if p := os.Getenv("BT_A2A_PORT"); p != "" {
		fmt.Sscanf(p, "%d", &a2aPort)
	}
	a2aBaseURL := fmt.Sprintf("http://localhost:%d", a2aPort)
	if u := os.Getenv("BT_A2A_BASE_URL"); u != "" {
		a2aBaseURL = u
	}

	a2aSrv, a2aErr := a2a_mod.NewServer(agentReg, llmClient, a2aPort, a2aBaseURL)
	if a2aErr != nil {
		btlog.Warn("a2a server init failed, continuing without A2A", "error", a2aErr)
	}

	// ── Agent Event Bus ─────────────────────────────────────────────────────
	agent.InitAgentBus(200)
	btlog.Info("agent event bus initialized", "max_history", 200)

	// ── Hermes Webhook Bridge (AgentBus → Hermes events) ─────────────────────
	whPublisher := agent.NewWebhookPublisher("http://localhost:8644", agent.DefaultWebhookSecrets())
	whPublisher.Attach(agent.GlobalAgentBus)
	btlog.Info("hermes webhook bridge attached")

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
				btlog.Error("a2a server failed", "error", err)
			}
		}()
		btlog.Info("a2a server started", "port", a2aPort, "agents", len(a2aSrv.CardCache))
	}

	if err := server.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
