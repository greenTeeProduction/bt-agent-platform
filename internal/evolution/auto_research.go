package evolution

// AutoResearchTree is a continuous automated research pipeline for improving
// the behavior tree platform and Hermes Agent harness.
//
// 5-phase pipeline:
//
//	FREE TIER FULL UTILIZATION:
//	- Daily: 50/50 chat (100%), 5/10 reports (50%), 3/10 mind maps (30%)
//	- Sunday: 3/10 deep research (30%), 3/3 audio (100%)
//	- Monthly: cross-notebook consolidation (1st of month)
//
// Research topics rotate weekly to cover the full platform.
func AutoResearchTree() *SerializableNode {
	return &SerializableNode{
		Type: "Sequence",
		Name: "AutoResearch_Main",
		Children: []SerializableNode{
			{Type: "Sequence", Name: "PreGate", Children: []SerializableNode{
				{Type: "Condition", Name: "ValidateInput"},
				{Type: "Action", Name: "SetupDefaultTools"},
			}},

			// Phase 1: Discover
			{
				Type: "Selector", Name: "DiscoverRouter", Children: []SerializableNode{

					{
						Type: "Sequence", Name: "DeepResearchPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsDeepResearchDay", Description: "Sunday — uses monthly deep research quota (4-5/10)"},
							{
								Type:     "ChainAction",
								Name:     "llm_call:Run Sunday deep research pipeline at FULL free tier: 1) 3x Deep Research on weekly topics from /mnt/ssd/clawd/wiki/bt-research/backlog.md (use 3/10 monthly = 30%). 2) Poll research_status, import sources via research_import. 3) Generate 3 Audio Overview podcasts (use 3/3 daily = 100% audio). 4) Download audio + report artifacts. 5) 3x comprehensive reports from findings. 6) 1x slide deck. 7) 1x infographic. 8) Save to /mnt/ssd/clawd/wiki/bt-research/weekly/YYYY-MM-DD.md. FULL utilization: 30% monthly research, 100% daily audio, 30% daily reports.",
								Metadata: map[string]any{"max_tokens": float64(400)},
							},
						},
					},

					// Daily chat queries + report + mind map (Mon-Sat)
					{
						Type: "Sequence", Name: "DailyQueryPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsDailyResearch", Description: "Daily — uses FULL 50 chat queries (100%) + 5 reports + 3 mind maps"},
							{
								Type:     "ChainAction",
								Name:     "llm_call:Run daily research pipeline at 100% free tier capacity. Execute ALL of the following exhaustively: 1) 50x notebook_query on BT research notebook across ALL rotating topics (genetic, decision-trees, Q-learning, ensemble, pruning, expert-systems, memetic, differential, recursive, CMA-ES, reinforcement, multi-objective, neural-architecture, hyperparameter, bayesian, monte-carlo, simulated-annealing, tabu-search, ant-colony, particle-swarm). 10 topics x 5 queries each = 50/50 daily chat (100%). Each query gets a unique angle. 2) 5x Reports from BT notebook on top findings (5/10 daily = 50%). 3) 3x Mind Maps (3/10 daily = 30%). 4) 1x Slide Deck from best findings. 5) 1x Infographic. 6) Web search for additional sources (unlimited). 7) Save ALL findings to /mnt/ssd/clawd/wiki/bt-research/daily/YYYY-MM-DD.md with per-query sections. 8) Cross-reference with backlog.md — flag any new implementable items. Use 100% of 50 daily chat limit.",
								Metadata: map[string]any{"max_tokens": float64(400)},
							},
						},
					},

					// Monthly consolidation (1st of month)
					{
						Type: "Sequence", Name: "MonthlyPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsMonthlyDay", Description: "1st of month — cross-notebook synthesis"},
							{
								Type:     "ChainAction",
								Name:     "llm_call:Run monthly research consolidation: 1) cross_notebook_query across all 3 research notebooks (BT algorithms, Hermes+Obsidian, main BT notebook). 2) Generate comprehensive monthly report. 3) Update research backlog priorities in /mnt/ssd/clawd/wiki/bt-research/backlog.md. 4) Archive old daily findings. 5) Update vault index. 6) Report: monthly findings count, implementations created, top insights.",
								Metadata: map[string]any{"max_tokens": float64(400)},
							},
						},
					},
				},
			},

			// Phase 2: Analyze
			{
				Type: "Sequence", Name: "AnalyzePhase",
				Children: []SerializableNode{
					{
						Type:     "ChainAction",
						Name:     "llm_call:Analyze this week's research findings. Read /mnt/ssd/clawd/wiki/bt-research/ for all recent notes. Extract: 1) New algorithms to implement. 2) Existing algorithms to improve. 3) Integration patterns. 4) Performance optimizations. For each finding, assess: feasibility (can we implement this in Go?), impact (how much improvement?), effort (hours to implement). Prioritize by impact/effort ratio. Save prioritized backlog to /mnt/ssd/clawd/wiki/bt-research/backlog.md.",
						Metadata: map[string]any{"max_tokens": float64(400)},
					},
				},
			},

			// Phase 3: Implement
			{
				Type: "Selector", Name: "ImplementRouter", Children: []SerializableNode{

					// Implement new algorithm
					{
						Type: "Sequence", Name: "NewAlgorithmPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "HasNewAlgorithm", Description: "Backlog has unimplemented algorithms"},
							{
								Type:     "ChainAction",
								Name:     "llm_call:Implement the highest-priority algorithm from the research backlog. 1) Read the research note for specifications. 2) Create new Go file in internal/evolution/ following existing patterns. 3) Implement the core algorithm: NewAlgorithmStruct, main loop, fitness function. 4) Add tests. 5) Add engine conditions if needed. 6) Register in bt-agent resolveTree. 7) Git commit with research attribution. 8) Mark as 'implemented' in backlog.md.",
								Metadata: map[string]any{"max_tokens": float64(400)},
							},
						},
					},

					// Improve existing algorithm
					{
						Type: "Sequence", Name: "ImproveAlgorithmPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "HasImprovement", Description: "Research suggests improvement to existing algorithm"},
							{
								Type:     "ChainAction",
								Name:     "llm_call:Improve an existing algorithm based on research. 1) Read the research suggestion. 2) Find the existing implementation in internal/evolution/. 3) Apply the improvement (e.g., better mutation strategy, improved fitness, adaptive parameters). 4) Run existing tests — ensure they still pass. 5) Add test for the improvement. 6) Git commit with research citation. 7) Update the research note with results.",
								Metadata: map[string]any{"max_tokens": float64(400)},
							},
						},
					},

					// Create integration pattern
					{
						Type: "Sequence", Name: "IntegrationPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "NeedsIntegration", Description: "Research suggests new integration pattern"},
							{
								Type:     "ChainAction",
								Name:     "llm_call:Create integration pattern from research. 1) Read the research suggestion for how algorithms should interact. 2) Update the gardener pipeline order in internal/gardener/gardener.go. 3) Add cross-algorithm fitness scoring. 4) Update the audit trail if needed. 5) Git commit. 6) Document the integration in wiki/.",
								Metadata: map[string]any{"max_tokens": float64(400)},
							},
						},
					},
				},
			},

			// Phase 4: Validate
			{
				Type: "Sequence", Name: "ValidatePhase",
				Children: []SerializableNode{
					{
						Type:     "ChainAction",
						Name:     "llm_call:Validate the implementation: 1) Run ALL evolution tests. 2) Run decision tree optimizer on a test tree. 3) If new algorithm: compare fitness before/after. 4) Check expert knowledge anti-patterns. 5) Git diff review — verify only intended files changed. 6) Report: tests passed, fitness delta, anti-pattern status. If any validation fails, do NOT commit — rollback and report the failure.",
						Metadata: map[string]any{"max_tokens": float64(400)},
					},
				},
			},

			// Phase 5: Integrate
			{
				Type: "Sequence", Name: "IntegratePhase",
				Children: []SerializableNode{
					{
						Type:     "ChainAction",
						Name:     "llm_call:Complete the integration: 1) Rebuild bt-agent binary. 2) Update knowledge graph with new tree (if created). 3) Create or update Hermes skill. 4) Update bt-first skill tree listing. 5) Update vault wiki/bt-research/status.md with completion. 6) Update _index.md. 7) Final git commit with full research attribution and fitness delta.",
						Metadata: map[string]any{"max_tokens": float64(400)},
					},
				},
			},

			// Report
			{
				Type:     "ChainAction",
				Name:     "llm_call:Generate research cycle report: 1) Research topic and week. 2) Findings count. 3) Implementations created/improved. 4) Fitness deltas. 5) New trees registered. 6) Skills created/updated. 7) Time spent. 8) Next week's topic preview. Save to /mnt/ssd/clawd/wiki/bt-research/reports/YYYY-MM-DD-cycle.md.",
				Metadata: map[string]any{"max_tokens": float64(400)},
			},

			// Quality
			{Type: "Action", Name: "ReflectOnOutcome"},
			{Type: "Selector", Name: "OutcomeSelector", Children: []SerializableNode{
				{Type: "Condition", Name: "WasSuccessful"},
				{Type: "ChainAction", Name: "llm_call:Research cycle partially failed. Diagnose: NotebookLM quota exhausted? Git conflict? Test failure? Rollback any partial changes. Log the failure in /mnt/ssd/clawd/wiki/bt-research/errors.md. Retry next cycle.", Metadata: map[string]any{"max_tokens": float64(400)}},
			}},
		},
	}
}
