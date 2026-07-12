package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	a2acard "github.com/a2aproject/a2a-go/v2/a2a"
	a2a_mod "github.com/nico/go-bt-evolve/internal/a2a"
	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/audit"
	"github.com/nico/go-bt-evolve/internal/blocks"
	"github.com/nico/go-bt-evolve/internal/config"
	"github.com/nico/go-bt-evolve/internal/dashboard"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/factory"
	"github.com/nico/go-bt-evolve/internal/hitl"
	"github.com/nico/go-bt-evolve/internal/knowledge"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/persona"
	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/tracing"
)

func resolveTree(id string) *evolution.SerializableNode {
	return domains.ResolveTreeID(id)
}

// feedbackSnapshotPath resolves the on-disk knowledge-graph feedback snapshot
// path the scheduler loads on startup and persists to. It mirrors
// agent.FeedbackFile() so the daemon and the scheduler agree on a single
// location, keeping Fitness/RunCount/tool-edges durable across restarts.
func feedbackSnapshotPath() string { return agent.FeedbackFile() }

// experienceBankDir resolves the on-disk directory backing the daemon's
// persistent ExperienceBank. Rooted under agent.HomeDir() — like the rest of
// the platform state — so it honors BT_AGENT_HOME redirection and mutation
// experiences recorded by bt_evolve_genetic survive restarts.
func experienceBankDir() string { return filepath.Join(agent.HomeDir(), "experience") }

// buildSchedulerConfig assembles the SchedulerConfig the daemon hands to
// agent.NewScheduler. It is factored out of main() so the production wiring —
// durable FileJobStore, per-agent circuit breakers, and the FeedbackPath that
// rehydrates knowledge-graph feedback (Fitness/RunCount/tool-edges) across
// restarts — is asserted end-to-end by wiring_test.go instead of only living
// inside main(), where it can silently regress.
func buildSchedulerConfig(cfg *config.Config, reg *agent.Registry, hist *agent.History, buildRevision string) agent.SchedulerConfig {
	return agent.SchedulerConfig{
		Registry: reg,
		History:  hist,
		JobStore: agent.NewFileJobStore(agent.SchedulerJobsFile()),
		CBStore: agent.NewAgentCircuitBreakerStore(agent.CircuitBreakerOptions{
			Threshold: cfg.CBThreshold,
			Cooldown:  time.Duration(cfg.CBCooldownSecs) * time.Second,
		}),
		// Rehydrate knowledge-graph feedback (Fitness/RunCount/tool-edges) from
		// the on-disk snapshot on startup and persist it back, closing the
		// learn→discover→evolve loop across restarts. FeedbackFlushInterval is
		// left zero — NewScheduler defaults it to 30s when FeedbackPath is set.
		FeedbackPath: feedbackSnapshotPath(),
		// Deploy-drift diagnosis (program 94b0b31): lets runJob WARN when repo
		// HEAD has moved past this running binary at cycle-complete, and stamps
		// build_revision onto every AgentBus/webhook event.
		BuildRevision: buildRevision,
	}
}

// hostURLOf returns the "<scheme>://<host>" prefix of a raw URL, or "" when it
// cannot be parsed. It reduces an A2A interface URL (which carries an
// "/agents/<name>" path) to the peer node's execution base URL that a
// RemoteExecutor targets.
func hostURLOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// endpointsFromCards reduces the live A2A card registry to the set of remote
// AgentEndpoints the horizontal-scaling router should distribute agent tasks
// across. Each card advertises a JSON-RPC interface URL of the form
// "<scheme>://<host>/agents/<name>"; the peer node's execution base URL is that
// URL's scheme+host. Cards served by this very node (interface base == the
// daemon's own selfBaseURL) are excluded so the router never routes tasks back
// to itself, cards with no reachable interface are skipped, and peers are
// de-duplicated by base URL so each node yields a single RemoteExecutor rather
// than one per advertised agent. On a single-node deployment this yields no
// peers, so NewRouterFromEndpoints falls back to the local in-process executor —
// but the seam is live the moment peer cards join the registry.
func endpointsFromCards(cards map[string]*a2acard.AgentCard, selfBaseURL string) []reliability.AgentEndpoint {
	self := hostURLOf(selfBaseURL)
	seen := map[string]bool{}
	eps := make([]reliability.AgentEndpoint, 0, len(cards))
	for _, card := range cards {
		if card == nil {
			continue
		}
		var ifaceURL string
		for _, iface := range card.SupportedInterfaces {
			if iface != nil && iface.URL != "" {
				ifaceURL = iface.URL
				break
			}
		}
		base := hostURLOf(ifaceURL)
		if base == "" || base == self || seen[base] {
			continue
		}
		seen[base] = true
		eps = append(eps, reliability.AgentEndpoint{Name: base, BaseURL: base})
	}
	return eps
}

