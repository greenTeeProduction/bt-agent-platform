// Package domains — production Superpowers Pipeline domain tree.
//
// Implements the Superpowers SDLC as real behavior-tree runtime phases:
// design artifact → safe worktree/baseline → implementation plan → native HITL
// → Claude Code task execution with TDD → verification → finish evidence.
package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// SuperpowersPipelineTree returns the production Superpowers software
// development methodology behavior tree. It intentionally has no ChainAgent
// placeholders and no unconditional skip paths.
func SuperpowersPipelineTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type:        "Sequence",
		Name:        "SuperpowersPipeline_Main",
		Description: "Production Superpowers SDLC: design→worktree→plan→HITL→Claude TDD implementation→verify→finish",
		TimeoutMs:   3600000,
		Children: []evolution.SerializableNode{
			act("InitSuperpowersRun", "Create or load typed Superpowers run state and artifact directory"),
			act("LoadSuperpowersSkills", "Load required Superpowers skill directives"),
			act("GenerateDesignArtifact", "Write or reuse design.md with architecture, acceptance criteria, tests, and risks"),
			act("ValidateDesignArtifact", "Strictly validate design.md required sections"),
			act("PrepareSuperpowersWorktree", "Create safe git worktree or dry-run repo context"),
			act("VerifySuperpowersBaseline", "Run deterministic baseline build before implementation"),
			act("GenerateImplementationPlan", "Write or reuse plan.md with TDD tasks"),
			act("ValidateImplementationPlanStrict", "Parse and validate plan tasks strictly"),
			sel("SuperpowersExecutionRouter", "Route to dry-run artifact generation, else the HITL-gated Claude Code apply path",
				seq("SuperpowersDryRunPath", "Generate per-task dry-run artifacts and evidence without Claude modifying files",
					cond("IsSuperpowersDryRun", "Dry-run requests generate artifacts and evidence without Claude modifying files"),
					act("ExecuteSuperpowersTaskBatch", "Generate per-task dry-run artifacts without invoking Claude"),
					act("VerifySuperpowersRun", "Run layered verification"),
					act("WriteSuperpowersFinishReport", "Write finish.md with evidence"),
				),
				seq("SuperpowersApplyPath", "HITL-gated apply path: Claude Code executes the plan, then verification and finish report",
					evolution.SerializableNode{
						Type:        "HumanApprovalGate",
						Name:        "ApproveSuperpowersPlan",
						Description: "Approve the written Superpowers implementation plan before Claude Code modifies files",
						Metadata: map[string]any{
							"phase":             "pre",
							"side_effect_class": "local_reversible",
							"hitl_prompt":       "Approve Claude Code execution of the written Superpowers plan?",
						},
						Children: []evolution.SerializableNode{
							act("ExecuteSuperpowersTaskBatch", "Execute approved plan tasks with Claude Code and TDD"),
							act("VerifySuperpowersRun", "Run layered worktree verification"),
							act("ApplySuperpowersRunToMainRepo", "Apply verified worktree patch to the main repo, run main-repo checks, graphify update, and commit"),
							act("WriteSuperpowersFinishReport", "Write finish.md with evidence"),
							act("UpdateBlackboard", "Mark Superpowers run complete"),
						},
					},
				),
			),
			outcome(),
			act("ReportPipelineComplete", "Return production Superpowers finish evidence"),
		},
	}
}
