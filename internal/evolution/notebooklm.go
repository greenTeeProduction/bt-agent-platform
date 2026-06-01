package evolution

// NotebookLMTree is a behavior tree for NotebookLM operations.
// It automates: notebook management, source ingestion, AI queries,
// studio content creation, and vault → NotebookLM → vault pipelines.
//
// Uses agent: ReAct chains with real shell_exec for nlm CLI commands,
// web_search + web_extract for finding sources, and file_read/file_write
// for vault I/O. No llm_call: nodes — everything is real execution.
//
// Requires: nlm CLI installed and authenticated ('nlm login')
//
// Strategy paths:
//  1. IngestVault — push Obsidian vault notes to NotebookLM as sources
//  2. QueryNotebook — ask AI questions grounded in notebook sources
//  3. CreateArtifact — generate studio content (podcast, briefing, FAQ)
//  4. ResearchDeep — web/Drive research with source import
//  5. SyncBack — export NotebookLM insights back to Obsidian vault
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
					{Type: "Action", Name: "SetupUniversalTools"},
				},
			},

			// Auth check — real shell_exec, not simulated
			{
				Type: "Sequence", Name: "AuthCheck",
				Children: []SerializableNode{
					{
						Type: "ChainAction",
						Name: "agent:Check NotebookLM authentication by running: nlm login --check . If it succeeds, say 'AUTH OK'. If it fails, say 'AUTH FAILED' and tell the user to run 'nlm login --manual -f nlm-cookies.json'.",
						Metadata: map[string]any{
							"max_tokens": float64(10),
							"system_msg": "You have shell_exec access. Run 'nlm login --check' to verify NotebookLM auth. Then run 'nlm notebook list' to list available notebooks. Use JSON output format. If auth fails, tell the user exactly what command to run.",
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
								Name: "agent:Task: {{.Task}}\n\nSteps:\n1. Use shell_exec to run 'nlm notebook list' to find the target notebook (default: BT Platform Research, id 463ca402-e972-470b-889c-b735e37c6746)\n2. Use file_read to read relevant vault notes from /mnt/ssd/clawd/wiki/bt-research/\n3. Use shell_exec to add sources: 'nlm source add text --notebook <id> --content \"...\"' or 'nlm source add url --notebook <id> --url \"...\"'\n4. Use shell_exec to run 'nlm label auto --notebook <id>' for organization\n5. Use shell_exec to run 'nlm notebook describe --id <id>' for AI summary\n6. Report: notebook ID, sources added, topics suggested. Use real output — do not fabricate.",
								Metadata: map[string]any{
									"max_tokens": float64(20),
									"system_msg": "You are a NotebookLM ingestion agent with shell_exec, file_read, file_write, web_search tools. Use shell_exec to run nlm CLI commands (nlm notebook list, nlm source add, nlm label). Use web_search to find relevant URLs. NEVER simulate or invent output — run the actual commands and report their real output verbatim.",
								},
							},
						},
					},

					// Path 2: Query notebook content
					{
						Type: "Sequence", Name: "QueryPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsQueryTask", Description: "Task mentions ask, query, question, research, analyze, what, how, find out about notebooks"},
							{
								Type: "ChainAction",
								Name: "agent:Task: {{.Task}}\n\nSteps:\n1. Use shell_exec to list notebooks: 'nlm notebook list'\n2. For the target notebook (default: 463ca402-e972-470b-889c-b735e37c6746), run: nlm notebook query <notebook_id> \"<your question>\"\n3. The query returns citation-backed answers from the notebook's sources\n4. Use file_write to save the answer to /mnt/ssd/clawd/wiki/bt-research/syntheses/nlm-query-<date>.md\n5. Report the answer with key citations. Include ALL data verbatim from the nlm query output — do not summarize or omit citations.",
								Metadata: map[string]any{
									"max_tokens": float64(20),
									"system_msg": "You are a NotebookLM query agent with shell_exec, file_read, file_write tools. Run 'nlm notebook query <id> \"<question>\"' via shell_exec to get real, citation-backed answers from notebook sources. NEVER simulate or fabricate answers — run the command and report its real JSON output. Save results to /mnt/ssd/clawd/wiki/bt-research/syntheses/.",
								},
							},
						},
					},

					// Path 3: Create studio content (podcasts, briefings)
					{
						Type: "Sequence", Name: "StudioPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsStudioTask", Description: "Task mentions podcast, briefing, FAQ, audio, timeline, create content, studio"},
							{
								Type: "ChainAction",
								Name: "agent:Task: {{.Task}}\n\nSteps:\n1. Identify the notebook (default: 463ca402-e972-470b-889c-b735e37c6746)\n2. Use shell_exec: (a) nlm studio create --notebook <id> --type audio (or briefing_doc, faq, timeline, study_guide) (b) nlm studio status --notebook <id> to poll (c) nlm download artifact --notebook <id> --type <type> --output /mnt/ssd/nlm-output/\n3. Report: artifact type, local path, key highlights. Use real command output.",
								Metadata: map[string]any{
									"max_tokens": float64(20),
									"system_msg": "You are a NotebookLM studio agent. Use shell_exec to run nlm CLI for studio content creation. Poll with 'nlm studio status' until complete. Download artifacts to /mnt/ssd/nlm-output/. NEVER simulate — run the actual commands.",
								},
							},
						},
					},

					// Path 4: Deep research + query
					{
						Type: "Sequence", Name: "ResearchPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsResearchTask", Description: "Task mentions research, web search, discover, find sources, deep research"},
							{
								Type: "ChainAction",
								Name: "agent:Task: {{.Task}}\n\nResearch workflow:\n1. Use web_search to find 3-5 relevant URLs/papers for the topic\n2. Use web_extract on the most promising URLs to get full content\n3. For each source, add it to the notebook: nlm source add url --notebook 463ca402-e972-470b-889c-b735e37c6746 --url \"<url>\"\n4. Run the research query: nlm notebook query 463ca402-e972-470b-889c-b735e37c6746 \"<your synthesis question>\"\n5. Save results: file_write to /mnt/ssd/clawd/wiki/bt-research/syntheses/nlm-research-<date>.md\n6. Report: sources found, query results with citations, file saved. Use real output from each step.",
								Metadata: map[string]any{
									"max_tokens": float64(25),
									"system_msg": "You are a NotebookLM research agent with shell_exec, web_search, web_extract, file_read, file_write tools. Find real sources via web_search, extract their content via web_extract, add them to the notebook via nlm CLI, then query the notebook for citations. NEVER fabricate — run every command and report real output. Save syntheses to /mnt/ssd/clawd/wiki/bt-research/syntheses/.",
								},
							},
						},
					},

					// Path 5: Sync back to vault (default fallback)
					{
						Type: "Sequence", Name: "SyncBackPath",
						Children: []SerializableNode{
							{
								Type: "ChainAction",
								Name: "agent:Task: {{.Task}}\n\nSync workflow:\n1. Run 'nlm notebook list' to see all notebooks\n2. For each: run 'nlm notebook describe --id <id>' for AI summary\n3. Combine insights and save to /mnt/ssd/clawd/wiki/bt-research/nlm-sync-<date>.md using file_write\n4. Report: notebooks synced, insights extracted, file saved.",
								Metadata: map[string]any{
									"max_tokens": float64(20),
									"system_msg": "You are a vault-NotebookLM sync agent. Use shell_exec for nlm commands (nlm notebook list, nlm notebook describe). Use file_write for saving. NEVER simulate — every answer must come from real command output.",
								},
							},
						},
					},
				},
			},

			// Quality verification
			{
				Type: "ChainAction",
				Name: "agent:Verify the previous NotebookLM operation: check that any nlm commands produced real output (JSON with citations), that file_write succeeded (check file exists via shell_exec 'ls -la <path>'), and that no fabrication occurred. Report: VERIFIED if real output was produced, or FABRICATED if responses look simulated.",
				Metadata: map[string]any{
					"max_tokens": float64(8),
					"system_msg": "Verify the output quality. Check that nlm commands returned real JSON with citations, not simulated text. Use shell_exec to verify saved files exist.",
				},
			},

			{Type: "Action", Name: "ReflectOnOutcome"},

			// Outcome
			{
				Type: "Selector", Name: "OutcomeSelector",
				Children: []SerializableNode{
					{Type: "Action", Name: "MarkSuccessful"},
					{
						Type: "ChainAction",
						Name: "agent:NotebookLM operation failed. Run 'nlm login --check' via shell_exec to verify auth. Check if the notebook exists via 'nlm notebook list'. Diagnose the actual error from previous output and suggest a fix.",
						Metadata: map[string]any{
							"max_tokens": float64(8),
							"system_msg": "Debug NotebookLM failure. Use shell_exec to check auth and notebook access. Report the real error and fix suggestion.",
						},
					},
				},
			},
		},
	}
}