// newLocalAgentExecutor builds the in-process executor the router falls back to
// when no remote peer handles a task: it runs the named agent through the
// daemon's own RunDeps and adapts the RunResult to a reliability.AgentResult.
func newLocalAgentExecutor(nodeURL string, runner *agent.RunDeps) *reliability.LocalExecutor {
	return reliability.NewLocalExecutor(nodeURL, func(agentName, task string) (*reliability.AgentResult, error) {
		res, err := runner.RunOnce(context.Background(), agentName, task, agent.RunOptions{
			InjectMemory:   true,
			EnforceQuality: true,
		})
		if err != nil {
			return nil, err
		}
		return &reliability.AgentResult{
			Agent:        agentName,
			Task:         task,
			Output:       res.Output,
			Duration:     res.Duration,
			Success:      res.Outcome == "success",
			QualityScore: res.Quality,
		}, nil
	})
}

// attemptOutcomeError builds the per-attempt error the scheduler retry policy
// sees when RunOnce returns a non-success outcome with a nil runErr. It folds
// the run output tail in via agent.OutcomeErrorDetail so retry-exhaustion DLQ
// entries record *why* the agent failed, not just the bare outcome word.
func attemptOutcomeError(outcome, output string) error {
	return fmt.Errorf("agent outcome: %s: %s", outcome, agent.OutcomeErrorDetail(output))
}

// schedulerRateLimitCarryover is the sentinel outcome an agent surfaces when a
// scheduled run gracefully pauses on a Claude rate limit. The scheduler treats
// it as terminal — an expected, healthy backoff, neither retried nor
// dead-lettered — and records it as a *deferred* outcome rather than a success,
// so a rate-limit pause never inflates the success-count/success-latency stats
// the gardener's validation gate reads.
const schedulerRateLimitCarryover = "goap_fusion_rate_limited"

// recordSchedulerAttempt records one scheduler attempt against the agent's SLO
// metrics and returns the error the retry policy should observe: nil stops the
// loop (terminal), non-nil keeps retrying.
//
// Three dispositions:
//   - success (outcome=="success", no runErr): RecordSuccess, terminal. A retry
//     that finally succeeds (attempts>1) also RecordRecovery.
//   - rate-limit carryover (outcome==schedulerRateLimitCarryover): RecordDeferred,
//     terminal. The pause leaves the success/failure counters and latency totals
//     untouched so success rate and success-latency are unaffected.
//   - anything else: RecordFailure, retryable (returns runErr, or the wrapped
//     outcome error when RunOnce reported a non-success outcome with no runErr).
func recordSchedulerAttempt(slo *engine.SLOMetrics, outcome string, runErr error, output string, attempts int, latency time.Duration) error {
	if runErr == nil && outcome == "success" {
		slo.RecordSuccess(latency)
		if attempts > 1 {
			slo.RecordRecovery(0)
		}
		return nil
	}
	if outcome == schedulerRateLimitCarryover {
		slo.RecordDeferred()
		return nil
	}
	slo.RecordFailure(latency)
	if runErr != nil {
		return runErr
	}
	return attemptOutcomeError(outcome, output)
}

// dlqReplayScanInterval is how often the daemon consumes requeued dead-letter
// entries (dashboard/MCP "replay" flags) through the drop-safe replay executor.
const dlqReplayScanInterval = 5 * time.Minute

// truncateDLQField bounds a DLQ entry field for the bt_dlq_list report.
func truncateDLQField(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// logA2AServeError logs an A2A listener failure. "address already in use" is
// EXPECTED sibling contention — every MCP/CLI-spawned bt-agent next to the
// daemon triggers it (CLAUDE.md documents it as warned-and-ignored) — so it
// logs at WARN; anything else is a genuine ERROR.
func logA2AServeError(err error) {
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), "address already in use") {
		engine.Warn("a2a port busy — another bt-agent instance holds it (expected for MCP/CLI siblings)", "error", err)
		return
	}
	engine.Error("a2a server failed", "error", err)
}

