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
		Type: "Sequence", Name: "TaskCreator_Main", Description: "Create a DoR-ready card in BACKLOG from a raw task request",
		Children: []evolution.SerializableNode{
			{Type: "Sequence", Name: "PreGate", Description: "Input validation gate that must pass before any board work runs", Children: []evolution.SerializableNode{
				cond("ValidateInput", "Non-empty task"),
				{Type: "Action", Name: "SetupDefaultTools", Description: "Populate the default toolset for board operations"},
			}},
			{
				Type:        "ChainAction",
				Name:        "llm_call:Create a new Focalboard task card: 1) Analyze the task description for completeness. 2) Generate an actionable title. 3) Write clear acceptance criteria as checkboxes (- [ ]). 4) Set priority (critical/high/medium/low). 5) Determine the AQAL quadrant (q-i/q-it/q-we/q-its/q-all). 6) Create the card in the BACKLOG column. 7) Report: card ID, title, priority, quadrant.",
				Description: "LLM step that drafts the card and files it in BACKLOG",
				Metadata: map[string]any{
					"max_tokens": float64(512),
					"system_msg": "You are a task creator for a 10-column Focalboard Kanban. Create well-structured cards with DoR-ready descriptions.",
				},
			},
			{Type: "Action", Name: "ReflectOnOutcome", Description: "Record what worked and what to improve for the next run"},
			{Type: "Selector", Name: "OutcomeSelector", Description: "Confirm success or fall through to LLM failure diagnosis", Children: []evolution.SerializableNode{
				cond("WasSuccessful", "Prior action reported success"),
				{Type: "ChainAction", Name: "llm_call:Card creation failed. Check: is the board accessible? Is the column name correct? Is the card format valid?", Description: "Diagnose why card creation failed", Metadata: map[string]any{"max_tokens": float64(128)}},
			}},
		},
	}
}

// KanbanRefinerTree refines TODO cards into REFINED state with DoR gate.
func KanbanRefinerTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "Refiner_Main", Description: "Refine a TODO card into REFINED with a complete DoR",
		Children: []evolution.SerializableNode{
			{Type: "Sequence", Name: "PreGate", Description: "Input validation gate that must pass before any board work runs", Children: []evolution.SerializableNode{
				cond("ValidateInput", "Non-empty task"),
				cond("IsKanbanTask", "Has card ID or task reference"),
			}},
			{
				Type:        "ChainAction",
				Name:        "llm_call:Refine a TODO card for Focalboard: 1) Read the card's current description and acceptance criteria. 2) Expand the description with implementation context. 3) Ensure acceptance criteria are specific and testable. 4) Add implementation notes and architecture constraints. 5) Verify DoR gate: description complete, AC present, priority set, quadrant set. 6) Move card from TODO → PLANNING → REFINED. 7) Report: card ID, refinement summary, DoR status.",
				Description: "LLM step that expands the card and walks it TODO→PLANNING→REFINED",
				Metadata: map[string]any{
					"max_tokens": float64(640),
					"system_msg": "You are a task refiner. Transform raw TODO cards into well-specified REFINED cards with complete DoR.",
				},
			},
			{Type: "Action", Name: "ReflectOnOutcome", Description: "Record what worked and what to improve for the next run"},
			{Type: "Selector", Name: "OutcomeSelector", Description: "Confirm success or fall through to LLM failure diagnosis", Children: []evolution.SerializableNode{
				cond("WasSuccessful", "Prior action reported success"),
				{Type: "ChainAction", Name: "llm_call:Refinement failed. Possible issues: card not in TODO column, board permissions, invalid card format. Diagnose and retry.", Description: "Diagnose why refinement failed and retry", Metadata: map[string]any{"max_tokens": float64(128)}},
			}},
		},
	}
}

