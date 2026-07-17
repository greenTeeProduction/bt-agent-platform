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

// resolveTreeForUser scopes tree resolution to one requesting user's own
// workspace, so a scheduled personal automation's deterministic slug tree ID
// (goal:automate_<slug>) can never resolve to a different user's compiled
// tree just because it was compiled first. Wired into agent.RunDeps so
// agent.RunOnce consults it whenever the run's Definition has a known owner.
func resolveTreeForUser(user, id string) *evolution.SerializableNode {
	return domains.ResolveTreeIDForUser(user, id)
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
// persistJobs selects job-table write access: the daemon (noMCPMode) owns the
// durable FileJobStore; MCP/CLI sibling instances get a read-only view so
// their scheduler state can never clobber the daemon's table — sibling saves
// were the attributed 2026-07-15 job-table wiper.
func buildSchedulerConfig(cfg *config.Config, reg *agent.Registry, hist *agent.History, buildRevision string, persistJobs bool) agent.SchedulerConfig {
	var jobStore agent.JobStore = agent.NewFileJobStore(agent.SchedulerJobsFile())
	if !persistJobs {
		jobStore = agent.NewReadOnlyJobStore(jobStore)
	}
	return agent.SchedulerConfig{
		Registry: reg,
		History:  hist,
		JobStore: jobStore,
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
		return localAgentResult(agentName, task, res, err)
	})
}

// routedRunResult adapts a reliability.AgentResult — the AgentExecutor/
// AgentRouter contract's thinner result shape — back into an agent.RunResult
// so scheduler and DLQ-replay callers built around agentRunner.RunOnce's
// richer result keep working unchanged whether a task executed locally or
// dispatched to a remote peer. Falls back to deriving outcome from Success
// when the executor backend left Outcome unset (e.g. a remote node that
// hasn't been updated to populate it). TraceID/SpanID never round-trip
// through this boundary, so DLQ trace-parent linking degrades to the
// caller's own context for router-dispatched runs — a tracing-correlation
// loss only, not an outcome one.
func routedRunResult(agentName, task string, ar *reliability.AgentResult) *agent.RunResult {
	if ar == nil {
		return nil
	}
	outcome := ar.Outcome
	if outcome == "" {
		if ar.Success {
			outcome = "success"
		} else {
			outcome = "failure" // canonical failure token, not the one-off "failed"
		}
	}
	return &agent.RunResult{
		AgentName: agentName,
		Task:      task,
		Outcome:   outcome,
		Output:    ar.Output,
		Quality:   ar.QualityScore,
		Duration:  ar.Duration,
	}
}

// attemptOutcomeError builds the per-attempt error the scheduler retry policy
// sees when RunOnce returns a non-success outcome with a nil runErr. It folds
// the run output tail in via agent.OutcomeErrorDetail so retry-exhaustion DLQ
// entries record *why* the agent failed, not just the bare outcome word.
func attemptOutcomeError(outcome, output string) error {
	return fmt.Errorf("agent outcome: %s: %s", outcome, agent.OutcomeErrorDetail(output))
}

// recordSchedulerAttempt records one scheduler attempt against the agent's SLO
// metrics and returns the error the retry policy should observe: nil stops the
// loop (terminal), non-nil keeps retrying.
//
// Three dispositions:
//   - success (outcome=="success", no runErr): RecordSuccess, terminal. A retry
//     that finally succeeds (attempts>1) also RecordRecovery.
//   - rate-limit carryover (agent.IsRateLimitCarryover(outcome)): RecordDeferred,
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
	if agent.IsRateLimitCarryover(outcome) {
		slo.RecordDeferred()
		return nil
	}
	// Healthy no-code outcomes (no_change: analysis-only; degraded:
	// deterministic fallback) are terminal by design — RunOnce returns them
	// with a nil error. Retrying them burned a full Claude cycle per attempt
	// and dead-lettered honest runs (2026-07-15). Like the rate-limit
	// carryover they are deferred: neither success nor failure in SLO stats.
	if runErr == nil && agent.IsHealthyOutcome(outcome) && outcome != "success" {
		slo.RecordDeferred()
		return nil
	}
	slo.RecordFailure(latency)
	if runErr != nil {
		return runErr
	}
	return attemptOutcomeError(outcome, output)
}

