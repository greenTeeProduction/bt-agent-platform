// Package main — MCP tool registration helpers and extracted handlers for bt-agent.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/agentexec"
	"github.com/nico/go-bt-evolve/internal/benchmark"
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
	"github.com/nico/go-bt-evolve/internal/startup"
	"github.com/nico/go-bt-evolve/internal/thinktank"

	btcore "github.com/rvitorper/go-bt/core"
)

// checkLLMHealth returns a ToolResult with a degradation error if the LLM is
// unhealthy, or nil if the LLM is available. LLM-dependent tool handlers should
// call this first to fail fast with a clear message instead of timing out.
// islandArchivePath resolves the durable island-model archive bt_evolve_island
// warm-starts from and persists to (milestone 3/5 of the durable
// quality-diversity program), scoped per base tree (milestone 1/5 of the
// production-safe island archive program) so runs on different base trees do
// not warm-start-merge each other's genomes through a single shared file.
// Rooted under agent.HomeDir() so it honors BT_AGENT_HOME redirection and
// survives restarts with the rest of the platform state.
func islandArchivePath(treeID string) string {
	return filepath.Join(agent.HomeDir(), "island_archive-"+sanitizeArchiveTreeID(treeID)+".json")
}

// qtableArchivePath resolves the durable QTable archive bt_evolve_qlearning
// warm-starts from and persists to (milestone 2/4 of the durable Q-learning
// program, Q2 Evolvability), scoped per base tree like islandArchivePath so
// runs on different base trees do not warm-start-merge each other's learned
// Q-values through a single shared file.
func qtableArchivePath(treeID string) string {
	return filepath.Join(agent.HomeDir(), "qtable_archive-"+sanitizeArchiveTreeID(treeID)+".json")
}

// expertArchivePath resolves the durable ExpertKnowledge.LearnedPatterns
// archive bt_evolve_qlearning persists to and bt_evolve_expert warm-starts
// from (milestone 2/2 of the durable Expert Knowledge program, Q2
// Evolvability), scoped per base tree like qtableArchivePath so runs on
// different base trees do not merge each other's learned patterns through a
// single shared file.
func expertArchivePath(treeID string) string {
	return filepath.Join(agent.HomeDir(), "expert_archive-"+sanitizeArchiveTreeID(treeID)+".json")
}

// mapElitesArchivePath resolves the durable MAP-Elites archive bt_evolve_qd
// warm-starts from and persists to (Q2 Evolvability, NotebookLM research),
// scoped per base tree like islandArchivePath and qtableArchivePath so runs
// on different base trees do not warm-start-merge each other's illuminated
// niches through a single shared file.
func mapElitesArchivePath(treeID string) string {
	return filepath.Join(agent.HomeDir(), "map_elites_archive-"+sanitizeArchiveTreeID(treeID)+".json")
}

// paretoFrontArchivePath resolves the durable Pareto front archive
// bt_evolve_pareto warm-starts from and persists to (Q2 Evolvability,
// milestone 2/5 of the durable cross-run archive program), scoped per base
// tree like the other archive helpers so runs on different base trees do not
// warm-start-merge each other's Pareto-optimal individuals through a single
// shared file.
func paretoFrontArchivePath(treeID string) string {
	return filepath.Join(agent.HomeDir(), "pareto_front_archive-"+sanitizeArchiveTreeID(treeID)+".json")
}

// nsgaArchivePath resolves the durable NSGA-II Pareto front archive
// bt_evolve_multiobjective warm-starts from and persists to (Q2
// Evolvability, milestone 4/5 of the durable cross-run archive program),
// scoped per base tree like the other archive helpers so runs on different
// base trees do not warm-start-merge each other's Pareto-optimal
// individuals through a single shared file.
func nsgaArchivePath(treeID string) string {
	return filepath.Join(agent.HomeDir(), "nsga_archive-"+sanitizeArchiveTreeID(treeID)+".json")
}

