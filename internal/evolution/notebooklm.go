package evolution

// NotebookLMTree is a behavior tree for NotebookLM operations.
// It automates: notebook management, source ingestion, AI queries,
// studio content creation, and vault → NotebookLM → vault pipelines.
//
// Requires: nlm login --manual -f cookies.json (headless auth)
//
// Strategy paths:
//   1. IngestVault — push Obsidian vault notes to NotebookLM as sources
//   2. QueryNotebook — ask AI questions grounded in notebook sources
//   3. CreateArtifact — generate studio content (podcast, briefing, FAQ)
//   4. ResearchDeep — web/Drive research with source import
//   5. SyncBack — export NotebookLM insights back to Obsidian vault
func NotebookLMTree() *SerializableNode {
	return &SerializableNode{
		Type: "Sequence",
		Name: "NotebookLM_Main",
		Children: []SerializableNode{
			// PreGate
			{
				Type: "Sequence", Name: "PreGate",
				Children: []SerializableNode{
					{Type: "Condition", Name: "ValidateInput"},
					{Type: "Action", Name: "SetupDefaultTools"},
				},
			},

			// Auth check
			{
				Type: "Sequence", Name: "AuthCheck",
				Children: []SerializableNode{
					{
						Type: "ChainAction",
						Name: "agent:Check NotebookLM authentication status. If not authenticated, guide the user: 'Use nlm login --manual -f cookies.json on your laptop to export Google cookies, then copy them to the Jetson. Or use nlm login on a machine with a display and sync the config.' If authenticated, proceed.",
						Metadata: map[string]any{
							"max_tokens": float64(5),
							"system_msg": "Check NotebookLM auth status. Be helpful and concise.",
						},
					},
				},
			},

			// StrategyRouter
			{
				Type: "Selector", Name: "NLMRouter",
				Children: []SerializableNode{

					// Path 1: Ingest vault notes into NotebookLM
					{
						Type: "Sequence", Name: "IngestVaultPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsIngestTask", Description: "Task mentions ingest, import, add source, push to notebooklm"},
							{
								Type: "ChainAction",
								Name: "agent:Ingest Obsidian vault content to NotebookLM: 1) Read relevant notes from /mnt/ssd/clawd/ based on the topic. 2) Create a new notebook or find existing one. 3) Add sources: paste note content as text sources, or add URLs. 4) Add descriptive labels for organization. 5) Run notebook_describe to get AI-generated summary. 6) Report: notebook ID, sources added, topics suggested. Use the notebooklm MCP tools: notebook_create, source_add, label, notebook_describe.",
								Metadata: map[string]any{
									"max_tokens": float64(10),
									"system_msg": "You are a NotebookLM ingestion agent. Push vault knowledge to NotebookLM for AI-powered analysis.",
								},
							},
						},
					},

					// Path 2: Query notebook content
					{
						Type: "Sequence", Name: "QueryPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsQueryTask", Description: "Task mentions ask, query, question, research, analyze notebooks"},
							{
								Type: "ChainAction",
								Name: "agent:Query NotebookLM with a question grounded in notebook sources: 1) Identify the target notebook. 2) Use notebook_query to ask the question — NotebookLM answers with citations from sources. 3) Cross-reference answer against vault for consistency. 4) If answer is useful, save to vault as a note. 5) Report: question, answer, key citations, confidence. Use notebook_query MCP tool — it returns grounded, citation-backed answers.",
								Metadata: map[string]any{
									"max_tokens": float64(10),
									"system_msg": "You are a NotebookLM query agent. Ask deep questions and get source-grounded answers.",
								},
							},
						},
					},

					// Path 3: Create studio content (podcasts, briefings)
					{
						Type: "Sequence", Name: "StudioPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsStudioTask", Description: "Task mentions podcast, briefing, FAQ, audio, timeline, create content"},
							{
								Type: "ChainAction",
								Name: "agent:Create NotebookLM studio content: 1) Choose artifact type: audio_overview (2-host podcast), briefing_doc, FAQ, timeline, study_guide. 2) Use studio_create with the notebook ID. 3) Poll status until complete. 4) Download artifact via download_artifact. 5) Save to appropriate location. 6) Report: artifact type, URL, key highlights. Use studio_create, studio_status, download_artifact MCP tools.",
								Metadata: map[string]any{
									"max_tokens": float64(10),
									"system_msg": "You are a NotebookLM studio agent. Generate rich content from notebook sources.",
								},
							},
						},
					},

					// Path 4: Deep research
					{
						Type: "Sequence", Name: "ResearchPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsResearchTask", Description: "Task mentions research, web search, discover, find sources"},
							{
								Type: "ChainAction",
								Name: "agent:Run NotebookLM deep research: 1) Use research_start with a research question. 2) Poll status until complete. 3) Import discovered sources via research_import. 4) Query the new sources for key insights. 5) Create a briefing doc from findings. 6) Save insights to Obsidian vault. Use research_start, research_status, research_import, notebook_query, studio_create MCP tools.",
								Metadata: map[string]any{
									"max_tokens": float64(12),
									"system_msg": "You are a NotebookLM research agent. Discover, import, and synthesize research findings.",
								},
							},
						},
					},

					// Path 5: Sync back to vault (default)
					{
						Type: "Sequence", Name: "SyncBackPath",
						Children: []SerializableNode{
							{
								Type: "ChainAction",
								Name: "agent:Sync NotebookLM insights back to Obsidian vault: 1) List all notebooks and their summaries. 2) For each notebook, extract key insights from notebook_describe. 3) Download any generated artifacts. 4) Save insights to /mnt/ssd/clawd/wiki/ as structured notes with YAML frontmatter. 5) Cross-link to existing vault pages using [[wikilinks]]. 6) Update vault _index.md. 7) Report: notes created, links added.",
								Metadata: map[string]any{
									"max_tokens": float64(10),
									"system_msg": "You are a vault-NotebookLM sync agent. Extract insights from NotebookLM into the Obsidian knowledge base.",
								},
							},
						},
					},
				},
			},

			// Quality gate
			{
				Type: "ChainAction",
				Name: "agent:Verify NotebookLM operation: check that sources were added correctly, queries returned grounded answers, artifacts were downloaded, and vault notes were properly linked. Report any issues.",
				Metadata: map[string]any{"max_tokens": float64(5)},
			},

			{Type: "Action", Name: "ReflectOnOutcome"},

			// Outcome
			{
				Type: "Selector", Name: "OutcomeSelector",
				Children: []SerializableNode{
					{Type: "Condition", Name: "WasSuccessful"},
					{
						Type: "ChainAction",
						Name: "agent:NotebookLM operation failed. Check: is auth valid? Is the notebook accessible? Are the MCP tools connected? Diagnose and retry.",
						Metadata: map[string]any{"max_tokens": float64(5)},
					},
				},
			},
		},
	}
}
