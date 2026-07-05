// Package domains — Superpowers Workflow domain tree (Task 10).
//
// SuperpowersWorkflowTree wires together every node type and action built in
// Tasks 1–9 into the full Superpowers skill graph as a single behavior tree:
// classify → skill-route (brainstorm / systematic-debug / direct) → worktree →
// plan → HITL → parallel-or-serial TDD task loop with review cycles → verify or
// debug → finish HITL with merge/PR/keep/discard routing.
//
// It is the literal encoding of Part B of the approved plan. The v1
// SuperpowersPipelineTree in superpowers_pipeline.go is intentionally untouched.
package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// SuperpowersWorkflowTree returns the full Superpowers workflow behavior tree.
// Root is a resume-safe PersistentMemSequence (Task 3) with a 1h timeout.
func SuperpowersWorkflowTree() *evolution.SerializableNode {
	// --- brainstorm branch (creative path): design → validate → grill → HITL ---
	brainstorm := evolution.SerializableNode{
		Type:        "MemSequence",
		Name:        "BrainstormBranch",
		Description: "Creative path: generate → validate → grill design artifact, then approve",
		Metadata:    map[string]any{"match": "creative"},
		Children: []evolution.SerializableNode{
			act("GenerateDesignArtifact", "Write or reuse design.md with architecture, acceptance criteria, tests, and risks"),
			act("ValidateDesignArtifact", "Strictly validate design.md required sections"),
			act("GrillDesignArtifact", "NotebookLM Q&A grill of the design with web-search fallback"),
			{
				Type:        "HumanApprovalGate",
				Name:        "ApproveDesign",
				Description: "Approve the grilled design artifact before proceeding",
				Metadata: map[string]any{
					"phase":       "pre",
					"hitl_prompt": "Approve the design artifact (after grilling) before implementation?",
				},
			},
		},
	}

	// --- bug path: inlined SystematicDebugging (SubTreeRef cannot resolve an
	// intra-tree name here, so we inline with distinct memory-node names). ---
	bugBranch := systematicDebugging("SystematicDebuggingRouted", "TDDTaskRoutedDebug")
	bugBranch.Metadata = map[string]any{"match": "bug"}

	directBranch := evolution.SerializableNode{
		Type:        "AlwaysSucceed",
		Name:        "DirectPath",
		Description: "Direct path: no design/debug phase for straightforward tasks",
		Metadata:    map[string]any{"default": true},
	}

	skillRouter := evolution.SerializableNode{
		Type:        "DecisionTree",
		Name:        "SkillRouter",
		Description: "Route by task_kind: creative → brainstorm, bug → systematic-debug, else direct",
		Metadata:    map[string]any{"key": "task_kind", "source": "chain_state"},
		Children:    []evolution.SerializableNode{brainstorm, bugBranch, directBranch},
	}

	// --- execution router under the plan-approval gate ---
	executionRouter := sel("ExecutionRouter", "Route to parallel task dispatch when the plan declares independent tasks, else the sequential task loop",
		seq("ParallelPath", "Dispatch independent plan tasks concurrently under the Claude concurrency guard",
			cond("PlanHasIndependentTasks", "True when the plan declares independent, parallelizable tasks"),
			evolution.SerializableNode{
				Type:        "Parallel",
				Name:        "DispatchParallelAgents",
				Description: "Dispatch independent tasks concurrently (sequential-tick model)",
				Children: []evolution.SerializableNode{
					{
						Type:        "SemaphoreGuard",
						Name:        "ClaudeConcurrencyGuard",
						Description: "Bound concurrent Claude Code invocations across all trees (permits=2)",
						Metadata:    map[string]any{"semaphore": "claude", "permits": 2},
						Children: []evolution.SerializableNode{
							foreachTaskLoop("TaskLoopParallel", "ReviewCycleParallel", "TDDTaskParallel"),
						},
					},
				},
			},
		),
		foreachTaskLoop("TaskLoop", "ReviewCycle", "TDDTask"),
	)

	approvePlan := evolution.SerializableNode{
		Type:        "HumanApprovalGate",
		Name:        "ApproveSuperpowersPlan",
		Description: "Approve the implementation plan before Claude Code modifies files",
		Metadata: map[string]any{
			"phase":             "pre",
			"side_effect_class": "local_reversible",
			"hitl_prompt":       "Approve Claude Code execution of the written Superpowers plan?",
		},
		Children: []evolution.SerializableNode{executionRouter},
	}

	// --- verify or debug ---
	verifyOrDebug := sel("VerifyOrDebug", "Verify the run, falling back to the systematic-debugging retry loop when verification fails",
		seq("VerifyPath", "Run layered worktree verification and confirm it succeeded",
			act("VerifySuperpowersRun", "Run layered worktree verification"),
			cond("WasSuccessful", "True when the last verification succeeded"),
		),
		evolution.SerializableNode{
			Type:        "Retry",
			Name:        "DebugRetry",
			Description: "Retry the systematic-debugging loop up to 2 times",
			MaxRetries:  2,
			Children: []evolution.SerializableNode{
				systematicDebugging("SystematicDebugging", "TDDTaskDebug"),
			},
		},
	)

	// --- finish routing ---
	finishRouter := evolution.SerializableNode{
		Type:        "DecisionTree",
		Name:        "FinishRouter",
		Description: "Route the finish choice: merge / PR / keep / discard",
		Metadata:    map[string]any{"key": "finish_choice", "source": "chain_state"},
		Children: []evolution.SerializableNode{
			withMatch(act("ApplySuperpowersRunToMainRepo", "Apply verified worktree patch to the main repo and commit"), "merge"),
			withMatch(act("PushBranchAndCreatePR", "Push the worktree branch and open a pull request"), "pr"),
			{
				Type:        "AlwaysSucceed",
				Name:        "KeepWorktree",
				Description: "Keep the worktree in place without merging or discarding",
				Metadata:    map[string]any{"match": "keep"},
			},
			{
				Type:        "Action",
				Name:        "DiscardSuperpowersWorktree",
				Description: "Discard the Superpowers worktree (default finish choice)",
				Metadata:    map[string]any{"default": true},
			},
		},
	}

	chooseFinish := evolution.SerializableNode{
		Type:        "HumanApprovalGate",
		Name:        "ChooseFinishOption",
		Description: "Choose how to finish the run: merge, PR, keep, or discard",
		Metadata: map[string]any{
			"phase":       "pre",
			"hitl_prompt": "Finish the run — choose: merge | PR | keep | discard",
		},
		Children: []evolution.SerializableNode{finishRouter},
	}

	return &evolution.SerializableNode{
		Type:        "PersistentMemSequence",
		Name:        "SuperpowersWorkflow_Main",
		Description: "Full Superpowers skill graph: classify→route→worktree→plan→HITL→TDD loop→verify/debug→finish",
		TimeoutMs:   3600000,
		Children: []evolution.SerializableNode{
			act("InitSuperpowersRun", "Create or load typed Superpowers run state and artifact directory"),
			act("LoadSuperpowersSkills", "Load required Superpowers skill directives"),
			act("ClassifyTaskKind", "Classify the task (creative/bug/other) into ChainState[task_kind]"),
			skillRouter,
			evolution.SerializableNode{
				Type:        "MemSequence",
				Name:        "WorkspacePhase",
				Description: "using-git-worktrees: create safe worktree then verify baseline",
				Children: []evolution.SerializableNode{
					act("PrepareSuperpowersWorktree", "Create safe git worktree or dry-run repo context"),
					act("VerifySuperpowersBaseline", "Run deterministic baseline build before implementation"),
				},
			},
			evolution.SerializableNode{
				Type:        "MemSequence",
				Name:        "PlanPhase",
				Description: "writing-plans: generate then strictly validate the implementation plan",
				Children: []evolution.SerializableNode{
					act("GenerateImplementationPlan", "Write or reuse plan.md with TDD tasks"),
					act("ValidateImplementationPlanStrict", "Parse and validate plan tasks strictly"),
				},
			},
			approvePlan,
			verifyOrDebug,
			act("WriteSuperpowersFinishReport", "Write finish.md with evidence"),
			chooseFinish,
			act("ReportPipelineComplete", "Return production Superpowers finish evidence"),
		},
	}
}

