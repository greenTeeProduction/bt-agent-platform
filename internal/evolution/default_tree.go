package evolution

// DefaultTree returns a generic 17-node behavior tree with 5 strategy paths:
//   - PreGate: input validation
//   - KnowledgePath: knowledge graph lookup
//   - CachePath: cached result reuse
//   - ExecutionPath: default LLM chain action
//   - OutcomeSelector: self-correction + escalation
//
// This is the baseline tree used by bt-agent when no domain-specific
// tree is selected. It resides here (not in mutate.go) because any
// structural change to it breaks tests across 7+ packages.
func DefaultTree() *SerializableNode {
	return &SerializableNode{
		Type: "Sequence",
		Name: "MainSequence",
		Children: []SerializableNode{
		{
			Type: "Sequence",
			Name: "PreGate",
			Children: []SerializableNode{
				{Type: "Condition", Name: "ValidateInput", Description: "Check input is non-empty"},
				{Type: "Condition", Name: "CheckPrerequisites", Description: "Verify capability"},
				{Type: "Action", Name: "SetupDefaultTools", Description: "Populate bb.ChainTools with standard tools"},
			},
		},
			{
				Type: "Selector",
				Name: "StrategyRouter",
				Children: []SerializableNode{
					{
						Type: "Sequence",
						Name: "KnowledgePath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "CheckKnowledgeGap", Description: "Detect if task needs external knowledge"},
							{Type: "Action", Name: "QueryKG", Description: "Search knowledge graph"},
							{Type: "Action", Name: "ApplyKnowledge", Description: "Enrich task with KG results"},
						},
					},
					{
						Type: "Sequence",
						Name: "CachePath",
						Children: []SerializableNode{
							{Type: "Condition", Name: "CheckCache", Description: "Look up cached result"},
							{Type: "Action", Name: "UseCachedResult", Description: "Return cached result"},
						},
					},
					{
						Type: "Sequence",
						Name: "ExecutionPath",
						Children: []SerializableNode{
							{
								Type: "ChainAction",
								Name: "llm_call:Complete this task: {{.Task}}. Use available tools. Think step by step and provide a thorough answer.",
								Metadata: map[string]any{"max_tokens": float64(2048)},
							},
						},
					},
				},
			},
			{
				Type: "Action",
				Name: "ReflectOnOutcome",
				Description: "Generate reflection and validate output quality",
			},
			{
				Type: "Selector",
				Name: "OutcomeSelector",
				Children: []SerializableNode{
					{Type: "Condition", Name: "WasSuccessful", Description: "Exit if task succeeded and output is valid"},
					{
						Type: "ChainAction",
						Name: "llm_call:Self-correct the previous task. Task: {{.Task}}. Fix errors and produce a correct answer.",
						Metadata: map[string]any{"max_tokens": float64(2048)},
					},
					{Type: "Action", Name: "EscalateToDeepSeek", Description: "Escalate to external LLM for difficult tasks"},
				},
			},
			{
				Type: "Action",
				Name: "UpdateBehaviorTree",
				Description: "Adapt tree on 3+ consecutive failures",
			},
		},
	}
}