func main() {
	engine.Init()
	engine.SetAsDefault()
	engine.Info("bt-agent starting", "version", "1.0.0", "binary", "go-bt-agent")

	// Embed the VCS build identity: publish the bt_build_info gauge and log
	// revision/commit-time/dirty so a running daemon's revision is comparable
	// against repo HEAD (stale-daemon-binary drift detection).
	buildID := dashboard.InstallBuildIdentity()
	engine.Info("build identity",
		"vcs_revision", buildID.Revision,
		"vcs_time", buildID.CommitTime,
		"vcs_dirty", buildID.Dirty)

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
	// Inject the live domain registry as the expected-domain set so CoverageGaps
	// audits against the real registry (domain:<name> IDs) instead of a stale
	// hardcoded slice. Injection here avoids an analytics→domains import cycle.
	expectedDomains := make([]string, 0, len(domains.AllDomainTrees()))
	for name := range domains.AllDomainTrees() {
		expectedDomains = append(expectedDomains, "domain:"+name)
	}
	kg.ExpectedDomains = expectedDomains
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
	engine.BuildRevision = buildID.Revision

	jobStoreDir := agent.JobsDir()
	_ = os.MkdirAll(jobStoreDir, 0755)

	// Persistent agent scheduler (with FileJobStore for durability across restarts)
	globalSched := agent.NewScheduler(buildSchedulerConfig(cfg, agentReg, agentHist, buildID.Revision))

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
			return recordSchedulerAttempt(slo, outcome, runErr, output, attempts, time.Since(attemptStart))
		})

		if saveErr := engine.SaveSLOMetrics(sloEvidencePath); saveErr != nil {
			engine.Error("failed to persist SLO evidence", "error", saveErr)
		}

		if err != nil {
			dlqParent := ctx.Context
			if res != nil && res.TraceID != "" {
				dlqParent = tracing.ContextWithTraceParentHeader(ctx.Context, "00-"+res.TraceID+"-"+res.SpanID+"-01")
			}
			_, dlqSpan := tracing.StartSpan(dlqParent, "agent.dlq_push")
			dlqSpan.SetAttribute("agent", ctx.AgentName)
			dlqSpan.RecordError(err)
			dlq.Push(reliability.DeadLetterEntry{
				ID:            fmt.Sprintf("%s-%d", ctx.AgentName, time.Now().UnixNano()),
				Task:          task,
				Agent:         ctx.AgentName,
				Error:         err.Error(),
				Attempts:      3,
				FailedAt:      time.Now(),
				Circuit:       "scheduler",
				BuildRevision: buildID.Revision,
			})
			dlqSpan.End()
		}

		return outcome, output, res, err
	})

	// Deploy-drift watcher (program 94b0b31) — daemon only; MCP-spawned sibling
	// instances (cycle sessions) must not run it. Detection-only by default:
	// logs a WARN when the running binary falls behind repo HEAD.
	// BT_AUTO_REBUILD_ON_DRIFT=1 opts into out-of-place rebuild+swap.
	if noMCPMode() {
		if repoDir, wdErr := os.Getwd(); wdErr == nil {
			agent.StartDriftWatcher(context.Background(), agent.DriftWatchConfig{
				RepoDir:         repoDir,
				RunningRevision: buildID.Revision,
				AutoRebuild:     agent.AutoRebuildEnabled(),
				Targets:         agent.DefaultRebuildTargets(repoDir),
				Binary:          "bt-agent",
			}, agent.DefaultDriftCheckInterval)
		}
	}

	// Drop-safe DLQ replay consumer (c8094002 ms2) — daemon only: MCP-spawned
	// sibling instances share the same DLQ file and must not double-replay.
	// Replays run the agent ONCE, without the scheduler's retry/DLQ wrapper,
	// so a failed replay can never push a duplicate dead letter.
	if noMCPMode() {
		dlq.SetReplayExecutor(func(e reliability.DeadLetterEntry) error {
			rctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			res, runErr := agentRunner.RunOnce(rctx, e.Agent, e.Task, agent.RunOptions{
				InjectMemory:   true,
				EnforceQuality: true,
			})
			if runErr != nil {
				return runErr
			}
			if res != nil && res.Outcome != "success" {
				return fmt.Errorf("agent outcome: %s: %s", res.Outcome, agent.OutcomeErrorDetail(res.Output))
			}
			return nil
		})
		go func() {
			ticker := time.NewTicker(dlqReplayScanInterval)
			defer ticker.Stop()
			for range ticker.C {
				// Each process holds its own in-memory queue over the shared
				// file; dashboard/MCP requeue stamps land on disk only, so a
				// scan against the stale view would never see them.
				dlq.Reload()
				for _, id := range dlq.RequeuedReady() {
					if entry, ok := dlq.Replay(id); ok {
						engine.Info("dlq: replayed requeued entry", "id", entry.ID, "agent", entry.Agent)
					}
				}
			}
		}()
	}

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

	// Persistent experience bank: warm-starts bt_evolve_genetic from prior
	// successful mutations and records new ones across restarts. A nil bank
	// (construction failure) degrades evolution to the memoryless path.
	expBank, expBankErr := evolution.NewExperienceBank(experienceBankDir())
	if expBankErr != nil {
		engine.Warn("experience bank unavailable — evolution runs memoryless", "error", expBankErr)
	}

	// Per-user personalization store (ADR-010 Phase 1). A nil store degrades
	// the bt_persona_* tools and bt_run_task user hooks to no-ops.
	personaStore, personaErr := persona.NewStore(agent.UsersDir())
	if personaErr != nil {
		engine.Warn("persona store unavailable — personalization disabled", "error", personaErr)
	}

	// Register all MCP tools via the extracted handler function.
	deps := &mcpDeps{
		bb:           bb,
		bt:           &bt,
		treeStore:    treeStore,
		refStore:     refStore,
		expBank:      expBank,
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
		personaStore: personaStore,
	}
	registerMCPTools(server, deps)

	// In-tree autopilot hook (ADR-010 Phase 4): trees embedding the
	// ConsiderTreeCompile action feed the automation autopilot directly
	// after a good run (injection-hook pattern, like DelegateToTreeFn).
	engine.ConsiderAutomationFn = func(user string) {
		if auto := considerAutomation(deps, user); auto["proposed"] == true {
			engine.Info("autopilot: automation proposed (in-tree)", "user", user,
				"tree", auto["tree_id"], "hitl", auto["hitl_id"])
		}
	}

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

	logShutdown := engine.InitLogExport("bt-agent")
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = logShutdown(ctx)
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
		// Supply the live candidate source to the auctioneer production wiring
		// (engine.AuctionDelegateFn is installed at link time by internal/agentexec).
		// Candidates are the same A2A cards this server serves.
		a2a_mod.AuctionCardsFn = a2aSrv.AuctionCardSource()
		a2aSrv.Executor.TreeMap = make(map[string]*evolution.SerializableNode)
		for _, inst := range agentReg.List() {
			if t := resolveTree(inst.Definition.Tree); t != nil {
				a2aSrv.Executor.TreeMap[inst.Definition.Name] = t
			} else {
				engine.Info("tree resolution failed for agent", "agent", inst.Definition.Name, "tree", inst.Definition.Tree)
			}
		}
		go func() {
			if err := a2aSrv.Start(); err != nil {
				logA2AServeError(err)
			}
		}()
		engine.Info("a2a server started", "port", a2aPort, "agents", len(a2aSrv.CardCache))

		// ── Horizontal-scaling substrate ────────────────────────────────────
		// Construct the AgentRouter from the live A2A card registry: reduce peer
		// cards to remote endpoints (excluding this node), and inject the local
		// in-process executor as the fallback. This is the first production
		// binary to build the RemoteExecutor + AgentRouter substrate from real
		// runtime state; a single-node registry yields no peers, so the router
		// routes every task to the local executor until peers join.
		localExec := newLocalAgentExecutor(a2aBaseURL, agentRunner)
		agentRouter := reliability.NewRouterFromEndpoints(localExec, endpointsFromCards(a2aSrv.CardCache, a2aBaseURL))
		engine.Info("agent router constructed from A2A card registry",
			"remote_peers", len(agentRouter.Executors()),
			"router", agentRouter.String())
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

	// If MCP server exits (e.g. stdin closed in --no-mcp daemon mode), block
	// until SIGTERM/SIGINT so the scheduler + A2A keep running.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	engine.Info("bt-agent running in daemon mode (--no-mcp), scheduler + A2A active")
	<-sigCh
	engine.Info("bt-agent shutdown signal received")
}

func noMCPMode() bool {
	for _, arg := range os.Args[1:] {
		if arg == "--no-mcp" || arg == "no-mcp" {
			return true
		}
	}
	return false
}
