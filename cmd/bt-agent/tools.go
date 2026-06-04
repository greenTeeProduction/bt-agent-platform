// Package main — MCP tool registration helpers and extracted handlers for bt-agent.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
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

	btcore "github.com/rvitorper/go-bt/core"
)

// checkLLMHealth returns a ToolResult with a degradation error if the LLM is
// unhealthy, or nil if the LLM is available. LLM-dependent tool handlers should
// call this first to fail fast with a clear message instead of timing out.
func checkLLMHealth(health *llm.HealthMonitor, toolName string) *mcp.ToolResult {
	if health == nil {
		return nil // no health monitor configured, proceed as normal
	}
	if !health.IsHealthy() {
		errMsg := fmt.Sprintf("LLM provider is currently %s — retry later when Ollama is available",
			health.State().Status().String())
		data, _ := json.Marshal(map[string]string{"error": errMsg, "tool": toolName, "degraded": "true"})
		return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
	}
	return nil
}

// mcpDeps bundles all shared state needed by tool handlers.
// This eliminates the 900-line closure chain from main() — every handler
// accesses state through this struct instead of capturing locals.
type mcpDeps struct {
	bb           *engine.Blackboard
	bt           *btcore.Command[engine.Blackboard]
	treeStore    *evolution.TreeStore
	refStore     *reflection.Store
	agentFactory *factory.AgentFactory
	kg           *knowledge.KnowledgeGraph
	llmClient    llm.LLM
	llmHealth    *llm.HealthMonitor
	cfg          *config.Config
	agentHome    string
	tracerHome   string
	// Agent platform
	agentReg    *agent.Registry
	agentHist   *agent.History
	agentMem    *agent.MemoryStore
	globalSched *agent.Scheduler
	dlq         *reliability.DeadLetterQueue
}

