package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// BTFusionTree is a deterministic meta-cognitive behavior tree that researches
// behavior-tree/self-improving-agent patterns, evaluates fit against this Go BT
// platform, writes a repo-grounded fusion report, and verifies build/tests.
//
// Every research action checks the persistent knowledge store
// (~/.go-bt-evolve/research/knowledge.json) before reporting: findings and
// vault notes recorded by earlier cycles are never re-reported, and a cycle
// with zero new knowledge routes to ReportNoNewResearch instead of rewriting
// and re-broadcasting the previous report.
//
// Destructive code changes should be implemented by future gated expansion
// paths; this default cycle is safe: read repo/logs, write vault report, verify.
func BTFusionTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type:        "Sequence",
		Name:        "BTFusion_Main",
		Description: "BT Fusion cycle: gather new research knowledge, then report a no-op or run the gated fusion apply path with verification",
		Children: []evolution.SerializableNode{
			seq("BTFusion_PreGate", "Validate the task is non-empty and fusion-related before the research cycle",
				cond("ValidateInput", "Task must be non-empty"),
				cond("IsFusionTask", "Detect fusion/improve/expand/capability/research/evolve/update keywords"),
			),
			act("SearchForBTPatterns", "Record candidate BT/self-improving-agent patterns in the research knowledge store; surface only unrecorded ones"),
			act("QueryNotebookLMResearch", "Scan vault research notes against the knowledge store and surface only new-since-last-cycle notes"),
			act("SynthesizeFindings", "Summarize this cycle's new knowledge entries as concrete fusion targets"),
			sel("StrategyRouter", "Report a brief no-op when this cycle recorded no new research, else run the gated fusion apply path",
				// Zero new knowledge → report the no-op briefly instead of
				// rewriting and re-broadcasting the previous fusion report.
				seq("BTFusion_NoNewResearch", "Report a brief no-op when this cycle recorded no new research knowledge",
					cond("NoNewResearch", "This cycle recorded no knowledge-store entries that were not already known"),
					act("ReportNoNewResearch", "State that all findings were already recorded and skip the duplicate report"),
				),
				seq("BTFusion_NewResearch", "Assess codebase fit and complexity, then apply the approved fusion report and verify build/tests",
					act("CheckCodebaseFit", "Inspect repository, agent registry, service state, and tree registrations"),
					act("AssessFusionComplexity", "Rank changes by risk: additive tree/report vs gardener pool vs core runtime"),
					act("PrioritizeFusionTargets", "Pick the highest impact safe next steps for this cycle"),
					evolution.SerializableNode{
						Type:        "HumanApprovalGate",
						Name:        "ApproveFusionReportWrite",
						Description: "Requires HITL approval before BT Fusion writes durable research output or future expansion changes",
						Metadata: map[string]any{
							"phase":             "pre",
							"side_effect_class": "local_reversible",
							"hitl_prompt":       "Approve this BT Fusion cycle to write/update the durable Obsidian research report and proceed to verification?",
							"auto_approve":      true,
						},
						Children: []evolution.SerializableNode{
							act("ApplyFusion", "Write the BT Fusion report to the Obsidian vault as durable research input"),
						},
					},
					act("VerifyFusionBuild", "Run domain tree smoke tests and bt-agent build"),
					act("ReportFusionStatus", "Summarize applied safe changes and next gated expansion target"),
				),
			),
			outcome(),
		},
	}
}
