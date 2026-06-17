package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// GoapFusionTree is a GOAP-driven behavior tree that reads NotebookLM research from
// the Obsidian vault, analyzes the codebase via graphify, plans tree improvements,
// implements safe changes, and verifies with build/tests.
//
// Goals (GOAP priority order):
//  1. ensure_research_loaded — NotebookLM syntheses and plans read from vault
//  2. gap_analysis — Codebase structure checked against research findings
//  3. apply_tree_improvement — One improvement implemented (domain tree, condition, action)
//  4. verify_improvement — Build and tests pass for changed packages
//
// Destructive core runtime changes require HITL gating.
func GoapFusionTree(withCheckpointVerifier bool) *evolution.SerializableNode {
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "GoapFusion_Main", Children: []evolution.SerializableNode{
		act("SetupFusionTools", "Give chain agents access to file_read, shell_exec, web_search, graphify, vault paths"),
		seq("PreGate",
			cond("ValidateInput", "Task must be non-empty"),
			cond("IsFusionTask", "Detect fusion/improve/expand/capability/research/evolve keywords"),
		),
		sel("StrategyRouter",
			seq("ResearchGapPath",
				cond("IsResearchOrGapRequest", "Detect research/gap/analyze/plan/assess keywords"),
				act("ReadVaultResearch", "Read all NotebookLM research syntheses and improvement plans from vault"),
				act("ReadGraphifyReport", "Read graphify-out/GRAPH_REPORT.md for codebase structure, god nodes, communities"),
				act("AnalyzeImprovementGaps", "Cross-reference research findings with codebase: what's missing, what's stale, what's improvable"),
				act("PrioritizeGoapGoals", "Build GOAP goal queue: highest-impact, lowest-risk tree improvements first"),
				chainAgent("ResearchGapAgent",
					"You are a GOAP fusion research agent. TASK: {{.Task}}. BLOCKED — use file_read to read the vault research and graphify report for analysis. Available actions in ChainState show what was already read. Use web_search only for external validation. Produce a structured gap analysis: (1) what research findings are NOT reflected in current trees, (2) what new domain trees or condition nodes are suggested, (3) what existing trees need improvement, (4) prioritized implementation order. DO NOT modify code — this is analysis only.",
					[]string{"file_read", "shell_exec", "web_search"}),
			),
			seq("ApplyImprovementPath",
				cond("IsApplyRequest", "Detect apply/implement/fix/create/build keywords"),
				act("ReadImprovementPlan", "Read the highest-priority plan from ChainState or vault plans directory"),
				chainAgent("GoapImplementAgent",
					"You are a GOAP implementation agent. TASK: {{.Task}}. You MUST read files before editing them. Use file_read to read the target source file, then use file_write to write the complete file with your improvement. Available context from previous actions is in bb.ChainState. Rules: (1) One focused improvement per run — new domain tree, improved condition node, enhanced blackboard context, or testability fix. (2) Do NOT modify evaluator/, gardener/, secrets, configs, graphify-out/. (3) Add the new tree to AllDomainTrees() if creating one. (4) Run go test <changed package> -count=1 -timeout 120s before committing. (5) Run go build ./... to verify. (6) git add + git commit with message 'improve: <area> — <what changed>'.",
					[]string{"file_read", "file_write", "shell_exec", "web_search"}),
				act("VerifyGoapBuild", "Run go test for changed packages and go build ./..."),
			),
			seq("ExecutionPath",
				chainAgent("GoapFusionAgent",
					"You are a GOAP-driven BT platform fusion agent. TASK: {{.Task}}. GOAP goals loaded in ChainState — check goap_fusion_* keys for research findings, gap analysis, and priorities. Use file_read to read the vault research, graphify report, and source files. Use file_write to implement safe tree improvements. Use shell_exec for go build, go test, git operations. Complete the highest-priority improvement in this cycle.",
					[]string{"file_read", "file_write", "shell_exec", "web_search"}),
			),
		),
		act("ReflectOnOutcome", "Reflect on fusion improvement quality: did we make a measurable improvement? was the research-actioned correctly?"),
		outcome(),
		act("UpdateBehaviorTree", "Evolve"),
	}}
	if withCheckpointVerifier {
		return evolution.WrapWithCheckpointVerifier(tree, 3, "has_result=true,task_status=completed")
	}
	return tree
}
