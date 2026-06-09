package evolution

// NotebooklmPlanImplementTree returns a behavior tree for the
// research→grill→plan→implement→verify→deploy pipeline.
//
// This is a linear Sequence workflow that:
//  1. Researches via NotebookLM (deep research + import sources)
//  2. Grill-me critical review of findings
//  3. Writes a detailed implementation plan to .hermes/plans/
//  4. Delegates implementation tasks via subagents
//  5. Verifies with test suite
//  6. Builds binary and deploys
func NotebooklmPlanImplementTree() *SerializableNode {
	return &SerializableNode{
		Type: "Sequence",
		Name: "NotebookLM_Plan_Implement",
		Description: "Research → Grill → Plan → Implement → Verify → Deploy pipeline using NotebookLM + subagents",
		Children: []SerializableNode{
			// Step 1: NotebookLM Research — query existing sources, deep research, import
			{
				Type:        "Action",
				Name:        "ResearchNotebookLM",
				Description: "Run full NotebookLM research pipeline: start deep research, poll status, import cited sources, save to vault",
			},

			// Step 2: Grill Me — critically review findings, surface gaps, demand concrete plan
			{
				Type:        "Action",
				Name:        "DoGrillMeReview",
				Description: "Critically review NotebookLM findings: surface gaps, challenge assumptions, demand a concrete implementation plan with file paths and tests",
			},

			// Step 3: Write Plan — write detailed implementation plan to .hermes/plans/
			{
				Type:        "Action",
				Name:        "WriteImplementationPlan",
				Description: "Write a detailed implementation plan with task breakdown, file paths, and test cases to .hermes/plans/",
			},

			// Step 4: Implement — delegate tasks via subagents using ChainAction
			{
				Type: "ChainAction",
				Name: "agent:{{.Task}}",
				Description: "Read the implementation plan and execute all tasks: modify/create source files, wire up registrations, implement functionality",
				Metadata: map[string]any{
					"system_msg": "You are an implementation agent executing a detailed plan. TASK: {{.Task}}. Read the implementation plan from .hermes/plans/, then execute each task: modify/create source files, wire up registrations, implement functionality. Use file_read to read plans and source files, file_write to create or modify files, shell_exec for go build and go test, go_build to compile, go_test to verify. Work through the task list methodically.",
					"tools":      []any{"file_read", "file_write", "shell_exec", "go_build", "go_test", "go_vet", "web_search"},
					"max_tokens": float64(15),
				},
			},

			// Step 5: Verify — run test suite
			{
				Type:        "Action",
				Name:        "RunTests",
				Description: "Execute test suite, capture results, check for regressions",
			},

			// Step 6: Deploy — build binary and restart service
			{
				Type: "Sequence",
				Name: "DeploySequence",
				Children: []SerializableNode{
					{
						Type:        "Action",
						Name:        "RunBuild",
						Description: "Build Go binary for deployment",
					},
					{
						Type:        "Action",
						Name:        "VerifyDeploy",
						Description: "Health check endpoint, smoke test after build/deploy",
					},
				},
			},

			// Outcome
			{
				Type: "Selector",
				Name: "OutcomeSelector",
				Children: []SerializableNode{
					{Type: "Action", Name: "MarkSuccessful", Description: "Mark task as successful"},
					{Type: "Action", Name: "DefaultFallback", Description: "Report failure"},
				},
			},
		},
	}
}
