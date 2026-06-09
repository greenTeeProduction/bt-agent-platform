package knowledge

// BuildKnowledgeGraph registers all behavior trees into the knowledge graph.
func BuildKnowledgeGraph() *KnowledgeGraph {
	kg := NewKnowledgeGraph()

	// ── CORE TREES ──────────────────────────────────────────────────────────

	kg.Register(&TreeMeta{
		ID:          "default",
		Category:    "core",
		Name:        "Default Agent",
		Description: "General-purpose BT agent with KG, cache, and agent-based execution",
		NodeCount:   17,
		Keywords:    []string{"task", "execute", "general", "assistant"},
		Capabilities: []Capability{
			{Action: "execute_task", Domain: "general", Strength: 0.7},
			{Action: "query_knowledge", Domain: "general", Strength: 0.6},
			{Action: "cache_lookup", Domain: "general", Strength: 0.5},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "godev",
		Category:    "core",
		Name:        "Go Developer",
		Description: "Go software development: code review, build, test, knowledge",
		NodeCount:   27,
		Keywords:    []string{"go", "golang", "code", "review", "build", "test", "compile"},
		Capabilities: []Capability{
			{Action: "review_code", Domain: "engineering", Strength: 0.8},
			{Action: "build_project", Domain: "engineering", Strength: 0.7},
			{Action: "run_tests", Domain: "engineering", Strength: 0.8},
			{Action: "compile_binary", Domain: "engineering", Strength: 0.6},
		},
	})

	// ── FINANCE TREES ───────────────────────────────────────────────────────

	kg.Register(&TreeMeta{
		ID:          "finance:pitch_agent",
		Category:    "finance",
		Name:        "Pitch Agent",
		Description: "Investment pitch creation with comps, DCF, LBO",
		NodeCount:   39,
		Keywords:    []string{"pitch", "investment", "comps", "valuation", "dcf", "lbo"},
		Capabilities: []Capability{
			{Action: "analyze_financials", Domain: "finance", Strength: 0.9},
			{Action: "build_pitch_deck", Domain: "finance", Strength: 0.9},
			{Action: "run_dcf", Domain: "finance", Strength: 0.85},
			{Action: "run_lbo", Domain: "finance", Strength: 0.8},
			{Action: "comparable_analysis", Domain: "finance", Strength: 0.85},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "finance:earnings_reviewer",
		Category:    "finance",
		Name:        "Earnings Reviewer",
		Description: "Quarterly earnings analysis and model updates",
		NodeCount:   29,
		Keywords:    []string{"earnings", "quarterly", "revenue", "eps", "consensus"},
		Capabilities: []Capability{
			{Action: "analyze_financials", Domain: "finance", Strength: 0.85},
			{Action: "review_earnings", Domain: "finance", Strength: 0.9},
			{Action: "update_models", Domain: "finance", Strength: 0.8},
			{Action: "compare_consensus", Domain: "finance", Strength: 0.75},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "finance:market_researcher",
		Category:    "finance",
		Name:        "Market Researcher",
		Description: "Industry and competitive research",
		NodeCount:   27,
		Keywords:    []string{"market", "research", "industry", "competitive", "tam"},
		Capabilities: []Capability{
			{Action: "analyze_financials", Domain: "finance", Strength: 0.7},
			{Action: "market_research", Domain: "finance", Strength: 0.9},
			{Action: "competitive_analysis", Domain: "finance", Strength: 0.85},
			{Action: "size_tam", Domain: "finance", Strength: 0.8},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "finance:model_builder",
		Category:    "finance",
		Name:        "Model Builder",
		Description: "Financial model construction (3-statement, DCF, LBO)",
		NodeCount:   27,
		Keywords:    []string{"model", "financial", "spreadsheet", "projection"},
		Capabilities: []Capability{
			{Action: "analyze_financials", Domain: "finance", Strength: 0.9},
			{Action: "build_financial_model", Domain: "finance", Strength: 0.95},
			{Action: "three_statement_model", Domain: "finance", Strength: 0.9},
			{Action: "run_dcf", Domain: "finance", Strength: 0.8},
			{Action: "run_lbo", Domain: "finance", Strength: 0.75},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "finance:valuation_reviewer",
		Category:    "finance",
		Name:        "Valuation Reviewer",
		Description: "GP package valuation review",
		NodeCount:   20,
		Keywords:    []string{"valuation", "gp", "nav", "lp", "capital"},
		Capabilities: []Capability{
			{Action: "analyze_financials", Domain: "finance", Strength: 0.8},
			{Action: "review_valuation", Domain: "finance", Strength: 0.9},
			{Action: "audit_nav", Domain: "finance", Strength: 0.85},
			{Action: "verify_capital_accounts", Domain: "finance", Strength: 0.8},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "finance:meeting_prep",
		Category:    "finance",
		Name:        "Meeting Prep",
		Description: "Client meeting preparation",
		NodeCount:   20,
		Keywords:    []string{"meeting", "prep", "briefing", "client"},
		Capabilities: []Capability{
			{Action: "analyze_financials", Domain: "finance", Strength: 0.7},
			{Action: "prepare_briefing", Domain: "finance", Strength: 0.9},
			{Action: "summarize_portfolio", Domain: "finance", Strength: 0.8},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "finance:gl_reconciler",
		Category:    "finance",
		Name:        "GL Reconciler",
		Description: "General ledger reconciliation",
		NodeCount:   21,
		Keywords:    []string{"reconcile", "ledger", "gl", "breaks"},
		Capabilities: []Capability{
			{Action: "analyze_financials", Domain: "finance", Strength: 0.85},
			{Action: "reconcile_ledger", Domain: "finance", Strength: 0.95},
			{Action: "identify_breaks", Domain: "finance", Strength: 0.9},
			{Action: "verify_transactions", Domain: "finance", Strength: 0.85},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "finance:month_end_closer",
		Category:    "finance",
		Name:        "Month-End Closer",
		Description: "Month-end closing procedures",
		NodeCount:   21,
		Keywords:    []string{"month-end", "close", "accruals", "variance"},
		Capabilities: []Capability{
			{Action: "analyze_financials", Domain: "finance", Strength: 0.85},
			{Action: "close_books", Domain: "finance", Strength: 0.9},
			{Action: "calculate_accruals", Domain: "finance", Strength: 0.85},
			{Action: "analyze_variance", Domain: "finance", Strength: 0.8},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "finance:statement_auditor",
		Category:    "finance",
		Name:        "Statement Auditor",
		Description: "LP statement audit",
		NodeCount:   21,
		Keywords:    []string{"audit", "statement", "lp", "verify"},
		Capabilities: []Capability{
			{Action: "analyze_financials", Domain: "finance", Strength: 0.85},
			{Action: "audit_statements", Domain: "finance", Strength: 0.9},
			{Action: "verify_lp_allocations", Domain: "finance", Strength: 0.85},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "finance:kyc_screener",
		Category:    "finance",
		Name:        "KYC Screener",
		Description: "KYC/AML document screening",
		NodeCount:   21,
		Keywords:    []string{"kyc", "aml", "screening", "compliance", "onboarding"},
		Capabilities: []Capability{
			{Action: "analyze_financials", Domain: "finance", Strength: 0.6},
			{Action: "screen_documents", Domain: "compliance", Strength: 0.9},
			{Action: "verify_identity", Domain: "compliance", Strength: 0.85},
			{Action: "check_sanctions", Domain: "compliance", Strength: 0.9},
		},
	})

	// ── RESEARCH TREES ──────────────────────────────────────────────────────

	kg.Register(&TreeMeta{
		ID:          "research:deep_research",
		Category:    "research",
		Name:        "Deep Research",
		Description: "5-phase agent-powered deep research with quality gate",
		NodeCount:   20,
		Keywords:    []string{"research", "deep", "investigate", "analyze", "synthesize", "report"},
		Capabilities: []Capability{
			{Action: "conduct_research", Domain: "research", Strength: 0.95},
			{Action: "synthesize_findings", Domain: "research", Strength: 0.9},
			{Action: "generate_report", Domain: "research", Strength: 0.85},
			{Action: "quality_gate", Domain: "research", Strength: 0.8},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "research:quick_research",
		Category:    "research",
		Name:        "Quick Research",
		Description: "Single-pass agent research for fast answers",
		NodeCount:   12,
		Keywords:    []string{"quick", "lookup", "fact", "summary"},
		Capabilities: []Capability{
			{Action: "conduct_research", Domain: "research", Strength: 0.7},
			{Action: "quick_lookup", Domain: "research", Strength: 0.9},
			{Action: "summarize_facts", Domain: "research", Strength: 0.8},
		},
	})

	// ── DOMAIN TREES ────────────────────────────────────────────────────────

	kg.Register(&TreeMeta{
		ID:          "domain:code_review",
		Category:    "domain",
		Name:        "Code Review",
		Description: "Code review with bug detection and improvement suggestions",
		NodeCount:   27,
		Keywords:    []string{"review", "code", "bug", "security", "style"},
		Capabilities: []Capability{
			{Action: "review_code", Domain: "engineering", Strength: 0.9},
			{Action: "detect_bugs", Domain: "engineering", Strength: 0.85},
			{Action: "suggest_improvements", Domain: "engineering", Strength: 0.8},
			{Action: "audit_security", Domain: "engineering", Strength: 0.7},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "domain:devops_ci",
		Category:    "domain",
		Name:        "DevOps CI",
		Description: "CI/CD pipeline management",
		NodeCount:   33,
		Keywords:    []string{"devops", "ci", "cd", "pipeline", "deploy", "build"},
		Capabilities: []Capability{
			{Action: "manage_pipeline", Domain: "engineering", Strength: 0.9},
			{Action: "deploy_service", Domain: "engineering", Strength: 0.85},
			{Action: "build_project", Domain: "engineering", Strength: 0.8},
			{Action: "monitor_health", Domain: "engineering", Strength: 0.7},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "domain:agent_monitor",
		Category:    "domain",
		Name:        "Agent Monitor",
		Description: "Agent health monitoring and restart",
		NodeCount:   31,
		Keywords:    []string{"monitor", "health", "agent", "restart", "metrics"},
		Capabilities: []Capability{
			{Action: "monitor_health", Domain: "engineering", Strength: 0.9},
			{Action: "restart_service", Domain: "engineering", Strength: 0.8},
			{Action: "collect_metrics", Domain: "engineering", Strength: 0.85},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "domain:refactoring",
		Category:    "domain",
		Name:        "Refactoring",
		Description: "Code refactoring and improvement",
		NodeCount:   24,
		Keywords:    []string{"refactor", "clean", "improve", "restructure"},
		Capabilities: []Capability{
			{Action: "review_code", Domain: "engineering", Strength: 0.8},
			{Action: "refactor_code", Domain: "engineering", Strength: 0.95},
			{Action: "improve_structure", Domain: "engineering", Strength: 0.9},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "domain:security_audit",
		Category:    "domain",
		Name:        "Security Audit",
		Description: "Security vulnerability scanning",
		NodeCount:   30,
		Keywords:    []string{"security", "vulnerability", "scan", "audit", "penetration"},
		Capabilities: []Capability{
			{Action: "audit_security", Domain: "engineering", Strength: 0.95},
			{Action: "scan_vulnerabilities", Domain: "engineering", Strength: 0.9},
			{Action: "penetration_test", Domain: "engineering", Strength: 0.8},
			{Action: "review_code", Domain: "engineering", Strength: 0.7},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "domain:data_pipeline",
		Category:    "domain",
		Name:        "Data Pipeline",
		Description: "Data pipeline design and monitoring",
		NodeCount:   25,
		Keywords:    []string{"data", "pipeline", "etl", "streaming", "warehouse"},
		Capabilities: []Capability{
			{Action: "build_pipeline", Domain: "engineering", Strength: 0.9},
			{Action: "process_data", Domain: "engineering", Strength: 0.85},
			{Action: "monitor_health", Domain: "engineering", Strength: 0.75},
			{Action: "manage_warehouse", Domain: "engineering", Strength: 0.7},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "domain:meeting_notes",
		Category:    "domain",
		Name:        "Meeting Notes",
		Description: "Meeting transcription and action items",
		NodeCount:   29,
		Keywords:    []string{"meeting", "notes", "transcription", "action", "minutes"},
		Capabilities: []Capability{
			{Action: "transcribe_meeting", Domain: "productivity", Strength: 0.9},
			{Action: "extract_action_items", Domain: "productivity", Strength: 0.85},
			{Action: "generate_minutes", Domain: "productivity", Strength: 0.8},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "domain:crash_investigator",
		Category:    "domain",
		Name:        "Crash Investigator",
		Description: "Crash and incident investigation",
		NodeCount:   29,
		Keywords:    []string{"crash", "incident", "debug", "stack", "trace"},
		Capabilities: []Capability{
			{Action: "investigate_crash", Domain: "engineering", Strength: 0.95},
			{Action: "analyze_stack_trace", Domain: "engineering", Strength: 0.9},
			{Action: "identify_root_cause", Domain: "engineering", Strength: 0.85},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "domain:game_ai",
		Category:    "domain",
		Name:        "Game AI",
		Description: "Game AI behavior tree",
		NodeCount:   35,
		Keywords:    []string{"game", "ai", "npc", "behavior", "strategy"},
		Capabilities: []Capability{
			{Action: "control_npc", Domain: "gaming", Strength: 0.9},
			{Action: "execute_strategy", Domain: "gaming", Strength: 0.85},
			{Action: "simulate_behavior", Domain: "gaming", Strength: 0.8},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "domain:trading_signal",
		Category:    "domain",
		Name:        "Trading Signal",
		Description: "Trading signal generation",
		NodeCount:   26,
		Keywords:    []string{"trading", "signal", "market", "indicator", "strategy"},
		Capabilities: []Capability{
			{Action: "analyze_financials", Domain: "finance", Strength: 0.8},
			{Action: "generate_signals", Domain: "finance", Strength: 0.9},
			{Action: "analyze_market", Domain: "finance", Strength: 0.85},
			{Action: "execute_strategy", Domain: "finance", Strength: 0.7},
		},
	})

	// ── NotebookLM Domain Trees ────────────────────────────────────────────

	kg.Register(&TreeMeta{
		ID:          "domain:notebooklm_plan_implement",
		Category:    "domain",
		Name:        "NotebookLM Plan-Implement",
		Description: "Research → Grill → Plan → Implement → Verify → Deploy pipeline: NotebookLM deep research, critical review, implementation plan, subagent delegation, tests, build/deploy",
		NodeCount:   12,
		Keywords:    []string{"notebooklm", "plan", "implement", "research", "grill", "deploy", "pipeline", "build", "test"},
		Capabilities: []Capability{
			{Action: "deep_research", Domain: "research", Strength: 0.9},
			{Action: "critical_review", Domain: "research", Strength: 0.85},
			{Action: "write_plan", Domain: "engineering", Strength: 0.85},
			{Action: "implement_code", Domain: "engineering", Strength: 0.9},
			{Action: "run_tests", Domain: "engineering", Strength: 0.85},
			{Action: "deploy_service", Domain: "engineering", Strength: 0.8},
		},
	})

	// ── GOAP DOMAIN TREES ─────────────────────────────────────────────────

	kg.Register(&TreeMeta{
		ID:          "domain:goap_planning",
		Category:    "domain",
		Name:        "GOAP Planning",
		Description: "Goal-Oriented Action Planning: multi-step sequential task planning",
		NodeCount:   12,
		Keywords:    []string{"goap", "plan", "planning", "goal", "action", "sequential", "multi-step"},
		Capabilities: []Capability{
			{Action: "plan_actions", Domain: "engineering", Strength: 0.9},
			{Action: "execute_sequence", Domain: "engineering", Strength: 0.85},
			{Action: "multi_step_task", Domain: "engineering", Strength: 0.8},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "domain:goap_research",
		Category:    "domain",
		Name:        "GOAP Research",
		Description: "GOAP multi-phase research: literature → hypothesis → experiment → conclusions",
		NodeCount:   12,
		Keywords:    []string{"goap", "research", "literature", "hypothesis", "experiment", "scientific"},
		Capabilities: []Capability{
			{Action: "research_pipeline", Domain: "research", Strength: 0.9},
			{Action: "scientific_method", Domain: "research", Strength: 0.85},
			{Action: "literature_review", Domain: "research", Strength: 0.8},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "domain:goap_devops",
		Category:    "domain",
		Name:        "GOAP DevOps",
		Description: "GOAP CI/CD pipeline: checkout → lint → test → build → deploy → smoke test",
		NodeCount:   12,
		Keywords:    []string{"goap", "devops", "ci", "cd", "pipeline", "deploy", "build", "test"},
		Capabilities: []Capability{
			{Action: "ci_cd_pipeline", Domain: "engineering", Strength: 0.9},
			{Action: "deploy_service", Domain: "engineering", Strength: 0.85},
			{Action: "build_project", Domain: "engineering", Strength: 0.8},
		},
	})

	// GOAP cross-domain relationships
	kg.Connect("domain:goap_planning", "domain:goap_research", "specializes")
	kg.Connect("domain:goap_planning", "domain:goap_devops", "specializes")

	// ── STARTUP TREES ───────────────────────────────────────────────────────

	kg.Register(&TreeMeta{
		ID:          "startup:ceo",
		Category:    "startup",
		Name:        "CEO",
		Description: "CEO role: strategy, fundraising, quarterly goals",
		NodeCount:   10,
		Keywords:    []string{"ceo", "strategy", "fundraising", "vision", "quarterly", "goals"},
		Capabilities: []Capability{
			{Action: "make_decisions", Domain: "strategy", Strength: 0.9},
			{Action: "plan_fundraising", Domain: "strategy", Strength: 0.85},
			{Action: "set_goals", Domain: "strategy", Strength: 0.8},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "startup:cto",
		Category:    "startup",
		Name:        "CTO",
		Description: "CTO role: architecture, tech decisions, roadmap",
		NodeCount:   10,
		Keywords:    []string{"cto", "architecture", "tech", "roadmap", "engineering"},
		Capabilities: []Capability{
			{Action: "make_decisions", Domain: "strategy", Strength: 0.85},
			{Action: "design_architecture", Domain: "engineering", Strength: 0.9},
			{Action: "plan_roadmap", Domain: "engineering", Strength: 0.85},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "startup:pm",
		Category:    "startup",
		Name:        "Product Manager",
		Description: "PM role: features, feedback, prioritization",
		NodeCount:   10,
		Keywords:    []string{"pm", "product", "features", "roadmap", "feedback", "prioritize"},
		Capabilities: []Capability{
			{Action: "make_decisions", Domain: "strategy", Strength: 0.8},
			{Action: "prioritize_features", Domain: "product", Strength: 0.9},
			{Action: "gather_feedback", Domain: "product", Strength: 0.85},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "startup:engineer",
		Category:    "startup",
		Name:        "Engineer",
		Description: "Engineer role: sprint planning, build, test, deploy",
		NodeCount:   10,
		Keywords:    []string{"engineer", "sprint", "build", "test", "deploy", "code"},
		Capabilities: []Capability{
			{Action: "review_code", Domain: "engineering", Strength: 0.8},
			{Action: "build_project", Domain: "engineering", Strength: 0.85},
			{Action: "run_tests", Domain: "engineering", Strength: 0.85},
			{Action: "deploy_service", Domain: "engineering", Strength: 0.75},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "startup:marketing",
		Category:    "startup",
		Name:        "Marketing",
		Description: "Marketing role: content, SEO, community, campaigns",
		NodeCount:   10,
		Keywords:    []string{"marketing", "content", "seo", "community", "campaign", "growth"},
		Capabilities: []Capability{
			{Action: "create_content", Domain: "marketing", Strength: 0.9},
			{Action: "optimize_seo", Domain: "marketing", Strength: 0.85},
			{Action: "run_campaign", Domain: "marketing", Strength: 0.85},
			{Action: "grow_community", Domain: "marketing", Strength: 0.8},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "startup:sales",
		Category:    "startup",
		Name:        "Sales",
		Description: "Sales role: leads, demos, pricing, close",
		NodeCount:   10,
		Keywords:    []string{"sales", "leads", "demo", "pricing", "close", "revenue"},
		Capabilities: []Capability{
			{Action: "qualify_leads", Domain: "sales", Strength: 0.9},
			{Action: "prepare_demo", Domain: "sales", Strength: 0.85},
			{Action: "negotiate_pricing", Domain: "sales", Strength: 0.8},
			{Action: "close_deals", Domain: "sales", Strength: 0.85},
		},
	})

	// ── THINKTANK TREES ─────────────────────────────────────────────────────

	kg.Register(&TreeMeta{
		ID:          "thinktank:research",
		Category:    "thinktank",
		Name:        "Think Tank Research",
		Description: "Fellow research phase",
		NodeCount:   12,
		Keywords:    []string{"thinktank", "fellow", "perspective", "analysis"},
		Capabilities: []Capability{
			{Action: "conduct_research", Domain: "research", Strength: 0.85},
			{Action: "analyze_perspective", Domain: "research", Strength: 0.8},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "thinktank:debate",
		Category:    "thinktank",
		Name:        "Think Tank Debate",
		Description: "Structured dialectic debate",
		NodeCount:   15,
		Keywords:    []string{"debate", "dialectic", "argument", "thesis", "antithesis"},
		Capabilities: []Capability{
			{Action: "conduct_debate", Domain: "research", Strength: 0.9},
			{Action: "evaluate_arguments", Domain: "research", Strength: 0.85},
			{Action: "synthesize_findings", Domain: "research", Strength: 0.8},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "thinktank:synthesis",
		Category:    "thinktank",
		Name:        "Think Tank Synthesis",
		Description: "Synthesize findings into recommendation",
		NodeCount:   12,
		Keywords:    []string{"synthesis", "combine", "resolve", "recommendation"},
		Capabilities: []Capability{
			{Action: "synthesize_findings", Domain: "research", Strength: 0.9},
			{Action: "resolve_conflicts", Domain: "research", Strength: 0.85},
			{Action: "generate_recommendation", Domain: "research", Strength: 0.85},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "thinktank:peer_review",
		Category:    "thinktank",
		Name:        "Peer Review",
		Description: "Peer review with fact-checking",
		NodeCount:   10,
		Keywords:    []string{"review", "fact-check", "verify", "audit"},
		Capabilities: []Capability{
			{Action: "review_research", Domain: "research", Strength: 0.9},
			{Action: "fact_check", Domain: "research", Strength: 0.95},
			{Action: "verify_sources", Domain: "research", Strength: 0.85},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "thinktank:report",
		Category:    "thinktank",
		Name:        "Report Generation",
		Description: "Final report with scenarios",
		NodeCount:   10,
		Keywords:    []string{"report", "scenario", "recommendation", "executive"},
		Capabilities: []Capability{
			{Action: "generate_report", Domain: "research", Strength: 0.9},
			{Action: "build_scenarios", Domain: "research", Strength: 0.85},
			{Action: "write_executive_summary", Domain: "research", Strength: 0.85},
		},
	})

	// ── EVOLUTION TREES ─────────────────────────────────────────────────────

	kg.Register(&TreeMeta{
		ID:          "hermes_evolve",
		Category:    "evolution",
		Name:        "Hermes Self-Evolution",
		Description: "Meta-cognitive self-improvement for Hermes Agent",
		NodeCount:   25,
		Keywords:    []string{"evolve", "self-improve", "meta", "reflect", "optimize"},
		Capabilities: []Capability{
			{Action: "self_improve", Domain: "meta", Strength: 0.95},
			{Action: "reflect_on_performance", Domain: "meta", Strength: 0.9},
			{Action: "optimize_behavior", Domain: "meta", Strength: 0.85},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "stockfish_evolve",
		Category:    "evolution",
		Name:        "Stockfish Evolution",
		Description: "Stockfish-adapted BT evolution with TT, killer moves, alpha-beta",
		NodeCount:   30,
		Keywords:    []string{"stockfish", "evolution", "optimize", "search", "prune"},
		Capabilities: []Capability{
			{Action: "optimize_search", Domain: "meta", Strength: 0.9},
			{Action: "self_improve", Domain: "meta", Strength: 0.85},
			{Action: "prune_tree", Domain: "meta", Strength: 0.8},
		},
	})

	kg.Register(&TreeMeta{
		ID:          "stockfish_loop",
		Category:    "evolution",
		Name:        "Stockfish Infinite Loop",
		Description: "Infinite Stockfish evolution loop",
		NodeCount:   25,
		Keywords:    []string{"loop", "infinite", "continuous", "forever"},
		Capabilities: []Capability{
			{Action: "optimize_search", Domain: "meta", Strength: 0.9},
			{Action: "self_improve", Domain: "meta", Strength: 0.85},
			{Action: "continuous_evolution", Domain: "meta", Strength: 0.9},
		},
	})

	// ── EDGES / RELATIONSHIPS ───────────────────────────────────────────────

	// Finance → finance relationships
	kg.Connect("finance:pitch_agent", "finance:model_builder", "depends_on")
	kg.Connect("finance:pitch_agent", "finance:market_researcher", "depends_on")
	kg.Connect("finance:model_builder", "finance:valuation_reviewer", "composes")
	kg.Connect("finance:earnings_reviewer", "finance:model_builder", "depends_on")
	kg.Connect("finance:month_end_closer", "finance:gl_reconciler", "depends_on")
	kg.Connect("finance:statement_auditor", "finance:gl_reconciler", "depends_on")

	// Research → research relationships
	kg.Connect("research:deep_research", "research:quick_research", "extends")

	// Domain → domain relationships
	kg.Connect("domain:refactoring", "domain:code_review", "extends")
	kg.Connect("domain:security_audit", "domain:code_review", "extends")
	kg.Connect("domain:crash_investigator", "domain:agent_monitor", "extends")
	kg.Connect("domain:devops_ci", "domain:agent_monitor", "depends_on")

	// Startup team relationships
	kg.Connect("startup:ceo", "startup:cto", "depends_on")
	kg.Connect("startup:cto", "startup:engineer", "depends_on")
	kg.Connect("startup:ceo", "startup:pm", "depends_on")
	kg.Connect("startup:pm", "startup:marketing", "depends_on")
	kg.Connect("startup:pm", "startup:sales", "depends_on")

	// Thinktank pipeline: research → debate → synthesis → peer_review → report
	kg.Connect("thinktank:research", "thinktank:debate", "composes")
	kg.Connect("thinktank:debate", "thinktank:synthesis", "composes")
	kg.Connect("thinktank:synthesis", "thinktank:peer_review", "composes")
	kg.Connect("thinktank:peer_review", "thinktank:report", "composes")

	// Evolution relationships
	kg.Connect("stockfish_loop", "stockfish_evolve", "extends")
	kg.Connect("hermes_evolve", "stockfish_evolve", "specializes")

	// Cross-category relationships
	kg.Connect("research:deep_research", "thinktank:research", "specializes")
	kg.Connect("domain:trading_signal", "finance:market_researcher", "specializes")
	kg.Connect("godev", "domain:code_review", "specializes")
	kg.Connect("domain:game_ai", "default", "extends")
	kg.Connect("domain:notebooklm_plan_implement", "domain:goap_planning", "specializes")
	kg.Connect("domain:notebooklm_plan_implement", "domain:devops_ci", "specializes")

	return kg
}

// GlobalGraph is the pre-built knowledge graph containing all 40 behavior trees.
var GlobalGraph = BuildKnowledgeGraph()
