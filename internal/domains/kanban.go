package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// Kanban trees for Hermes Agent's Focalboard-based 10-column workflow.
//
// Workflow: ON HOLD → BACKLOG → TODO → PLANNING → REFINED → APPROVED
//           → IN PROGRESS → QA → REVIEW → DONE
//
// Agents: task-creator (→BACKLOG), refiner (TODO→REFINED),
//         developer (APPROVED→QA), qa (QA→REVIEW)

// KanbanTaskCreatorTree creates new task cards from gaps, needs, or analysis output.
func KanbanTaskCreatorTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "TaskCreator_Main",
		Children: []evolution.SerializableNode{
			{Type: "Sequence", Name: "PreGate", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "ValidateInput"},
				{Type: "Action", Name: "SetupDefaultTools"},
			}},
			{
				Type: "ChainAction",
				Name: "llm_call:Create a new Focalboard task card: 1) Analyze the task description for completeness. 2) Generate an actionable title. 3) Write clear acceptance criteria as checkboxes (- [ ]). 4) Set priority (critical/high/medium/low). 5) Determine the AQAL quadrant (q-i/q-it/q-we/q-its/q-all). 6) Create the card in the BACKLOG column. 7) Report: card ID, title, priority, quadrant.",
				Metadata: map[string]any{
					"max_tokens": float64(10),
					"system_msg": "You are a task creator for a 10-column Focalboard Kanban. Create well-structured cards with DoR-ready descriptions.",
				},
			},
			{Type: "Action", Name: "ReflectOnOutcome"},
			{Type: "Selector", Name: "OutcomeSelector", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "WasSuccessful"},
				{Type: "ChainAction", Name: "llm_call:Card creation failed. Check: is the board accessible? Is the column name correct? Is the card format valid?", Metadata: map[string]any{"max_tokens": float64(4)}},
			}},
		},
	}
}

// KanbanRefinerTree refines TODO cards into REFINED state with DoR gate.
func KanbanRefinerTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "Refiner_Main",
		Children: []evolution.SerializableNode{
			{Type: "Sequence", Name: "PreGate", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "ValidateInput"},
				{Type: "Condition", Name: "IsKanbanTask", Description: "Has card ID or task reference"},
			}},
			{
				Type: "ChainAction",
				Name: "llm_call:Refine a TODO card for Focalboard: 1) Read the card's current description and acceptance criteria. 2) Expand the description with implementation context. 3) Ensure acceptance criteria are specific and testable. 4) Add implementation notes and architecture constraints. 5) Verify DoR gate: description complete, AC present, priority set, quadrant set. 6) Move card from TODO → PLANNING → REFINED. 7) Report: card ID, refinement summary, DoR status.",
				Metadata: map[string]any{
					"max_tokens": float64(10),
					"system_msg": "You are a task refiner. Transform raw TODO cards into well-specified REFINED cards with complete DoR.",
				},
			},
			{Type: "Action", Name: "ReflectOnOutcome"},
			{Type: "Selector", Name: "OutcomeSelector", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "WasSuccessful"},
				{Type: "ChainAction", Name: "llm_call:Refinement failed. Possible issues: card not in TODO column, board permissions, invalid card format. Diagnose and retry.", Metadata: map[string]any{"max_tokens": float64(4)}},
			}},
		},
	}
}

// KanbanQATree validates cards moving from QA to REVIEW.
func KanbanQATree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "QA_Main",
		Children: []evolution.SerializableNode{
			{Type: "Sequence", Name: "PreGate", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "ValidateInput"},
				{Type: "Condition", Name: "IsKanbanTask"},
			}},
			{
				Type: "ChainAction",
				Name: "llm_call:Run QA validation on a Focalboard card: 1) Check all acceptance criteria are [x] completed. 2) Verify the implementation matches the description. 3) Run quality checks: code style, security, performance concerns. 4) Check for regressions or side effects. 5) Generate QA report with PASS/FAIL status. 6) If PASS: move card from QA → REVIEW. If FAIL: move back to IN PROGRESS with specific issues. 7) Report: card ID, QA result, issues found.",
				Metadata: map[string]any{
					"max_tokens": float64(10),
					"system_msg": "You are a QA agent. Validate that implementations meet the spec. Be thorough but fair.",
				},
			},
			{Type: "Action", Name: "ReflectOnOutcome"},
			{Type: "Selector", Name: "OutcomeSelector", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "WasSuccessful"},
				{Type: "ChainAction", Name: "llm_call:QA check failed to execute. Verify board access and card state. Retry with corrected approach.", Metadata: map[string]any{"max_tokens": float64(4)}},
			}},
		},
	}
}

