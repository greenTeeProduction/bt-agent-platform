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
		Type:        "Sequence",
		Name:        "GoapFusionLoop_Main",
		Description: "One GOAP fusion loop cycle: seed backlog, grill and research NotebookLM, prioritize goals, implement via Superpowers or fall back to analysis, verify evidence",
		TimeoutMs:   7200_000, // 2-hour ceiling: goal-driven plans run up to 3 RED→GREEN Claude tasks per cycle
		Children: []evolution.SerializableNode{
			// ── Phase 0: Setup ──
			act("SetupFusionTools",
				"Give loop actions access to vault, graphify, git, and NotebookLM runtime tools"),

			// ── Phase 0.2: Fleet PR shepherd ──
			// One NON-BLOCKING pass over the fleet's landing PR before any
			// new work: adopt upstream merges (ff local master), ship
			// accrued local-master commits (push fleet branch + open PR),
			// fix a red pipeline (one bounded Claude attempt), merge a green
			// one. Never waits on CI — progress happens between cycles. The
			// action returns SUCCESS on every path so a quiet steady state
			// cannot fail the Sequence into the ClaudeErrorHandler wrapper.
			act("ShepherdFleetPR",
				"One non-blocking fleet-PR pass: sync upstream merges, open/refresh the landing PR, fix red CI (bounded), merge green CI"),

			// ── Phase 0.5: Self-seeding backlog ──
			// Runs FIRST (before research/grill can fail) so the loop always
			// has a program to work on. Placed after Phase 4 earlier, a cycle
			// whose research phase hard-failed (Claude review returning prose
			// the actionability filter rejects) aborted the Sequence before
			// ever reaching seeding — the loop starved itself. Seeding here
			// means: idle cycle → seed a program → Phase 4 queues its first
			// milestone → Phase 5 executes it THIS cycle (no wasted cycle).
			// Selector (not AlwaysSucceed-wrapper: that leaf eats children):
			// the seed sequence runs, else falls through to a terminal
			// AlwaysSucceed leaf, so a rejected/failed proposal is non-fatal.
			sel("BacklogReplenish", "Seed the next multi-cycle program when no milestones are pending, else fall through to a non-fatal skip",
				seq("SeedWhenIdle", "Seed the next multi-cycle program from research when no milestones are pending",
					cond("NeedsFreshProgram", "No program has pending milestones"),
					act("SeedNextProgram", "Ask research for the next multi-cycle program (PROGRAM/MILESTONEn), validate file-scoped milestones, persist to the program store"),
				),
				evolution.SerializableNode{Type: "AlwaysSucceed", Name: "SeedSkipped",
					Description: "Non-fatal skip: a rejected or failed seed proposal must not abort the cycle"},
			),

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
			// Non-fatal: when BOTH nlm and Claude review produce nothing
			// usable, the terminal AlwaysSucceed leaf keeps the cycle alive
			// so it can still execute an active program milestone (whose
			// goal does not depend on fresh research). Without it, a barren
			// research phase aborted the whole cycle — the loop could not
			// make progress on an already-queued program when research dried
			// up (2026-07-04 16:xx: "Claude Review Fallback Failed").
			sel("ResearchRouter", "Route research to NotebookLM, then the Claude Code review fallback, then a non-fatal skip so barren research cannot abort the cycle",
				act("RunGoapFusionNotebookLMResearch",
					"Query BT Platform Research notebook directly and save GOAP-owned findings to vault"),
				act("RunClaudeCodeReviewResearch",
					"Fallback when NotebookLM is unavailable: Claude Code reviews recent daemon commits (or graphify hotspots) and emits GOAL/GAP/FILES/TESTS findings to the vault"),
				evolution.SerializableNode{Type: "AlwaysSucceed", Name: "ResearchOptional",
					Description: "Non-fatal skip: barren research must not abort a cycle that can still execute a queued milestone"},
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
			sel("ExecutionRouter", "Route new goals to the Superpowers/Claude implementation path, else the deterministic scheduled analysis fallback",
				seq("ClaudeSuperpowersPath", "Implement new research-backed goals via the Superpowers/Claude runtime, verify, and report",
					cond("HasNewGaps",
						"Only proceed with implementation if goals differ from previous run"),
					act("WriteSuperpowersImplementationPlan",
						"Write a goal-driven Superpowers plan: one complete-change task per file-scoped prioritized goal (up to 3), file scope and test packages derived from the goals themselves"),
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
								"Execute plan through production Superpowers runtime and Claude Code, then sync arc42 documentation into the same commit"),
							act("VerifyGoapBuild",
								"Run production-safe GOAP/Superpowers build and focused tests"),
							act("ReportSuperpowersImplementation",
								"Report artifacts, finish evidence, and changed files"),
						},
					},
					act("CleanupGraphifyOut",
						"Reset graphify-out to prevent perpetual staged changes"),
				),
				seq("ScheduledAnalysisPath", "Deterministic fallback: write fusion analysis, verify build, refresh graphify, and report the cycle",
					cond("NoNewGapsOrImplDegraded",
						"Use deterministic analysis fallback when goals are unchanged OR the implementation path degraded (any ClaudeSuperpowersPath failure)"),
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
