package evolution

// FusionDeliberationTree reproduces OpenRouter Fusion as a native BT:
// pre-gate setup, conditional multi-model deliberation, direct fallback,
// reflection, outcome handling, and adaptation.
func FusionDeliberationTree() *SerializableNode {
	return NewTree("FusionDeliberation",
		NewPreGate(
			NewCondition("ValidateInput", "Task must be non-empty"),
			NewAction("AssignComplexity", "Classify task complexity"),
			NewAction("SetupDefaultTools", "Attach available web/search/fetch tools"),
		),
		NewStrategyRouter(
			NewStrategy("FusionPath",
				NewCondition("ShouldUseFusion", "Only deliberate when multiple perspectives are valuable"),
				SerializableNode{
					Type: "ChainAction",
					Name: "fusion:{{.Task}}",
					Metadata: map[string]any{
						"params": map[string]any{
							"max_tool_calls": "8",
						},
					},
				},
			),
			NewStrategy("DirectPath",
				NewAction("MarkFusionSkipped", "Record that fusion was not needed"),
				NewChainAction("llm_call:{{.Task}}", 4096),
			),
		),
		NewReflect(),
		NewDefaultOutcomeSelector(4096),
		NewAdapt(),
	)
}
