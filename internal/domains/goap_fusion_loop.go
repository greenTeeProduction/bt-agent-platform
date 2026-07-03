package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// GoapFusionLoopWireFn is the production wiring seam for the scheduled
// goap_fusion_loop tree: internal/agentexec installs
// engine.WireGoapFusionLoopTree here so every runtime resolution gets the
// Phase-0 preflight, the gated ClaudeSuperpowersPath, and the
// PublishGoapFusionStateHash producer. It is a hook var because domains
// cannot import engine (domains' in-package tests import engine, so a
// production domains→engine import is a test-build cycle). The identity
// default keeps evolution/gardener tooling operating on the raw tree.
var GoapFusionLoopWireFn = func(tree evolution.SerializableNode) evolution.SerializableNode {
	return tree
}

// GoapFusionLoopTree is a single-cycle GOAP fusion improvement runner that:
//  1. Grills NotebookLM with critical review ("grill me" pattern)
//  2. Runs NotebookLM research for fresh recommendations (falls back to a
//     Claude Code review of recent commits when NotebookLM is unavailable)
//  3. Reads vault research + graphify codebase analysis
//  4. Identifies gaps, prioritizes goals
//  5. Implements improvements via Claude Code (auto-approved)
//  6. Verifies with build/test
//
// The LOOP is driven by the cron scheduler — this tree runs one full cycle
// per tick. Schedule it every 30 minutes: 0,30 * * * *
// Each cycle advances the "grill me" conversation round (1→2→3→1).
func GoapFusionLoopTree() *evolution.SerializableNode {
	wired := GoapFusionLoopWireFn(rawGoapFusionLoopTree())
	return &wired
}

func rawGoapFusionLoopTree() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:      "Sequence",
		Name:      "GoapFusionLoop_Main",
		TimeoutMs: 3600_000, // 1-hour ceiling per cycle
		Children: []evolution.SerializableNode{
			// ── Phase 0: Setup ──
			act("SetupFusionTools",
				"Give loop actions access to vault, graphify, git, and NotebookLM runtime tools"),

			// ── Phase 1: Grill Me — Critical Review (multi-turn) ──
			// Uses conversation_id to chain rounds across cron ticks.
			// Round 1: "What is the BT framework missing? Be critical."
			// Round 2: "Push harder. What exact code should change?"
			// Round 3: "Final demand: implementation plan with tests."
			act("GrillMeNotebookLM",
				"Multi-turn critical review: grill the research notebook on what the BT framework is missing, demand concrete implementation targets with file paths and test commands"),

			// ── Phase 2: Fresh Research ──
			// NotebookLM first; when it is unavailable (daily quota exhausted,
			// auth expired, circuit open) Claude Code reviews the daemon's
			// recent commits instead so the cycle still produces findings.
			sel("ResearchRouter",
				act("RunGoapFusionNotebookLMResearch",
					"Query BT Platform Research notebook directly and save GOAP-owned findings to vault"),
				act("RunClaudeCodeReviewResearch",
					"Fallback when NotebookLM is unavailable: Claude Code reviews recent daemon commits (or graphify hotspots) and emits GOAL/GAP/FILES/TESTS findings to the vault"),
			),

			// ── Phase 3: Read Context ──
			act("ReadVaultResearch",
				"Read all NotebookLM research syntheses and improvement plans from vault"),
			act("ReadGraphifyReport",
				"Read graphify-out/GRAPH_REPORT.md for codebase structure, god nodes, communities"),

			// ── Phase 4: Analyze ──
			act("AnalyzeImprovementGaps",
				"Cross-reference research findings with codebase gaps"),
			act("PrioritizeGoapGoals",
				"Build GOAP goal queue: highest-impact, lowest-risk improvements first, always prefer fresh NotebookLM recommendations"),

			// ── Phase 5: Implement or Analyze ──
			sel("ExecutionRouter",
				seq("ClaudeSuperpowersPath",
					cond("HasNewGaps",
						"Only proceed with implementation if goals differ from previous run"),
					act("WriteSuperpowersImplementationPlan",
						"Write a concrete Superpowers plan with files, tests, risks, and verification commands"),
					evolution.SerializableNode{
						Type:        "HumanApprovalGate",
						Name:        "ApproveGoapFusionApply",
						Description: "GOAP fusion loop automatically implements findings via Superpowers runtime.",
						Metadata: map[string]any{
							"phase":             "pre",
							"side_effect_class": "local_reversible",
							"hitl_prompt":       "GOAP fusion loop will run Superpowers/Claude Code to implement BT platform improvements.",
							"auto_approve":      true,
						},
						Children: []evolution.SerializableNode{
							act("RunSuperpowersClaudeImplementation",
								"Execute plan through production Superpowers runtime and Claude Code"),
							act("VerifyGoapBuild",
								"Run production-safe GOAP/Superpowers build and focused tests"),
							act("ReportSuperpowersImplementation",
								"Report artifacts, finish evidence, and changed files"),
						},
					},
					act("CleanupGraphifyOut",
						"Reset graphify-out to prevent perpetual staged changes"),
				),
				seq("ScheduledAnalysisPath",
					cond("NoNewGaps",
						"Only use deterministic analysis fallback when goals are unchanged"),
					act("WriteFusionAnalysis",
						"Write deterministic fusion analysis to the vault"),
					act("VerifyGoapBuild",
						"Run production-safe build and focused tests"),
					act("RunGraphifyUpdate",
						"Refresh graphify-out after local verification"),
					act("ReportFusionCycle",
						"Report deterministic analysis and verification evidence"),
					act("CleanupGraphifyOut",
						"Reset graphify-out to prevent perpetual staged changes"),
				),
			),

			// ── Phase 6: Verify & Report ──
			act("VerifyGoapFusionEvidence",
				"Reject fabricated/self-corrected outputs and require concrete artifact evidence"),
			act("MarkSuccessful",
				"Mark GOAP fusion cycle complete only after deterministic evidence verification"),
		},
	}
}
