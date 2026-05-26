package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/factory"
	"github.com/nico/go-bt-evolve/internal/finance"
	"github.com/nico/go-bt-evolve/internal/knowledge"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/mcp"
	"github.com/nico/go-bt-evolve/internal/reflection"
	"github.com/nico/go-bt-evolve/internal/research"
	"github.com/nico/go-bt-evolve/internal/startup"
	"github.com/nico/go-bt-evolve/internal/thinktank"
)

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
	if id == "kanban:task_creator" { return evolution.KanbanTaskCreatorTree() }
	if id == "kanban:refiner"     { return evolution.KanbanRefinerTree() }
	if id == "kanban:qa"          { return evolution.KanbanQATree() }
	if id == "kanban:monitor"     { return evolution.KanbanBoardMonitorTree() }
	if id == "kanban:workflow"    { return evolution.KanbanWorkflowTree() }
	if id == "kanban:autopilot"   { return evolution.KanbanAutoPilotTree() }
	// NotebookLM tree
	if id == "notebooklm"          { return evolution.NotebookLMTree() }
	if id == "hermes_obsidian"     { return evolution.HermesObsidianOptimizerTree() }
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
		// Return individual fellow tree or the synthesis tree
		role := id[10:]
		switch role {
		case "synthesis": return thinktank.SynthesisTree()
		case "peer_review": return thinktank.PeerReviewTree()
		case "report": return thinktank.ReportGenerationTree()
		}
	}
	return nil
}

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}

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

	llmClient, err := llm.NewClient(llm.DefaultConfig())
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}

	agentFactory, err := factory.NewAgentFactory(llmClient, home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: factory: %v\n", err)
		os.Exit(1)
	}

	// Initialize knowledge graph and seed with known trees.
	kg := knowledge.NewKnowledgeGraph()
	kg.Register(&knowledge.TreeMeta{
		ID: "core:default", Name: "Default Tree", Category: "core",
		Description: "Default behavior tree for general tasks",
		Keywords:    []string{"task", "general", "help"},
		Capabilities: []knowledge.Capability{{Action: "execute_task", Domain: "general", Strength: 0.5}},
	})
	kg.Register(&knowledge.TreeMeta{
		ID: "domain:godev", Name: "Go Developer", Category: "domain",
		Description: "Go software development: review, compile, test, refactor",
		Keywords:    []string{"go", "golang", "code", "compile", "test", "build", "review", "refactor"},
		Capabilities: []knowledge.Capability{{Action: "develop_go", Domain: "engineering", Strength: 0.9}},
	})
	kg.Register(&knowledge.TreeMeta{
		ID: "domain:code_review", Name: "Code Review", Category: "domain",
		Description: "Automated code review and improvement suggestions",
		Keywords:    []string{"review", "code", "audit", "check", "inspect"},
		Capabilities: []knowledge.Capability{{Action: "review_code", Domain: "engineering", Strength: 0.8}},
	})
	kg.Register(&knowledge.TreeMeta{
		ID: "finance:pitch_agent", Name: "Pitch Agent", Category: "finance",
		Description: "Craft investment pitch decks and narratives",
		Keywords:    []string{"pitch", "investment", "investor", "deck", "narrative", "presentation"},
		Capabilities: []knowledge.Capability{{Action: "craft_pitch", Domain: "finance", Strength: 0.9}},
	})
	kg.Register(&knowledge.TreeMeta{
		ID: "finance:market_researcher", Name: "Market Researcher", Category: "finance",
		Description: "Research markets, competitors, and trends",
		Keywords:    []string{"market", "research", "competitor", "trend", "analysis"},
		Capabilities: []knowledge.Capability{{Action: "research_market", Domain: "finance", Strength: 0.9}},
	})
	kg.Register(&knowledge.TreeMeta{
		ID: "finance:earnings_reviewer", Name: "Earnings Reviewer", Category: "finance",
		Description: "Review earnings reports and financial statements",
		Keywords:    []string{"earnings", "report", "financial", "statement", "quarterly", "review"},
		Capabilities: []knowledge.Capability{{Action: "review_earnings", Domain: "finance", Strength: 0.9}},
	})
	kg.Register(&knowledge.TreeMeta{
		ID: "research:deep_research", Name: "Deep Research", Category: "research",
		Description: "In-depth multi-source research on any topic",
		Keywords:    []string{"research", "deep", "study", "investigate", "explore", "literature"},
		Capabilities: []knowledge.Capability{{Action: "deep_research", Domain: "research", Strength: 0.9}},
	})
	kg.Register(&knowledge.TreeMeta{
		ID: "startup:ceo", Name: "Startup CEO", Category: "startup",
		Description: "CEO perspective: strategy, vision, fundraising",
		Keywords:    []string{"ceo", "strategy", "vision", "fundraising", "leadership"},
		Capabilities: []knowledge.Capability{{Action: "ceo_strategy", Domain: "startup", Strength: 0.8}},
	})
	kg.Register(&knowledge.TreeMeta{
		ID: "thinktank:synthesis", Name: "Think Tank Synthesis", Category: "thinktank",
		Description: "Synthesize multi-perspective analysis into recommendations",
		Keywords:    []string{"synthesis", "analyze", "perspective", "recommendation", "council"},
		Capabilities: []knowledge.Capability{{Action: "synthesize_analysis", Domain: "strategy", Strength: 0.9}},
	})

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

	server := mcp.NewServer("go-bt-agent")

	server.RegisterTool("bt_run_task", "Execute a task through the behavior tree",
		map[string]mcp.Property{"task": {Type: "string", Description: "The task to execute"}},
		[]string{"task"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct{ Task string `json:"task"` }
			if err := json.Unmarshal(args, &params); err != nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}}}
			}
			bb.Task = params.Task
			bb.Complexity = ""
			bb.Plan = ""
			bb.Result = ""
			bb.Outcome = ""
			bb.KgResults = ""
			bb.CachedResult = ""
			result := engine.RunTask(bb, bt)
			if bb.Outcome == string(reflection.Failure) {
				bb.FailureCount = refStore.CountFailures()
			} else {
				bb.FailureCount = 0
			}
			response := fmt.Sprintf(`{"result": %q, "outcome": %q, "complexity": %q, "duration_ms": %d, "plan": %q}`,
				result, bb.Outcome, bb.Complexity, bb.DurationMs, bb.Plan)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: response}}}
		})

	server.RegisterTool("bt_get_tree", "Get the current behavior tree definition",
		map[string]mcp.Property{}, nil,
		func(args json.RawMessage) *mcp.ToolResult {
			tree, err := treeStore.Load()
			if err != nil || tree == nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: `{"error": "no tree found"}`}}}
			}
			data, _ := json.MarshalIndent(tree, "", "  ")
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_get_reflections", "Get all reflection records",
		map[string]mcp.Property{}, nil,
		func(args json.RawMessage) *mcp.ToolResult {
			records, err := refStore.LoadAll()
			if err != nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			summary := map[string]interface{}{"total": len(records), "failures": refStore.CountFailures(), "records": records}
			data, _ := json.MarshalIndent(summary, "", "  ")
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_evolve", "Run tree evolution (adapt on failures)",
		map[string]mcp.Property{}, nil,
		func(args json.RawMessage) *mcp.ToolResult {
			tree, err := treeStore.Load()
			if err != nil || tree == nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: `{"error": "no tree to evolve"}`}}}
			}
			failures := refStore.CountFailures()
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
				_ = treeStore.Save(tree)
			}
			result := map[string]interface{}{"evolved": applied > 0, "applied": applied, "nodes_before": before, "nodes_after": after}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_reset", "Reset the behavior tree to the default",
		map[string]mcp.Property{}, nil,
		func(args json.RawMessage) *mcp.ToolResult {
			tree := evolution.DefaultTree()
			if err := treeStore.Save(tree); err != nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"reset": false, "error": %q}`, err.Error())}}}
			}
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"reset": true, "nodes": %d}`, evolution.CountNodes(tree))}}}
		})

	server.RegisterTool("bt_get_fitness", "Get tree fitness stats",
		map[string]mcp.Property{}, nil,
		func(args json.RawMessage) *mcp.ToolResult {
			tree, _ := treeStore.Load()
			records, _ := refStore.LoadAll()
			failures := refStore.CountFailures()
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
			var params struct{ SkillPath string `json:"skill_path"` }
			json.Unmarshal(args, &params)
			agent, err := agentFactory.CreateFromSkillDir(params.SkillPath)
			if err != nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			result := map[string]interface{}{"created": true, "agent_name": agent.Name, "root_type": agent.SerTree.Type, "node_count": evolution.CountNodes(agent.SerTree)}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_use_go_tree", "Switch to Go developer tree",
		map[string]mcp.Property{}, nil,
		func(args json.RawMessage) *mcp.ToolResult {
			tree := evolution.GoDeveloperTree()
			treeStore.Save(tree)
			bt = engine.BuildTree(tree, bb)
			result := map[string]interface{}{"switched": true, "tree": "GoDeveloperTree", "node_count": evolution.CountNodes(tree), "strategies": 5}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_use_finance_tree", "Switch to an Anthropic finance agent behavior tree",
		map[string]mcp.Property{"agent": {Type: "string", Description: "Agent name: pitch_agent, earnings_reviewer, market_researcher, model_builder, meeting_prep, valuation_reviewer, gl_reconciler, month_end_closer, statement_auditor, kyc_screener"}},
		[]string{"agent"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct{ Agent string `json:"agent"` }
			json.Unmarshal(args, &params)
			allTrees := finance.AllFinanceTrees()
			tree, ok := allTrees[params.Agent]
			if !ok {
				names := ""
				for k := range allTrees {
					names += k + ", "
				}
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": "unknown agent", "available": %q}`, names)}}}
			}
			treeStore.Save(tree)
			bt = engine.BuildTree(tree, bb)
			result := map[string]interface{}{
				"switched":   true,
				"agent":      params.Agent,
				"description": finance.AgentDescriptions[params.Agent],
				"node_count": evolution.CountNodes(tree),
			}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_list_finance_trees", "List available Anthropic finance agent trees",
		map[string]mcp.Property{}, nil,
		func(args json.RawMessage) *mcp.ToolResult {
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

	// --- Research tree tools ---

	server.RegisterTool("bt_use_research_tree", "Switch to deep research or quick research behavior tree",
		map[string]mcp.Property{"variant": {Type: "string", Description: "deep_research or quick_research"}},
		[]string{"variant"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct{ Variant string `json:"variant"` }
			json.Unmarshal(args, &params)
			if params.Variant == "" {
				params.Variant = "deep_research"
			}
			trees := research.ResearchTrees()
			tree, ok := trees[params.Variant]
			if !ok {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: `{"error": "unknown variant, use: deep_research, quick_research"}`}}}
			}
			treeStore.Save(tree)
			bt = engine.BuildTree(tree, bb)
			result := map[string]interface{}{
				"switched":    true,
				"variant":     params.Variant,
				"description": research.Descriptions[params.Variant],
				"node_count":  evolution.CountNodes(tree),
			}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// --- Domain tree tool ---

	server.RegisterTool("bt_use_domain_tree", "Switch to a domain behavior tree (code_review, devops_ci, agent_monitor, refactoring, security_audit, data_pipeline, meeting_notes, crash_investigator, game_ai, trading_signal)",
		map[string]mcp.Property{"tree": {Type: "string", Description: "Tree name"}},
		[]string{"tree"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct{ Tree string `json:"tree"` }
			json.Unmarshal(args, &params)
			allTrees := domains.AllDomainTrees()
			tree, ok := allTrees[params.Tree]
			if !ok {
				names := ""
				for k := range allTrees { names += k + ", " }
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": "unknown tree", "available": %q}`, names)}}}
			}
			treeStore.Save(tree)
			bt = engine.BuildTree(tree, bb)
			result := map[string]interface{}{
				"switched": true, "tree": params.Tree,
				"description": domains.Descriptions[params.Tree],
				"node_count": evolution.CountNodes(tree),
			}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// --- Startup company simulation tools ---

	server.RegisterTool("bt_startup_simulate", "Run a startup company simulation: sprint, quarter, or year",
		map[string]mcp.Property{
			"mode":   {Type: "string", Description: "sprint, quarter, or year"},
			"company": {Type: "string", Description: "Company name (default: HermesAI)"},
		},
		[]string{"mode"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				Mode    string `json:"mode"`
				Company string `json:"company"`
			}
			json.Unmarshal(args, &params)

			company := startup.NewDefaultCompany()
			if params.Company != "" {
				company.Name = params.Company
			}
			orch := startup.NewOrchestrator(company, llmClient)

			var result map[string]interface{}
			switch params.Mode {
			case "sprint":
				s := orch.RunSprint()
				result = map[string]interface{}{
					"sprint":       s.SprintNum,
					"goal":         s.Goal,
					"completed":    s.Completed,
					"velocity":     s.Velocity,
					"company_state": company,
				}
			case "quarter":
				q := orch.RunQuarter()
				result = map[string]interface{}{
					"quarter":     q.Quarter,
					"revenue":     q.Revenue,
					"growth_pct":  q.Growth,
					"highlights":  q.Highlights,
					"company_state": company,
				}
			case "year":
				quarters := orch.RunYear()
				result = map[string]interface{}{
					"quarters":     quarters,
					"company_state": company,
				}
			default:
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: `{"error": "unknown mode, use sprint/quarter/year"}`}}}
			}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_startup_summary", "Get current state summary of the simulated startup company",
		nil, nil,
		func(args json.RawMessage) *mcp.ToolResult {
			company := startup.NewDefaultCompany()
			summary := fmt.Sprintf("Company: %s | Stage: %s | Round: %s | Team: %d | MRR: $%.0f | Users: %d | Runway: %dmo | Cash: $%.0f",
				company.Name, company.ProductStage, company.FundingRound,
				company.TeamSize, company.MRR, company.Users,
				company.Runway, company.CashInBank)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: summary}}}
		})

	// --- Think tank tools ---

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
			json.Unmarshal(args, &params)
			if params.Name == "" {
				params.Name = "AI Strategy Council"
			}

			tt := thinktank.NewThinkTank(params.Name, params.Topic)
			orch := &thinktank.ThinkTankOrchestrator{Tank: tt, LLM: llmClient}
			orch.RunFullAnalysis(params.Topic)

			var scenarios []map[string]interface{}
			if tt.FinalReport != nil {
				for _, s := range tt.FinalReport.Scenarios {
					scenarios = append(scenarios, map[string]interface{}{
						"name": s.Name, "probability": s.Probability, "impact": s.Impact,
					})
				}
			}

			result := map[string]interface{}{
				"topic":       params.Topic,
				"fellows":      len(tt.Fellows),
				"findings":     len(tt.ResearchFindings),
				"debate_turns": len(tt.DebateTranscript),
				"scenarios":    scenarios,
			}
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

	server.RegisterTool("bt_delegate_to_tree", "Delegate a task to a specific behavior tree for execution",
		map[string]mcp.Property{
			"tree": {Type: "string", Description: "Tree type: godev, finance:<name>, research:<name>, domain:<name>, startup:<role>, thinktank:<role>"},
			"task": {Type: "string", Description: "The task to delegate"},
		},
		[]string{"tree", "task"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				Tree string `json:"tree"`
				Task string `json:"task"`
			}
			json.Unmarshal(args, &params)

			tree := resolveTree(params.Tree)
			if tree == nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error":"unknown tree: %s"}`, params.Tree)}}}
			}

			bb.Task = params.Task
			bt = engine.BuildTree(tree, bb)
			output := engine.RunTask(bb, bt)

			result := map[string]interface{}{
				"delegated_to": params.Tree,
				"outcome":      bb.Outcome,
				"output":       output,
			}
			data, _ := json.Marshal(result)
		return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
	})

	// --- Knowledge graph tools ---

	server.RegisterTool("bt_kg_discover", "Discover the best behavior tree for a given task",
		map[string]mcp.Property{"task": {Type: "string", Description: "Task description to match against known trees"}},
		[]string{"task"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct{ Task string `json:"task"` }
			if err := json.Unmarshal(args, &params); err != nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			treeID, confidence := kg.Discover(params.Task)
			result := map[string]interface{}{"tree_id": treeID, "confidence": confidence, "found": treeID != ""}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_kg_query", "Query the knowledge graph for trees matching a capability",
		map[string]mcp.Property{"capability": {Type: "string", Description: "Capability to search for (e.g., code_review, pitch, research)"}},
		[]string{"capability"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct{ Capability string `json:"capability"` }
			if err := json.Unmarshal(args, &params); err != nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			trees := kg.Query(params.Capability)
			var results []map[string]interface{}
			for _, t := range trees {
				results = append(results, map[string]interface{}{
					"id": t.ID, "name": t.Name, "category": t.Category,
					"description": t.Description, "fitness": t.Fitness, "node_count": t.NodeCount,
				})
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
			var params struct{ Task string `json:"task"` }
			if err := json.Unmarshal(args, &params); err != nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			autoTree, treeID, err := knowledge.AutoCreateTree(kg, params.Task)
			if err != nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			action := "created"
			if autoTree == nil {
				action = "discovered"
			}
			result := map[string]interface{}{
				"action":  action,
				"tree_id": treeID,
			}
			if autoTree != nil {
				result["node_count"] = evolution.CountNodes(autoTree)
			}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_kg_summary", "Get knowledge graph summary: tree counts by category, total edges",
		map[string]mcp.Property{}, nil,
		func(args json.RawMessage) *mcp.ToolResult {
			summary := kg.Summary()
			categories := make(map[string]int)
			for _, t := range kg.Trees {
				categories[t.Category]++
			}
			result := map[string]interface{}{
				"summary":       summary,
				"total_trees":   len(kg.Trees),
				"total_edges":   len(kg.Edges),
				"categories":    categories,
			}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_kg_list", "List all trees in a category",
		map[string]mcp.Property{"category": {Type: "string", Description: "Category to list (finance, domain, research, startup, thinktank, evolution, core)"}},
		[]string{"category"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct{ Category string `json:"category"` }
			if err := json.Unmarshal(args, &params); err != nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, err.Error())}}}
			}
			trees := kg.ListByCategory(params.Category)
			var results []map[string]interface{}
			for _, t := range trees {
				results = append(results, map[string]interface{}{
					"id": t.ID, "name": t.Name, "description": t.Description,
					"fitness": t.Fitness, "node_count": t.NodeCount,
				})
			}
			if results == nil {
				results = []map[string]interface{}{}
			}
			data, _ := json.Marshal(map[string]interface{}{"category": params.Category, "total": len(results), "trees": results})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// --- Evolution algorithm tools ---

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
			json.Unmarshal(args, &params)
			if params.Population <= 0 { params.Population = 20 }
			if params.Generations <= 0 { params.Generations = 10 }

			baseTree := resolveTree(params.Tree)
			if baseTree == nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: `{"error":"unknown tree"}`}}}
			}

			pop := evolution.NewPopulation(params.Population, baseTree)
			fitnessFn := func(t *evolution.SerializableNode) float64 {
				return float64(evolution.CountNodes(t)) * 2.0
			}
			best := pop.Evolve(params.Generations, fitnessFn)

			data, _ := json.Marshal(map[string]interface{}{
				"tree": params.Tree, "generations": pop.Generation,
				"best_fitness": pop.BestFitness, "diversity": pop.Diversity(),
				"convergence_rate": pop.ConvergenceRate(), "best_nodes": evolution.CountNodes(best),
			})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// --- Expert knowledge tool ---

	server.RegisterTool("bt_evolve_expert", "Get expert knowledge recommendations for a tree",
		map[string]mcp.Property{
			"tree": {Type: "string", Description: "Tree ID to analyze"},
		},
		[]string{"tree"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct{ Tree string `json:"tree"` }
			json.Unmarshal(args, &params)
			t := resolveTree(params.Tree)
			if t == nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: `{"error":"unknown tree"}`}}}
			}
			ek := evolution.NewExpertKnowledge()
			patterns := ek.RecommendMutations(t)
			antiPatterns := ek.DetectAntiPatterns(t)
			var recs []map[string]interface{}
			for _, p := range patterns {
				recs = append(recs, map[string]interface{}{
					"name": p.Name, "mutation": p.Mutation, "target": p.Target,
					"expected_gain": p.ExpectedGain, "confidence": p.Confidence,
				})
			}
			var issues []map[string]interface{}
			for _, ap := range antiPatterns {
				issues = append(issues, map[string]interface{}{
					"name": ap.Name, "severity": ap.Severity, "fix": ap.Fix,
				})
			}
			data, _ := json.Marshal(map[string]interface{}{
				"tree": params.Tree, "recommendations": recs, "anti_patterns": issues,
			})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// --- Factory tool ---

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
			json.Unmarshal(args, &params)

			f := knowledge.NewFactory(kg)
			var tree *evolution.SerializableNode
			var treeID string

			if params.ParentA != "" && params.ParentB != "" {
				tree, treeID = f.CreateFromParents(params.ParentA, params.ParentB, params.Task)
			} else {
				category := params.ParentA // can use parent_a as category hint
				if category == "" { category = "core" }
				tree, treeID = f.CreateTree(params.Task, category, nil)
			}

			data, _ := json.Marshal(map[string]interface{}{
				"tree_id": treeID, "node_count": evolution.CountNodes(tree),
				"parents": []string{params.ParentA, params.ParentB},
				"category": treeID[:strings.Index(treeID, ":")],
			})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// --- Workflow tools ---

	server.RegisterTool("bt_workflow_run", "Run full thinktank->company pipeline: analyze, create tasks, execute",
		map[string]mcp.Property{
			"topic": {Type: "string", Description: "Topic for thinktank analysis"},
		},
		[]string{"topic"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct{ Topic string `json:"topic"` }
			json.Unmarshal(args, &params)
			data, _ := json.Marshal(map[string]interface{}{
				"topic": params.Topic, "status": "pipeline ready — use bt_thinktank_analyze + bt_startup_simulate",
			})
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
			json.Unmarshal(args, &params)
			status := "approved"
			if params.Action == "reject" { status = "rejected" }
			data, _ := json.Marshal(map[string]interface{}{"task_id": params.TaskID, "status": status})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// --- Agent Platform Integration Tools ---
	// Initialize agent registry and history
	agentHome, _ := os.UserHomeDir()
	agentReg, _ := agent.NewRegistry(agentHome + "/.go-bt-evolve/agents")
	agentHist, _ := agent.NewHistory(agentHome + "/.go-bt-evolve/history")

	server.RegisterTool("bt_agent_create", "Create a new agent from a template or custom definition",
		map[string]mcp.Property{
			"name":        {Type: "string", Description: "Agent name"},
			"description": {Type: "string", Description: "Agent description"},
			"tree":        {Type: "string", Description: "Tree ID (e.g., domain:code_review, research:deep_research)"},
			"schedule":    {Type: "string", Description: "Schedule (on_demand, every 1h, 0 9 * * *)"},
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
			json.Unmarshal(args, &params)
			if params.Schedule == "" { params.Schedule = "on_demand" }

			var inst *agent.Instance
			var err error
			if params.FromTemplate != "" {
				tmplDir := agentHome + "/go-bt-evolve/agents/templates"
				cat := agent.NewCatalog(agentReg, tmplDir)
				inst, err = cat.InstallFromTemplate(params.FromTemplate)
			} else {
				def := agent.Definition{Name: params.Name, Description: params.Description, Tree: params.Tree, Schedule: params.Schedule}
				inst, err = agentReg.Create(def)
			}
			if err != nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
			}
			data, _ := json.Marshal(map[string]interface{}{
				"status": "created", "agent": inst.Definition.Name, "tree": inst.Definition.Tree, "id": inst.ID,
			})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_agent_list", "List all installed agents with their status and stats",
		nil, nil,
		func(args json.RawMessage) *mcp.ToolResult {
			var result []map[string]interface{}
			for _, inst := range agentReg.List() {
				stats := agentHist.Stats(inst.Definition.Name)
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
			var params struct{ Agent string `json:"agent"`; Task string `json:"task"` }
			json.Unmarshal(args, &params)
			bb := &engine.Blackboard{Task: params.Task, LLM: llmClient}
			tree := resolveTree(params.Agent)
			if tree == nil {
				inst, err := agentReg.Get(params.Agent)
				if err != nil {
					data, _ := json.Marshal(map[string]string{"error": "agent not found: " + params.Agent})
					return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
				}
				tree = resolveTree(inst.Definition.Tree)
			}
			if tree == nil {
				data, _ := json.Marshal(map[string]string{"error": "no tree found for: " + params.Agent})
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
			}
			start := time.Now()
			bt := engine.BuildTree(tree, bb)
			outcome := engine.RunTask(bb, bt)
			duration := time.Since(start)
			agentHist.Record(agent.RunRecord{
				AgentName: params.Agent, Task: params.Task, Outcome: outcome,
				Output: bb.Result, Duration: duration.String(), Quality: bb.QualityScore,
				StartedAt: start, EndedAt: time.Now(),
			})
			data, _ := json.Marshal(map[string]interface{}{
				"outcome": outcome, "result": bb.Result, "quality": bb.QualityScore, "duration": duration.String(),
			})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_agent_history", "View run history for an agent",
		map[string]mcp.Property{
			"agent": {Type: "string", Description: "Agent name"},
			"limit": {Type: "integer", Description: "Max records (default 10)"},
		},
		[]string{"agent"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct{ Agent string `json:"agent"`; Limit int `json:"limit"` }
			json.Unmarshal(args, &params)
			if params.Limit <= 0 { params.Limit = 10 }
			runs := agentHist.List(params.Agent, params.Limit)
			stats := agentHist.Stats(params.Agent)
			data, _ := json.Marshal(map[string]interface{}{
				"agent": params.Agent, "stats": stats, "runs": runs,
			})
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
			var params struct{ Agent string `json:"agent"`; Schedule string `json:"schedule"`; Timeout string `json:"timeout"` }
			json.Unmarshal(args, &params)
			if params.Timeout == "" { params.Timeout = "2h" }
			sched := agent.NewScheduler(agent.SchedulerConfig{Registry: agentReg, History: agentHist})
			job, err := sched.Schedule(params.Agent, params.Schedule, params.Timeout, 3)
			if err != nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
			}
			data, _ := json.Marshal(map[string]interface{}{
				"status": "scheduled", "job_id": job.ID, "agent": job.AgentName,
				"schedule": job.Schedule, "next_run": job.NextRun,
			})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_agent_delete", "Delete an agent",
		map[string]mcp.Property{"agent": {Type: "string", Description: "Agent name"}},
		[]string{"agent"},
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct{ Agent string `json:"agent"` }
			json.Unmarshal(args, &params)
			if err := agentReg.Delete(params.Agent); err != nil {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
			}
			data, _ := json.Marshal(map[string]string{"status": "deleted", "agent": params.Agent})
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	if err := server.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
