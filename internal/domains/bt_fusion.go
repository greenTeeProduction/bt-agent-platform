package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// BTFusionTree is a deterministic meta-cognitive behavior tree that researches
// behavior-tree/self-improving-agent patterns, evaluates fit against this Go BT
// platform, writes a repo-grounded fusion report, and verifies build/tests.
//
// Destructive code changes should be implemented by future gated expansion
// paths; this default cycle is safe: read repo/logs, write vault report, verify.
func BTFusionTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "BTFusion_Main",
		Children: []evolution.SerializableNode{
			seq("BTFusion_PreGate",
				cond("ValidateInput", "Task must be non-empty"),
				cond("IsFusionTask", "Detect fusion/improve/expand/capability/research/evolve/update keywords"),
			),
			act("SearchForBTPatterns", "Collect current BT/self-improving-agent patterns relevant to this Go BT platform"),
			act("QueryNotebookLMResearch", "Read local NotebookLM/vault BT research notes for prior findings"),
			act("SynthesizeFindings", "Deduplicate research and extract concrete fusion targets"),
			act("CheckCodebaseFit", "Inspect repository, agent registry, service state, and tree registrations"),
			act("AssessFusionComplexity", "Rank changes by risk: additive tree/report vs gardener pool vs core runtime"),
			act("PrioritizeFusionTargets", "Pick the highest impact safe next steps for this cycle"),
			{
				Type:        "HumanApprovalGate",
				Name:        "ApproveFusionReportWrite",
				Description: "Requires HITL approval before BT Fusion writes durable research output or future expansion changes",
				Metadata: map[string]any{
					"phase":             "pre",
					"side_effect_class": "external",
					"hitl_prompt":       "Approve this BT Fusion cycle to write/update the durable Obsidian research report and proceed to verification?",
				},
				Children: []evolution.SerializableNode{
					act("ApplyFusion", "Write the BT Fusion report to the Obsidian vault as durable research input"),
				},
			},
			act("VerifyFusionBuild", "Run domain tree smoke tests and bt-agent build"),
			act("ReportFusionStatus", "Summarize applied safe changes and next gated expansion target"),
			outcome(),
		},
	}
}