// dlqReplayOutcomeError classifies a drop-safe DLQ replay's RunResult into
// the error the replay executor returns to reliability.DeadLetterQueue's
// Replay: nil drops the entry (reliability.go:249-251), non-nil keeps it
// queued for another replay. Mirrors recordSchedulerAttempt/
// IsBreakerSuccess above — rate-limit carryover and the other healthy
// no-code outcomes (no_change, degraded) are terminal-and-healthy, not
// failures, so a replay that gracefully pauses or lands on an
// analysis-only/deterministic-fallback outcome is dropped instead of
// endlessly re-replayed. A nil result classifies as healthy — there is
// nothing to flag as a failure.
func dlqReplayOutcomeError(res *agent.RunResult) error {
	if res == nil {
		return nil
	}
	if agent.IsRateLimitCarryover(res.Outcome) || agent.IsHealthyOutcome(res.Outcome) {
		return nil
	}
	return fmt.Errorf("agent outcome: %s: %s", res.Outcome, agent.OutcomeErrorDetail(res.Output))
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

// validateDomainRegistry runs engine.ValidateTree over every tree in a domain
// registry and returns the resulting messages, each prefixed with the
// offending domain name, so a startup failure names the broken tree instead
// of just a bare node name.
func validateDomainRegistry(registry map[string]*evolution.SerializableNode) []string {
	var msgs []string
	for name, tree := range registry {
		for _, m := range engine.ValidateTree(tree) {
			msgs = append(msgs, fmt.Sprintf("%s: %s", name, m))
		}
	}
	return msgs
}

func main() {
	engine.Init()
	engine.SetAsDefault()

	// `bt-agent --version` prints the build identity and exits 0 without
	// starting the daemon. Used by the deploy-drift restart handoff to
	// smoke-test a freshly rebuilt binary before adopting it.
	if versionRequested() {
		id := dashboard.ReadBuildIdentity()
		fmt.Printf("bt-agent revision=%s vcs_time=%s dirty=%v\n", id.Revision, id.CommitTime, id.Dirty)
		return
	}

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
	domainRegistry := domains.AllDomainTrees()
	if msgs := validateDomainRegistry(domainRegistry); len(msgs) > 0 {
		for _, m := range msgs {
			engine.Error("domain tree failed validation", "message", m)
		}
		fmt.Fprintf(os.Stderr, "fatal: %d domain tree validation error(s), see log above\n", len(msgs))
		os.Exit(1)
	}
	expectedDomains := make([]string, 0, len(domainRegistry))
	for name := range domainRegistry {
		expectedDomains = append(expectedDomains, "domain:"+name)
	}
	kg.ExpectedDomains = expectedDomains
	// Wire the NotebookLM domain fitness function into the graph's per-tree
	// fitness update so genuine notebooklm/notebooklm_consumer runs are scored
	// by its anti-fabrication-aware function instead of the generic EMA.
	domains.RegisterNotebookLMFitness(kg)
	reliability.SafeGo("kg-build-index", func() {
		if err := kg.BuildIndex(); err != nil {
			fmt.Fprintf(os.Stderr, "KG: embedding build skipped: %v\n", err)
		}
	}, nil)

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
	agentReg, regErr := agent.NewRegistry(agent.RegistryDir())
	if regErr != nil {
		// A failed/partial registry is why empty-registry reconciles happen;
		// keep going (the daemon can still serve MCP/A2A) but say so loudly.
		engine.Error("agent registry construction failed — scheduling and agent runs will be degraded", "dir", agent.RegistryDir(), "error", regErr)
	}
	agentHist, _ := agent.NewHistory(agent.HistoryDir())
	agentLocalMem := agent.MemoryDir()
	dlq := reliability.NewDeadLetterQueue(agent.DLQFile())
	engine.TaskDLQ = dlq
	engine.BuildRevision = buildID.Revision

	jobStoreDir := agent.JobsDir()
	_ = os.MkdirAll(jobStoreDir, 0755)

	// Persistent agent scheduler (with FileJobStore for durability across
	// restarts; read-only for MCP/CLI siblings — see buildSchedulerConfig).
	//
	// Idle-window drift adoption (daemon only): the moment a cycle completes
	// with nothing in flight, adopt a drifted binary SYNCHRONOUSLY in the
	// scheduler loop — the queue is blocked for the rebuild's duration, so the
	// tick loop cannot race a new job into flight (the async kick variant lost
	// exactly that race on saturated fleets, 2026-07-15). globalSched is
	// assigned below before Start(), and the hook only ever fires from the
	// scheduler's own loop, so the closure reads are ordered.
	var globalSched *agent.Scheduler
	scfg := buildSchedulerConfig(cfg, agentReg, agentHist, buildID.Revision, noMCPMode())
	if noMCPMode() {
		if repoDir, wdErr := os.Getwd(); wdErr == nil {
			idleDriftCfg := agent.DriftWatchConfig{
				RepoDir:         repoDir,
				RunningRevision: buildID.Revision,
				AutoRebuild:     agent.AutoRebuildEnabled(),
				AutoRestart:     agent.AutoRestartEnabled(),
				Targets:         agent.DefaultRebuildTargets(repoDir),
				Binary:          "bt-agent",
				Backoff:         agent.NewRebuildBackoff(),
				// Post-rebuild restart re-check; nil-safe: no scheduler yet
				// means "assume busy" and defer the restart.
				InFlightFn: func() bool { return globalSched == nil || globalSched.AnyInFlight() },
			}
			scfg.OnCycleIdle = func() { agent.AdoptDriftOnIdle(idleDriftCfg) }
		}
	}
	globalSched = agent.NewScheduler(scfg)

	agentRunner := &agent.RunDeps{
		Registry:           agentReg,
		History:            agentHist,
		LLM:                llmClient,
		RefStore:           refStore,
		TreeStore:          treeStore,
		ResolveTree:        resolveTree,
		ResolveTreeForUser: resolveTreeForUser,
	}

	// ── Horizontal-scaling substrate ────────────────────────────────────────
	// Resolve the A2A base URL up front (env-overridable, same value the A2A
	// server below binds to) and construct the AgentRouter here, before the
	// scheduler and DLQ replay closures that must dispatch through it — both
	// capture agentRouter by reference, so it must exist at this point in
	// source even though it initially has no remote peers. Peers discovered
	// from the live A2A card registry are folded in via AddEndpoints once the
	// A2A server has started further down; until then (and on any run where
	// a2aErr != nil) the router simply routes every task to the local
	// executor, matching pre-substrate behavior exactly.
	a2aPort := 8686
	if p := os.Getenv("BT_A2A_PORT"); p != "" {
		_, _ = fmt.Sscanf(p, "%d", &a2aPort)
	}
	a2aBaseURL := fmt.Sprintf("http://localhost:%d", a2aPort)
	if u := os.Getenv("BT_A2A_BASE_URL"); u != "" {
		a2aBaseURL = u
	}
	localExec := newLocalAgentExecutor(a2aBaseURL, agentRunner)
	agentRouter := reliability.NewRouterFromEndpoints(localExec, nil)

	// Sibling gate (2026-07-15): only the daemon (noMCPMode) runs the cron
	// loop. MCP/CLI sibling instances previously started a full scheduler too —
	// firing phantom duplicate cycles, marking the daemon's in-flight jobs
	// "crashed", and clobbering the shared job table. Siblings keep the
	// scheduler OBJECT (read-only job view for MCP schedule tools) but never
	// tick it.
	if noMCPMode() {
		reliability.SafeGo("scheduler-start", func() {
			globalSched.Start(func(ctx agent.RunContext) (outcome, output string, res *agent.RunResult, err error) {
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
					// Dispatch through agentRouter instead of calling
					// agentRunner.RunOnce directly, so a scheduled run can reach a
					// remote peer once one joins the registry (single-node
					// deployments fall through to the local executor unchanged).
					routedRes, runErr := agentRouter.Execute(ctx.AgentName, task)
					attemptRes := routedRunResult(ctx.AgentName, task, routedRes)
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
		}, nil)
	}

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
				AutoRestart:     agent.AutoRestartEnabled(),
				// Fleet owner: only THIS watcher restarts sibling units after
				// a sweep (bt-dashboard's watcher rebuilds its own binary only).
				RestartSiblings: true,
				Targets:         agent.DefaultRebuildTargets(repoDir),
				Binary:          "bt-agent",
				Backoff:         agent.NewRebuildBackoff(),
				InFlightFn:      globalSched.AnyInFlight,
			}, agent.DefaultDriftCheckInterval)
		}
	}

	// Drop-safe DLQ replay consumer (c8094002 ms2) — daemon only: MCP-spawned
	// sibling instances share the same DLQ file and must not double-replay.
	// Replays run the agent ONCE, without the scheduler's retry/DLQ wrapper,
	// so a failed replay can never push a duplicate dead letter.
	if noMCPMode() {
		dlq.SetReplayExecutor(func(e reliability.DeadLetterEntry) error {
			// Dispatch through agentRouter instead of calling
			// agentRunner.RunOnce directly, so a replayed dead letter can
			// reach a remote peer once one joins the registry. The
			// AgentExecutor contract carries no context, so the previous
			// 30-minute deadline no longer applies to routed replays — an
			// inherent limit of the router seam, not of this call site.
			routedRes, runErr := agentRouter.Execute(e.Agent, e.Task)
			if runErr != nil {
				return runErr
			}
			res := routedRunResult(e.Agent, e.Task, routedRes)
			return dlqReplayOutcomeError(res)
		})
		reliability.SafeGo("dlq-replay-scan-ticker", func() {
			ticker := time.NewTicker(dlqReplayScanInterval)
			defer ticker.Stop()
			for range ticker.C {
				runDLQReplayScanOnce(dlq)
			}
		}, nil)
	}

	// Auto-load agent schedules on startup — daemon only. Sibling instances
	// must not re-Schedule() shared jobs (noMCPMode() gate, 2026-07-15): their
	// calls rewrote NextRun and registry YAML from a process that never runs
	// the jobs.
	if noMCPMode() {
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

	// Per-user personalization store (ADR-133 Phase 1). A nil store degrades
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

	// In-tree autopilot hook (ADR-133 Phase 4): trees embedding the
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
	// a2aPort/a2aBaseURL were resolved earlier, alongside the AgentRouter
	// construction that needs them ahead of the scheduler/DLQ closures.
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
		// Share the platform's run-history store so tasks executed over A2A
		// leave a RunRecord, matching what runJob/RunTaskResult already record
		// for scheduler- and dashboard-driven runs.
		a2aSrv.Executor.History = agentHist
		// Let bt_agent_create and autopilot's activateAutomation refresh the
		// A2A card registry after they mutate agentReg, so newly created
		// agents become reachable over A2A/auctions without a restart.
		deps.refreshA2ACards = a2aSrv.RefreshCards
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
		reliability.SafeGo("a2a-server-start", func() {
			if err := a2aSrv.Start(); err != nil {
				logA2AServeError(err)
			}
		}, nil)
		engine.Info("a2a server started", "port", a2aPort, "agents", len(a2aSrv.CardCache))

		// ── Horizontal-scaling substrate ────────────────────────────────────
		// Fold remote peers from the live A2A card registry into the
		// AgentRouter constructed earlier (before the scheduler/DLQ replay
		// closures that dispatch through it): reduce peer cards to remote
		// endpoints (excluding this node) and add them alongside the local
		// in-process fallback already installed. A single-node registry
		// yields no peers, so the router keeps routing every task to the
		// local executor until peers join.
		agentRouter.AddEndpoints(endpointsFromCards(a2aSrv.CardCache, a2aBaseURL))
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

// runDLQReplayScanOnce performs a single tick of the drop-safe DLQ replay
// scan: reload the shared on-disk queue, then replay every ready entry.
// Reload is required each tick because each process holds its own in-memory
// queue over the shared file; dashboard/MCP requeue stamps land on disk
// only, so a scan against the stale view would never see them.
//
// The whole tick body runs under reliability.Recover — mirroring the
// per-tick pattern in internal/llm/health.go's ticker loop — so a single
// panicking dlq.Replay (e.g. a poison-pill entry whose replay executor
// panics) can't escape and unwind the caller's ticker goroutine, which
// would silently end all future scan ticks.
func runDLQReplayScanOnce(dlq *reliability.DeadLetterQueue) {
	_ = reliability.Recover("dlq-replay-scan-tick", func() {
		dlq.Reload()
		for _, id := range dlq.RequeuedReady() {
			if entry, ok := dlq.Replay(id); ok {
				engine.Info("dlq: replayed requeued entry", "id", entry.ID, "agent", entry.Agent)
			}
		}
	})
}

func noMCPMode() bool {
	for _, arg := range os.Args[1:] {
		if arg == "--no-mcp" || arg == "no-mcp" {
			return true
		}
	}
	return false
}

func versionRequested() bool {
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-version" || arg == "version" {
			return true
		}
	}
	return false
}

// localAgentResult adapts a RunOnce result to the AgentExecutor contract.
// The result survives ALONGSIDE a non-nil error: RunOnce wraps every
// non-"success", non-healthy outcome as an error, and dropping the result on
// that path lost the rate-limit carryover sentinel (2026-07-16 20:30 — the
// scheduler saw outcome "" instead of goap_fusion_rate_limited, retried a
// healthy pause 3x, and dead-lettered it). Outcome preserves RunOnce's raw
// disposition across the boundary so callers dispatching through
// agentRouter.Execute can still distinguish it instead of collapsing every
// non-"success" run to a bare failure.
func localAgentResult(agentName, task string, res *agent.RunResult, err error) (*reliability.AgentResult, error) {
	if res == nil {
		return nil, err
	}
	ar := &reliability.AgentResult{
		Agent:        agentName,
		Task:         task,
		Output:       res.Output,
		Duration:     res.Duration,
		Success:      err == nil && res.Outcome == "success",
		QualityScore: res.Quality,
		Outcome:      res.Outcome,
	}
	if err != nil {
		ar.Error = err.Error()
	}
	return ar, err
}