// registerMCPTools registers all 36 MCP tools on the server.
// Each tool handler accesses shared state through deps instead of main() locals.
func registerMCPTools(server *mcp.Server, deps *mcpDeps) {
	// ─── TREE EXECUTION ───────────────────────────────────────────────

	server.RegisterTool("bt_run_task", "Execute a task through the behavior tree",
		map[string]mcp.Property{"task": {Type: "string", Description: "The task to execute"}},
		[]string{"task"},
		func(args json.RawMessage) *mcp.ToolResult {
			if degraded := checkLLMHealth(deps.llmHealth, "bt_run_task"); degraded != nil {
				return degraded
			}
			var params struct {
				Task string `json:"task"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				btlog.Error("bt_run_task: invalid arguments", "error", err)
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}}}
			}
			btlog.Info("bt_run_task: executing", "task", params.Task)
			start := time.Now()
			deps.bb.Task = params.Task
			deps.bb.Complexity = ""
			deps.bb.Plan = ""
			deps.bb.Result = ""
			deps.bb.Outcome = ""
			deps.bb.KgResults = ""
			deps.bb.CachedResult = ""
			result := engine.RunTask(deps.bb, *deps.bt)
			duration := time.Since(start)
			if deps.bb.Outcome == string(reflection.Failure) {
				deps.bb.FailureCount = deps.refStore.CountFailures()
				btlog.Warn("bt_run_task: failed", "task", params.Task, "outcome", deps.bb.Outcome, "duration_ms", duration.Milliseconds())
			} else {
				btlog.Info("bt_run_task: completed", "task", params.Task, "outcome", deps.bb.Outcome, "duration_ms", duration.Milliseconds())
			}
			response := fmt.Sprintf(`{"result": %q, "outcome": %q, "complexity": %q, "duration_ms": %d, "plan": %q}`,
				result, deps.bb.Outcome, deps.bb.Complexity, deps.bb.DurationMs, deps.bb.Plan)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: response}}}
		})

	server.RegisterTool("bt_get_tree", "Get the current behavior tree definition",
		map[string]mcp.Property{}, nil,
		func(_ json.RawMessage) *mcp.ToolResult {
			tree, err := deps.treeStore.Load()
			if err != nil || tree == nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: `{"error": "no tree found"}`}}}
			}
			data, _ := json.MarshalIndent(tree, "", "  ")
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_get_reflections", "Get all reflection records",
		map[string]mcp.Property{}, nil,
		func(_ json.RawMessage) *mcp.ToolResult {
			records, err := deps.refStore.LoadAll()
			if err != nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			summary := map[string]interface{}{"total": len(records), "failures": deps.refStore.CountFailures(), "records": records}
			data, _ := json.MarshalIndent(summary, "", "  ")
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_evolve", "Run tree evolution (adapt on failures)",
		map[string]mcp.Property{}, nil,
		func(_ json.RawMessage) *mcp.ToolResult {
			tree, err := deps.treeStore.Load()
			if err != nil || tree == nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: `{"error": "no tree to evolve"}`}}}
			}
			failures := deps.refStore.CountFailures()
			if failures < 3 {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"evolved": false, "reason": "need 3+ failures, have %d"}`, failures)}}}
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
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_reset", "Reset the behavior tree to the default",
		map[string]mcp.Property{}, nil,
		func(_ json.RawMessage) *mcp.ToolResult {
			tree := evolution.DefaultTree()
			if err := deps.treeStore.Save(tree); err != nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"reset": false, "error": %q}`, err.Error())}}}
			}
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"reset": true, "nodes": %d}`, evolution.CountNodes(tree))}}}
		})

	server.RegisterTool("bt_get_fitness", "Get tree fitness stats",
		map[string]mcp.Property{}, nil,
		func(_ json.RawMessage) *mcp.ToolResult {
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
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_create_agent", "Create a behavior tree agent from a skill file",
		map[string]mcp.Property{"skill_path": {Type: "string", Description: "Path to SKILL.md"}},
		[]string{"skill_path"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				SkillPath string `json:"skill_path"`
			}
			_ = json.Unmarshal(args, &params)
			agent, err := deps.agentFactory.CreateFromSkillDir(params.SkillPath)
			if err != nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			result := map[string]interface{}{"created": true, "agent_name": agent.Name, "root_type": agent.SerTree.Type, "node_count": evolution.CountNodes(agent.SerTree)}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── DOMAIN SWITCHING ─────────────────────────────────────────────

	server.RegisterTool("bt_use_go_tree", "Switch to Go developer tree",
		map[string]mcp.Property{}, nil,
		func(_ json.RawMessage) *mcp.ToolResult {
			tree := evolution.GoDeveloperTree()
			_ = deps.treeStore.Save(tree)
			newBt := engine.BuildTree(tree, deps.bb)
			*deps.bt = newBt
			result := map[string]interface{}{"switched": true, "tree": "GoDeveloperTree", "node_count": evolution.CountNodes(tree), "strategies": 5}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_use_finance_tree", "Switch to an Anthropic finance agent behavior tree",
		map[string]mcp.Property{"agent": {Type: "string", Description: "Agent name: pitch_agent, earnings_reviewer, market_researcher, model_builder, meeting_prep, valuation_reviewer, gl_reconciler, month_end_closer, statement_auditor, kyc_screener"}},
		[]string{"agent"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				Agent string `json:"agent"`
			}
			_ = json.Unmarshal(args, &params)
			allTrees := finance.AllFinanceTrees()
			tree, ok := allTrees[params.Agent]
			if !ok {
				names := ""
				for k := range allTrees {
					names += k + ", "
				}
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": "unknown agent", "available": %q}`, names)}}}
			}
			_ = deps.treeStore.Save(tree)
			*deps.bt = engine.BuildTree(tree, deps.bb)
			result := map[string]interface{}{"switched": true, "agent": params.Agent, "description": finance.AgentDescriptions[params.Agent], "node_count": evolution.CountNodes(tree)}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_list_finance_trees", "List available Anthropic finance agent trees",
		map[string]mcp.Property{}, nil,
		func(_ json.RawMessage) *mcp.ToolResult {
			type agent struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Nodes       int    `json:"nodes"`
			}
			var agents []agent
			for name, tree := range finance.AllFinanceTrees() {
				agents = append(agents, agent{Name: name, Description: finance.AgentDescriptions[name], Nodes: evolution.CountNodes(tree)})
			}
			result := map[string]interface{}{"total": len(agents), "agents": agents}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_use_research_tree", "Switch to deep research or quick research behavior tree",
		map[string]mcp.Property{"variant": {Type: "string", Description: "deep_research or quick_research"}},
		[]string{"variant"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				Variant string `json:"variant"`
			}
			_ = json.Unmarshal(args, &params)
			if params.Variant == "" {
				params.Variant = "deep_research"
			}
			trees := research.ResearchTrees()
			tree, ok := trees[params.Variant]
			if !ok {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: `{"error": "unknown variant, use: deep_research, quick_research"}`}}}
			}
			_ = deps.treeStore.Save(tree)
			*deps.bt = engine.BuildTree(tree, deps.bb)
			result := map[string]interface{}{"switched": true, "variant": params.Variant, "description": research.Descriptions[params.Variant], "node_count": evolution.CountNodes(tree)}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_use_domain_tree", "Switch to a domain behavior tree (code_review, devops_ci, agent_monitor, refactoring, security_audit, data_pipeline, meeting_notes, crash_investigator, game_ai, trading_signal)",
		map[string]mcp.Property{"tree": {Type: "string", Description: "Tree name"}},
		[]string{"tree"},
		func(args json.RawMessage) *mcp.ToolResult {
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
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": "unknown tree", "available": %q}`, names)}}}
			}
			_ = deps.treeStore.Save(tree)
			*deps.bt = engine.BuildTree(tree, deps.bb)
			result := map[string]interface{}{"switched": true, "tree": params.Tree, "description": domains.Descriptions[params.Tree], "node_count": evolution.CountNodes(tree)}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── STARTUP SIMULATION ───────────────────────────────────────────

	server.RegisterTool("bt_startup_simulate", "Run a startup company simulation: sprint, quarter, or year",
		map[string]mcp.Property{
			"mode":    {Type: "string", Description: "sprint, quarter, or year"},
			"company": {Type: "string", Description: "Company name (default: HermesAI)"},
		},
		[]string{"mode"},
		func(args json.RawMessage) *mcp.ToolResult {
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
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: `{"error": "unknown mode, use sprint/quarter/year"}`}}}
			}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_startup_summary", "Get current state summary of the simulated startup company",
		nil, nil,
		func(_ json.RawMessage) *mcp.ToolResult {
			company := startup.NewDefaultCompany()
			summary := fmt.Sprintf("Company: %s | Stage: %s | Round: %s | Team: %d | MRR: $%.0f | Users: %d | Runway: %dmo | Cash: $%.0f",
				company.Name, company.ProductStage, company.FundingRound,
				company.TeamSize, company.MRR, company.Users,
				company.Runway, company.CashInBank)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: summary}}}
		})

	// ─── THINK TANK ───────────────────────────────────────────────────

	server.RegisterTool("bt_thinktank_analyze", "Run a full think tank analysis on a topic with 5 analytical perspectives",
		map[string]mcp.Property{
			"topic": {Type: "string", Description: "The topic/question to analyze"},
			"name":  {Type: "string", Description: "Think tank name (default: AI Strategy Council)"},
		},
		[]string{"topic"},
		func(args json.RawMessage) *mcp.ToolResult {
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
			_ = orch.RunFullAnalysis(params.Topic)
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
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── DELEGATION ───────────────────────────────────────────────────

	server.RegisterTool("bt_delegate_to_tree", "Delegate a task to a specific behavior tree for execution",
		map[string]mcp.Property{
			"tree": {Type: "string", Description: "Tree type: godev, finance:<name>, research:<name>, domain:<name>, startup:<role>, thinktank:<role>"},
			"task": {Type: "string", Description: "The task to delegate"},
		},
		[]string{"tree", "task"},
		func(args json.RawMessage) *mcp.ToolResult {
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
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error":"unknown tree: %s"}`, params.Tree)}}}
			}
			deps.bb.Task = params.Task
			*deps.bt = engine.BuildTree(tree, deps.bb)
			output := engine.RunTask(deps.bb, *deps.bt)
			result := map[string]interface{}{"delegated_to": params.Tree, "outcome": deps.bb.Outcome, "output": output}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── KNOWLEDGE GRAPH ─────────────────────────────────────────────

	server.RegisterTool("bt_kg_discover", "Discover the best behavior tree for a given task",
		map[string]mcp.Property{"task": {Type: "string", Description: "Task description to match against known trees"}},
		[]string{"task"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				Task string `json:"task"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			treeID, confidence := deps.kg.Discover(params.Task)
			result := map[string]interface{}{"tree_id": treeID, "confidence": confidence, "found": treeID != ""}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_kg_query", "Query the knowledge graph for trees matching a capability",
		map[string]mcp.Property{"capability": {Type: "string", Description: "Capability to search for (e.g., code_review, pitch, research)"}},
		[]string{"capability"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				Capability string `json:"capability"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
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
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_kg_auto_create", "Auto-discover or create a behavior tree for a task",
		map[string]mcp.Property{"task": {Type: "string", Description: "Task to discover or create a tree for"}},
		[]string{"task"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				Task string `json:"task"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			autoTree, treeID, err := knowledge.AutoCreateTree(deps.kg, params.Task)
			if err != nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			action := "created"
			if autoTree == nil {
				action = "discovered"
			}
			result := map[string]interface{}{"action": action, "tree_id": treeID}
			if autoTree != nil {
				result["node_count"] = evolution.CountNodes(autoTree)
			}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_kg_summary", "Get knowledge graph summary: tree counts by category, total edges",
		map[string]mcp.Property{}, nil,
		func(_ json.RawMessage) *mcp.ToolResult {
			summary := deps.kg.Summary()
			categories := make(map[string]int)
			for _, t := range deps.kg.Trees {
				categories[t.Category]++
			}
			result := map[string]interface{}{"summary": summary, "total_trees": len(deps.kg.Trees), "total_edges": len(deps.kg.Edges), "categories": categories}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_kg_list", "List all trees in a category",
		map[string]mcp.Property{"category": {Type: "string", Description: "Category to list (finance, domain, research, startup, thinktank, evolution, core)"}},
		[]string{"category"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				Category string `json:"category"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
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
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_kg_analytics", "Compute cross-tree analytics: centrality, tool contention, coverage gaps, bottlenecks, and suggested actions",
		map[string]mcp.Property{}, nil,
		func(_ json.RawMessage) *mcp.ToolResult {
			a := deps.kg.ComputeAnalytics()
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: a.FormatAnalytics()}}}
		})

	server.RegisterTool("bt_kg_explain", "Explain why a tree's last run failed, with the full execution path",
		map[string]mcp.Property{"tree": {Type: "string", Description: "Tree ID to explain (e.g., 'research:deep_research')"}},
		[]string{"tree"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				Tree string `json:"tree"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			explanation := deps.kg.ExplainLastFailure(params.Tree)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: explanation}}}
		})

	// ─── EVOLUTION ────────────────────────────────────────────────────

	server.RegisterTool("bt_evolve_genetic", "Run genetic algorithm evolution on a population of trees",
		map[string]mcp.Property{
			"tree":        {Type: "string", Description: "Base tree ID"},
			"population":  {Type: "integer", Description: "Population size (default: 20)"},
			"generations": {Type: "integer", Description: "Number of generations (default: 10)"},
		},
		[]string{"tree"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				Tree        string `json:"tree"`
				Population  int    `json:"population"`
				Generations int    `json:"generations"`
			}
			_ = json.Unmarshal(args, &params)
			if params.Population <= 0 {
				params.Population = 20
			}
			if params.Generations <= 0 {
				params.Generations = 10
			}
			baseTree := resolveTree(params.Tree)
			if baseTree == nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: `{"error":"unknown tree"}`}}}
			}
			pop := evolution.NewPopulation(params.Population, baseTree)
			fitnessFn := func(t *evolution.SerializableNode) float64 {
				// Structural quality: balance size, diversity, and anti-pattern avoidance
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
			best := pop.Evolve(params.Generations, fitnessFn)
			data, _ := json.Marshal(map[string]interface{}{
				"tree": params.Tree, "generations": pop.Generation,
				"best_fitness": pop.BestFitness, "diversity": pop.Diversity(),
				"convergence_rate": pop.ConvergenceRate(), "best_nodes": evolution.CountNodes(best),
				"regression_rate": fmt.Sprintf("%.1f%%", pop.RegressionRate()),
				"total_mutations": pop.TotalMutations, "regressions": pop.Regressions,
				"niche_diversity": fmt.Sprintf("%.2f", pop.NicheDiversity()),
			})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_evolve_expert", "Get expert knowledge recommendations for a tree",
		map[string]mcp.Property{"tree": {Type: "string", Description: "Tree ID to analyze"}},
		[]string{"tree"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				Tree string `json:"tree"`
			}
			_ = json.Unmarshal(args, &params)
			t := resolveTree(params.Tree)
			if t == nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: `{"error":"unknown tree"}`}}}
			}
			ek := evolution.NewExpertKnowledge()
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
			data, _ := json.Marshal(map[string]interface{}{"tree": params.Tree, "recommendations": recs, "anti_patterns": issues})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── FACTORY ──────────────────────────────────────────────────────

	server.RegisterTool("bt_factory_create", "Breed a new behavior tree from existing parent trees",
		map[string]mcp.Property{
			"task":     {Type: "string", Description: "Task description for the new tree"},
			"parent_a": {Type: "string", Description: "First parent tree ID (e.g., finance:pitch_agent)"},
			"parent_b": {Type: "string", Description: "Second parent tree ID (e.g., research:deep_research)"},
		},
		[]string{"task"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				Task    string `json:"task"`
				ParentA string `json:"parent_a"`
				ParentB string `json:"parent_b"`
			}
			_ = json.Unmarshal(args, &params)
			f := knowledge.NewFactory(deps.kg)
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
			data, _ := json.Marshal(map[string]interface{}{"tree_id": treeID, "node_count": evolution.CountNodes(tree), "parents": []string{params.ParentA, params.ParentB}, "category": treeID[:strings.Index(treeID, ":")]})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── WORKFLOW ─────────────────────────────────────────────────────

	server.RegisterTool("bt_workflow_run", "Run full thinktank->company pipeline: analyze, create tasks, execute",
		map[string]mcp.Property{"topic": {Type: "string", Description: "Topic for thinktank analysis"}},
		[]string{"topic"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				Topic string `json:"topic"`
			}
			_ = json.Unmarshal(args, &params)
			data, _ := json.Marshal(map[string]interface{}{"topic": params.Topic, "status": "pipeline ready — use bt_thinktank_analyze + bt_startup_simulate"})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_workflow_approve", "Approve or reject a task",
		map[string]mcp.Property{
			"task_id": {Type: "string", Description: "Task ID"},
			"action":  {Type: "string", Description: "approve or reject"},
		},
		[]string{"task_id", "action"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				TaskID string `json:"task_id"`
				Action string `json:"action"`
			}
			_ = json.Unmarshal(args, &params)
			status := "approved"
			if params.Action == "reject" {
				status = "rejected"
			}
			data, _ := json.Marshal(map[string]interface{}{"task_id": params.TaskID, "status": status})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── AGENT PLATFORM ───────────────────────────────────────────────

	server.RegisterTool("bt_agent_create", "Create a new agent from a template or custom definition",
		map[string]mcp.Property{
			"name":          {Type: "string", Description: "Agent name"},
			"description":   {Type: "string", Description: "Agent description"},
			"tree":          {Type: "string", Description: "Tree ID (e.g., domain:code_review, research:deep_research)"},
			"schedule":      {Type: "string", Description: "Schedule (on_demand, every 1h, 0 9 * * *)"},
			"from_template": {Type: "string", Description: "Create from template name instead of custom"},
		},
		[]string{"name", "tree"},
		func(args json.RawMessage) *mcp.ToolResult {
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
				tmplDir := deps.agentHome + "/go-bt-evolve/agents/templates"
				cat := agent.NewCatalog(deps.agentReg, tmplDir)
				inst, err = cat.InstallFromTemplate(params.FromTemplate)
			} else {
				def := agent.Definition{Name: params.Name, Description: params.Description, Tree: params.Tree, Schedule: params.Schedule}
				inst, err = deps.agentReg.Create(def)
			}
			if err != nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
			}
			data, _ := json.Marshal(map[string]interface{}{"status": "created", "agent": inst.Definition.Name, "tree": inst.Definition.Tree, "id": inst.ID})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_agent_list", "List all installed agents with their status and stats",
		nil, nil,
		func(_ json.RawMessage) *mcp.ToolResult {
			var result []map[string]interface{}
			for _, inst := range deps.agentReg.List() {
				stats := deps.agentHist.Stats(inst.Definition.Name)
				result = append(result, map[string]interface{}{
					"name": inst.Definition.Name, "description": inst.Definition.Description,
					"tree": inst.Definition.Tree, "state": inst.State,
					"total_runs": stats.TotalRuns, "success_rate": stats.SuccessRate,
					"avg_quality": stats.AvgQuality, "last_run": stats.LastRun,
				})
			}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_agent_run", "Run an agent with a task immediately",
		map[string]mcp.Property{
			"agent": {Type: "string", Description: "Agent name or tree ID to run"},
			"task":  {Type: "string", Description: "Task to execute"},
		},
		[]string{"agent", "task"},
		func(args json.RawMessage) *mcp.ToolResult {
			if degraded := checkLLMHealth(deps.llmHealth, "bt_agent_run"); degraded != nil {
				return degraded
			}
			var params struct {
				Agent string `json:"agent"`
				Task  string `json:"task"`
			}
			_ = json.Unmarshal(args, &params)
			bb := &engine.Blackboard{Task: params.Task, LLM: deps.llmClient}

			// Resolve through agent registry first — agent names are not tree IDs.
			// Only fall back to direct tree resolution if no agent found.
			var tree *evolution.SerializableNode
			inst, err := deps.agentReg.Get(params.Agent)
			if err == nil {
				tree = resolveTree(inst.Definition.Tree)
			}
			if tree == nil {
				tree = resolveTree(params.Agent)
			}
			if tree == nil {
				data, _ := json.Marshal(map[string]string{"error": "no tree found for: " + params.Agent})
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
			}
			start := time.Now()
			bt := engine.BuildTree(tree, bb)
			_ = engine.RunTask(bb, bt)
			outcome := bb.Outcome
			duration := time.Since(start)
			_ = deps.agentHist.Record(agent.RunRecord{
				AgentName: params.Agent, Task: params.Task, Outcome: outcome,
				Output: bb.Result, Duration: duration.String(), Quality: bb.QualityScore,
				StartedAt: start, EndedAt: time.Now(),
			})
			data, _ := json.Marshal(map[string]interface{}{"outcome": outcome, "result": bb.Result, "quality": bb.QualityScore, "duration": duration.String()})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_agent_history", "View run history for an agent",
		map[string]mcp.Property{
			"agent": {Type: "string", Description: "Agent name"},
			"limit": {Type: "integer", Description: "Max records (default 10)"},
		},
		[]string{"agent"},
		func(args json.RawMessage) *mcp.ToolResult {
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
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_agent_schedule", "Schedule an agent for recurring execution",
		map[string]mcp.Property{
			"agent":    {Type: "string", Description: "Agent name"},
			"schedule": {Type: "string", Description: "Cron expression (every 1h, 0 9 * * *)"},
			"timeout":  {Type: "string", Description: "Max run duration (30m, 2h)"},
		},
		[]string{"agent", "schedule"},
		func(args json.RawMessage) *mcp.ToolResult {
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
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
			}
			data, _ := json.Marshal(map[string]interface{}{"status": "scheduled", "job_id": job.ID, "agent": job.AgentName, "schedule": job.Schedule, "next_run": job.NextRun})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_agent_delete", "Delete an agent",
		map[string]mcp.Property{"agent": {Type: "string", Description: "Agent name"}},
		[]string{"agent"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				Agent string `json:"agent"`
			}
			_ = json.Unmarshal(args, &params)
			if err := deps.agentReg.Delete(params.Agent); err != nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
			}
			data, _ := json.Marshal(map[string]string{"status": "deleted", "agent": params.Agent})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── AGENT MEMORY ────────────────────────────────────────────────

	server.RegisterTool("bt_agent_memory_write", "Write a key-value entry to an agent's persistent memory. Categories: fact, pattern, pitfall, preference, state. Priority: high, medium, low.",
		map[string]mcp.Property{
			"agent":    {Type: "string", Description: "Agent name"},
			"key":      {Type: "string", Description: "Memory key (e.g. 'pitfall:outcome_selector')"},
			"value":    {Type: "string", Description: "Value to store"},
			"category": {Type: "string", Description: "Category: fact, pattern, pitfall, preference, state"},
			"priority": {Type: "string", Description: "Priority: high, medium, low"},
			"source":   {Type: "string", Description: "Source: agent, reflection, manual, extracted"},
		},
		[]string{"agent", "key", "value"},
		func(args json.RawMessage) *mcp.ToolResult {
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
			agentMem, err := agent.NewMemoryStore(deps.agentHome+"/.go-bt-evolve/memory", params.Agent, 100)
			if err != nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
			}

			if err := agentMem.Write(params.Key, params.Value, params.Category, params.Priority, params.Source); err != nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
			}
			stats := agentMem.Stats()
			data, _ := json.Marshal(map[string]interface{}{
				"status": "written",
				"agent":  params.Agent,
				"key":    params.Key,
				"stats":  stats,
			})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_agent_memory_read", "Read an agent's persistent memory. Use key to read specific entry, or omit for context block.",
		map[string]mcp.Property{
			"agent":    {Type: "string", Description: "Agent name"},
			"key":      {Type: "string", Description: "Memory key to read (omit for all context)"},
			"category": {Type: "string", Description: "Filter by category prefix"},
			"priority": {Type: "string", Description: "Filter by priority"},
			"limit":    {Type: "integer", Description: "Max entries (default 10)"},
		},
		[]string{"agent"},
		func(args json.RawMessage) *mcp.ToolResult {
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
			agentMem, err := agent.NewMemoryStore(deps.agentHome+"/.go-bt-evolve/memory", params.Agent, 100)
			if err != nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
			}

			if params.Key != "" {
				val := agentMem.Read(params.Key)
				data, _ := json.Marshal(map[string]string{"key": params.Key, "value": val, "found": fmt.Sprintf("%t", val != "")})
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
			}

			entries := agentMem.Query(params.Category, params.Priority, params.Limit)
			contextBlock := agentMem.ContextBlock()
			data, _ := json.Marshal(map[string]interface{}{
				"agent":   params.Agent,
				"entries": entries,
				"context": contextBlock,
				"count":   len(entries),
			})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_agent_memory_delete", "Delete a memory entry from an agent",
		map[string]mcp.Property{
			"agent": {Type: "string", Description: "Agent name"},
			"key":   {Type: "string", Description: "Memory key to delete"},
		},
		[]string{"agent", "key"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				Agent string `json:"agent"`
				Key   string `json:"key"`
			}
			_ = json.Unmarshal(args, &params)

			agentMem, err := agent.NewMemoryStore(deps.agentHome+"/.go-bt-evolve/memory", params.Agent, 100)
			if err != nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
			}

			deleted := agentMem.Delete(params.Key)
			data, _ := json.Marshal(map[string]interface{}{"status": "deleted", "agent": params.Agent, "key": params.Key, "deleted": deleted})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── HEALTH ───────────────────────────────────────────────────────

	server.RegisterTool("bt_health", "Health check: reports LLM provider availability and server status",
		map[string]mcp.Property{},
		[]string{},
		func(_ json.RawMessage) *mcp.ToolResult {
			snap := deps.llmHealth.State().Snapshot()
			snap["server"] = "bt-agent"
			snap["llm_provider"] = deps.cfg.LLMProvider
			data, _ := json.Marshal(snap)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// ─── CIRCUIT BREAKER STATUS ──────────────────────────────────────────

	server.RegisterTool("bt_circuit_status", "Circuit breaker status for all scheduled agents. Shows open/closed states, failure counts, and cooldowns.",
		map[string]mcp.Property{
			"agent": {Type: "string", Description: "Optional: specific agent name to query (default: all)"},
		},
		[]string{},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				Agent string `json:"agent"`
			}
			_ = json.Unmarshal(args, &params)

			if deps.globalSched == nil {
				data, _ := json.Marshal(map[string]string{"error": "scheduler not initialized"})
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
			}

			cbStore := deps.globalSched.GetCBStore()
			if cbStore == nil {
				data, _ := json.Marshal(map[string]string{"status": "disabled", "message": "circuit breakers not configured"})
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
			}

			status := cbStore.Status()
			if params.Agent != "" {
				if s, ok := status[params.Agent]; ok {
					data, _ := json.Marshal(map[string]interface{}{"agent": params.Agent, "status": s})
					return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
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
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
			}

			data, _ := json.Marshal(map[string]interface{}{"circuit_breakers": status, "agent_count": len(status)})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	registerBlockTools(server, deps)
	registerHITLTools(server, deps)
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
