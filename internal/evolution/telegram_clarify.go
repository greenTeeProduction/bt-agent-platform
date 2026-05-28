package evolution

// TelegramClarifyTree returns a behavior tree that validates Telegram
// responses use the clarify() tool for questions. Used as a quality gate
// in the merged tree's Telegram path or standalone via bt_delegate_to_tree.
//
// Tree structure (13 nodes):
//
//	Sequence: TelegramClarify
//	  ├── PreGate (Sequence)
//	  │     ├── Condition: IsTelegram       → skip if not on Telegram
//	  │     └── Condition: HasQuestion      → skip if no question in response
//	  ├── StrategyRouter (Selector)
//	  │     ├── Path: ValidateClarifyUsed   → checks clarify() was called
//	  │     └── Path: SuggestClarifyFix     → fallback: report violation
//	  └── Reflect + Outcome
//
// Conditions and actions are registered in cmd/bt-agent/main.go
// (engine cannot be imported here due to circular dependency).
func TelegramClarifyTree() *SerializableNode {
	return &SerializableNode{
		Type: "Sequence",
		Name: "TelegramClarify",
		Children: []SerializableNode{
			// PreGate: only validate when on Telegram AND asking a question
			{
				Type: "Sequence",
				Name: "PreGate",
				Children: []SerializableNode{
					{
						Type: "Condition",
						Name: "IsTelegram",
						Metadata: map[string]any{
							"description": "Check if current messaging platform is Telegram",
						},
					},
					{
						Type: "Condition",
						Name: "HasQuestion",
						Metadata: map[string]any{
							"description": "Check if the response contains a question to the user",
						},
					},
				},
			},
			// StrategyRouter: validate or report violation
			{
				Type: "Selector",
				Name: "StrategyRouter",
				Children: []SerializableNode{
					// Path 1: Happy path — clarify was used correctly
					{
						Type: "Sequence",
						Name: "ClarifyUsedPath",
						Children: []SerializableNode{
							{
								Type: "Condition",
								Name: "IsClarifyUsed",
								Metadata: map[string]any{
									"description": "Check if clarify() tool with choices was called",
								},
							},
							{
								Type: "Action",
								Name: "MarkClarifyOK",
								Metadata: map[string]any{
									"description": "Response passed clarify quality gate",
								},
							},
						},
					},
					// Path 2: Fallback — clarify violation detected
					{
						Type: "Sequence",
						Name: "ClarifyViolationPath",
						Children: []SerializableNode{
							{
								Type: "Action",
								Name: "ReportClarifyViolation",
								Metadata: map[string]any{
									"description": "Report: question asked without clarify() tool",
									"max_tokens":  float64(400),
								},
							},
							{
								Type: "Action",
								Name: "SuggestFix",
								Metadata: map[string]any{
									"description": "Suggest using clarify(question=..., choices=[...]) instead",
									"max_tokens":  float64(200),
								},
							},
						},
					},
				},
			},
			// Reflect + outcome
			{
				Type: "Action",
				Name: "ReflectOnOutcome",
			},
			{
				Type: "Selector",
				Name: "OutcomeSelector",
				Children: []SerializableNode{
					{
						Type: "Condition",
						Name: "WasSuccessful",
					},
					{
						Type: "Action",
						Name: "UpdateBehaviorTree",
					},
				},
			},
		},
	}
}