// sanitizeArchiveTreeID maps a base-tree ID to a cross-platform-safe file name
// fragment (":" is invalid on Windows, "/" everywhere), mirroring the policy
// of evolution.TreeFileName. That helper is deliberately not reused: its
// tree-*.json naming would make the gardener registry adopt the archive as a
// generated tree.
func sanitizeArchiveTreeID(id string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(id) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// benchmarkRunSuiteFn is a package-level indirection over benchmark.RunSuite,
// mirroring the DelegateToA2AFn/AuctionDelegateFn test-seam pattern already
// used in this package. NSGA-II (and the other evolution algorithms this
// gate will be reused for) mutate via unseeded math/rand, so an evolved
// winner's actual structure — and therefore its real benchmark outcome — is
// not reproducible across test runs; only a seam lets a test force a
// regression deterministically.
var benchmarkRunSuiteFn = benchmark.RunSuite

// benchmarkGateEvolvedWinner runs both the base tree and an evolved winner
// through treeID's real internal/benchmark suite (Sandbox mode with a mock
// LLM — deterministic, no real LLM calls, -short-safe) and reports whether
// the winner regressed on SuccessRate. The evolve tools' structural fitness
// (evolution.StructuralMultiFitness and friends) scores mutations from
// structural heuristics alone and can rate one as elite while it actually
// performs worse than the untouched base tree, so callers must skip
// persisting a regressed winner to a durable cross-run archive (Q2
// Evolvability: gate durable-archive winners through the benchmark suite,
// not structural fitness alone).
func benchmarkGateEvolvedWinner(treeID string, base, winner *evolution.SerializableNode) (rejected bool, baseRate, winnerRate float64) {
	suite := benchmark.SuiteForTree(treeID)
	mock := &llm.MockLLM{}
	baseMetrics := benchmarkRunSuiteFn(base, suite, mock)
	winnerMetrics := benchmarkRunSuiteFn(winner, suite, mock)
	return winnerMetrics.SuccessRate < baseMetrics.SuccessRate, baseMetrics.SuccessRate, winnerMetrics.SuccessRate
}

func checkLLMHealth(health *llm.HealthMonitor, toolName string) *engine.ToolResult {
	if health == nil {
		return nil // no health monitor configured, proceed as normal
	}
	if !health.IsHealthy() {
		errMsg := fmt.Sprintf("LLM provider is currently %s — retry later when Ollama is available",
			health.State().Status().String())
		data, _ := json.Marshal(map[string]string{"error": errMsg, "tool": toolName, "degraded": "true"})
		return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
	}
	return nil
}

// persistGeneratedTree validates a runtime-generated tree and persists it as
// tree-<id>.json so it becomes resolvable by ID (agentexec dynamic resolver)
// and visible to the gardener registry (ADR-010 Phase 0). The outcome is
// recorded in the tool result: an invalid tree stays KG-registered for
// discovery but is never persisted, so it can never be executed.
// recordEvolvedFitness writes a winning QD/island elite's structural fitness
// back into the knowledge graph via the monotone, clamped "evolved" outcome so
// fitness-aware discovery can surface archive-improved trees (milestone 4/5). A
// missing graph or unregistered tree is a no-op — RecordRun ignores unknown
// tree IDs — so this is safe to call unconditionally after evolution.
func recordEvolvedFitness(deps *mcpDeps, treeID string, eliteFitness float64) {
	if deps.kg == nil || treeID == "" {
		return
	}
	deps.kg.RecordRun(knowledge.RunRecord{
		TreeID:  treeID,
		Task:    "quality-diversity archive elite",
		Outcome: "evolved",
		Quality: eliteFitness,
	})
}

// addFailureContext threads a bottleneck's structured last-failure task/outcome
// into its per-tree evolution report entry, making the re-evolution
// failure-targeted: each evolved tree is annotated with the concrete failing
// task that motivated it. Both empty (no recorded failure) is a no-op, so trees
// without a trace get an unannotated entry rather than empty keys.
func addFailureContext(entry map[string]interface{}, failureTask, failureOutcome string) {
	if failureTask == "" && failureOutcome == "" {
		return
	}
	entry["last_failure_task"] = failureTask
	entry["last_failure_outcome"] = failureOutcome
}

func persistGeneratedTree(deps *mcpDeps, treeID string, tree *evolution.SerializableNode, result map[string]interface{}) {
	result["persisted"] = false
	if info := engine.ValidateTreeFull(tree); !info.Valid() {
		result["validation_errors"] = info.Errors
		return
	}
	if deps.treeStore == nil {
		result["persist_error"] = "tree store not configured"
		return
	}
	path, err := deps.treeStore.SaveNamed(treeID, tree)
	if err != nil {
		result["persist_error"] = err.Error()
		return
	}
	result["persisted"] = true
	result["file"] = path
}

// persistEvolvedWinner persists the winner tree a production genetic-evolution
// pass produced under a derived "<baseTreeID>-evolved" id via the existing
// persistGeneratedTree seam, then registers it in the knowledge graph
// (inheriting the base tree's capabilities and connecting back via an
// evolved_from edge) so fitness-aware discovery and the gardener can find the
// bred winner on the next run instead of only its scalar fitness surviving.
//
// The knowledge graph is consulted first — via the non-mutating
// EvolvedFitnessImproves peek — so a later, weaker genetic-evolution pass
// never even attempts to overwrite a stronger winner already persisted on
// disk. The bookkeeping commit (RegisterEvolved) only runs after
// persistGeneratedTree actually reports persisted=true, so a winner that
// fails validation or fails to write never leaves the knowledge graph
// claiming a fitness, node count, or evolved count that disk does not back —
// the two stay atomic: either both update or neither does.
func persistEvolvedWinner(deps *mcpDeps, baseTreeID string, winner *evolution.SerializableNode, fitness float64, result map[string]interface{}) {
	evolvedID := baseTreeID + "-evolved"
	result["evolved_tree_id"] = evolvedID
	if deps.kg != nil && !deps.kg.EvolvedFitnessImproves(evolvedID, fitness) {
		result["persisted"] = false
		result["skip_reason"] = "fitness does not improve on stored evolved winner"
		return
	}
	persistGeneratedTree(deps, evolvedID, winner, result)
	if persisted, _ := result["persisted"].(bool); !persisted {
		return
	}
	if deps.kg != nil {
		deps.kg.RegisterEvolved(baseTreeID, evolvedID, evolution.CountNodes(winner), fitness)
	}
}

// persistGeneratedTreeForUser persists a user-attributed generated tree into
// the user's own workspace (users/<user>/trees, ADR-010 Phase 5) so the
// gardener evolves it per user and the dynamic resolver's user-workspace
// fallback finds it. Falls back to the shared store when no user or persona
// store is available, so behavior degrades to Phase 0 rather than failing.
func persistGeneratedTreeForUser(deps *mcpDeps, user, treeID string, tree *evolution.SerializableNode, result map[string]interface{}) {
	if strings.TrimSpace(user) == "" || deps.personaStore == nil {
		persistGeneratedTree(deps, treeID, tree, result)
		return
	}
	result["persisted"] = false
	if info := engine.ValidateTreeFull(tree); !info.Valid() {
		result["validation_errors"] = info.Errors
		return
	}
	ws := deps.personaStore.Workspace(user)
	path, err := evolution.SaveNamedTree(ws.TreesDir(), treeID, tree)
	if err != nil {
		result["persist_error"] = err.Error()
		return
	}
	result["persisted"] = true
	result["file"] = path
	result["owner"] = user
}

// seedCompileReflection writes the compile-time plan validation as the tree's
// first reflection record (ADR-010 Phase 5). Freshly compiled trees would
// otherwise carry zero evidence and stay frozen behind the gardener's
// evidence gate forever. The TaskID is derived from the tree ID, so
// recompiling the same goal overwrites the seed instead of accumulating
// synthetic evidence.
func seedCompileReflection(deps *mcpDeps, user, treeID, goalName string, planSteps []string) {
	if deps.refStore == nil {
		return
	}
	rec := &evolution.Record{
		TaskID:   "seed-" + goalTreeSlug(treeID),
		Task:     "Compile-time validation for goal: " + goalName,
		Plan:     strings.Join(planSteps, " → "),
		TreeName: treeID,
		User:     user,
		WhatWentWell: []string{
			"GOAP planner reached the goal state",
			"compiled tree passed full engine validation",
		},
		WhatToImprove: []string{"gather real run evidence to replace this compile-time seed"},
		Outcome:       evolution.Success,
	}
	if err := deps.refStore.Save(rec); err != nil {
		engine.Warn("seed reflection not saved", "tree", treeID, "error", err)
	}
}

// newTreeFactory builds a knowledge factory with real structural crossover
// enabled (ADR-010 Phase 3): parents resolve to their actual tree structures
// (compiled-in catalogs + persisted generated trees) and spliced children
// are gated by full engine validation before they replace the synthetic
// template path.
func newTreeFactory(deps *mcpDeps) *knowledge.Factory {
	f := knowledge.NewFactory(deps.kg)
	f.Resolve = func(id string) *evolution.SerializableNode {
		tree := resolveTree(id)
		// domains.ResolveTreeID never returns nil — unknown IDs fall back to
		// DefaultTree ("MainSequence"). For crossover that fallback is a miss:
		// splicing the generic default would fake structure the parent lacks.
		if tree == nil || tree.Name == "MainSequence" {
			return nil
		}
		return tree
	}
	f.Validate = func(tree *evolution.SerializableNode) error {
		if info := engine.ValidateTreeFull(tree); !info.Valid() {
			return fmt.Errorf("tree validation failed: %s", strings.Join(info.Errors, "; "))
		}
		return nil
	}
	return f
}

// mcpDeps bundles all shared state needed by tool handlers.
// This eliminates the 900-line closure chain from main() — every handler
// accesses state through this struct instead of capturing locals.
type mcpDeps struct {
	bb           *engine.Blackboard
	bt           *btcore.Command[engine.Blackboard]
	treeStore    *evolution.TreeStore
	refStore     *evolution.Store
	expBank      *evolution.ExperienceBank
	agentFactory *factory.AgentFactory
	kg           *knowledge.KnowledgeGraph
	llmClient    llm.LLM
	llmHealth    *llm.HealthMonitor
	cfg          *config.Config
	// Agent platform
	agentReg    *agent.Registry
	agentHist   *agent.History
	agentMem    *agent.MemoryStore
	globalSched *agent.Scheduler
	dlq         *reliability.DeadLetterQueue
	agentRunner *agent.RunDeps
	// Personalization (ADR-010 Phase 1)
	personaStore *persona.Store
}

// newProductionPopulation builds an evolution population for the MCP tools
// with the specialist registry attached, so crisis resurrection is live in
// every production evolution pass instead of reachable only from tests. All
// tool call sites must construct populations through this helper (pinned by
// TestToolsBuildPopulationsViaProductionHelper).
func newProductionPopulation(size int, base *evolution.SerializableNode) *evolution.Population {
	pop := evolution.NewPopulation(size, base)
	pop.Specialists = evolution.SeedSpecialistRegistry()
	return pop
}

// evolveHealthProjection renders a population's post-run self-healing state —
// Population.HealthSnapshot() — as a JSON object for the evolve tool responses,
// surfacing which crises fired, how many extinct specialists were resurrected,
// and the mutation rate actually applied. Unlike marshalling PopulationHealth
// directly (whose CrisisReasons field is omitempty and would serialize to null
// or vanish when the run stayed healthy), crisis_reasons is always emitted as an
// array so a consumer can parse it unconditionally.
func evolveHealthProjection(pop *evolution.Population) map[string]interface{} {
	snap := pop.HealthSnapshot()
	reasons := snap.CrisisReasons
	if reasons == nil {
		reasons = []string{}
	}
	return map[string]interface{}{
		"crisis_reasons":     reasons,
		"resurrections":      snap.Resurrections,
		"last_mutation_rate": snap.LastMutationRate,
		"generation":         snap.Generation,
	}
}

// registerMCPTools registers all 79 MCP tools on the server.
// Each tool handler accesses shared state through deps instead of main() locals.
func registerMCPTools(server *engine.Server, deps *mcpDeps) {
	// ─── TREE EXECUTION ───────────────────────────────────────────────

	server.RegisterTool("bt_run_task", "Execute a task through the behavior tree",
		map[string]engine.Property{
			"task": {Type: "string", Description: "The task to execute"},
			"user": {Type: "string", Description: "Optional user ID: injects the user's profile context and records the run in their interaction log for habit mining"},
		},
		[]string{"task"},
		func(args json.RawMessage) *engine.ToolResult {
			if degraded := checkLLMHealth(deps.llmHealth, "bt_run_task"); degraded != nil {
				return degraded
			}
			var params struct {
				Task string `json:"task"`
				User string `json:"user"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				engine.Error("bt_run_task: invalid arguments", "error", err)
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}}}
			}
			engine.Info("bt_run_task: executing", "task", params.Task)
			start := time.Now()
			deps.bb.Task = params.Task
			deps.bb.Complexity = ""
			deps.bb.Plan = ""
			deps.bb.Result = ""
			deps.bb.Outcome = ""
			deps.bb.KgResults = ""
			deps.bb.CachedResult = ""
			injectPersonaContext(deps, params.User)
			result := engine.RunTask(deps.bb, *deps.bt)
			duration := time.Since(start)
			recordPersonaInteraction(deps, params.User, params.Task, "", deps.bb.Outcome, duration.Milliseconds())
			// Interaction-time autopilot (ADR-010 Phase 4): after a good
			// user-attributed run, check whether a recurring habit should
			// become an automation proposal. Best-effort by design.
			if params.User != "" && deps.bb.Outcome != string(evolution.Failure) {
				if auto := considerAutomation(deps, params.User); auto["proposed"] == true {
					engine.Info("autopilot: automation proposed", "user", params.User,
						"tree", auto["tree_id"], "hitl", auto["hitl_id"])
				}
			}
			if deps.bb.Outcome == string(evolution.Failure) {
				deps.bb.FailureCount = deps.refStore.CountFailures()
				engine.Warn("bt_run_task: failed", "task", params.Task, "outcome", deps.bb.Outcome, "duration_ms", duration.Milliseconds())
			} else {
				engine.Info("bt_run_task: completed", "task", params.Task, "outcome", deps.bb.Outcome, "duration_ms", duration.Milliseconds())
			}
			response := fmt.Sprintf(`{"result": %q, "outcome": %q, "complexity": %q, "duration_ms": %d, "plan": %q}`,
				result, deps.bb.Outcome, deps.bb.Complexity, deps.bb.DurationMs, deps.bb.Plan)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: response}}}
		})

	server.RegisterTool("bt_get_tree", "Get the current behavior tree definition",
		map[string]engine.Property{}, nil,
		func(args json.RawMessage) *engine.ToolResult {
			tree, err := deps.treeStore.Load()
			if err != nil || tree == nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: `{"error": "no tree found"}`}}}
			}
			data, _ := json.MarshalIndent(tree, "", "  ")
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_get_reflections", "Get all reflection records",
		map[string]engine.Property{}, nil,
		func(args json.RawMessage) *engine.ToolResult {
			records, err := deps.refStore.LoadAll()
			if err != nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			summary := map[string]interface{}{"total": len(records), "failures": deps.refStore.CountFailures(), "records": records}
			data, _ := json.MarshalIndent(summary, "", "  ")
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_evolve", "Run tree evolution (adapt on failures)",
		map[string]engine.Property{}, nil,
		func(args json.RawMessage) *engine.ToolResult {
			tree, err := deps.treeStore.Load()
			if err != nil || tree == nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: `{"error": "no tree to evolve"}`}}}
			}
			failures := deps.refStore.CountFailures()
			if failures < 3 {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"evolved": false, "reason": "need 3+ failures, have %d"}`, failures)}}}
			}
			ops := []evolution.MutationOp{
				{Operation: "wrap_retry", Target: "AnalyzeTask"},
				{Operation: "increase_retries", Target: "RetrySelfCorrect"},
			}
			before := evolution.CountNodes(tree)
			applied := evolution.ApplyMutations(tree, ops)
			after := evolution.CountNodes(tree)
			if applied > 0 {
				_ = deps.treeStore.Save(tree)
			}
			result := map[string]interface{}{"evolved": applied > 0, "applied": applied, "nodes_before": before, "nodes_after": after}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_reset", "Reset the behavior tree to the default",
		map[string]engine.Property{}, nil,
		func(args json.RawMessage) *engine.ToolResult {
			tree := evolution.DefaultTree()
			if err := deps.treeStore.Save(tree); err != nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"reset": false, "error": %q}`, err.Error())}}}
			}
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"reset": true, "nodes": %d}`, evolution.CountNodes(tree))}}}
		})

	server.RegisterTool("bt_get_fitness", "Get tree fitness stats",
		map[string]engine.Property{}, nil,
		func(args json.RawMessage) *engine.ToolResult {
			tree, _ := deps.treeStore.Load()
			records, _ := deps.refStore.LoadAll()
			failures := deps.refStore.CountFailures()
			successes := len(records) - failures
			successRate := 0.0
			if len(records) > 0 {
				successRate = float64(successes) / float64(len(records))
			}
			stats := map[string]interface{}{"total_tasks": len(records), "successes": successes, "failures": failures, "success_rate": fmt.Sprintf("%.2f", successRate), "node_count": evolution.CountNodes(tree)}
			data, _ := json.Marshal(stats)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_create_agent", "Create a behavior tree agent from a skill file",
		map[string]engine.Property{"skill_path": {Type: "string", Description: "Path to SKILL.md"}},
		[]string{"skill_path"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				SkillPath string `json:"skill_path"`
			}
			_ = json.Unmarshal(args, &params)
			agent, err := deps.agentFactory.CreateFromSkillDir(params.SkillPath)
			if err != nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			result := map[string]interface{}{"created": true, "agent_name": agent.Name, "root_type": agent.SerTree.Type, "node_count": evolution.CountNodes(agent.SerTree)}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── DOMAIN SWITCHING ─────────────────────────────────────────────

	server.RegisterTool("bt_use_go_tree", "Switch to Go developer tree",
		map[string]engine.Property{}, nil,
		func(args json.RawMessage) *engine.ToolResult {
			tree := evolution.GoDeveloperTree()
			_ = deps.treeStore.Save(tree)
			newBt := engine.BuildTree(tree, deps.bb)
			*deps.bt = newBt
			result := map[string]interface{}{"switched": true, "tree": "GoDeveloperTree", "node_count": evolution.CountNodes(tree), "strategies": 5}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_use_finance_tree", "Switch to an Anthropic finance agent behavior tree",
		map[string]engine.Property{"agent": {Type: "string", Description: "Agent name: pitch_agent, earnings_reviewer, market_researcher, model_builder, meeting_prep, valuation_reviewer, gl_reconciler, month_end_closer, statement_auditor, kyc_screener"}},
		[]string{"agent"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Agent string `json:"agent"`
			}
			_ = json.Unmarshal(args, &params)
			allTrees := evolution.AllFinanceTrees()
			tree, ok := allTrees[params.Agent]
			if !ok {
				names := ""
				for k := range allTrees {
					names += k + ", "
				}
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": "unknown agent", "available": %q}`, names)}}}
			}
			_ = deps.treeStore.Save(tree)
			*deps.bt = engine.BuildTree(tree, deps.bb)
			result := map[string]interface{}{"switched": true, "agent": params.Agent, "description": evolution.AgentDescriptions[params.Agent], "node_count": evolution.CountNodes(tree)}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_list_finance_trees", "List available Anthropic finance agent trees",
		map[string]engine.Property{}, nil,
		func(args json.RawMessage) *engine.ToolResult {
			type agent struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Nodes       int    `json:"nodes"`
			}
			trees := evolution.AllFinanceTrees()
			agents := make([]agent, 0, len(trees))
			for name, tree := range trees {
				agents = append(agents, agent{Name: name, Description: evolution.AgentDescriptions[name], Nodes: evolution.CountNodes(tree)})
			}
			result := map[string]interface{}{"total": len(agents), "agents": agents}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_use_research_tree", "Switch to deep research or quick research behavior tree",
		map[string]engine.Property{"variant": {Type: "string", Description: "deep_research or quick_research"}},
		[]string{"variant"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Variant string `json:"variant"`
			}
			_ = json.Unmarshal(args, &params)
			if params.Variant == "" {
				params.Variant = "deep_research"
			}
			trees := evolution.ResearchTrees()
			tree, ok := trees[params.Variant]
			if !ok {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: `{"error": "unknown variant, use: deep_research, quick_research"}`}}}
			}
			_ = deps.treeStore.Save(tree)
			*deps.bt = engine.BuildTree(tree, deps.bb)
			result := map[string]interface{}{"switched": true, "variant": params.Variant, "description": evolution.Descriptions[params.Variant], "node_count": evolution.CountNodes(tree)}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_use_domain_tree", "Switch to a domain behavior tree (code_review, devops_ci, agent_monitor, refactoring, security_audit, data_pipeline, meeting_notes, crash_investigator, game_ai, trading_signal)",
		map[string]engine.Property{"tree": {Type: "string", Description: "Tree name"}},
		[]string{"tree"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Tree string `json:"tree"`
			}
			_ = json.Unmarshal(args, &params)
			allTrees := domains.AllDomainTrees()
			tree, ok := allTrees[params.Tree]
			if !ok {
				names := ""
				for k := range allTrees {
					names += k + ", "
				}
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": "unknown tree", "available": %q}`, names)}}}
			}
			_ = deps.treeStore.Save(tree)
			*deps.bt = engine.BuildTree(tree, deps.bb)
			result := map[string]interface{}{"switched": true, "tree": params.Tree, "description": domains.Descriptions[params.Tree], "node_count": evolution.CountNodes(tree)}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── STARTUP SIMULATION ───────────────────────────────────────────

	server.RegisterTool("bt_startup_simulate", "Run a startup company simulation: sprint, quarter, or year",
		map[string]engine.Property{
			"mode":    {Type: "string", Description: "sprint, quarter, or year"},
			"company": {Type: "string", Description: "Company name (default: HermesAI)"},
		},
		[]string{"mode"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Mode    string `json:"mode"`
				Company string `json:"company"`
			}
			_ = json.Unmarshal(args, &params)
			company := startup.NewDefaultCompany()
			if params.Company != "" {
				company.Name = params.Company
			}
			orch := startup.NewOrchestrator(company, deps.llmClient)
			var result map[string]interface{}
			switch params.Mode {
			case "sprint":
				s := orch.RunSprint()
				result = map[string]interface{}{"sprint": s.SprintNum, "goal": s.Goal, "completed": s.Completed, "velocity": s.Velocity, "company_state": company}
			case "quarter":
				q := orch.RunQuarter()
				result = map[string]interface{}{"quarter": q.Quarter, "revenue": q.Revenue, "growth_pct": q.Growth, "highlights": q.Highlights, "company_state": company}
			case "year":
				quarters := orch.RunYear()
				result = map[string]interface{}{"quarters": quarters, "company_state": company}
			default:
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: `{"error": "unknown mode, use sprint/quarter/year"}`}}}
			}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_startup_summary", "Get current state summary of the simulated startup company",
		nil, nil,
		func(args json.RawMessage) *engine.ToolResult {
			company := startup.NewDefaultCompany()
			summary := fmt.Sprintf("Company: %s | Stage: %s | Round: %s | Team: %d | MRR: $%.0f | Users: %d | Runway: %dmo | Cash: $%.0f",
				company.Name, company.ProductStage, company.FundingRound,
				company.TeamSize, company.MRR, company.Users,
				company.Runway, company.CashInBank)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: summary}}}
		})

	// ─── THINK TANK ───────────────────────────────────────────────────

	server.RegisterTool("bt_thinktank_analyze", "Run a full think tank analysis on a topic with 5 analytical perspectives",
		map[string]engine.Property{
			"topic": {Type: "string", Description: "The topic/question to analyze"},
			"name":  {Type: "string", Description: "Think tank name (default: AI Strategy Council)"},
		},
		[]string{"topic"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Topic string `json:"topic"`
				Name  string `json:"name"`
			}
			_ = json.Unmarshal(args, &params)
			if params.Name == "" {
				params.Name = "AI Strategy Council"
			}
			tt := thinktank.NewThinkTank(params.Name, params.Topic)
			orch := &thinktank.ThinkTankOrchestrator{Tank: tt, LLM: deps.llmClient}
			if err := orch.RunFullAnalysis(params.Topic); err != nil {
				data, _ := json.Marshal(map[string]interface{}{"error": "think tank analysis failed: " + err.Error(), "topic": params.Topic})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}
			var scenarios []map[string]interface{}
			if tt.FinalReport != nil {
				for _, s := range tt.FinalReport.Scenarios {
					scenarios = append(scenarios, map[string]interface{}{"name": s.Name, "probability": s.Probability, "impact": s.Impact})
				}
			}
			result := map[string]interface{}{"topic": params.Topic, "fellows": len(tt.Fellows), "findings": len(tt.ResearchFindings), "debate_turns": len(tt.DebateTranscript), "scenarios": scenarios}
			if tt.FinalReport != nil {
				result["recommendation"] = tt.FinalReport.Recommendation
				result["confidence"] = tt.FinalReport.ConfidenceLevel
				result["executive_summary"] = tt.FinalReport.ExecutiveSummary
			}
			if tt.Synthesis != nil {
				result["synthesis"] = tt.Synthesis.Synthesis
				result["agreement"] = tt.Synthesis.PointsOfAgreement
				result["disagreement"] = tt.Synthesis.PointsOfDisagreement
			}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── DELEGATION ───────────────────────────────────────────────────

	server.RegisterTool("bt_delegate_to_tree", "Delegate a task to a specific behavior tree for execution",
		map[string]engine.Property{
			"tree": {Type: "string", Description: "Tree type: godev, finance:<name>, research:<name>, domain:<name>, startup:<role>, thinktank:<role>"},
			"task": {Type: "string", Description: "The task to delegate"},
		},
		[]string{"tree", "task"},
		func(args json.RawMessage) *engine.ToolResult {
			if degraded := checkLLMHealth(deps.llmHealth, "bt_delegate_to_tree"); degraded != nil {
				return degraded
			}
			var params struct {
				Tree string `json:"tree"`
				Task string `json:"task"`
			}
			_ = json.Unmarshal(args, &params)
			tree := resolveTree(params.Tree)
			if tree == nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error":"unknown tree: %s"}`, params.Tree)}}}
			}
			deps.bb.Task = params.Task
			*deps.bt = engine.BuildTree(tree, deps.bb)
			output := engine.RunTask(deps.bb, *deps.bt)
			result := map[string]interface{}{"delegated_to": params.Tree, "outcome": deps.bb.Outcome, "output": output}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── KNOWLEDGE GRAPH ─────────────────────────────────────────────

	server.RegisterTool("bt_kg_discover", "Discover the best behavior tree for a given task",
		map[string]engine.Property{"task": {Type: "string", Description: "Task description to match against known trees"}},
		[]string{"task"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Task string `json:"task"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			treeID, confidence := deps.kg.Discover(params.Task)
			result := map[string]interface{}{"tree_id": treeID, "confidence": confidence, "found": treeID != ""}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_kg_query", "Query the knowledge graph for trees matching a capability",
		map[string]engine.Property{"capability": {Type: "string", Description: "Capability to search for (e.g., code_review, pitch, research)"}},
		[]string{"capability"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Capability string `json:"capability"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			trees := deps.kg.Query(params.Capability)
			var results []map[string]interface{}
			for _, t := range trees {
				results = append(results, map[string]interface{}{"id": t.ID, "name": t.Name, "category": t.Category, "description": t.Description, "fitness": t.Fitness, "node_count": t.NodeCount})
			}
			if results == nil {
				results = []map[string]interface{}{}
			}
			data, _ := json.Marshal(map[string]interface{}{"total": len(results), "trees": results})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_kg_auto_create", "Auto-discover or create a behavior tree for a task",
		map[string]engine.Property{"task": {Type: "string", Description: "Task to discover or create a tree for"}},
		[]string{"task"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Task string `json:"task"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			autoTree, treeID, err := knowledge.AutoCreateTreeWith(newTreeFactory(deps), params.Task)
			if err != nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			action := "created"
			if autoTree == nil {
				action = "discovered"
			}
			result := map[string]interface{}{"action": action, "tree_id": treeID}
			if autoTree != nil {
				result["node_count"] = evolution.CountNodes(autoTree)
				persistGeneratedTree(deps, treeID, autoTree, result)
			}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_kg_summary", "Get knowledge graph summary: tree counts by category, total edges",
		map[string]engine.Property{}, nil,
		func(args json.RawMessage) *engine.ToolResult {
			summary := deps.kg.Summary()
			categories := make(map[string]int)
			for _, t := range deps.kg.Trees {
				categories[t.Category]++
			}
			result := map[string]interface{}{"summary": summary, "total_trees": len(deps.kg.Trees), "total_edges": len(deps.kg.Edges), "categories": categories}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_kg_list", "List all trees in a category",
		map[string]engine.Property{"category": {Type: "string", Description: "Category to list (finance, domain, research, startup, thinktank, evolution, core)"}},
		[]string{"category"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Category string `json:"category"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			trees := deps.kg.ListByCategory(params.Category)
			var results []map[string]interface{}
			for _, t := range trees {
				results = append(results, map[string]interface{}{"id": t.ID, "name": t.Name, "description": t.Description, "fitness": t.Fitness, "node_count": t.NodeCount})
			}
			if results == nil {
				results = []map[string]interface{}{}
			}
			data, _ := json.Marshal(map[string]interface{}{"category": params.Category, "total": len(results), "trees": results})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_kg_analytics", "Compute cross-tree analytics: centrality, tool contention, coverage gaps, bottlenecks, and suggested actions",
		map[string]engine.Property{}, nil,
		func(args json.RawMessage) *engine.ToolResult {
			a := deps.kg.ComputeAnalytics()
			// Publish the analytics signals as Prometheus gauges so coverage,
			// bottleneck, and selection-pressure drift is observable in
			// Grafana, not only in the text report this tool returns.
			dashboard.RecordKGAnalytics(len(a.CoverageGaps), len(a.Bottlenecks), len(a.SelectionPressure))
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: a.FormatAnalytics()}}}
		})

	server.RegisterTool("bt_kg_explain", "Explain why a tree's last run failed, with the full execution path",
		map[string]engine.Property{"tree": {Type: "string", Description: "Tree ID to explain (e.g., 'research:deep_research')"}},
		[]string{"tree"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Tree string `json:"tree"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			explanation := deps.kg.ExplainLastFailure(params.Tree)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: explanation}}}
		})

	// ─── EVOLUTION ────────────────────────────────────────────────────

	server.RegisterTool("bt_evolve_genetic", "Run genetic algorithm evolution on a population of trees",
		map[string]engine.Property{
			"tree":        {Type: "string", Description: "Base tree ID"},
			"population":  {Type: "integer", Description: "Population size (default: 20)"},
			"generations": {Type: "integer", Description: "Number of generations (default: 10)"},
		},
		[]string{"tree"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Tree        string `json:"tree"`
				Population  *int   `json:"population"`
				Generations int    `json:"generations"`
			}
			_ = json.Unmarshal(args, &params)
			population, reject := resolveEvolvePopulation(params.Population)
			if reject != nil {
				return reject
			}
			if params.Generations <= 0 {
				params.Generations = 10
			}
			baseTree := resolveTree(params.Tree)
			if baseTree == nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: `{"error":"unknown tree"}`}}}
			}
			pop := newProductionPopulation(population, baseTree)
			// Warm-start from the daemon's persistent experience bank when one
			// is wired; a nil bank degrades to plain Evolve inside
			// EvolveWithExperience, keeping the result shape uniform.
			retrievalHits := evolution.ExperienceRetrievalHits(deps.expBank, baseTree)
			best := pop.EvolveWithExperience(params.Generations, structuralFitnessFn, deps.expBank)
			bankEntries := 0
			if deps.expBank != nil {
				bankEntries = deps.expBank.Count()
			}
			result := map[string]interface{}{
				"tree": params.Tree, "generations": pop.Generation,
				"best_fitness": pop.BestFitness, "diversity": pop.Diversity(),
				"convergence_rate": pop.ConvergenceRate(), "best_nodes": evolution.CountNodes(best),
				"regression_rate": fmt.Sprintf("%.1f%%", pop.RegressionRate()),
				"total_mutations": pop.TotalMutations, "regressions": pop.Regressions,
				"niche_diversity":           fmt.Sprintf("%.2f", pop.NicheDiversity()),
				"experience_bank_entries":   bankEntries,
				"experience_retrieval_hits": retrievalHits,
				"health":                    evolveHealthProjection(pop),
			}
			// Persist the winner instead of discarding it after computing its
			// fitness (Q2 Evolvability): it becomes resolvable by id and
			// discoverable via the knowledge graph, not just a scalar number.
			persistEvolvedWinner(deps, params.Tree, best, pop.BestFitness, result)
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_evolve_qd", "Run MAP-Elites quality-diversity evolution: illuminate a behavior space and report diversity metrics, warm-starting from and persisting to a durable per-tree archive",
		map[string]engine.Property{
			"tree":        {Type: "string", Description: "Base tree ID"},
			"population":  {Type: "integer", Description: "Population size (default: 20)"},
			"generations": {Type: "integer", Description: "Number of generations (default: 10)"},
			"domain":      {Type: "string", Description: "Domain label for specialist attribution (default: general)"},
			"archive_cap": {Type: "integer", Description: "Max niches retained in the durable MAP-Elites archive (default: population*5)"},
		},
		[]string{"tree"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Tree        string `json:"tree"`
				Population  *int   `json:"population"`
				Generations int    `json:"generations"`
				Domain      string `json:"domain"`
				ArchiveCap  *int   `json:"archive_cap"`
			}
			_ = json.Unmarshal(args, &params)
			population, reject := resolveEvolvePopulation(params.Population)
			if reject != nil {
				return reject
			}
			if params.Generations <= 0 {
				params.Generations = 10
			}
			if params.Domain == "" {
				params.Domain = "general"
			}
			baseTree := resolveTree(params.Tree)
			if baseTree == nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: `{"error":"unknown tree"}`}}}
			}
			// Deterministic, LLM-free evolution reusing the shared structural
			// fitness — avoids EvolveMAPElites, which invokes the LLM supervisor.
			pop := newProductionPopulation(population, baseTree)
			pop.Evolve(params.Generations, structuralFitnessFn)
			grid := evolution.NewMAPElitesGrid(population / 2)
			// Bound the durable archive against runaway growth, mirroring
			// bt_evolve_qlearning's state_cap: the default derives from this
			// run's own population so ordinary calls stay bounded without every
			// caller having to pass an explicit value. Cap must be set before
			// Load so enforceCap trims a merged, oversized archive.
			if params.ArchiveCap != nil {
				grid.Cap = *params.ArchiveCap
			} else {
				grid.Cap = population * 5
			}
			// Warm-start from the durable MAP-Elites archive so illuminated
			// niches accumulate across runs instead of resetting to an empty
			// grid every call (Q2 Evolvability). A missing archive is a cold
			// start; a corrupt one degrades to a cold start surfaced
			// non-fatally so the evolution still runs, mirroring
			// bt_evolve_island and bt_evolve_qlearning.
			archivePath := mapElitesArchivePath(params.Tree)
			_, statErr := os.Stat(archivePath)
			warmStarted := statErr == nil
			archiveLoadErr := ""
			if err := grid.Load(archivePath); err != nil {
				warmStarted = false
				archiveLoadErr = err.Error()
			}
			grid.InsertFromPopulation(pop, params.Domain)
			// Write the best illuminated elite's structural fitness back into the
			// knowledge graph so fitness-aware discovery can surface the
			// archive-improved tree on the next run (milestone 4/5).
			recordEvolvedFitness(deps, params.Tree, grid.Stats().BestFitness)
			result := map[string]interface{}{
				"tree": params.Tree, "domain": params.Domain, "generations": pop.Generation,
				"diversity_score": grid.DiversityScore(), "cell_count": grid.CellCount(),
				"elites": len(grid.Elites()), "specialist_distribution": grid.SpecialistDistribution(),
				"warm_started": warmStarted,
			}
			if archiveLoadErr != "" {
				result["archive_load_error"] = archiveLoadErr
			}
			// Persist the merged, illuminated grid so the next invocation
			// resumes from this run's niches. A save failure is surfaced
			// non-fatally alongside the evolution result.
			if err := grid.Save(archivePath); err != nil {
				result["archive_save_error"] = err.Error()
			}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_evolve_multiobjective", "Run NSGA-II multi-objective evolution over structural fitness dimensions and report the Pareto front",
		map[string]engine.Property{
			"tree":        {Type: "string", Description: "Base tree ID"},
			"population":  {Type: "integer", Description: "Population size (default: 20)"},
			"generations": {Type: "integer", Description: "Number of generations (default: 10)"},
		},
		[]string{"tree"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Tree        string `json:"tree"`
				Population  *int   `json:"population"`
				Generations int    `json:"generations"`
			}
			_ = json.Unmarshal(args, &params)
			population, reject := resolveEvolvePopulation(params.Population)
			if reject != nil {
				return reject
			}
			if params.Generations <= 0 {
				params.Generations = 10
			}
			baseTree := resolveTree(params.Tree)
			if baseTree == nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: `{"error":"unknown tree"}`}}}
			}
			// Deterministic, LLM-free NSGA-II over the three fixed structural axes,
			// reusing the shared StructuralMultiFitness (Quick-tier, no LLM calls).
			dims := []evolution.FitnessDimension{
				evolution.DimSuccessRate,
				evolution.DimNodeEfficiency,
				evolution.DimStability,
			}
			nsga := evolution.NewNSGAIIPopulation(population, baseTree, dims)
			nsga.Specialists = evolution.SeedSpecialistRegistry()
			nsga.Cap = population * 5
			// Warm-start the ExpertKnowledge learned-pattern archive so
			// genuinely fitness-improving mutations observed across this and
			// prior runs accumulate in the same per-tree archive
			// bt_evolve_expert reads (Q2 Evolvability), mirroring
			// bt_evolve_qlearning's ek.Load/Observe/Save sequence. Wiring
			// nsga.ExpertKnowledge before evolving makes Evolve's mutation
			// step observe every genuinely-improving mutation directly
			// (multi_objective.go:344), the same ek plumbing
			// EvolvePareto/EvolveAll/EvolveQLearning already have. A load
			// error is surfaced non-fatally.
			ek := evolution.NewExpertKnowledge()
			expertPath := expertArchivePath(params.Tree)
			expertLoadErr := ""
			if err := ek.Load(expertPath); err != nil {
				expertLoadErr = err.Error()
			}
			nsga.ExpertKnowledge = ek
			best := nsga.Evolve(params.Generations, evolution.StructuralMultiFitness)
			// Per-dimension best scores across the final population.
			dimNames := make([]string, len(dims))
			dimBests := make(map[string]float64, len(dims))
			for i, d := range dims {
				dimNames[i] = string(d)
				dimBests[string(d)] = 0
			}
			for _, fv := range nsga.FitnessVecs {
				for _, d := range dims {
					if s := fv.Get(d); s > dimBests[string(d)] {
						dimBests[string(d)] = s
					}
				}
			}
			// Pareto front is front 0 from the final non-dominated sort.
			frontSize := 0
			if len(nsga.Fronts) > 0 {
				frontSize = len(nsga.Fronts[0].Indices)
			}
			// Warm-start a durable NSGA-II archive from front 0 so the
			// Pareto-optimal front accumulates across runs instead of
			// resetting on every call (Q2 Evolvability), mirroring
			// bt_evolve_pareto. NSGAIIPopulation.Load/Save already implement
			// the merge/cap-eviction persistence (milestone 3/5); this just
			// adopts them.
			archivePath := nsgaArchivePath(params.Tree)
			_, statErr := os.Stat(archivePath)
			warmStarted := statErr == nil
			archiveLoadErr := ""
			if err := nsga.Load(archivePath); err != nil {
				warmStarted = false
				archiveLoadErr = err.Error()
			}
			result := map[string]interface{}{
				"tree": params.Tree, "generations": nsga.Generation,
				"dimensions": dimNames, "node_count": evolution.CountNodes(best),
				"dimension_bests": dimBests, "pareto_front_size": frontSize,
				"health":       evolveHealthProjection(nsga.Population),
				"warm_started": warmStarted,
			}
			if archiveLoadErr != "" {
				result["archive_load_error"] = archiveLoadErr
			}
			if expertLoadErr != "" {
				result["expert_archive_load_error"] = expertLoadErr
			}
			// Gate the front's lead individual through the tree's real
			// benchmark suite before trusting it enough to persist — structural
			// fitness alone can rate a mutation as elite while it actually
			// regresses (Q2 Evolvability). A rejected winner skips the save
			// entirely so the durable archive never accumulates a worse tree.
			gateRejected, baseRate, winnerRate := benchmarkGateEvolvedWinner(params.Tree, baseTree, best)
			result["benchmark_gate_rejected"] = gateRejected
			result["benchmark_base_success_rate"] = baseRate
			result["benchmark_winner_success_rate"] = winnerRate
			if !gateRejected {
				// Persist front 0 so the next invocation resumes from this run's
				// Pareto-optimal individuals. A save failure is surfaced
				// non-fatally alongside the evolution result.
				if err := nsga.Save(archivePath); err != nil {
					result["archive_save_error"] = err.Error()
				}
				// Persist the ExpertKnowledge archive Evolve observed
				// genuinely-improving mutations into during the run above,
				// mirroring bt_evolve_qlearning's ek.Save. Persisted only
				// alongside a gate-accepted winner, mirroring the NSGA-II
				// archive save above.
				if err := ek.Save(expertPath); err != nil {
					result["expert_archive_save_error"] = err.Error()
				}
			}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_evolve_pareto", "Run Pareto front-elitism multi-objective evolution over structural fitness dimensions and report front diversity metrics",
		map[string]engine.Property{
			"tree":        {Type: "string", Description: "Base tree ID"},
			"population":  {Type: "integer", Description: "Population size (default: 20)"},
			"generations": {Type: "integer", Description: "Number of generations (default: 10)"},
		},
		[]string{"tree"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Tree        string `json:"tree"`
				Population  *int   `json:"population"`
				Generations int    `json:"generations"`
			}
			_ = json.Unmarshal(args, &params)
			population, reject := resolveEvolvePopulation(params.Population)
			if reject != nil {
				return reject
			}
			if params.Generations <= 0 {
				params.Generations = 10
			}
			baseTree := resolveTree(params.Tree)
			if baseTree == nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: `{"error":"unknown tree"}`}}}
			}
			// Deterministic, LLM-free Pareto front-elitism evolution over the
			// full structural fitness vector. The population is built through
			// newProductionPopulation (not evolution.NewParetoPopulation) so its
			// seeded specialist registry backs crisis resurrection on this path
			// too, mirroring bt_evolve_multiobjective.
			dims := []evolution.FitnessDimension{
				evolution.DimSuccessRate,
				evolution.DimPathCoverage,
				evolution.DimStability,
				evolution.DimNodeEfficiency,
				evolution.DimExecutionSpeed,
			}
			pp := &evolution.ParetoPopulation{
				Population: newProductionPopulation(population, baseTree),
				Front:      evolution.NewParetoFront(dims),
			}
			// Warm-start the ExpertKnowledge learned-pattern archive so
			// genuinely fitness-improving mutations observed across this and
			// prior runs accumulate in the same per-tree archive
			// bt_evolve_expert reads (Q2 Evolvability), mirroring
			// bt_evolve_qlearning's ek.Load/Observe/Save sequence. Wiring
			// pp.ExpertKnowledge before evolving makes EvolvePareto's
			// mutation step observe every genuinely-improving mutation
			// directly (pareto.go:418), the same ek plumbing
			// EvolveAll/NSGA-II Evolve/EvolveQLearning already have. A load
			// error is surfaced non-fatally.
			ek := evolution.NewExpertKnowledge()
			expertPath := expertArchivePath(params.Tree)
			expertLoadErr := ""
			if err := ek.Load(expertPath); err != nil {
				expertLoadErr = err.Error()
			}
			pp.ExpertKnowledge = ek
			best := pp.EvolvePareto(params.Generations, evolution.StructuralMultiFitness)
			// Warm-start a durable Pareto front archive from the evolved
			// population so Pareto-optimal individuals accumulate across runs
			// instead of resetting on every call (Q2 Evolvability). This is a
			// ParetoFront separate from pp.Front: EvolvePareto's internal
			// Evaluate rebuilds pp.Front from scratch every generation, so the
			// durable archive instead merges in once, after evolution,
			// against the final population — mirroring how bt_evolve_qd's
			// grid is loaded and then filled via InsertFromPopulation.
			archive := evolution.NewParetoFront(dims)
			archive.Cap = population * 5
			archivePath := paretoFrontArchivePath(params.Tree)
			_, statErr := os.Stat(archivePath)
			warmStarted := statErr == nil
			archiveLoadErr := ""
			if err := archive.Load(archivePath); err != nil {
				warmStarted = false
				archiveLoadErr = err.Error()
			}
			archive.AddFromPopulation(pp.Population, evolution.StructuralMultiFitness)
			stats := archive.Stats()
			result := map[string]interface{}{
				"tree": params.Tree, "generations": pp.Generation,
				"node_count":      evolution.CountNodes(best),
				"front_size":      stats.FrontSize,
				"diversity_score": stats.DiversityScore,
				"best_per_dim":    stats.BestPerDim,
				"health":          evolveHealthProjection(pp.Population),
				"warm_started":    warmStarted,
			}
			if archiveLoadErr != "" {
				result["archive_load_error"] = archiveLoadErr
			}
			if expertLoadErr != "" {
				result["expert_archive_load_error"] = expertLoadErr
			}
			// Persist the merged front so the next invocation resumes from
			// this run's Pareto-optimal individuals. A save failure is
			// surfaced non-fatally alongside the evolution result.
			if err := archive.Save(archivePath); err != nil {
				result["archive_save_error"] = err.Error()
			}
			// Persist the ExpertKnowledge archive EvolvePareto observed
			// genuinely-improving mutations into during the run above,
			// mirroring bt_evolve_qlearning's ek.Save. Unlike
			// bt_evolve_island/bt_evolve_multiobjective, this tool has no
			// benchmark gate, so the save below always runs alongside the
			// unconditional archive.Save.
			if err := ek.Save(expertPath); err != nil {
				result["expert_archive_save_error"] = err.Error()
			}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_evolve_memetic", "Run memetic evolution (GA + local search refinement) with a selectable local search strategy: hill-climb (default), simulated-annealing, or tabu",
		map[string]engine.Property{
			"tree":        {Type: "string", Description: "Base tree ID"},
			"population":  {Type: "integer", Description: "Population size (default: 20)"},
			"generations": {Type: "integer", Description: "Number of generations (default: 10)"},
			"strategy":    {Type: "string", Description: "Local search strategy: hill-climb, simulated-annealing, or tabu (default: hill-climb)"},
		},
		[]string{"tree"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Tree        string `json:"tree"`
				Population  *int   `json:"population"`
				Generations int    `json:"generations"`
				Strategy    string `json:"strategy"`
			}
			_ = json.Unmarshal(args, &params)
			population, reject := resolveEvolvePopulation(params.Population)
			if reject != nil {
				return reject
			}
			if params.Generations <= 0 {
				params.Generations = 10
			}
			// An omitted strategy falls back to the documented default; an
			// unknown value is rejected with a structured error naming it —
			// silently defaulting would mask caller typos.
			if params.Strategy == "" {
				params.Strategy = "hill-climb"
			}
			var searchStrategy evolution.LocalSearchStrategy
			switch params.Strategy {
			case "hill-climb":
				searchStrategy = evolution.HillClimbSearch
			case "simulated-annealing":
				searchStrategy = evolution.SimulatedAnnealingSearch
			case "tabu":
				searchStrategy = evolution.TabuSearch
			default:
				data, _ := json.Marshal(map[string]interface{}{
					"error": "unknown strategy: " + params.Strategy,
				})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}
			baseTree := resolveTree(params.Tree)
			if baseTree == nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: `{"error":"unknown tree"}`}}}
			}
			// Deterministic, LLM-free memetic evolution reusing the shared
			// structural fitness so the tool stays -short-safe.
			pop := newProductionPopulation(population, baseTree)
			searcher := evolution.NewLocalSearcher(searchStrategy)
			best := pop.MemeticEvolve(params.Generations, structuralFitnessFn, searcher, 2)
			data, _ := json.Marshal(map[string]interface{}{
				"tree": params.Tree, "strategy": params.Strategy,
				"generations": pop.Generation, "best_fitness": pop.BestFitness,
				"best_nodes": evolution.CountNodes(best), "diversity": pop.Diversity(),
			})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_evolve_qlearning", "Run Q-learning-guided evolution: an epsilon-greedy QTable selects the mutation category per tree state each generation and learns from fitness deltas, reporting the learned per-state best actions alongside the evolved winner",
		map[string]engine.Property{
			"tree":          {Type: "string", Description: "Base tree ID"},
			"population":    {Type: "integer", Description: "Population size (default: 20)"},
			"generations":   {Type: "integer", Description: "Number of generations (default: 10)"},
			"epsilon":       {Type: "number", Description: "Exploration rate 0-1 (default: 0.2); 0 = deterministic greedy selection"},
			"learning_rate": {Type: "number", Description: "Q-value learning rate (default: 0.1)"},
			"state_cap":     {Type: "integer", Description: "Max states retained in the durable QTable archive (default: population*10)"},
		},
		[]string{"tree"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Tree         string   `json:"tree"`
				Population   *int     `json:"population"`
				Generations  int      `json:"generations"`
				Epsilon      *float64 `json:"epsilon"`
				LearningRate float64  `json:"learning_rate"`
				StateCap     *int     `json:"state_cap"`
			}
			_ = json.Unmarshal(args, &params)
			population, reject := resolveEvolvePopulation(params.Population)
			if reject != nil {
				return reject
			}
			if params.Generations <= 0 {
				params.Generations = 10
			}
			// A pointer distinguishes an explicit epsilon=0 (deterministic
			// greedy, echoed back) from an omitted one (exploration default).
			epsilon := 0.2
			if params.Epsilon != nil {
				epsilon = *params.Epsilon
			}
			if epsilon < 0 {
				epsilon = 0
			} else if epsilon > 1 {
				epsilon = 1
			}
			if params.LearningRate <= 0 {
				params.LearningRate = 0.1
			}
			baseTree := resolveTree(params.Tree)
			if baseTree == nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: `{"error":"unknown tree"}`}}}
			}
			// QTable.GetState prefixes states with the category, so a tree id
			// like "domain:x" would break the "category:bucket:depth" encoding.
			category := strings.ReplaceAll(params.Tree, ":", "_")
			// Deterministic, LLM-free Q-learning evolution reusing the shared
			// structural fitness so the tool stays -short-safe.
			qt := evolution.NewQTable()
			// Bound the durable archive against runaway growth (milestone 4/4),
			// mirroring bt_evolve_island's population_cap: the default derives
			// from this run's own population so ordinary calls stay bounded
			// without every caller having to pass an explicit value. Cap must be
			// set before Load so enforceCap trims a merged, oversized archive.
			if params.StateCap != nil {
				qt.Cap = *params.StateCap
			} else {
				qt.Cap = population * 10
			}
			// Warm-start from the durable QTable archive so learned Q-values
			// accumulate across runs instead of resetting to an empty table
			// every call (milestone 2/4). A missing archive is a cold start; a
			// corrupt one degrades to a cold start surfaced non-fatally so the
			// evolution still runs, mirroring bt_evolve_island.
			archivePath := qtableArchivePath(params.Tree)
			_, statErr := os.Stat(archivePath)
			warmStarted := statErr == nil
			archiveLoadErr := ""
			if err := qt.Load(archivePath); err != nil {
				warmStarted = false
				archiveLoadErr = err.Error()
			}
			learnedStatesBefore := len(qt.LearnedActions())
			// Warm-start the ExpertKnowledge learned-pattern archive so
			// genuinely fitness-improving mutations observed across this and
			// prior runs accumulate in the same per-tree archive
			// bt_evolve_expert reads (milestone 2/2 of the durable Expert
			// Knowledge program, Q2 Evolvability). A load error is surfaced
			// non-fatally, mirroring the QTable archive above.
			ek := evolution.NewExpertKnowledge()
			expertPath := expertArchivePath(params.Tree)
			expertLoadErr := ""
			if err := ek.Load(expertPath); err != nil {
				expertLoadErr = err.Error()
			}
			pop := newProductionPopulation(population, baseTree)
			best := pop.EvolveQLearning(params.Generations, structuralFitnessFn, qt, category, epsilon, params.LearningRate, ek)
			learned := qt.LearnedActions()
			result := map[string]interface{}{
				"tree": params.Tree, "generations": pop.Generation,
				"best_fitness": pop.BestFitness, "best_nodes": evolution.CountNodes(best),
				"diversity": pop.Diversity(), "epsilon": epsilon,
				"learning_rate":         params.LearningRate,
				"learned_actions":       learned,
				"learned_states":        len(learned),
				"warm_started":          warmStarted,
				"learned_states_before": learnedStatesBefore,
				"learned_states_after":  len(learned),
				"total_mutations":       pop.TotalMutations, "regressions": pop.Regressions,
				"evicted_states": qt.EvictedStates,
			}
			if archiveLoadErr != "" {
				result["archive_load_error"] = archiveLoadErr
			}
			// Persist the merged, learned table so the next invocation resumes
			// from this run's Q-values. A save failure is surfaced non-fatally
			// alongside the evolution result.
			if err := qt.Save(archivePath); err != nil {
				result["archive_save_error"] = err.Error()
			}
			if expertLoadErr != "" {
				result["expert_archive_load_error"] = expertLoadErr
			}
			// Persist the merged learned-pattern archive so bt_evolve_expert
			// can warm-start from the same accumulated patterns this run
			// observed. A save failure is surfaced non-fatally.
			if err := ek.Save(expertPath); err != nil {
				result["expert_archive_save_error"] = err.Error()
			}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_evolve_island", "Run island-model evolution: isolated per-island populations with periodic migration, reporting per-island bests and cross-island diversity",
		map[string]engine.Property{
			"tree":               {Type: "string", Description: "Base tree ID"},
			"islands":            {Type: "integer", Description: "Number of islands (default: 3)"},
			"population":         {Type: "integer", Description: "Per-island population size (default: 10)"},
			"generations":        {Type: "integer", Description: "Number of generations (default: 10)"},
			"migration_interval": {Type: "integer", Description: "Generations between migrations (default: 2)"},
			"migration_rate":     {Type: "number", Description: "Fraction of each island migrating (default: 0.1)"},
			"domains":            {Type: "string", Description: "Comma-separated registered domain-tree names; each seeds its own island (overrides 'islands')"},
			"population_cap":     {Type: "integer", Description: "Max individuals retained per island after warm-start merge, bounding the durable archive (default: population*3)"},
			"island_cap":         {Type: "integer", Description: "Max distinct islands retained after warm-start merge, bounding the durable archive (default: islands*3)"},
		},
		[]string{"tree"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Tree              string  `json:"tree"`
				Islands           int     `json:"islands"`
				Population        *int    `json:"population"`
				Generations       int     `json:"generations"`
				MigrationInterval int     `json:"migration_interval"`
				MigrationRate     float64 `json:"migration_rate"`
				Domains           string  `json:"domains"`
				PopulationCap     *int    `json:"population_cap"`
				IslandCap         *int    `json:"island_cap"`
			}
			_ = json.Unmarshal(args, &params)
			if params.Islands <= 0 {
				params.Islands = 3
			}
			population, reject := resolveEvolvePopulation(params.Population)
			if reject != nil {
				return reject
			}
			if params.Population == nil {
				// The documented per-island default is 10, not the shared
				// resolveEvolvePopulation default of 20.
				population = 10
			}
			if params.Generations <= 0 {
				params.Generations = 10
			}
			if params.MigrationInterval <= 0 {
				params.MigrationInterval = 2
			}
			if params.MigrationRate <= 0 {
				params.MigrationRate = 0.1
			}
			baseTree := resolveTree(params.Tree)
			if baseTree == nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: `{"error":"unknown tree"}`}}}
			}
			// Deterministic, LLM-free island evolution reusing the shared
			// structural fitness across every island. With 'domains', each
			// named registered domain tree seeds its own island (the numeric
			// 'islands' param is ignored); resolution failures abort before
			// any evolution work so no partial result leaks out.
			im := evolution.NewIslandModel(params.MigrationInterval, params.MigrationRate)
			im.Bank = deps.expBank
			var seeded []string
			if params.Domains != "" {
				var names []string
				for _, raw := range strings.Split(params.Domains, ",") {
					if name := strings.TrimSpace(raw); name != "" {
						names = append(names, name)
					}
				}
				seeds := make(map[string]*evolution.SerializableNode, len(names))
				for _, name := range names {
					domainTree := resolveTree("domain:" + name)
					if domainTree == nil {
						msg, _ := json.Marshal(map[string]string{"error": "unknown domain: " + name})
						return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(msg)}}}
					}
					seeds[name] = domainTree
				}
				params.Islands = len(names)
				seeded = names
				for _, name := range names {
					im.AddIsland(name, newProductionPopulation(population, seeds[name]))
				}
			} else {
				for i := 0; i < params.Islands; i++ {
					name := fmt.Sprintf("island_%d", i)
					seeded = append(seeded, name)
					im.AddIsland(name, newProductionPopulation(population, baseTree))
				}
			}
			// Bound the durable archive against runaway growth (milestone 3/4):
			// defaults derive from this run's own population/island counts so
			// ordinary calls stay bounded without every caller having to pass
			// explicit values, while staying generous enough that legitimate
			// multi-run accumulation and legacy-archive adoption aren't
			// clipped away. Both caps must be set before any im.Load call.
			if params.PopulationCap != nil {
				im.Cap = *params.PopulationCap
			} else {
				im.Cap = population * 3
			}
			if params.IslandCap != nil {
				im.IslandCap = *params.IslandCap
			} else {
				im.IslandCap = len(seeded) * 3
			}
			// Warm-start from the durable island archive so illuminated
			// behavior accumulates across runs instead of restarting from
			// scratch each call (milestone 3/5). Load merges the archive's
			// per-domain subpopulations into the freshly seeded islands; a
			// missing archive is a cold start, and a corrupt one degrades to
			// a cold start surfaced non-fatally so the evolution still runs.
			archivePath := islandArchivePath(params.Tree)
			// One-time legacy adoption: pre-scoping runs accumulated a single
			// GLOBAL island_archive.json that per-tree scoping (33f8c13)
			// silently orphaned — its state cold-started away and the stale
			// file lingered under BT_AGENT_HOME forever. When THIS tree has no
			// archive yet but the legacy file exists, merge it in before
			// evolving and rename it aside so it is consumed exactly once and
			// can never re-pollute another tree's archive. Adoption failures
			// degrade non-fatally: the run proceeds as a cold start and the
			// cause is surfaced in the result.
			legacyAdopted := false
			legacyAdoptErr := ""
			if _, err := os.Stat(archivePath); os.IsNotExist(err) {
				legacyPath := filepath.Join(agent.HomeDir(), "island_archive.json")
				if _, legacyStatErr := os.Stat(legacyPath); legacyStatErr == nil {
					if loadErr := im.Load(legacyPath); loadErr != nil {
						legacyAdoptErr = loadErr.Error()
					} else if mvErr := os.Rename(legacyPath, legacyPath+".migrated"); mvErr != nil {
						legacyAdoptErr = mvErr.Error()
					} else {
						legacyAdopted = true
					}
				}
			}
			_, statErr := os.Stat(archivePath)
			warmStarted := statErr == nil
			archiveLoadErr := ""
			if err := im.Load(archivePath); err != nil {
				warmStarted = false
				archiveLoadErr = err.Error()
			}
			// Warm-start the ExpertKnowledge learned-pattern archive so
			// genuinely fitness-improving mutations observed across this and
			// prior runs accumulate in the same per-tree archive
			// bt_evolve_expert reads (Q2 Evolvability), mirroring
			// bt_evolve_qlearning's ek.Load/Observe/Save sequence. Wiring
			// im.ExpertKnowledge before evolving makes EvolveAll's per-island
			// mutation step observe every genuinely-improving mutation
			// directly (island.go:191), the same ek plumbing
			// EvolvePareto/NSGA-II Evolve/EvolveQLearning already have. A
			// load error is surfaced non-fatally, mirroring the island
			// archive above.
			ek := evolution.NewExpertKnowledge()
			expertPath := expertArchivePath(params.Tree)
			expertLoadErr := ""
			if err := ek.Load(expertPath); err != nil {
				expertLoadErr = err.Error()
			}
			im.ExpertKnowledge = ek
			var bestTrees map[string]*evolution.SerializableNode
			for g := 0; g < params.Generations; g++ {
				bestTrees = im.EvolveAll(structuralFitnessFn)
			}
			stats := im.Stats()
			// Report per-island bests only for the islands this run seeded:
			// archived domains from earlier runs keep accumulating in the
			// model (and in the saved archive) but stay out of the result
			// shape the caller asked for.
			perIslandBest := make(map[string]float64, len(seeded))
			for _, name := range seeded {
				if best, present := stats.BestPerDomain[name]; present {
					perIslandBest[name] = best
				}
			}
			// Write evolved fitness back into the knowledge graph so
			// fitness-aware discovery can surface archive-improved trees on
			// the next run (milestone 4/5). Attribution follows seeding: in
			// domains mode each island's elites descend from its own domain
			// tree's genome, so each domain:<name> entry is credited with its
			// island's best; the base tree seeded nothing and gets no credit.
			// In default mode the base tree seeded every island and alone
			// receives the cross-island best.
			if params.Domains != "" {
				for _, name := range seeded {
					if best, present := perIslandBest[name]; present {
						recordEvolvedFitness(deps, "domain:"+name, best)
					}
				}
			} else {
				bestElite := 0.0
				for _, best := range perIslandBest {
					if best > bestElite {
						bestElite = best
					}
				}
				recordEvolvedFitness(deps, params.Tree, bestElite)
			}
			result := map[string]interface{}{
				"tree": params.Tree, "islands": params.Islands, "generations": params.Generations,
				"per_island_best": perIslandBest, "migrations": stats.Migrations,
				"cross_diversity": stats.CrossDiversity, "warm_started": warmStarted,
				"evicted_individuals": stats.EvictedIndividuals, "evicted_islands": stats.EvictedIslands,
			}
			if archiveLoadErr != "" {
				result["archive_load_error"] = archiveLoadErr
			}
			if expertLoadErr != "" {
				result["expert_archive_load_error"] = expertLoadErr
			}
			if legacyAdopted {
				result["legacy_archive_adopted"] = true
			}
			if legacyAdoptErr != "" {
				result["legacy_archive_error"] = legacyAdoptErr
			}
			// Pick the cross-island best individual: the seeded domain with
			// the highest structural fitness this run, using the final
			// generation's per-domain best tree (EvolveAll's return value,
			// previously discarded). A degenerate run with no seeded domain
			// scoring skips the gate rather than gating a nil tree.
			var winnerTree *evolution.SerializableNode
			bestDomain, bestFitness := "", -1.0
			for _, name := range seeded {
				if best, present := perIslandBest[name]; present && best > bestFitness {
					bestFitness = best
					bestDomain = name
				}
			}
			if bestDomain != "" {
				winnerTree = bestTrees[bestDomain]
			}
			// Gate the cross-island winner through the tree's real benchmark
			// suite before trusting it enough to persist — structural fitness
			// alone can rate a mutation as elite while it actually regresses
			// (Q2 Evolvability). A rejected winner skips the save entirely so
			// the durable archive never accumulates a worse tree.
			gateRejected, baseRate, winnerRate := false, 0.0, 0.0
			if winnerTree != nil {
				gateRejected, baseRate, winnerRate = benchmarkGateEvolvedWinner(params.Tree, baseTree, winnerTree)
			}
			result["benchmark_gate_rejected"] = gateRejected
			result["benchmark_base_success_rate"] = baseRate
			result["benchmark_winner_success_rate"] = winnerRate
			// Persist the merged, evolved model so the next invocation
			// resumes from this run's state. A save failure is surfaced
			// non-fatally alongside the evolution result.
			if !gateRejected {
				if err := im.Save(archivePath); err != nil {
					result["archive_save_error"] = err.Error()
				}
				// Persist the ExpertKnowledge archive EvolveAll observed
				// genuinely-improving mutations into during the loop above,
				// mirroring bt_evolve_qlearning's ek.Save. Persisted only
				// alongside a gate-accepted winner, mirroring the island
				// archive save above.
				if err := ek.Save(expertPath); err != nil {
					result["expert_archive_save_error"] = err.Error()
				}
			}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_evolve_selectors", "Apply the deterministic Selector-ordering optimizer to a named tree: load durable Selector telemetry, reorder each Selector's children by learned success rate (fallback/AlwaysSucceed children stay last), persist the reordered tree, and report the per-Selector reorder count and information-gain reduction",
		map[string]engine.Property{
			"tree":       {Type: "string", Description: "Base tree ID to reorder"},
			"stats_path": {Type: "string", Description: "Path to durable Selector telemetry (SelectorOptimizer stats JSON); an empty or missing file yields zero reorders rather than an error"},
		},
		[]string{"tree"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Tree      string `json:"tree"`
				StatsPath string `json:"stats_path"`
			}
			_ = json.Unmarshal(args, &params)
			baseTree := resolveTree(params.Tree)
			if baseTree == nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: `{"error":"unknown tree"}`}}}
			}
			// Seed the optimizer from durable telemetry. A missing file leaves the
			// optimizer empty (LoadSelectorStats returns nil for os.IsNotExist), so
			// an un-seeded tree yields zero reorders and no error — the boundary the
			// milestone requires to be handled cleanly rather than panicking. A
			// corrupt file is surfaced non-fatally under stats_load_error so the
			// empty-telemetry contract still holds.
			so := evolution.NewSelectorOptimizer(evolution.OrderBySuccessRate)
			result := map[string]interface{}{"tree": params.Tree}
			if params.StatsPath != "" {
				if err := so.LoadSelectorStats(params.StatsPath); err != nil {
					result["stats_load_error"] = err.Error()
				}
			}
			// Information-gain reduction: the sum over every telemetry-bearing
			// Selector of its best child's expected entropy reduction (trying the
			// most informative child first). Non-negative by construction, and zero
			// when there is no telemetry.
			infoGain := 0.0
			for _, ss := range so.Stats {
				best := 0.0
				for _, cs := range ss.Children {
					if ig := evolution.InformationGain(cs, ss); ig > best {
						best = ig
					}
				}
				infoGain += best
			}
			reorders := so.ApplyLearnedOrdering(baseTree)
			result["reorders"] = reorders
			result["information_gain"] = infoGain
			// Run the reordered tree through the shared persistence path so its
			// outcome (persisted true, or a validation/persist error under bare
			// deps) is reported alongside the reorder count.
			persistGeneratedTree(deps, params.Tree, baseTree, result)
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_evolve_bottlenecks", "Evolve every knowledge-graph bottleneck tree (RunCount >= 3, Fitness < 30), routing trees with tunable parameters to CMA-ES tuning and the rest to experience-grounded genetic evolution, and report per-tree before/after fitness plus the algorithm that handled each tree",
		map[string]engine.Property{
			"population":  {Type: "integer", Description: "Population size per bottleneck (default: 20)"},
			"generations": {Type: "integer", Description: "Number of generations per bottleneck (default: 10)"},
		},
		[]string{},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Population  *int `json:"population"`
				Generations int  `json:"generations"`
			}
			_ = json.Unmarshal(args, &params)
			// Degenerate-param rejection precedes the dependency check so a bad
			// population is reported as such even without a knowledge graph.
			population, reject := resolveEvolvePopulation(params.Population)
			if reject != nil {
				return reject
			}
			if params.Generations <= 0 {
				params.Generations = 10
			}
			if deps.kg == nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: `{"error":"knowledge graph unavailable"}`}}}
			}
			// Deterministic, LLM-free closure of the learn→discover→evolve loop:
			// the same RunCount/Fitness criteria ComputeAnalytics surfaces as
			// human-readable SuggestedActions drive structural evolution directly.
			bottlenecks := deps.kg.ComputeAnalytics().Bottlenecks
			bankEntries := 0
			if deps.expBank != nil {
				bankEntries = deps.expBank.Count()
			}
			report := []map[string]interface{}{}
			skipped := []string{}
			algorithms := map[string]int{}
			for _, b := range bottlenecks {
				baseTree := resolveTree(b.TreeID)
				if baseTree == nil {
					// A KG entry without a real behavior tree must not abort the
					// remaining bottlenecks.
					skipped = append(skipped, b.TreeID)
					continue
				}
				// Failure-targeted re-evolution: read the structured failing
				// task/outcome that ComputeAnalytics captured on the bottleneck
				// and tie each per-tree evolution report to the concrete failure
				// that motivated it, rather than re-parsing the human-readable
				// SuggestedAction prose. Threaded into the report below via
				// addFailureContext.
				// Trees with tunable parameters get CMA-ES parameter tuning;
				// parameterless trees fall back to structural genetic evolution.
				if _, tunedParams, bestFitness, tuned := evolution.TuneTreeParameters(baseTree, population, params.Generations, structuralFitnessFn); tuned {
					algorithms["cmaes"]++
					entry := map[string]interface{}{
						"tree":           b.TreeID,
						"before_fitness": b.SuccessRate,
						"after_fitness":  bestFitness,
						"runs":           b.Runs,
						"tuned_params":   len(tunedParams),
						"algorithm":      "cmaes",
					}
					addFailureContext(entry, b.LastFailureTask, b.LastFailureOutcome)
					report = append(report, entry)
					continue
				}
				algorithms["genetic"]++
				pop := newProductionPopulation(population, baseTree)
				// Condition the warm-start on the bottleneck's concrete failing
				// task rather than just its tree type (Q2 Evolvability); an
				// empty LastFailureTask falls back to RetrieveByTreeType inside
				// EvolveWithExperienceContext, matching EvolveWithExperience.
				best := pop.EvolveWithExperienceContext(params.Generations, structuralFitnessFn, deps.expBank, b.LastFailureTask)
				entry := map[string]interface{}{
					"tree":           b.TreeID,
					"before_fitness": b.SuccessRate,
					"after_fitness":  pop.BestFitness,
					"runs":           b.Runs,
					"generations":    pop.Generation,
					"algorithm":      "genetic",
					"health":         evolveHealthProjection(pop),
				}
				addFailureContext(entry, b.LastFailureTask, b.LastFailureOutcome)
				// Persist the bred winner instead of discarding it after
				// computing its fitness (Q2 Evolvability).
				persistEvolvedWinner(deps, b.TreeID, best, pop.BestFitness, entry)
				report = append(report, entry)
			}
			data, _ := json.Marshal(map[string]interface{}{
				"bottlenecks":             len(bottlenecks),
				"report":                  report,
				"skipped":                 skipped,
				"algorithms":              algorithms,
				"experience_bank_entries": bankEntries,
			})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_evolve_selection_pressure", "Evolve every knowledge-graph tree under selection pressure (proven fitness >= 70 but underbred, RunCount < 5) via experience-grounded genetic evolution, writing each elite's fitness back through the evolved path and reporting per-tree before/after fitness",
		map[string]engine.Property{
			"population":  {Type: "integer", Description: "Population size per pressured tree (default: 20)"},
			"generations": {Type: "integer", Description: "Number of generations per pressured tree (default: 10)"},
		},
		[]string{},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Population  *int `json:"population"`
				Generations int  `json:"generations"`
			}
			_ = json.Unmarshal(args, &params)
			// Degenerate-param rejection precedes the dependency check so a bad
			// population is reported as such even without a knowledge graph.
			population, reject := resolveEvolvePopulation(params.Population)
			if reject != nil {
				return reject
			}
			if params.Generations <= 0 {
				params.Generations = 10
			}
			if deps.kg == nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: `{"error":"knowledge graph unavailable"}`}}}
			}
			// Deterministic, LLM-free closure of the learn→discover→evolve loop:
			// the same proven-but-underbred criteria ComputeAnalytics surfaces as
			// human-readable SuggestedActions strings drive structural evolution
			// directly, and each bred elite's fitness is written back through the
			// evolved path so fitness-driven discovery can surface the winners.
			pressure := deps.kg.ComputeAnalytics().SelectionPressure
			bankEntries := 0
			if deps.expBank != nil {
				bankEntries = deps.expBank.Count()
			}
			report := []map[string]interface{}{}
			skipped := []string{}
			for _, sp := range pressure {
				baseTree := resolveTree(sp.TreeID)
				if baseTree == nil {
					// A KG entry without a real behavior tree must not abort the
					// remaining pressure entries.
					skipped = append(skipped, sp.TreeID)
					continue
				}
				pop := newProductionPopulation(population, baseTree)
				best := pop.EvolveWithExperience(params.Generations, structuralFitnessFn, deps.expBank)
				recordEvolvedFitness(deps, sp.TreeID, pop.BestFitness)
				entry := map[string]interface{}{
					"tree":           sp.TreeID,
					"before_fitness": sp.Fitness,
					"after_fitness":  pop.BestFitness,
					"runs":           sp.RunCount,
					"generations":    pop.Generation,
					"algorithm":      "genetic",
					"health":         evolveHealthProjection(pop),
				}
				// Persist the bred elite instead of discarding it after
				// computing its fitness (Q2 Evolvability).
				persistEvolvedWinner(deps, sp.TreeID, best, pop.BestFitness, entry)
				report = append(report, entry)
			}
			data, _ := json.Marshal(map[string]interface{}{
				"selection_pressure":      len(pressure),
				"report":                  report,
				"skipped":                 skipped,
				"experience_bank_entries": bankEntries,
			})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── DEAD LETTER QUEUE (drop-safe replay, c8094002 ms3) ───────────────

	server.RegisterTool("bt_dlq_list", "List retained dead-letter entries (failed agent runs kept for inspection and drop-safe replay): id, agent, task, error, category, attempts, requeue/abandon state",
		map[string]engine.Property{
			"limit": {Type: "integer", Description: "Maximum entries to return, newest last (default: 50)"},
		},
		[]string{},
		func(args json.RawMessage) *engine.ToolResult {
			if engine.TaskDLQ == nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: `{"error":"dead letter queue unavailable in this instance"}`}}}
			}
			var params struct {
				Limit int `json:"limit"`
			}
			_ = json.Unmarshal(args, &params)
			if params.Limit <= 0 {
				params.Limit = 50
			}
			engine.TaskDLQ.Reload()
			entries := engine.TaskDLQ.List()
			total := len(entries)
			if len(entries) > params.Limit {
				entries = entries[len(entries)-params.Limit:]
			}
			out := make([]map[string]interface{}, 0, len(entries))
			for _, e := range entries {
				item := map[string]interface{}{
					"id":        e.ID,
					"agent":     e.Agent,
					"task":      truncateDLQField(e.Task, 140),
					"error":     truncateDLQField(e.Error, 200),
					"category":  e.Category,
					"attempts":  e.Attempts,
					"failed_at": e.FailedAt,
					"abandoned": e.Abandoned,
				}
				if !e.RequeuedAt.IsZero() {
					item["requeued_at"] = e.RequeuedAt
				}
				out = append(out, item)
			}
			data, _ := json.Marshal(map[string]interface{}{"count": total, "entries": out})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_dlq_replay", "Requeue a dead-letter entry for drop-safe re-execution — the entry is removed only after the replay succeeds. wait=true replays synchronously through this instance's executor and reports the outcome; otherwise the daemon's background scan consumes the requeue flag",
		map[string]engine.Property{
			"id":   {Type: "string", Description: "DLQ entry id"},
			"wait": {Type: "boolean", Description: "Replay synchronously and report the outcome (default: false)"},
		},
		[]string{"id"},
		func(args json.RawMessage) *engine.ToolResult {
			if engine.TaskDLQ == nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: `{"error":"dead letter queue unavailable in this instance"}`}}}
			}
			var params struct {
				ID   string `json:"id"`
				Wait bool   `json:"wait"`
			}
			_ = json.Unmarshal(args, &params)
			result := map[string]interface{}{}
			// Requeue merge-saves from this instance's in-memory view; reload
			// first so sibling stamps on the shared file aren't overwritten
			// with stale state.
			engine.TaskDLQ.Reload()
			if _, ok := engine.TaskDLQ.Requeue(params.ID); !ok {
				result["requeued"] = false
				result["reason"] = "unknown id, abandoned entry, or replay attempts exhausted"
				data, _ := json.Marshal(result)
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}
			result["requeued"] = true
			if params.Wait {
				if entry, ok := engine.TaskDLQ.Replay(params.ID); ok {
					result["replayed"] = true
					result["agent"] = entry.Agent
				} else {
					result["replayed"] = false
					result["reason"] = "replay did not succeed (executor missing in this instance, or the task failed again); entry retained"
					// A failed attempt stamps LastReplayError on the retained
					// entry; surface it so the caller sees the actual failure
					// instead of the ambiguous canned reason. No stamp means
					// the executor was missing and no attempt ran.
					for _, e := range engine.TaskDLQ.List() {
						if e.ID == params.ID && e.LastReplayError != "" {
							result["reason"] = "replay failed again; entry retained"
							result["last_replay_error"] = e.LastReplayError
							result["last_replay_at"] = e.LastReplayAt.Format(time.RFC3339)
							break
						}
					}
				}
			} else {
				result["note"] = "the daemon's background scan will replay this entry"
			}
			result["remaining"] = engine.TaskDLQ.Len()
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_evolve_expert", "Get expert knowledge recommendations for a tree",
		map[string]engine.Property{"tree": {Type: "string", Description: "Tree ID to analyze"}},
		[]string{"tree"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Tree string `json:"tree"`
			}
			_ = json.Unmarshal(args, &params)
			t := resolveTree(params.Tree)
			if t == nil {
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: `{"error":"unknown tree"}`}}}
			}
			ek := evolution.NewExpertKnowledge()
			// Warm-start from the same per-tree archive bt_evolve_qlearning
			// persists LearnedPatterns to (milestone 2/2 of the durable
			// Expert Knowledge program, Q2 Evolvability), so advisory calls
			// surface accumulated cross-run learned patterns alongside the
			// hardcoded benchmark catalog. A missing archive is a cold start;
			// a corrupt one degrades to the hardcoded catalog alone, surfaced
			// non-fatally.
			expertLoadErr := ""
			if err := ek.Load(expertArchivePath(params.Tree)); err != nil {
				expertLoadErr = err.Error()
			}
			patterns := ek.RecommendMutations(t)
			antiPatterns := ek.DetectAntiPatterns(t)
			var recs []map[string]interface{}
			for _, p := range patterns {
				recs = append(recs, map[string]interface{}{"name": p.Name, "mutation": p.Mutation, "target": p.Target, "expected_gain": p.ExpectedGain, "confidence": p.Confidence})
			}
			var issues []map[string]interface{}
			for _, ap := range antiPatterns {
				issues = append(issues, map[string]interface{}{"name": ap.Name, "severity": ap.Severity, "fix": ap.Fix})
			}
			result := map[string]interface{}{"tree": params.Tree, "recommendations": recs, "anti_patterns": issues, "learned_patterns": ek.LearnedPatterns}
			if expertLoadErr != "" {
				result["expert_archive_load_error"] = expertLoadErr
			}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── FACTORY ──────────────────────────────────────────────────────

	server.RegisterTool("bt_factory_create", "Breed a new behavior tree from existing parent trees",
		map[string]engine.Property{
			"task":     {Type: "string", Description: "Task description for the new tree"},
			"parent_a": {Type: "string", Description: "First parent tree ID (e.g., finance:pitch_agent)"},
			"parent_b": {Type: "string", Description: "Second parent tree ID (e.g., research:deep_research)"},
		},
		[]string{"task"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Task    string `json:"task"`
				ParentA string `json:"parent_a"`
				ParentB string `json:"parent_b"`
			}
			_ = json.Unmarshal(args, &params)
			f := newTreeFactory(deps)
			var tree *evolution.SerializableNode
			var treeID string
			if params.ParentA != "" && params.ParentB != "" {
				tree, treeID = f.CreateFromParents(params.ParentA, params.ParentB, params.Task)
			} else {
				category := params.ParentA
				if category == "" {
					category = "core"
				}
				tree, treeID = f.CreateTree(params.Task, category, nil)
			}
			cat := treeID
			if idx := strings.Index(treeID, ":"); idx >= 0 {
				cat = treeID[:idx]
			}
			result := map[string]interface{}{"tree_id": treeID, "node_count": evolution.CountNodes(tree), "parents": []string{params.ParentA, params.ParentB}, "category": cat}
			persistGeneratedTree(deps, treeID, tree, result)
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── WORKFLOW ─────────────────────────────────────────────────────

	server.RegisterTool("bt_workflow_run", "Run a YAML workflow pipeline or thinktank analysis on a topic",
		map[string]engine.Property{
			"pipeline": {Type: "string", Description: "Workflow name from agents/workflows/ (e.g. daily-research)"},
			"input":    {Type: "string", Description: "Initial input passed to the workflow"},
			"topic":    {Type: "string", Description: "Topic for thinktank:synthesis when pipeline is omitted"},
		},
		[]string{},
		func(args json.RawMessage) *engine.ToolResult {
			if degraded := checkLLMHealth(deps.llmHealth, "bt_workflow_run"); degraded != nil {
				return degraded
			}
			var params struct {
				Pipeline string `json:"pipeline"`
				Input    string `json:"input"`
				Topic    string `json:"topic"`
			}
			_ = json.Unmarshal(args, &params)

			if deps.agentRunner == nil {
				data, _ := json.Marshal(map[string]string{"error": "agent runner not configured"})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}

			if params.Pipeline != "" {
				pipeline, err := agentexec.LoadPipeline(params.Pipeline)
				if err != nil {
					data, _ := json.Marshal(map[string]string{"error": err.Error()})
					return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
				}
				result, err := agentexec.RunPipeline(context.Background(), deps.agentRunner, pipeline, params.Input)
				if err != nil && result == nil {
					data, _ := json.Marshal(map[string]string{"error": err.Error()})
					return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
				}
				resp := map[string]interface{}{
					"pipeline": params.Pipeline,
					"run_id":   result.RunID,
					"workflow": result.Workflow,
					"outcome":  result.Outcome,
					"duration": result.Duration.String(),
					"steps":    result.Steps,
				}
				if err != nil {
					resp["error"] = err.Error()
				}
				data, _ := json.Marshal(resp)
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}

			if params.Topic == "" {
				data, _ := json.Marshal(map[string]string{
					"error": "provide pipeline (YAML workflow name) or topic (thinktank analysis)",
				})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}

			res, err := deps.agentRunner.RunOnce(context.Background(), "thinktank:synthesis", params.Topic, agent.RunOptions{
				InjectMemory:   true,
				EnforceQuality: false,
				RecordHistory:  true,
				DisplayName:    "thinktank:synthesis",
			})
			if res == nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}
			resp := map[string]interface{}{
				"topic":   params.Topic,
				"tree":    res.TreeID,
				"outcome": res.Outcome,
				"result":  res.Output,
			}
			if err != nil {
				resp["error"] = err.Error()
			}
			data, _ := json.Marshal(resp)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_workflow_approve", "Approve or reject a workflow/HITL task (by task_id or request_id)",
		map[string]engine.Property{
			"task_id":    {Type: "string", Description: "Workflow approval task id (wf:name:step:run) or dashboard task id"},
			"request_id": {Type: "string", Description: "HITL request id (hitl-xxxxxxxx)"},
			"action":     {Type: "string", Description: "approve or reject"},
			"reviewer":   {Type: "string", Description: "Reviewer name (default: mcp)"},
			"comment":    {Type: "string", Description: "Approval comment or rejection reason"},
		},
		[]string{"action"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				TaskID    string `json:"task_id"`
				RequestID string `json:"request_id"`
				Action    string `json:"action"`
				Reviewer  string `json:"reviewer"`
				Comment   string `json:"comment"`
			}
			_ = json.Unmarshal(args, &params)
			if hitl.DefaultStore == nil {
				data, _ := json.Marshal(map[string]string{"error": "HITL store not initialized"})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}
			if params.Reviewer == "" {
				params.Reviewer = "mcp"
			}
			reject := params.Action == "reject"
			if params.RequestID != "" {
				var req *hitl.Request
				var err error
				if reject {
					req, err = hitl.DefaultStore.Reject(params.RequestID, params.Reviewer, params.Comment)
				} else {
					req, err = hitl.DefaultStore.Approve(params.RequestID, params.Reviewer, params.Comment)
				}
				if err != nil {
					data, _ := json.Marshal(map[string]string{"error": err.Error()})
					return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
				}
				data, _ := json.Marshal(req)
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}
			if params.TaskID == "" {
				data, _ := json.Marshal(map[string]string{"error": "task_id or request_id required"})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}
			resolvedFrom := "pending"
			if pending, ok := hitl.DefaultStore.FindPendingByTaskID(params.TaskID); ok && pending.Status == hitl.StatusEscalated {
				resolvedFrom = "escalated"
			}
			var req *hitl.Request
			var err error
			if reject {
				req, err = hitl.DefaultStore.RejectByTaskID(params.TaskID, params.Reviewer, params.Comment)
			} else {
				req, err = hitl.DefaultStore.ApproveByTaskID(params.TaskID, params.Reviewer, params.Comment)
			}
			if err != nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}
			data, _ := json.Marshal(struct {
				*hitl.Request
				ResolvedFrom string `json:"resolved_from"`
			}{req, resolvedFrom})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── AGENT PLATFORM ───────────────────────────────────────────────

	server.RegisterTool("bt_agent_create", "Create a new agent from a template or custom definition",
		map[string]engine.Property{
			"name":          {Type: "string", Description: "Agent name"},
			"description":   {Type: "string", Description: "Agent description"},
			"tree":          {Type: "string", Description: "Tree ID (e.g., domain:code_review, research:deep_research)"},
			"schedule":      {Type: "string", Description: "Schedule (on_demand, every 1h, 0 9 * * *)"},
			"from_template": {Type: "string", Description: "Create from template name instead of custom"},
		},
		[]string{"name", "tree"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Name         string `json:"name"`
				Description  string `json:"description"`
				Tree         string `json:"tree"`
				Schedule     string `json:"schedule"`
				FromTemplate string `json:"from_template"`
			}
			_ = json.Unmarshal(args, &params)
			if params.Schedule == "" {
				params.Schedule = "on_demand"
			}
			var inst *agent.Instance
			var err error
			if params.FromTemplate != "" {
				tmplDir := agent.TemplatesDir()
				cat := agent.NewCatalog(deps.agentReg, tmplDir)
				inst, err = cat.InstallFromTemplate(params.FromTemplate)
			} else {
				def := agent.Definition{Name: params.Name, Description: params.Description, Tree: params.Tree, Schedule: params.Schedule}
				inst, err = deps.agentReg.Create(def)
			}
			if err != nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}
			data, _ := json.Marshal(map[string]interface{}{"status": "created", "agent": inst.Definition.Name, "tree": inst.Definition.Tree, "id": inst.ID})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_agent_list", "List all installed agents with their status and stats",
		nil, nil,
		func(args json.RawMessage) *engine.ToolResult {
			instances := deps.agentReg.List()
			result := make([]map[string]interface{}, 0, len(instances))
			for _, inst := range instances {
				stats := deps.agentHist.Stats(inst.Definition.Name)
				result = append(result, map[string]interface{}{
					"name": inst.Definition.Name, "description": inst.Definition.Description,
					"tree": inst.Definition.Tree, "state": inst.State,
					"total_runs": stats.TotalRuns, "success_rate": stats.SuccessRate,
					"avg_quality": stats.AvgQuality, "last_run": stats.LastRun,
				})
			}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_agent_run", "Run an agent with a task immediately",
		map[string]engine.Property{
			"agent":  {Type: "string", Description: "Agent name or tree ID to run"},
			"task":   {Type: "string", Description: "Task to execute"},
			"inputs": {Type: "object", Description: "Named input values matching agent YAML inputs spec"},
		},
		[]string{"agent", "task"},
		func(args json.RawMessage) *engine.ToolResult {
			if degraded := checkLLMHealth(deps.llmHealth, "bt_agent_run"); degraded != nil {
				return degraded
			}
			var params struct {
				Agent  string            `json:"agent"`
				Task   string            `json:"task"`
				Inputs map[string]string `json:"inputs"`
			}
			_ = json.Unmarshal(args, &params)
			if deps.agentRunner == nil {
				data, _ := json.Marshal(map[string]string{"error": "agent runner not configured"})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}
			res, err := deps.agentRunner.RunOnce(context.Background(), params.Agent, params.Task, agent.RunOptions{
				InjectMemory:   true,
				EnforceQuality: true,
				RecordHistory:  true,
				InputValues:    params.Inputs,
			})
			if res == nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}
			resp := map[string]interface{}{
				"outcome": res.Outcome, "result": res.Output, "quality": res.Quality,
				"quality_passed": res.QualityPassed, "duration": res.Duration.String(),
				"tree": res.TreeID, "output_passed": res.OutputPassed,
			}
			if res.RunID != "" {
				resp["run_id"] = res.RunID
			}
			if res.SessionID != "" {
				resp["session_id"] = res.SessionID
			}
			if len(res.QualityReasons) > 0 {
				resp["quality_reasons"] = res.QualityReasons
			}
			if len(res.OutputReasons) > 0 {
				resp["output_reasons"] = res.OutputReasons
			}
			if err != nil {
				resp["error"] = err.Error()
			}
			data, _ := json.Marshal(resp)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_agent_history", "View run history for an agent",
		map[string]engine.Property{
			"agent": {Type: "string", Description: "Agent name"},
			"limit": {Type: "integer", Description: "Max records (default 10)"},
		},
		[]string{"agent"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Agent string `json:"agent"`
				Limit int    `json:"limit"`
			}
			_ = json.Unmarshal(args, &params)
			if params.Limit <= 0 {
				params.Limit = 10
			}
			runs := deps.agentHist.List(params.Agent, params.Limit)
			stats := deps.agentHist.Stats(params.Agent)
			data, _ := json.Marshal(map[string]interface{}{"agent": params.Agent, "stats": stats, "runs": runs})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_agent_schedule", "Schedule an agent for recurring execution",
		map[string]engine.Property{
			"agent":    {Type: "string", Description: "Agent name"},
			"schedule": {Type: "string", Description: "Cron expression (every 1h, 0 9 * * *)"},
			"timeout":  {Type: "string", Description: "Max run duration (30m, 2h)"},
		},
		[]string{"agent", "schedule"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Agent    string `json:"agent"`
				Schedule string `json:"schedule"`
				Timeout  string `json:"timeout"`
			}
			_ = json.Unmarshal(args, &params)
			if params.Timeout == "" {
				params.Timeout = "2h"
			}
			job, err := deps.globalSched.Schedule(params.Agent, params.Schedule, params.Timeout, 3)
			if err != nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}
			data, _ := json.Marshal(map[string]interface{}{"status": "scheduled", "job_id": job.ID, "agent": job.AgentName, "schedule": job.Schedule, "next_run": job.NextRun})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_agent_delete", "Delete an agent",
		map[string]engine.Property{"agent": {Type: "string", Description: "Agent name"}},
		[]string{"agent"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Agent string `json:"agent"`
			}
			_ = json.Unmarshal(args, &params)
			if deps.globalSched != nil {
				_, _ = deps.globalSched.Schedule(params.Agent, "on_demand", "2h", 3)
			}
			if err := agent.DeleteRegisteredAgent(deps.agentReg, params.Agent); err != nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}
			data, _ := json.Marshal(map[string]string{"status": "deleted", "agent": params.Agent})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── AGENT MEMORY ────────────────────────────────────────────────

	server.RegisterTool("bt_agent_memory_write", "Write a key-value entry to an agent's persistent memory. Categories: fact, pattern, pitfall, preference, state. Priority: high, medium, low.",
		map[string]engine.Property{
			"agent":    {Type: "string", Description: "Agent name"},
			"key":      {Type: "string", Description: "Memory key (e.g. 'pitfall:outcome_selector')"},
			"value":    {Type: "string", Description: "Value to store"},
			"category": {Type: "string", Description: "Category: fact, pattern, pitfall, preference, state"},
			"priority": {Type: "string", Description: "Priority: high, medium, low"},
			"source":   {Type: "string", Description: "Source: agent, reflection, manual, extracted"},
		},
		[]string{"agent", "key", "value"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Agent    string `json:"agent"`
				Key      string `json:"key"`
				Value    string `json:"value"`
				Category string `json:"category"`
				Priority string `json:"priority"`
				Source   string `json:"source"`
			}
			_ = json.Unmarshal(args, &params)
			if params.Category == "" {
				params.Category = "fact"
			}
			if params.Priority == "" {
				params.Priority = "medium"
			}
			if params.Source == "" {
				params.Source = "manual"
			}

			// Create per-agent memory store
			agentMem, err := agent.NewMemoryStore(agent.MemoryDir(), params.Agent, 100)
			if err != nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}

			if err := agentMem.Write(params.Key, params.Value, params.Category, params.Priority, params.Source); err != nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}
			stats := agentMem.Stats()
			data, _ := json.Marshal(map[string]interface{}{
				"status": "written",
				"agent":  params.Agent,
				"key":    params.Key,
				"stats":  stats,
			})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_agent_memory_read", "Read an agent's persistent memory. Use key to read specific entry, or omit for context block.",
		map[string]engine.Property{
			"agent":    {Type: "string", Description: "Agent name"},
			"key":      {Type: "string", Description: "Memory key to read (omit for all context)"},
			"category": {Type: "string", Description: "Filter by category prefix"},
			"priority": {Type: "string", Description: "Filter by priority"},
			"limit":    {Type: "integer", Description: "Max entries (default 10)"},
		},
		[]string{"agent"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Agent    string `json:"agent"`
				Key      string `json:"key"`
				Category string `json:"category"`
				Priority string `json:"priority"`
				Limit    int    `json:"limit"`
			}
			_ = json.Unmarshal(args, &params)
			if params.Limit <= 0 {
				params.Limit = 10
			}

			// Create per-agent memory store
			agentMem, err := agent.NewMemoryStore(agent.MemoryDir(), params.Agent, 100)
			if err != nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}

			if params.Key != "" {
				val := agentMem.Read(params.Key)
				data, _ := json.Marshal(map[string]string{"key": params.Key, "value": val, "found": fmt.Sprintf("%t", val != "")})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}

			entries := agentMem.Query(params.Category, params.Priority, params.Limit)
			contextBlock := agentMem.ContextBlock()
			data, _ := json.Marshal(map[string]interface{}{
				"agent":   params.Agent,
				"entries": entries,
				"context": contextBlock,
				"count":   len(entries),
			})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_agent_memory_delete", "Delete a memory entry from an agent",
		map[string]engine.Property{
			"agent": {Type: "string", Description: "Agent name"},
			"key":   {Type: "string", Description: "Memory key to delete"},
		},
		[]string{"agent", "key"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Agent string `json:"agent"`
				Key   string `json:"key"`
			}
			_ = json.Unmarshal(args, &params)

			agentMem, err := agent.NewMemoryStore(agent.MemoryDir(), params.Agent, 100)
			if err != nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}

			deleted := agentMem.Delete(params.Key)
			data, _ := json.Marshal(map[string]interface{}{"status": "deleted", "agent": params.Agent, "key": params.Key, "deleted": deleted})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── BLACKBOARD (run / session / agent scopes) ───────────────────

	registerBlackboardTools(server, deps)

	// ─── HEALTH ───────────────────────────────────────────────────────

	server.RegisterTool("bt_health", "Health check: reports LLM provider availability and server status",
		map[string]engine.Property{},
		[]string{},
		func(args json.RawMessage) *engine.ToolResult {
			snap := deps.llmHealth.State().Snapshot()
			snap["server"] = "bt-agent"
			snap["llm_provider"] = deps.cfg.LLMProvider
			data, _ := json.Marshal(snap)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── CIRCUIT BREAKER STATUS ──────────────────────────────────────────

	server.RegisterTool("bt_circuit_status", "Circuit breaker status for all scheduled agents. Shows open/closed states, failure counts, and cooldowns.",
		map[string]engine.Property{
			"agent": {Type: "string", Description: "Optional: specific agent name to query (default: all)"},
		},
		[]string{},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				Agent string `json:"agent"`
			}
			_ = json.Unmarshal(args, &params)

			if deps.globalSched == nil {
				data, _ := json.Marshal(map[string]string{"error": "scheduler not initialized"})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}

			cbStore := deps.globalSched.GetCBStore()
			if cbStore == nil {
				data, _ := json.Marshal(map[string]string{"status": "disabled", "message": "circuit breakers not configured"})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}

			status := cbStore.Status()
			if params.Agent != "" {
				if s, ok := status[params.Agent]; ok {
					data, _ := json.Marshal(map[string]interface{}{"agent": params.Agent, "status": s})
					return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
				}
				data, _ := json.Marshal(map[string]interface{}{
					"agent": params.Agent,
					"status": agent.CircuitSummary{
						State:        agent.CircuitClosed,
						FailureCount: 0,
						SuccessCount: 0,
						Threshold:    3,
						Cooldown:     300000000000,
					},
				})
				return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
			}

			data, _ := json.Marshal(map[string]interface{}{"circuit_breakers": status, "agent_count": len(status)})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	registerBlockTools(server, deps)
	registerHITLTools(server, deps)
	registerPersonaTools(server, deps)
	registerGoalTools(server, deps)
	registerAutomationTools(server, deps)
	registerFeedbackTools(server, deps)
	registerImpactTools(server, deps)
}

// resolveEvolvePopulation validates an evolve tool's population parameter at
// the MCP boundary, before any engine or dependency work runs. A nil value
// (omitted parameter) keeps the documented default of 20; an explicitly
// supplied value below 2 is rejected with a structured error, because elitism
// and crossover both need two individuals — a smaller "population" cannot
// evolve and historically panicked deep in the engine. A non-nil ToolResult
// is the rejection to return verbatim.
func resolveEvolvePopulation(population *int) (int, *engine.ToolResult) {
	if population == nil {
		return 20, nil
	}
	if *population < 2 {
		return 0, &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: `{"error":"population must be at least 2"}`}}}
	}
	return *population, nil
}

// structuralFitnessFn scores a tree's structural quality without invoking the
// LLM: it balances node count, depth, and node-type diversity against known
// anti-patterns. Shared by the deterministic bt_evolve_genetic and bt_evolve_qd
// evolution paths so both stay -short-safe.
func structuralFitnessFn(t *evolution.SerializableNode) float64 {
	nodeCount := float64(evolution.CountNodes(t))
	depth := float64(maxTreeDepth(t, 0))
	diversity := treeDiversityScore(t)

	// Base score: moderate node count (penalize both too small and too large)
	baseScore := 0.0
	if nodeCount >= 5 && nodeCount <= 80 {
		baseScore = nodeCount * 2.0
	} else if nodeCount < 5 {
		baseScore = nodeCount * 1.0 // penalize too simple
	} else {
		baseScore = 80.0 + (nodeCount-80)*0.5 // diminishing returns on huge trees
	}

	// Depth bonus (deep trees are better for complex tasks, up to a point)
	depthBonus := math.Min(depth*3.0, 30.0)

	// Diversity bonus (more node types = more capability)
	diversityBonus := diversity * 15.0

	// Anti-pattern penalty
	antiPatternPenalty := detectAntiPatternsInTree(t) * -10.0

	return baseScore + depthBonus + diversityBonus + antiPatternPenalty
}

// treeDiversityScore counts unique node types in the tree as a diversity metric.
func treeDiversityScore(node *evolution.SerializableNode) float64 {
	types := make(map[string]int)
	countTypes(node, types)
	// Score: number of unique types, normalized to 0-1 range (max ~8 types)
	return math.Min(float64(len(types))/8.0, 1.0)
}

func countTypes(node *evolution.SerializableNode, types map[string]int) {
	if node == nil {
		return
	}
	types[node.Type]++
	for i := range node.Children {
		countTypes(&node.Children[i], types)
	}
}

// detectAntiPatternsInTree scans for known quality issues.
func detectAntiPatternsInTree(node *evolution.SerializableNode) float64 {
	count := 0.0
	// Check for Retry nodes with MaxRetries > 5 (unbounded retry)
	walkTree(node, func(n *evolution.SerializableNode) {
		if n.Type == "Retry" && n.MaxRetries > 5 {
			count++
		}
		// Check for conditions with single-word names (keyword collision risk)
		if n.Type == "Condition" && len(strings.Fields(n.Name)) < 2 {
			count += 0.5
		}
		// Check for actions with no metadata (template-only execution)
		if n.Type == "Action" && n.Metadata == nil {
			count += 0.3
		}
	})
	return count
}

func walkTree(node *evolution.SerializableNode, fn func(*evolution.SerializableNode)) {
	if node == nil {
		return
	}
	fn(node)
	for i := range node.Children {
		walkTree(&node.Children[i], fn)
	}
}

func maxTreeDepth(node *evolution.SerializableNode, current int) int {
	if node == nil {
		return current
	}
	maxD := current
	for i := range node.Children {
		d := maxTreeDepth(&node.Children[i], current+1)
		if d > maxD {
			maxD = d
		}
	}
	return maxD
}