// tddTask builds the TDD phase-split MemSequence (test-driven-development skill).
// The name is parameterized so inlined copies remain uniquely named.
func tddTask(name string) evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "MemSequence",
		Name:        name,
		Description: "TDD phase-split: red → verify-red → green → verify-green → commit",
		Children: []evolution.SerializableNode{
			act("SuperpowersTaskRed", "Write the failing test (RED)"),
			act("SuperpowersTaskVerifyRed", "Verify the test fails for the right reason"),
			act("SuperpowersTaskGreen", "Implement minimal code to pass (GREEN)"),
			act("SuperpowersTaskVerifyGreen", "Verify the test now passes"),
			act("SuperpowersTaskCommit", "Commit the verified task increment"),
		},
	}
}

// foreachTaskLoop builds an index-based, cursor-persistent ForEachTask (Task 6)
// wrapping a ReviewCycle (Task 7) around a TDD task template.
func foreachTaskLoop(loopName, reviewName, tddName string) evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "ForEachTask",
		Name:        loopName,
		Description: "Index-based, cursor-persistent iteration over plan tasks",
		Children: []evolution.SerializableNode{
			{
				Type:        "ReviewCycle",
				Name:        reviewName,
				Description: "requesting/receiving-code-review two-stage loop (max 3 iterations)",
				Metadata: map[string]any{
					"reviewer_action": "SuperpowersTaskReview",
					"max_iterations":  3,
				},
				Children: []evolution.SerializableNode{tddTask(tddName)},
			},
		},
	}
}

// systematicDebugging builds the four-phase systematic-debugging MemSequence.
// The TDD fix step is the inlined "SubTreeRef → TDDTask" from Part B.
func systematicDebugging(name, tddName string) evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "MemSequence",
		Name:        name,
		Description: "systematic-debugging: root-cause → pattern → hypothesis → TDD fix → rerun",
		Children: []evolution.SerializableNode{
			act("DebugRootCauseInvestigation", "Investigate the root cause of the failure"),
			act("DebugPatternAnalysis", "Analyze failure patterns across the codebase"),
			act("DebugHypothesisTest", "Form and test a hypothesis for the fix"),
			tddTask(tddName),
			act("RerunVerification", "Re-run verification after the TDD fix"),
		},
	}
}

// withMatch tags a node with DecisionTree child-match metadata.
func withMatch(node evolution.SerializableNode, match string) evolution.SerializableNode {
	if node.Metadata == nil {
		node.Metadata = map[string]any{}
	}
	node.Metadata["match"] = match
	return node
}
