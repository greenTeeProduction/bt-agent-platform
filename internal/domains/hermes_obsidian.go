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
		Type: "Sequence", Name: "HermesObsidian_Main",
		Children: []evolution.SerializableNode{
			{Type: "Sequence", Name: "PreGate", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "ValidateInput"},
				{Type: "Action", Name: "SetupDefaultTools"},
			}},

			{Type: "Selector", Name: "PipelineRouter", Children: []evolution.SerializableNode{

				// Path 1: Session start — load context
				{
					Type: "Sequence", Name: "SessionStartPath",
					Children: []evolution.SerializableNode{
						{Type: "Condition", Name: "IsSessionStart"},
						{
							Type:     "ChainAction",
							Name:     "llm_call:Initialize session from Obsidian vault: 1) Read AGENTS.md for system rules. 2) Read today's memory/YYYY-MM-DD.md. 3) Read SESSION_HANDOFF.md for previous context. 4) Check HEARTBEAT.md for pending tasks. 5) Scan vault index for active projects. 6) Report: active projects, pending tasks, recent decisions. Always use concrete paths under /mnt/ssd/clawd/.",
							Metadata: map[string]any{"max_tokens": float64(8)},
						},
					},
				},

				// Path 2: Ingest source material
				{
					Type: "Sequence", Name: "IngestPath",
					Children: []evolution.SerializableNode{
						{Type: "Condition", Name: "HasNewContent"},
						{
							Type:     "ChainAction",
							Name:     "llm_call:Ingest new content with quality flags: 1) Save immutable copy to raw/ with appropriate subfolder. 2) Flag input quality (good transcript, poor transcript, AI-generated). 3) Convert to plain text if needed. 4) Extract key information using structured template. 5) Synthesize to wiki/ with source attributions. 6) Cross-link to people, project, and meeting notes. 7) NEVER modify raw/ files. Every wiki update must include direct quotes from source.",
							Metadata: map[string]any{"max_tokens": float64(10)},
						},
					},
				},

				// Path 3: Sweep — update derivative notes
				{
					Type: "Sequence", Name: "SweepPath",
					Children: []evolution.SerializableNode{
						{Type: "Condition", Name: "NeedsSweep", Description: "Task mentions sweep, update, refresh, or maintain"},
						{
							Type:     "ChainAction",
							Name:     "llm_call:Run vault sweep: 1) Scan raw/ for new source material. 2) Update corresponding people notes with new information and direct quotes. 3) Update project notes with progress and new context. 4) Verify all cross-links are valid. 5) Flag stale notes (>30 days without update). 6) Report: notes updated, links fixed, stale items found. Trust source material over assumptions.",
							Metadata: map[string]any{"max_tokens": float64(10)},
						},
					},
				},

				// Path 4: Knowledge audit
				{
					Type: "Sequence", Name: "AuditPath",
					Children: []evolution.SerializableNode{
						{Type: "Condition", Name: "NeedsAudit", Description: "Task mentions audit, review, check, verify, or assess"},
						{
							Type:     "ChainAction",
							Name:     "llm_call:Conduct knowledge audit: 1) Identify knowledge gaps — what should be documented but isn't? 2) Check for broken wikilinks. 3) Verify source attributions exist. 4) Assess vault coverage by topic area. 5) Check for orphan pages (no incoming links). 6) Rate vault health: completeness, accuracy, freshness. 7) Report: gaps found, links broken, overall health score.",
							Metadata: map[string]any{"max_tokens": float64(10)},
						},
					},
				},

				// Path 5: Publish output
				{
					Type: "Sequence", Name: "PublishPath",
					Children: []evolution.SerializableNode{
						{Type: "Condition", Name: "NeedsPublish", Description: "Task mentions publish, export, generate, report, or slide"},
						{
							Type:     "ChainAction",
							Name:     "llm_call:Generate output from wiki knowledge: 1) Identify the target audience and format. 2) Extract relevant wiki notes with source attributions. 3) Generate the deliverable: report as markdown, presentation as slides, briefing as documents. 4) Save to output/ with appropriate naming. 5) Maintain source traceability: every claim links back to a wiki note. 6) Human-in-loop: mark confidence levels, flag items needing review.",
							Metadata: map[string]any{"max_tokens": float64(10)},
						},
					},
				},

				// Path 6: Improve skills (default)
				{
					Type: "Sequence", Name: "ImproveSkillPath",
					Children: []evolution.SerializableNode{
						{
							Type:     "ChainAction",
							Name:     "llm_call:Improve agent skills based on edge cases: 1) Review recent session logs for repeated errors or manual corrections. 2) Identify skills that could be updated to handle the edge case. 3) Update the skill file with the new pattern. 4) Document the edge case in knowledge/lessons/. 5) Update _index.md. 6) Report: skills updated, lessons learned. Every edge case that requires correction today should be impossible tomorrow.",
							Metadata: map[string]any{"max_tokens": float64(10)},
						},
					},
				},
			}},

			// Quality: source attribution check
			{
				Type:     "ChainAction",
				Name:     "llm_call:Verify quality gates: 1) All wiki changes have direct source quotes. 2) No raw/ files were modified. 3) Cross-links are valid. 4) Human-in-loop items are flagged. 5) The vault is cleaner than when we started.",
				Metadata: map[string]any{"max_tokens": float64(5)},
			},

			// AQAL audit
			{
				Type:     "ChainAction",
				Name:     "llm_call:Run AQAL check: I (subjective) — are notes clear and useful? It (objective) — are formatting, links, metadata correct? We (cultural) — will future sessions understand this? Its (systemic) — does it maintain vault conventions? Rate each 1-5.",
				Metadata: map[string]any{"max_tokens": float64(5)},
			},

			{Type: "Action", Name: "ReflectOnOutcome"},
			{Type: "Selector", Name: "OutcomeSelector", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "WasSuccessful"},
				{Type: "ChainAction", Name: "llm_call:Vault operation failed. Diagnose: file permissions, vault path, missing source material. Retry with corrected approach.", Metadata: map[string]any{"max_tokens": float64(4)}},
			}},
		},
	}
}