// KanbanQATree validates cards moving from QA to REVIEW.
func KanbanQATree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "QA_Main", Description: "Validate a QA card and move it to REVIEW or back to IN PROGRESS",
		Children: []evolution.SerializableNode{
			{Type: "Sequence", Name: "PreGate", Description: "Input validation gate that must pass before any board work runs", Children: []evolution.SerializableNode{
				cond("ValidateInput", "Non-empty task"),
				cond("IsKanbanTask", "Has card ID or task reference"),
			}},
			{
				Type:        "ChainAction",
				Name:        "llm_call:Run QA validation on a Focalboard card: 1) Check all acceptance criteria are [x] completed. 2) Verify the implementation matches the description. 3) Run quality checks: code style, security, performance concerns. 4) Check for regressions or side effects. 5) Generate QA report with PASS/FAIL status. 6) If PASS: move card from QA → REVIEW. If FAIL: move back to IN PROGRESS with specific issues. 7) Report: card ID, QA result, issues found.",
				Description: "LLM step that runs the QA checklist and moves the card on PASS/FAIL",
				Metadata: map[string]any{
					"max_tokens": float64(640),
					"system_msg": "You are a QA agent. Validate that implementations meet the spec. Be thorough but fair.",
				},
			},
			{Type: "Action", Name: "ReflectOnOutcome", Description: "Record what worked and what to improve for the next run"},
			{Type: "Selector", Name: "OutcomeSelector", Description: "Confirm success or fall through to LLM failure diagnosis", Children: []evolution.SerializableNode{
				cond("WasSuccessful", "Prior action reported success"),
				{Type: "ChainAction", Name: "llm_call:QA check failed to execute. Verify board access and card state. Retry with corrected approach.", Description: "Diagnose why the QA check could not run", Metadata: map[string]any{"max_tokens": float64(128)}},
			}},
		},
	}
}

// KanbanBoardMonitorTree scans the board for issues: stale cards, bottlenecks, overdue items.
func KanbanBoardMonitorTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "BoardMonitor_Main", Description: "Scan the board for stale cards, bottlenecks, and dispatchable work",
		Children: []evolution.SerializableNode{
			{Type: "Sequence", Name: "PreGate", Description: "Input validation gate that must pass before any board work runs", Children: []evolution.SerializableNode{
				cond("ValidateInput", "Non-empty task"),
				{Type: "Action", Name: "SetupDefaultTools", Description: "Populate the default toolset for board operations"},
			}},
			{
				Type: "Selector", Name: "MonitorRouter", Description: "Route to stale scan, dispatch, or standup based on the task", Children: []evolution.SerializableNode{
					// Check for stale cards
					{
						Type: "Sequence", Name: "StaleCheck", Description: "Flag stale and stuck cards across the board",
						Children: []evolution.SerializableNode{
							cond("IsBoardCheck", "Task mentions scan, check, monitor, or stale"),
							{
								Type:        "ChainAction",
								Name:        "llm_call:Scan the Focalboard Kanban for issues: 1) IN PROGRESS cards idle > 2 days → flag as stale. 2) TODO cards > 1 week without refinement → flag for refiner. 3) REVIEW cards waiting > 3 days → notify for human review. 4) APPROVED cards without developer assignment → flag. 5) Bottleneck detection: which column has the most cards? 6) Report: stale count, bottlenecks, recommendations.",
								Description: "LLM step that scans columns for stale cards and bottlenecks",
								Metadata: map[string]any{
									"max_tokens": float64(384),
									"system_msg": "You are a Kanban board monitor. Find stuck work before it becomes a problem.",
								},
							},
						},
					},
					// Dispatch next ready card
					{
						Type: "Sequence", Name: "DispatchPath", Description: "Dispatch the next ready card to its pipeline agent",
						Children: []evolution.SerializableNode{
							cond("NeedsDispatch", "Task mentions dispatch, next, assign, or start"),
							{
								Type:        "ChainAction",
								Name:        "llm_call:Find the next dispatchable card: 1) Scan TODO column → dispatch refiner. 2) Scan APPROVED column → dispatch developer. 3) Scan QA column → dispatch QA agent. 4) For each: verify the card meets the column gate before moving. 5) Report: cards dispatched, agent assignments.",
								Description: "LLM step that finds dispatchable cards and assigns agents",
								Metadata: map[string]any{
									"max_tokens": float64(320),
									"system_msg": "You are a task dispatcher. Move cards to the next agent in the workflow.",
								},
							},
						},
					},
					// Daily standup
					{
						Type: "Sequence", Name: "StandupPath", Description: "Produce the daily standup summary",
						Children: []evolution.SerializableNode{
							cond("IsStandup", "Task mentions standup, daily, or status"),
							{
								Type:        "ChainAction",
								Name:        "llm_call:Generate a daily standup summary: 1) Cards completed (moved to DONE). 2) Cards in progress with status. 3) Blocked cards and blockers. 4) Upcoming cards ready for review. 5) Velocity: cards completed this week vs last week. Format as concise standup report.",
								Description: "LLM step that generates the standup report",
								Metadata: map[string]any{
									"max_tokens": float64(384),
									"system_msg": "You are a standup bot. Produce clear, actionable status reports.",
								},
							},
						},
					},
				},
			},
			{Type: "Action", Name: "ReflectOnOutcome", Description: "Record what worked and what to improve for the next run"},
		},
	}
}

