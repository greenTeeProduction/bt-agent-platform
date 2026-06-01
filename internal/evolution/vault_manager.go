package evolution

// VaultManagerTree is a behavior tree for Obsidian vault management.
// It automates the full vault workflow: session start → ingestion → synthesis → session end.
//
// Strategy paths:
//  1. SessionStart — load AGENTS.md, today's memory, SCHEMA.md
//  2. IngestRaw — process new source material into raw/
//  3. SynthesizeWiki — extract knowledge from raw to wiki/
//  4. CrossLink — ensure ≥2 links per wiki page
//  5. UpdateIndex — refresh _index.md
//  6. SessionEnd — write daily summary, update log.md
//  7. WeeklySweep — extract durable knowledge from past week
func VaultManagerTree() *SerializableNode {
	return &SerializableNode{
		Type: "Sequence",
		Name: "VaultManager_Main",
		Children: []SerializableNode{
			// PreGate
			{
				Type: "Sequence", Name: "PreGate",
				Children: []SerializableNode{
					{Type: "Condition", Name: "ValidateInput"},
					{Type: "Action", Name: "SetupDefaultTools"},
				},
			},

			// StrategyRouter: pick workflow based on task
			{
				Type: "Selector", Name: "VaultRouter",
				Children: []SerializableNode{

					// Path 1: Session Start
					{
						Type: "Sequence", Name: "SessionStartPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsSessionStart", Description: "Task mentions session start, boot, or wake"},
							{
								Type: "ChainAction",
								Name: "llm_call:At vault root, read AGENTS.md to understand agent identity. Read today's memory/YYYY-MM-DD.md for recent context. Read SCHEMA.md for vault conventions. Read HEARTBEAT.md if present. Summarize: what's active, what needs attention, any open tasks.",
								Metadata: map[string]any{
									"max_tokens": float64(8),
									"system_msg": "You are a vault session manager starting a new work session.",
								},
							},
						},
					},

					// Path 2: Ingest new material
					{
						Type: "Sequence", Name: "IngestPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "HasNewContent", Description: "Task mentions new content, ingest, import, or source material"},
							{
								Type: "ChainAction",
								Name: "llm_call:Process new source material: 1) Save raw content to raw/ folder (appropriate subfolder based on type: article, transcript, paper). 2) Never edit raw files — append-only. 3) Extract key insights. 4) If insights are durable, proceed to synthesize into wiki/.",
								Metadata: map[string]any{
									"max_tokens": float64(10),
									"system_msg": "You are a vault ingestion agent. Raw is sacred — never modify source files.",
								},
							},
						},
					},

					// Path 3: Synthesize wiki
					{
						Type: "Sequence", Name: "SynthesizePath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "NeedsSynthesis", Description: "Task mentions synthesize, wiki, extract, or create note"},
							{
								Type: "ChainAction",
								Name: "llm_call:Synthesize knowledge into wiki/: 1) Search vault for existing pages on this topic — avoid duplicates. 2) Create new note with YAML frontmatter: tags, confidence, created date. 3) Content: definition, key principles, related concepts. 4) Cross-link to at least 2 existing wiki pages using [[wikilinks]]. 5) Mark confidence level: high (verified), medium (likely), low (speculative).",
								Metadata: map[string]any{
									"max_tokens": float64(12),
									"system_msg": "You are a knowledge synthesizer. Create dense, well-linked atomic notes.",
								},
							},
						},
					},

					// Path 4: Cross-link audit
					{
						Type: "Sequence", Name: "CrossLinkPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "NeedsCrossLinks", Description: "Task mentions link, cross-link, audit, or connect"},
							{
								Type: "ChainAction",
								Name: "llm_call:Audit wiki pages for cross-linking: 1) Find pages with fewer than 2 outgoing [[wikilinks]]. 2) Search for related concepts that should be linked. 3) Add missing links with brief context. 4) Report: pages fixed, orphan count remaining.",
								Metadata: map[string]any{
									"max_tokens": float64(8),
									"system_msg": "You are a link auditor. Every wiki page needs at least 2 connections.",
								},
							},
						},
					},

					// Path 5: Update index
					{
						Type: "Sequence", Name: "UpdateIndexPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "NeedsIndexUpdate", Description: "Task mentions index, update, refresh, or MOC"},
							{
								Type: "ChainAction",
								Name: "llm_call:Update wiki/_index.md: 1) Count total notes by category. 2) Add any new pages to the appropriate section. 3) Update quadrant balance analysis. 4) Update the 'last updated' date. 5) Flag any categories that are underrepresented.",
								Metadata: map[string]any{
									"max_tokens": float64(8),
									"system_msg": "You are an index maintainer. Keep the map of content accurate and useful.",
								},
							},
						},
					},

					// Path 6: Session end
					{
						Type: "Sequence", Name: "SessionEndPath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsSessionEnd", Description: "Task mentions session end, wrap up, close, or daily summary"},
							{
								Type: "ChainAction",
								Name: "llm_call:Close the session: 1) Write daily summary to memory/YYYY-MM-DD.md: decisions made, work completed, open questions, next steps. 2) Extract durable knowledge to appropriate wiki/ folder. 3) Append operational log entry to log.md with timestamp. 4) Update SESSION_HANDOFF.md for next session: current task, decisions, open questions, next steps.",
								Metadata: map[string]any{
									"max_tokens": float64(10),
									"system_msg": "You are a session closer. Leave the vault ready for the next session.",
								},
							},
						},
					},

					// Path 7: Weekly sweep (default)
					{
						Type: "Sequence", Name: "WeeklySweepPath",
						Children: []SerializableNode{
							{
								Type: "ChainAction",
								Name: "llm_call:Run weekly vault sweep: 1) Review memory/ from past 7 days. 2) Extract durable knowledge → appropriate wiki/ folder. 3) Detect patterns: 3+ related lessons → create pattern note. 4) Update templates/ if repeated structures emerged. 5) Prune stale content. 6) Verify _index.md accuracy. 7) Report: files created, patterns detected, index updated.",
								Metadata: map[string]any{
									"max_tokens": float64(12),
									"system_msg": "You are a vault curator. Extract gold from daily logs, keep the structure clean and growing.",
								},
							},
						},
					},
				},
			},

			// Quality gate
			{
				Type: "Sequence", Name: "QualityGate",
				Children: []SerializableNode{
					{
						Type:     "ChainAction",
						Name:     "llm_call:Verify vault integrity: 1) Check no duplicate pages were created. 2) Verify all wiki pages have YAML frontmatter with required fields. 3) Check no raw/ files were modified. 4) Confirm _index.md reflects current state. Report any issues found.",
						Metadata: map[string]any{"max_tokens": float64(6)},
					},
				},
			},

			// AQAL audit
			{
				Type: "ChainAction",
				Name: "llm_call:Run AQAL four-quadrant audit on the vault work just completed. I (subjective): Was the work clear and useful? It (objective): Are formatting, links, metadata correct? We (cultural): Will future sessions understand this? Its (systemic): Does it maintain vault conventions? Rate each quadrant 1-5.",
				Metadata: map[string]any{
					"max_tokens": float64(6),
					"system_msg": "You are an AQAL auditor. Check all four perspectives.",
				},
			},

			// Reflect
			{Type: "Action", Name: "ReflectOnOutcome"},

			// Outcome
			{
				Type: "Selector", Name: "OutcomeSelector",
				Children: []SerializableNode{
					{Type: "Condition", Name: "WasSuccessful"},
					{
						Type:     "ChainAction",
						Name:     "llm_call:Vault operation issue detected. Self-correct: check file permissions, verify vault path, retry with corrected approach.",
						Metadata: map[string]any{"max_tokens": float64(5)},
					},
				},
			},
		},
	}
}
