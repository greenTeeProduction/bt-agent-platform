package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// HermesObsidianOptimizerTree automates the full Hermes+Obsidian optimization pipeline
// based on NotebookLM research (24 citations from 5 sources).
//
// Best practices encoded:
//  1. Session continuity — AGENTS.md, daily memory, SESSION_HANDOFF.md
//  2. Knowledge capture — ingest raw, synthesize wiki, cross-link
//  3. Automated maintenance — sweeps, index updates, sanitization
//  4. Quality gates — source attribution, knowledge audits, human-in-loop
//  5. Continuous improvement — skill updates, edge case fixes, compounding
//
// Strategy paths:
//
//	SessionStart — load context and plan
//	IngestSource — raw → wiki pipeline with quality flags
//	Sweep — update people/project notes from raw sources
//	Audit — knowledge gaps, stale content, broken links
//	Publish — wiki → output (reports, slides, briefings)
//	ImproveSkill — update skills based on edge cases
func HermesObsidianOptimizerTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "HermesObsidian_Main", Description: "Run the Hermes+Obsidian vault pipeline end to end with quality gates",
		Children: []evolution.SerializableNode{
			{Type: "Sequence", Name: "PreGate", Description: "Input validation gate that must pass before any vault work runs", Children: []evolution.SerializableNode{
				cond("ValidateInput", "Task is non-empty and the vault path is available before any pipeline runs"),
				{Type: "Action", Name: "SetupDefaultTools", Description: "Populate the default toolset for vault operations"},
			}},

			{Type: "Selector", Name: "PipelineRouter", Description: "Route to the vault pipeline stage that matches the task", Children: []evolution.SerializableNode{

				// Path 1: Session start — load context
				{
					Type: "Sequence", Name: "SessionStartPath", Description: "Load session context from the vault",
					Children: []evolution.SerializableNode{
						cond("IsSessionStart", "Task mentions session start, resume, load context, or handoff"),
						{
							Type:        "ChainAction",
							Name:        "llm_call:Initialize session from Obsidian vault: 1) Read AGENTS.md for system rules. 2) Read today's memory/YYYY-MM-DD.md. 3) Read SESSION_HANDOFF.md for previous context. 4) Check HEARTBEAT.md for pending tasks. 5) Scan vault index for active projects. 6) Report: active projects, pending tasks, recent decisions. Always use concrete paths under /mnt/ssd/clawd/.",
							Description: "LLM step that initializes the session from vault context files",
							Metadata:    map[string]any{"max_tokens": float64(384)},
						},
					},
				},

				// Path 2: Ingest source material
				{
					Type: "Sequence", Name: "IngestPath", Description: "Ingest new source material into raw/ and wiki/",
					Children: []evolution.SerializableNode{
						cond("HasNewContent", "Task mentions ingest, capture, new source, transcript, or content to file"),
						{
							Type:        "ChainAction",
							Name:        "llm_call:Ingest new content with quality flags: 1) Save immutable copy to raw/ with appropriate subfolder. 2) Flag input quality (good transcript, poor transcript, AI-generated). 3) Convert to plain text if needed. 4) Extract key information using structured template. 5) Synthesize to wiki/ with source attributions. 6) Cross-link to people, project, and meeting notes. 7) NEVER modify raw/ files. Every wiki update must include direct quotes from source.",
							Description: "LLM step that captures the source and synthesizes wiki notes",
							Metadata:    map[string]any{"max_tokens": float64(640)},
						},
					},
				},

				// Path 3: Sweep — update derivative notes
				{
					Type: "Sequence", Name: "SweepPath", Description: "Update derivative notes from raw sources",
					Children: []evolution.SerializableNode{
						cond("NeedsSweep", "Task mentions sweep, update, refresh, or maintain"),
						{
							Type:        "ChainAction",
							Name:        "llm_call:Run vault sweep: 1) Scan raw/ for new source material. 2) Update corresponding people notes with new information and direct quotes. 3) Update project notes with progress and new context. 4) Verify all cross-links are valid. 5) Flag stale notes (>30 days without update). 6) Report: notes updated, links fixed, stale items found. Trust source material over assumptions.",
							Description: "LLM step that sweeps the vault and refreshes people and project notes",
							Metadata:    map[string]any{"max_tokens": float64(512)},
						},
					},
				},

				// Path 4: Knowledge audit
				{
					Type: "Sequence", Name: "AuditPath", Description: "Audit vault knowledge health",
					Children: []evolution.SerializableNode{
						cond("NeedsAudit", "Task mentions audit, review, check, verify, or assess"),
						{
							Type:        "ChainAction",
							Name:        "llm_call:Conduct knowledge audit: 1) Identify knowledge gaps — what should be documented but isn't? 2) Check for broken wikilinks. 3) Verify source attributions exist. 4) Assess vault coverage by topic area. 5) Check for orphan pages (no incoming links). 6) Rate vault health: completeness, accuracy, freshness. 7) Report: gaps found, links broken, overall health score.",
							Description: "LLM step that finds gaps, broken links, and stale content",
							Metadata:    map[string]any{"max_tokens": float64(512)},
						},
					},
				},

				// Path 5: Publish output
				{
					Type: "Sequence", Name: "PublishPath", Description: "Generate deliverables from wiki knowledge",
					Children: []evolution.SerializableNode{
						cond("NeedsPublish", "Task mentions publish, export, generate, report, or slide"),
						{
							Type:        "ChainAction",
							Name:        "llm_call:Generate output from wiki knowledge: 1) Identify the target audience and format. 2) Extract relevant wiki notes with source attributions. 3) Generate the deliverable: report as markdown, presentation as slides, briefing as documents. 4) Save to output/ with appropriate naming. 5) Maintain source traceability: every claim links back to a wiki note. 6) Human-in-loop: mark confidence levels, flag items needing review.",
							Description: "LLM step that produces the requested output with source traceability",
							Metadata:    map[string]any{"max_tokens": float64(768)},
						},
					},
				},

				// Path 6: Improve skills (default)
				{
					Type: "Sequence", Name: "ImproveSkillPath", Description: "Default path that hardens skills against recent edge cases",
					Children: []evolution.SerializableNode{
						{
							Type:        "ChainAction",
							Name:        "llm_call:Improve agent skills based on edge cases: 1) Review recent session logs for repeated errors or manual corrections. 2) Identify skills that could be updated to handle the edge case. 3) Update the skill file with the new pattern. 4) Document the edge case in knowledge/lessons/. 5) Update _index.md. 6) Report: skills updated, lessons learned. Every edge case that requires correction today should be impossible tomorrow.",
							Description: "LLM step that updates skills and documents lessons learned",
							Metadata:    map[string]any{"max_tokens": float64(512)},
						},
					},
				},
			}},

			// Quality: source attribution check
			{
				Type:        "ChainAction",
				Name:        "llm_call:Verify quality gates: 1) All wiki changes have direct source quotes. 2) No raw/ files were modified. 3) Cross-links are valid. 4) Human-in-loop items are flagged. 5) The vault is cleaner than when we started.",
				Description: "LLM step that verifies the vault quality gates",
				Metadata:    map[string]any{"max_tokens": float64(256)},
			},

			// AQAL audit
			{
				Type:        "ChainAction",
				Name:        "llm_call:Run AQAL check: I (subjective) — are notes clear and useful? It (objective) — are formatting, links, metadata correct? We (cultural) — will future sessions understand this? Its (systemic) — does it maintain vault conventions? Rate each 1-5.",
				Description: "LLM step that rates the session on the four AQAL quadrants",
				Metadata:    map[string]any{"max_tokens": float64(256)},
			},

			{Type: "Action", Name: "ReflectOnOutcome", Description: "Record what worked and what to improve for the next run"},
			{Type: "Selector", Name: "OutcomeSelector", Description: "Confirm success or fall through to LLM failure diagnosis", Children: []evolution.SerializableNode{
				cond("WasSuccessful", "Vault operation completed without errors before reflecting on the outcome"),
				{Type: "ChainAction", Name: "llm_call:Vault operation failed. Diagnose: file permissions, vault path, missing source material. Retry with corrected approach.", Description: "Diagnose why the vault operation failed and retry", Metadata: map[string]any{"max_tokens": float64(128)}},
			}},
		},
	}
}