// KanbanWorkflowTree orchestrates the full Kanban pipeline: create → refine → approve → build → QA → review.
func KanbanWorkflowTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "KanbanWorkflow_Main", Description: "Orchestrate create, refine, QA, and scan operations across the pipeline",
		Children: []evolution.SerializableNode{
			{Type: "Sequence", Name: "PreGate", Description: "Input validation gate that must pass before any board work runs", Children: []evolution.SerializableNode{
				cond("ValidateInput", "Non-empty task"),
				{Type: "Action", Name: "SetupDefaultTools", Description: "Populate the default toolset for board operations"},
			}},
			{
				Type: "Selector", Name: "WorkflowRouter", Description: "Route to create, refine, QA, or the default board scan", Children: []evolution.SerializableNode{
					// Create new card
					{
						Type: "Sequence", Name: "CreatePath", Description: "Create a new DoR-ready card",
						Children: []evolution.SerializableNode{
							cond("IsCreateTask", "Task mentions create, new card, add"),
							{
								Type:        "ChainAction",
								Name:        "llm_call:Create a new task card following DoR standards: actionable title, description, acceptance criteria (- [ ] items), priority (critical/high/medium/low), AQAL quadrant (q-i/q-it/q-we/q-its/q-all). Place in BACKLOG column.",
								Description: "LLM step that creates the card in BACKLOG",
								Metadata:    map[string]any{"max_tokens": float64(320)},
							},
						},
					},
					// Refine card
					{
						Type: "Sequence", Name: "RefinePath", Description: "Refine a TODO card through the DoR gate",
						Children: []evolution.SerializableNode{
							cond("IsRefinement", "Task mentions refine, expand, detail"),
							{
								Type:        "ChainAction",
								Name:        "llm_call:Refine a TODO card: expand description, add implementation notes, ensure AC are testable, add architecture constraints. Move TODO→PLANNING→REFINED. Verify DoR gate passes.",
								Description: "LLM step that refines the card to REFINED",
								Metadata:    map[string]any{"max_tokens": float64(320)},
							},
						},
					},
					// QA check
					{
						Type: "Sequence", Name: "QAPath", Description: "Run QA on a card and move it by result",
						Children: []evolution.SerializableNode{
							cond("IsQA", "Task mentions qa, test, validate, verify"),
							{
								Type:        "ChainAction",
								Name:        "llm_call:Run QA on a card: check all AC are [x], verify implementation, check for regressions, generate PASS/FAIL report. PASS → move QA→REVIEW. FAIL → move back to IN PROGRESS with issues.",
								Description: "LLM step that runs QA and moves the card on PASS/FAIL",
								Metadata:    map[string]any{"max_tokens": float64(320)},
							},
						},
					},
					// Board scan (default)
					{
						Type: "Sequence", Name: "ScanPath", Description: "Default board health scan when no specific operation matches",
						Children: []evolution.SerializableNode{
							{
								Type:        "ChainAction",
								Name:        "llm_call:Scan the full Kanban board: count cards per column, detect stale items, identify bottlenecks, check for cards ready for next phase. Produce a board health report with recommendations.",
								Description: "LLM step that produces a board health report",
								Metadata:    map[string]any{"max_tokens": float64(384)},
							},
						},
					},
				},
			},
			// DoD gate
			{
				Type:        "ChainAction",
				Name:        "llm_call:Verify Definition of Done: all checkboxes [x], QA report PASS, description reflects implementation. Flag any DoD violations.",
				Description: "LLM step that verifies the Definition of Done before closing",
				Metadata:    map[string]any{"max_tokens": float64(192)},
			},
			{Type: "Action", Name: "ReflectOnOutcome", Description: "Record what worked and what to improve for the next run"},
			{Type: "Selector", Name: "OutcomeSelector", Description: "Confirm success or fall through to LLM failure diagnosis", Children: []evolution.SerializableNode{
				cond("WasSuccessful", "Prior action reported success"),
				{Type: "ChainAction", Name: "llm_call:Kanban operation failed. Verify board is accessible, check column names, validate card format.", Description: "Diagnose why the Kanban operation failed", Metadata: map[string]any{"max_tokens": float64(128)}},
			}},
		},
	}
}