// KanbanBoardMonitorTree scans the board for issues: stale cards, bottlenecks, overdue items.
func KanbanBoardMonitorTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "BoardMonitor_Main",
		Children: []evolution.SerializableNode{
			{Type: "Sequence", Name: "PreGate", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "ValidateInput"},
				{Type: "Action", Name: "SetupDefaultTools"},
			}},
			{
				Type: "Selector", Name: "MonitorRouter", Children: []evolution.SerializableNode{
					// Check for stale cards
					{
						Type: "Sequence", Name: "StaleCheck",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsBoardCheck", Description: "Task mentions scan, check, monitor, or stale"},
							{
								Type: "ChainAction",
								Name: "llm_call:Scan the Focalboard Kanban for issues: 1) IN PROGRESS cards idle > 2 days → flag as stale. 2) TODO cards > 1 week without refinement → flag for refiner. 3) REVIEW cards waiting > 3 days → notify for human review. 4) APPROVED cards without developer assignment → flag. 5) Bottleneck detection: which column has the most cards? 6) Report: stale count, bottlenecks, recommendations.",
								Metadata: map[string]any{
									"max_tokens": float64(8),
									"system_msg": "You are a Kanban board monitor. Find stuck work before it becomes a problem.",
								},
							},
						},
					},
					// Dispatch next ready card
					{
						Type: "Sequence", Name: "DispatchPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "NeedsDispatch", Description: "Task mentions dispatch, next, assign, or start"},
							{
								Type: "ChainAction",
								Name: "llm_call:Find the next dispatchable card: 1) Scan TODO column → dispatch refiner. 2) Scan APPROVED column → dispatch developer. 3) Scan QA column → dispatch QA agent. 4) For each: verify the card meets the column gate before moving. 5) Report: cards dispatched, agent assignments.",
								Metadata: map[string]any{
									"max_tokens": float64(8),
									"system_msg": "You are a task dispatcher. Move cards to the next agent in the workflow.",
								},
							},
						},
					},
					// Daily standup
					{
						Type: "Sequence", Name: "StandupPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsStandup", Description: "Task mentions standup, daily, or status"},
							{
								Type: "ChainAction",
								Name: "llm_call:Generate a daily standup summary: 1) Cards completed (moved to DONE). 2) Cards in progress with status. 3) Blocked cards and blockers. 4) Upcoming cards ready for review. 5) Velocity: cards completed this week vs last week. Format as concise standup report.",
								Metadata: map[string]any{
									"max_tokens": float64(8),
									"system_msg": "You are a standup bot. Produce clear, actionable status reports.",
								},
							},
						},
					},
				},
			},
			{Type: "Action", Name: "ReflectOnOutcome"},
		},
	}
}

