package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// GoapFusionTree is a dual-mode GOAP fusion runner.
//
// Scheduled/default tasks stay deterministic and non-HITL:
// shared analysis -> ScheduledAnalysisPath -> report.
//
// Explicit apply/fix/implement tasks write a Superpowers implementation plan,
// request native HITL approval, then delegate code-changing work to the typed
// production Superpowers runtime. There is intentionally no ChainAgent fallback.
func GoapFusionTree(withCheckpointVerifier bool) *evolution.SerializableNode {
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "GoapFusion_Main", TimeoutMs: 3600_000, Children: []evolution.SerializableNode{
		act("SetupFusionTools", "Give GOAP fusion actions access to vault, graphify, git, and Superpowers runtime tools"),
		seq("PreGate",
			cond("ValidateInput", "Task must be non-empty"),
			cond("IsFusionTask", "Detect fusion/improve/expand/capability/research/evolve keywords"),
		),
		act("ReadVaultResearch", "Read all NotebookLM research syntheses and improvement plans from vault"),
		act("ReadGraphifyReport", "Read graphify-out/GRAPH_REPORT.md for codebase structure, god nodes, communities"),
		act("AnalyzeImprovementGaps", "Cross-reference research findings with codebase gaps"),
		act("PrioritizeGoapGoals", "Build GOAP goal queue: highest-impact, lowest-risk improvements first"),
		sel("ExecutionRouter",
			seq("ClaudeSuperpowersPath",
				cond("IsApplyRequest", "Explicit apply/implement/fix/create/build requests use Superpowers runtime after HITL"),
				act("WriteSuperpowersImplementationPlan", "Write a concrete Superpowers plan with files, tests, risks, and verification commands"),
				evolution.SerializableNode{
					Type:        "HumanApprovalGate",
					Name:        "ApproveGoapFusionApply",
					Description: "Approve the written GOAP fusion Superpowers plan before Claude Code modifies files",
					Metadata: map[string]any{
						"phase":             "pre",
						"side_effect_class": "external",
						"hitl_prompt":       "Approve GOAP fusion to run Claude Code through the production Superpowers runtime? The previous action wrote a plan with changed files, tests, risk, and verification evidence; reject if the plan is unclear or too broad.",
					},
					Children: []evolution.SerializableNode{
						act("RunSuperpowersClaudeImplementation", "Execute approved plan through production Superpowers runtime and Claude Code"),
						act("VerifyGoapBuild", "Run production-safe GOAP/Superpowers build and focused tests"),
						act("ReportSuperpowersImplementation", "Report artifacts, finish evidence, and changed files"),
					},
				},
			),
			seq("ScheduledAnalysisPath",
				act("WriteFusionAnalysis", "Write deterministic fusion analysis to the vault"),
				act("VerifyGoapBuild", "Run production-safe GOAP/Superpowers build and focused tests"),
				act("ReportFusionCycle", "Summarize deterministic scheduled cycle with verification evidence"),
			),
		),
		act("ReflectOnOutcome", "Reflect on fusion cycle quality"),
		outcome(),
		act("UpdateBehaviorTree", "Evolve"),
	}}
	if withCheckpointVerifier {
		return evolution.WrapWithCheckpointVerifier(tree, 3, "has_result=true,task_status=completed")
	}
	return tree
}
