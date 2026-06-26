package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// GoapFusionTree is a GOAP-driven BT platform improvement runner.
// Explicit apply tasks can run Claude Code via the Superpowers runtime; scheduled
// and default runs stay analysis-only so they do not repeatedly mutate isolated
// worktrees or create duplicate HITL gates.
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
				act("WriteSuperpowersImplementationPlan", "Write a concrete Superpowers plan with files, tests, risks, and verification commands"),
				evolution.SerializableNode{
					Type:        "HumanApprovalGate",
					Name:        "ApproveGoapFusionApply",
					Description: "GOAP fusion automatically implements findings via Superpowers runtime.",
					Metadata: map[string]any{
						"phase":             "pre",
						"side_effect_class": "local_reversible",
						"hitl_prompt":       "GOAP fusion will run Claude Code to implement BT platform improvements.",
						"auto_approve":      true,
					},
					Children: []evolution.SerializableNode{
						act("RunSuperpowersClaudeImplementation", "Execute plan through production Superpowers runtime and Claude Code"),
						act("VerifyGoapBuild", "Run production-safe GOAP/Superpowers build and focused tests"),
						act("ReportSuperpowersImplementation", "Report artifacts, finish evidence, and changed files"),
					},
				},
			),
			seq("ScheduledAnalysisPath",
				act("WriteFusionAnalysis", "Write deterministic fusion analysis to the vault"),
				act("VerifyGoapBuild", "Run production-safe GOAP/Superpowers build and focused tests"),
				act("RunGraphifyUpdate", "Refresh graphify-out after local verification"),
				act("ReportFusionCycle", "Report deterministic analysis and verification evidence"),
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
