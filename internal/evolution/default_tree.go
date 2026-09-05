package evolution

// DefaultTree returns a generic behavior tree built from composable primitives.
// Structure: PreGate → StrategyRouter → ReflectOnOutcome → OutcomeSelector → AdaptOnFailure
//
// Strategy paths:
//   - KnowledgePath: knowledge graph lookup
//   - CachePath: cached result reuse
//   - ExecutionPath: default LLM chain action
func DefaultTree() *SerializableNode {
	return NewTree("MainSequence",
		NewPreGate(
			NewCondition("ValidateInput", "Check input is non-empty"),
			NewCondition("CheckPrerequisites", "Verify capability"),
			NewAction("SetupDefaultTools", "Populate bb.ChainTools with standard tools"),
		),
		NewStrategyRouter(
			NewStrategy("KnowledgePath",
				NewCondition("CheckKnowledgeGap", "Detect if task needs external knowledge"),
				NewAction("QueryKG", "Search knowledge graph"),
				NewAction("ApplyKnowledge", "Enrich task with KG results"),
			),
			NewStrategy("CachePath",
				NewCondition("CheckCache", "Look up cached result"),
				NewAction("UseCachedResult", "Return cached result"),
			),
			NewStrategy("ExecutionPath",
				NewChainAction(
					"llm_call:Complete this task: {{.Task}}. Use available tools. Think step by step and provide a thorough answer.",
					2048,
				),
			),
		),
		NewReflect(),
		NewDefaultOutcomeSelector(2048),
		NewAdapt(),
	)
}