// KanbanWorkflowTree orchestrates the full Kanban pipeline: create → refine → approve → build → QA → review.
func KanbanWorkflowTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "KanbanWorkflow_Main",
		Children: []evolution.SerializableNode{
			{Type: "Sequence", Name: "PreGate", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "ValidateInput"},
				{Type: "Action", Name: "SetupDefaultTools"},
			}},
			{
				Type: "Selector", Name: "WorkflowRouter", Children: []evolution.SerializableNode{
					// Create new card
					{
						Type: "Sequence", Name: "CreatePath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsCreateTask", Description: "Task mentions create, new card, add"},
							{
								Type:     "ChainAction",
								Name:     "llm_call:Create a new task card following DoR standards: actionable title, description, acceptance criteria (- [ ] items), priority (critical/high/medium/low), AQAL quadrant (q-i/q-it/q-we/q-its/q-all). Place in BACKLOG column.",
								Metadata: map[string]any{"max_tokens": float64(8)},
							},
						},
					},
					// Refine card
					{
						Type: "Sequence", Name: "RefinePath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsRefinement", Description: "Task mentions refine, expand, detail"},
							{
								Type:     "ChainAction",
								Name:     "llm_call:Refine a TODO card: expand description, add implementation notes, ensure AC are testable, add architecture constraints. Move TODO→PLANNING→REFINED. Verify DoR gate passes.",
								Metadata: map[string]any{"max_tokens": float64(8)},
							},
						},
					},
					// QA check
					{
						Type: "Sequence", Name: "QAPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsQA", Description: "Task mentions qa, test, validate, verify"},
							{
								Type:     "ChainAction",
								Name:     "llm_call:Run QA on a card: check all AC are [x], verify implementation, check for regressions, generate PASS/FAIL report. PASS → move QA→REVIEW. FAIL → move back to IN PROGRESS with issues.",
								Metadata: map[string]any{"max_tokens": float64(8)},
							},
						},
					},
					// Board scan (default)
					{
						Type: "Sequence", Name: "ScanPath",
						Children: []evolution.SerializableNode{
							{
								Type:     "ChainAction",
								Name:     "llm_call:Scan the full Kanban board: count cards per column, detect stale items, identify bottlenecks, check for cards ready for next phase. Produce a board health report with recommendations.",
								Metadata: map[string]any{"max_tokens": float64(8)},
							},
						},
					},
				},
			},
			// DoD gate
			{
				Type:     "ChainAction",
				Name:     "llm_call:Verify Definition of Done: all checkboxes [x], QA report PASS, description reflects implementation. Flag any DoD violations.",
				Metadata: map[string]any{"max_tokens": float64(5)},
			},
			{Type: "Action", Name: "ReflectOnOutcome"},
			{Type: "Selector", Name: "OutcomeSelector", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "WasSuccessful"},
				{Type: "ChainAction", Name: "llm_call:Kanban operation failed. Verify board is accessible, check column names, validate card format.", Metadata: map[string]any{"max_tokens": float64(4)}},
			}},
		},
	}
}

// KanbanAutoPilotTree automatically processes cards through the full pipeline.
// It scans for dispatchable cards and moves them forward.
func KanbanAutoPilotTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "AutoPilot_Main",
		Children: []evolution.SerializableNode{
			{Type: "Sequence", Name: "PreGate", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "ValidateInput"},
				{Type: "Action", Name: "SetupDefaultTools"},
			}},
			{
				Type: "ChainAction",
				Name: "llm_call:Run the Kanban autopilot: 1) Scan TODO → dispatch refiner for unrefined cards. 2) Scan APPROVED → dispatch developer. 3) Scan QA → dispatch QA agent. 4) Scan IN PROGRESS → check for stale cards (>2 days idle). 5) For each card processed, validate the column gate before moving. 6) Report: cards processed, movements, issues found.",
				Metadata: map[string]any{
					"max_tokens": float64(12),
					"system_msg": "You are a Kanban autopilot. Keep cards flowing through the 10-column pipeline automatically.",
				},
			},
			// Quality: verify proper transitions
			{
				Type:     "ChainAction",
				Name:     "llm_call:Audit card transitions: verify no card skipped a column, no unauthorized transitions (BACKLOG→TODO, REFINED→APPROVED, REVIEW→DONE require human approval), all cards have proper gates met.",
				Metadata: map[string]any{"max_tokens": float64(6)},
			},
			{Type: "Action", Name: "ReflectOnOutcome"},
			{Type: "Selector", Name: "OutcomeSelector", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "WasSuccessful"},
				{Type: "ChainAction", Name: "llm_call:Autopilot issue. Check board connectivity, agent availability, card state.", Metadata: map[string]any{"max_tokens": float64(4)}},
			}},
		},
	}
}
