package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// GoapFusionTree is a GOAP-driven BT platform improvement runner.
// New research-backed goals run through the Superpowers/Claude implementation
// path. Only unchanged goals fall back to deterministic analysis so implementation
// failures cannot be silently reported as analysis success.
func GoapFusionTree(withCheckpointVerifier bool) *evolution.SerializableNode {
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "GoapFusion_Main", TimeoutMs: 3600_000, Children: []evolution.SerializableNode{
		act("SetupFusionTools", "Give GOAP fusion actions access to vault, graphify, git, and Superpowers runtime tools"),
		seq("PreGate",
			cond("ValidateInput", "Task must be non-empty"),
			cond("IsFusionTask", "Detect fusion/improve/expand/capability/research/evolve keywords"),
		),
		act("RunGoapFusionNotebookLMResearch", "Query BT Platform Research notebook directly and save GOAP-owned findings to vault"),
		act("ReadVaultResearch", "Read all NotebookLM research syntheses and improvement plans from vault"),
		act("ReadGraphifyReport", "Read graphify-out/GRAPH_REPORT.md for codebase structure, god nodes, communities"),
		act("AnalyzeImprovementGaps", "Cross-reference research findings with codebase gaps"),
		act("PrioritizeGoapGoals", "Build GOAP goal queue: highest-impact, lowest-risk improvements first"),
		sel("ExecutionRouter",
			seq("ClaudeSuperpowersPath",
				cond("HasNewGaps", "Only proceed with implementation if goals differ from previous run"),
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
				act("CleanupGraphifyOut", "Reset graphify-out to prevent perpetual staged changes"),
			),
			seq("ScheduledAnalysisPath",
				cond("NoNewGaps", "Only use deterministic analysis fallback when goals are unchanged"),
				act("WriteFusionAnalysis", "Write deterministic fusion analysis to the vault"),
				act("VerifyGoapBuild", "Run production-safe GOAP/Superpowers build and focused tests"),
				act("RunGraphifyUpdate", "Refresh graphify-out after local verification"),
				act("ReportFusionCycle", "Report deterministic analysis and verification evidence"),
				act("CleanupGraphifyOut", "Reset graphify-out to prevent perpetual staged changes"),
			),
		),
		act("VerifyGoapFusionEvidence", "Reject fabricated/self-corrected outputs and require concrete artifact evidence"),
		act("MarkSuccessful", "Mark GOAP fusion complete only after deterministic evidence verification"),
	}}
	if withCheckpointVerifier {
		return evolution.WrapWithCheckpointVerifier(tree, 3, "has_result=true,task_status=completed")
	}
	return tree
}
