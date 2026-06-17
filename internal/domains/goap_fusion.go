package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// GoapFusionTree is a GOAP-driven behavior tree that reads NotebookLM research from
// the Obsidian vault, analyzes the codebase via graphify, plans tree improvements,
// implements safe changes via Claude Code, and verifies with build/tests.
//
// Architecture (flat Sequence — deterministic first, then route to implementation):
//
//	PreGate → ResearchAnalysis (always runs, <1s) → ImplementationRouter (Claude or fallback) → Reflect → Evolve
//
// Goals (GOAP priority order):
//  1. ensure_research_loaded — NotebookLM syntheses and plans read from vault
//  2. gap_analysis — Codebase structure checked against research findings
//  3. apply_tree_improvement — One improvement implemented (Claude Code)
//  4. verify_improvement — Build and tests pass for changed packages
//
// Destructive core runtime changes require HITL gating.
func GoapFusionTree(withCheckpointVerifier bool) *evolution.SerializableNode {
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "GoapFusion_Main", TimeoutMs: 3600_000, Children: []evolution.SerializableNode{
		act("SetupFusionTools", "Give chain agents access to file_read, shell_exec, web_search, graphify, vault paths"),
		seq("PreGate",
			cond("ValidateInput", "Task must be non-empty"),
			cond("IsFusionTask", "Detect fusion/improve/expand/capability/research/evolve keywords"),
		),
		// ── Phase 1: Deterministic research analysis (always runs, <1s total) ──
		act("ReadVaultResearch", "Read all NotebookLM research syntheses and improvement plans from vault"),
		act("ReadGraphifyReport", "Read graphify-out/GRAPH_REPORT.md for codebase structure, god nodes, communities"),
		act("AnalyzeImprovementGaps", "Cross-reference research findings with codebase: what's missing, what's stale, what's improvable"),
		act("PrioritizeGoapGoals", "Build GOAP goal queue: highest-impact, lowest-risk tree improvements first"),
		// ── Phase 2: Route to appropriate implementation engine ──
		sel("ImplementationRouter",
			seq("ClaudePath",
				cond("IsApplyRequest", "Detect apply/implement/fix/create/build keywords — use Claude Code"),
				act("ReadImprovementPlan", "Read the highest-priority plan from ChainState or vault plans directory"),
				evolution.SerializableNode{
					Type:        "HumanApprovalGate",
					Name:        "ApproveGoapFusionApply",
					Description: "Requires HITL approval before Claude Code implements BT platform changes",
					Metadata: map[string]any{
						"phase":             "pre",
						"side_effect_class": "external",
						"hitl_prompt":       "Approve this GOAP fusion cycle to implement code changes via Claude Code? Research + gap analysis complete. Only additive, reversible changes.",
					},
					Children: []evolution.SerializableNode{
						act("ApplyImprovementWithClaude", "Launch Claude Code with full GOAP context to implement the highest-priority improvement"),
						act("VerifyGoapBuild", "Run go test for changed packages and go build ./..."),
					},
				},
			),
			seq("ExecutionPath",
				chainAgent("GoapFusionAgent",
					"You are a GOAP-driven BT platform fusion agent. TASK: {{.Task}}. GOAP goals loaded in ChainState — check goap_fusion_* keys for research findings, gap analysis, and priorities. Use file_read to read the vault research, graphify report, and source files. Use file_write to implement safe tree improvements. Use shell_exec for go build, go test, git operations. Complete the highest-priority improvement in this cycle.",
					[]string{"file_read", "file_write", "shell_exec", "web_search"}),
			),
		),
		act("ReflectOnOutcome", "Reflect on fusion improvement quality: did we make a measurable improvement? was the research correctly actioned?"),
		outcome(),
		act("UpdateBehaviorTree", "Evolve"),
	}}
	if withCheckpointVerifier {
		return evolution.WrapWithCheckpointVerifier(tree, 3, "has_result=true,task_status=completed")
	}
	return tree
}
