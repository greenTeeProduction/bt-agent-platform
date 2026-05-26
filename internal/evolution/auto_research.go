package evolution

// AutoResearchTree is a continuous automated research pipeline for improving
// the behavior tree platform and Hermes Agent harness.
//
// 5-phase pipeline:
//   1. DISCOVER — NotebookLM deep research (respects free tier: 10/month)
//   2. ANALYZE — query findings, extract actionable improvements
//   3. IMPLEMENT — create new evolution algorithms as behavior trees
//   4. VALIDATE — benchmark against existing trees
//   5. INTEGRATE — register new trees, update skills, commit to git
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
								Type: "ChainAction",
								Name: "agent:Run Sunday deep research pipeline: 1) 1x Deep Research on weekly topic from /mnt/ssd/clawd/wiki/bt-research/backlog.md. 2) Poll research_status until complete, import sources via research_import. 3) Generate 1 Audio Overview podcast (use 1/3 daily = 33%). 4) Download audio + report artifacts. 5) Generate 1 comprehensive report from findings. 6) Save to /mnt/ssd/clawd/wiki/bt-research/weekly/YYYY-MM-DD.md. Rate limits: Deep Research 4-5/10 monthly (40-50%), Audio 1/3 daily (33%), staying well within free tier.",
								Metadata: map[string]any{"max_tokens": float64(15)},
							},
						},
					},

					// Daily chat queries + report + mind map (Mon-Sat)
					{
						Type: "Sequence", Name: "DailyQueryPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsDailyResearch", Description: "Daily — uses 5 chat queries + 1 report + 1 mind map"},
							{
								Type: "ChainAction",
								Name: "agent:Run daily research pipeline at FULL free tier capacity: 1) 5x chat queries on rotating topics (genetic, decision-trees, Q-learning, ensemble, pruning, expert-systems, memetic, differential, recursive, CMA-ES, reinforcement, multi-objective). Use 5/50 daily = 10%. 2) 1x Report from BT research notebook (use 1/10 daily = 10%). 3) 1x Mind Map from research findings (use 1/10 daily = 10%). 4) Web search for additional findings (unlimited). 5) Save ALL to /mnt/ssd/clawd/wiki/bt-research/daily/YYYY-MM-DD.md. Each query result captured separately.",
								Metadata: map[string]any{"max_tokens": float64(15)},
							},
						},
					},

					// Monthly consolidation (1st of month)
					{
						Type: "Sequence", Name: "MonthlyPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsMonthlyDay", Description: "1st of month — cross-notebook synthesis"},
							{
								Type: "ChainAction",
								Name: "agent:Run monthly research consolidation: 1) cross_notebook_query across all 3 research notebooks (BT algorithms, Hermes+Obsidian, main BT notebook). 2) Generate comprehensive monthly report. 3) Update research backlog priorities in /mnt/ssd/clawd/wiki/bt-research/backlog.md. 4) Archive old daily findings. 5) Update vault index. 6) Report: monthly findings count, implementations created, top insights.",
								Metadata: map[string]any{"max_tokens": float64(12)},
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
						Type: "ChainAction",
						Name: "agent:Analyze this week's research findings. Read /mnt/ssd/clawd/wiki/bt-research/ for all recent notes. Extract: 1) New algorithms to implement. 2) Existing algorithms to improve. 3) Integration patterns. 4) Performance optimizations. For each finding, assess: feasibility (can we implement this in Go?), impact (how much improvement?), effort (hours to implement). Prioritize by impact/effort ratio. Save prioritized backlog to /mnt/ssd/clawd/wiki/bt-research/backlog.md.",
						Metadata: map[string]any{"max_tokens": float64(10)},
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
								Type: "ChainAction",
								Name: "agent:Implement the highest-priority algorithm from the research backlog. 1) Read the research note for specifications. 2) Create new Go file in internal/evolution/ following existing patterns. 3) Implement the core algorithm: NewAlgorithmStruct, main loop, fitness function. 4) Add tests. 5) Add engine conditions if needed. 6) Register in bt-agent resolveTree. 7) Git commit with research attribution. 8) Mark as 'implemented' in backlog.md.",
								Metadata: map[string]any{"max_tokens": float64(15)},
							},
						},
					},

					// Improve existing algorithm
					{
						Type: "Sequence", Name: "ImproveAlgorithmPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "HasImprovement", Description: "Research suggests improvement to existing algorithm"},
							{
								Type: "ChainAction",
								Name: "agent:Improve an existing algorithm based on research. 1) Read the research suggestion. 2) Find the existing implementation in internal/evolution/. 3) Apply the improvement (e.g., better mutation strategy, improved fitness, adaptive parameters). 4) Run existing tests — ensure they still pass. 5) Add test for the improvement. 6) Git commit with research citation. 7) Update the research note with results.",
								Metadata: map[string]any{"max_tokens": float64(12)},
							},
						},
					},

					// Create integration pattern
					{
						Type: "Sequence", Name: "IntegrationPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "NeedsIntegration", Description: "Research suggests new integration pattern"},
							{
								Type: "ChainAction",
								Name: "agent:Create integration pattern from research. 1) Read the research suggestion for how algorithms should interact. 2) Update the gardener pipeline order in internal/gardener/gardener.go. 3) Add cross-algorithm fitness scoring. 4) Update the audit trail if needed. 5) Git commit. 6) Document the integration in wiki/.",
								Metadata: map[string]any{"max_tokens": float64(10)},
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
						Type: "ChainAction",
						Name: "agent:Validate the implementation: 1) Run ALL evolution tests. 2) Run decision tree optimizer on a test tree. 3) If new algorithm: compare fitness before/after. 4) Check expert knowledge anti-patterns. 5) Git diff review — verify only intended files changed. 6) Report: tests passed, fitness delta, anti-pattern status. If any validation fails, do NOT commit — rollback and report the failure.",
						Metadata: map[string]any{"max_tokens": float64(10)},
					},
				},
			},

			// Phase 5: Integrate
			{
				Type: "Sequence", Name: "IntegratePhase",
				Children: []SerializableNode{
					{
						Type: "ChainAction",
						Name: "agent:Complete the integration: 1) Rebuild bt-agent binary. 2) Update knowledge graph with new tree (if created). 3) Create or update Hermes skill. 4) Update bt-first skill tree listing. 5) Update vault wiki/bt-research/status.md with completion. 6) Update _index.md. 7) Final git commit with full research attribution and fitness delta.",
						Metadata: map[string]any{"max_tokens": float64(10)},
					},
				},
			},

			// Report
			{
				Type: "ChainAction",
				Name: "agent:Generate research cycle report: 1) Research topic and week. 2) Findings count. 3) Implementations created/improved. 4) Fitness deltas. 5) New trees registered. 6) Skills created/updated. 7) Time spent. 8) Next week's topic preview. Save to /mnt/ssd/clawd/wiki/bt-research/reports/YYYY-MM-DD-cycle.md.",
				Metadata: map[string]any{"max_tokens": float64(8)},
			},

			// Quality
			{Type: "Action", Name: "ReflectOnOutcome"},
			{Type: "Selector", Name: "OutcomeSelector", Children: []SerializableNode{
				{Type: "Condition", Name: "WasSuccessful"},
				{Type: "ChainAction", Name: "agent:Research cycle partially failed. Diagnose: NotebookLM quota exhausted? Git conflict? Test failure? Rollback any partial changes. Log the failure in /mnt/ssd/clawd/wiki/bt-research/errors.md. Retry next cycle.", Metadata: map[string]any{"max_tokens": float64(6)}},
			}},
		},
	}
}
