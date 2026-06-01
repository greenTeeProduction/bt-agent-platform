package evolution

// UIReviewFixTree is a behavior tree that reviews, tests, and fixes the BT Studio dashboard UI.
// It systematically tests every tab, every button, every API endpoint, and every use case
// using Playwright browser automation, then applies fixes for any issues found.
//
// Strategy paths:
//  1. API Health Check — verify all endpoints respond correctly
//  2. Tab Navigation — test every sidebar tab loads
//  3. Task Flow — test approve → reject → sprint execute pipeline
//  4. Chat Flow — test chat sends and receives
//  5. MindMap Flow — test tree visualization renders
//  6. Mobile Layout — test responsive breakpoints
//  7. Fix Issues — apply fixes to dashboard code
//  8. Re-verify — confirm fixes work
func UIReviewFixTree() *SerializableNode {
	return &SerializableNode{
		Type: "Sequence",
		Name: "UIReviewFix_Main",
		Children: []SerializableNode{
			// Phase 0: Setup
			{
				Type: "Sequence", Name: "Setup",
				Children: []SerializableNode{
					{Type: "Condition", Name: "ValidateInput"},
					{Type: "Action", Name: "SetupDefaultTools"},
				},
			},
			// Phase 1: API Health Check
			{
				Type: "Sequence", Name: "APICheck",
				Children: []SerializableNode{
					{
						Type:     "ChainAction",
						Name:     "llm_call:Test ALL dashboard API endpoints using curl: /api/summary, /api/trees, /api/tasks, /api/thinktank/fellows, /api/company/default, /api/tree/structure?id=godev, /api/tree/structure?id=stockfish_evolve, /api/chat?msg=test&tab=trees. Check that each returns valid JSON with expected fields. Report any endpoints that fail or return errors.",
						Metadata: map[string]any{"max_tokens": float64(8)},
					},
				},
			},
			// Phase 2: Tab Navigation Test
			{
				Type: "Sequence", Name: "TabNavigation",
				Children: []SerializableNode{
					{
						Type:     "ChainAction",
						Name:     "llm_call:Navigate the dashboard at http://localhost:9800. Click each sidebar tab: Overview, ThinkTank, Company, Tasks, Trees, MindMap, Evolution. Verify each tab loads content (not empty or error). Check that the active tab is highlighted. Report any tabs that fail to load.",
						Metadata: map[string]any{"max_tokens": float64(8)},
					},
				},
			},
			// Phase 3: Task Pipeline Test (approve → reject → execute)
			{
				Type: "Sequence", Name: "TaskPipeline",
				Children: []SerializableNode{
					{
						Type:     "ChainAction",
						Name:     "llm_call:Test the complete task pipeline via Playwright: 1) Navigate to Tasks tab, 2) Verify 6 tasks loaded from API, 3) Click Approve on rec-001, verify status badge changes to 'approved', 4) Click Reject on agree-001, verify status changes to 'rejected', 5) Click '🔍 Details' on a task, verify modal opens with status history, assignee, sprint, SP, 6) Click '📋 Kanban' toggle, verify columns appear, 7) Filter by 'Approved', verify only approved tasks show. Report any broken buttons or missing features.",
						Metadata: map[string]any{"max_tokens": float64(12)},
					},
				},
			},
			// Phase 4: Chat Flow Test
			{
				Type: "Sequence", Name: "ChatFlow",
				Children: []SerializableNode{
					{
						Type:     "ChainAction",
						Name:     "llm_call:Test the chat feature: 1) Click the 💬 chat button (bottom-right), 2) Verify chat panel opens, 3) Verify agent name matches current tab, 4) Type a message and send, 5) Verify 'Agent is thinking...' appears, 6) Wait up to 60s for response. Report if chat opens, sends messages, and receives responses.",
						Metadata: map[string]any{"max_tokens": float64(8)},
					},
				},
			},
			// Phase 5: MindMap Test
			{
				Type: "Sequence", Name: "MindMapFlow",
				Children: []SerializableNode{
					{
						Type:     "ChainAction",
						Name:     "llm_call:Test the mind map: 1) Navigate to MindMap tab, 2) Verify SVG renders (check for <path> and <rect> elements), 3) Switch tree selector to 'Stockfish Evolution', 4) Verify tree re-renders, 5) Click zoom buttons, 6) Hover a node and verify detail popup appears. Report any rendering issues.",
						Metadata: map[string]any{"max_tokens": float64(8)},
					},
				},
			},
			// Phase 6: Mobile Layout
			{
				Type: "Sequence", Name: "MobileLayout",
				Children: []SerializableNode{
					{
						Type:     "ChainAction",
						Name:     "llm_call:Test mobile responsive layout: 1) Resize browser to 375x812 (iPhone), 2) Verify sidebar is hidden, 3) Verify main content fills screen, 4) Verify chat panel uses full width, 5) Verify grid switches to 2-column, 6) Click tasks tab, verify tasks are readable. Report layout issues on mobile.",
						Metadata: map[string]any{"max_tokens": float64(8)},
					},
				},
			},
			// Phase 7: Fix Issues
			{
				Type: "Selector", Name: "FixRouter",
				Children: []SerializableNode{
					// Fix missing features
					{
						Type: "Sequence", Name: "FixMissingFeatures",
						Children: []SerializableNode{
							{Type: "Condition", Name: "HasFeatureGaps"},
							{
								Type:     "ChainAction",
								Name:     "llm_call:Review the test results from phases 1-6. Identify missing features, broken buttons, or layout issues. For each issue, propose a specific code fix to /home/nico/go-bt-evolve/cmd/bt-dashboard/main.go. Focus on: missing API handlers, incorrect CSS, broken JavaScript functions, missing error handling, accessibility issues. Provide the exact code changes needed.",
								Metadata: map[string]any{"max_tokens": float64(15)},
							},
						},
					},
					// Fix layout issues
					{
						Type: "Sequence", Name: "FixLayout",
						Children: []SerializableNode{
							{Type: "Condition", Name: "HasLayoutIssues"},
							{
								Type:     "ChainAction",
								Name:     "llm_call:Review layout issues found. Fix CSS problems: missing responsive breakpoints, overflow issues, alignment problems, color contrast. Suggest specific CSS changes for the dashboard HTML template.",
								Metadata: map[string]any{"max_tokens": float64(10)},
							},
						},
					},
					// Fix API issues
					{
						Type: "Sequence", Name: "FixAPI",
						Children: []SerializableNode{
							{Type: "Condition", Name: "HasAPIIssues"},
							{
								Type:     "ChainAction",
								Name:     "llm_call:Fix API issues: missing endpoints, incorrect JSON responses, slow responses that need async handling, error handling gaps. Suggest specific Go code changes.",
								Metadata: map[string]any{"max_tokens": float64(10)},
							},
						},
					},
				},
			},
			// Phase 8: Re-verify fixes
			{
				Type: "Sequence", Name: "ReVerify",
				Children: []SerializableNode{
					{
						Type:     "ChainAction",
						Name:     "llm_call:Re-build the dashboard binary and restart the server. Then re-run ALL tests from phases 1-6. Confirm that previously failing tests now pass. Report final pass/fail status for each test.",
						Metadata: map[string]any{"max_tokens": float64(10)},
					},
				},
			},
			// Phase 9: Report
			{
				Type: "Sequence", Name: "ReportPhase",
				Children: []SerializableNode{
					{
						Type:     "ChainAction",
						Name:     "llm_call:Generate a final UI Review Report. Include: test summary (total passed/failed), issues found and fixed, remaining issues, overall UI quality score (1-10), and recommendations for future improvements.",
						Metadata: map[string]any{"max_tokens": float64(8)},
					},
					{Type: "Action", Name: "ReflectOnOutcome"},
				},
			},
			// Outcome
			{
				Type: "Selector", Name: "OutcomeSelector",
				Children: []SerializableNode{
					{Type: "Condition", Name: "WasSuccessful"},
					{
						Type:     "ChainAction",
						Name:     "llm_call:UI review found issues. Self-correct: re-analyze the failing tests and suggest alternative fixes.",
						Metadata: map[string]any{"max_tokens": float64(5)},
					},
				},
			},
		},
	}
}

// HasFeatureGaps condition — checks if any test phases reported missing features.
// HasLayoutIssues condition — checks for CSS/responsive problems.
// HasAPIIssues condition — checks for API endpoint failures.
// These are evaluated by the tree engine during execution.
