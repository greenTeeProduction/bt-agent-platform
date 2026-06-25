package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// GoapFusionTree is a GOAP-driven BT platform improvement runner.
// Both scheduled and explicit tasks can apply code changes via the Superpowers runtime.
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
					Description: "Approve GOAP fusion to run Claude Code through the production Superpowers runtime.",
					Metadata: map[string]any{
						"phase":             "pre",
						"side_effect_class": "external",
						"hitl_prompt":       "GOAP fusion will run Claude Code to implement BT platform improvements.",
						"auto_approve":      true,
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
				evolution.SerializableNode{
					Type:        "HumanApprovalGate",
					Name:        "ApproveScheduledGoapApply",
					Description: "Auto-approve GOAP fusion for scheduled runs.",
					Metadata: map[string]any{
						"phase":        "pre",
						"auto_approve": true,
					},
					Children: []evolution.SerializableNode{
						act("RunSuperpowersClaudeImplementation", "Execute GOAP fusion through Superpowers runtime and Claude Code"),
						act("VerifyGoapBuild", "Run production-safe GOAP/Superpowers build and focused tests"),
						act("ReportSuperpowersImplementation", "Report artifacts, finish evidence, and changed files"),
					},
				},
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