// KanbanAutoPilotTree automatically processes cards through the full pipeline.
// It scans for dispatchable cards and moves them forward.
func KanbanAutoPilotTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "AutoPilot_Main", Description: "Automatically advance dispatchable cards through the pipeline",
		Children: []evolution.SerializableNode{
			{Type: "Sequence", Name: "PreGate", Description: "Input validation gate that must pass before any board work runs", Children: []evolution.SerializableNode{
				cond("ValidateInput", "Non-empty task"),
				{Type: "Action", Name: "SetupDefaultTools", Description: "Populate the default toolset for board operations"},
			}},
			{
				Type:        "ChainAction",
				Name:        "llm_call:Run the Kanban autopilot: 1) Scan TODO → dispatch refiner for unrefined cards. 2) Scan APPROVED → dispatch developer. 3) Scan QA → dispatch QA agent. 4) Scan IN PROGRESS → check for stale cards (>2 days idle). 5) For each card processed, validate the column gate before moving. 6) Report: cards processed, movements, issues found.",
				Description: "LLM step that scans columns and moves ready cards forward",
				Metadata: map[string]any{
					"max_tokens": float64(640),
					"system_msg": "You are a Kanban autopilot. Keep cards flowing through the 10-column pipeline automatically.",
				},
			},
			// Quality: verify proper transitions
			{
				Type:        "ChainAction",
				Name:        "llm_call:Audit card transitions: verify no card skipped a column, no unauthorized transitions (BACKLOG→TODO, REFINED→APPROVED, REVIEW→DONE require human approval), all cards have proper gates met.",
				Description: "LLM step that audits transitions for skipped columns and missing gates",
				Metadata:    map[string]any{"max_tokens": float64(320)},
			},
			{Type: "Action", Name: "ReflectOnOutcome", Description: "Record what worked and what to improve for the next run"},
			{Type: "Selector", Name: "OutcomeSelector", Description: "Confirm success or fall through to LLM failure diagnosis", Children: []evolution.SerializableNode{
				cond("WasSuccessful", "Prior action reported success"),
				{Type: "ChainAction", Name: "llm_call:Autopilot issue. Check board connectivity, agent availability, card state.", Description: "Diagnose autopilot connectivity or card-state issues", Metadata: map[string]any{"max_tokens": float64(128)}},
			}},
		},
	}
}
