package evolution

// Composable tree builders — decompose the "god node" DefaultTree into
// reusable primitives. Every BT tree follows the same 5-section pattern:
//   PreGate → StrategyRouter → ReflectOnOutcome → OutcomeSelector → AdaptOnFailure
//
// These helpers let domain trees compose from shared building blocks
// instead of repeating nested SerializableNode struct literals.

// NewTree creates a top-level Sequence node with the given children.
func NewTree(name string, children ...SerializableNode) *SerializableNode {
	return &SerializableNode{
		Type:     "Sequence",
		Name:     name,
		Children: children,
	}
}

// NewPreGate creates a validation/setup gate (Sequence of conditions + actions).
// Typical use: ValidateInput, CheckPrerequisites, SetupTools.
func NewPreGate(children ...SerializableNode) SerializableNode {
	return SerializableNode{
		Type:     "Sequence",
		Name:     "PreGate",
		Children: children,
	}
}

// NewAction creates a leaf Action node.
func NewAction(name, description string) SerializableNode {
	return SerializableNode{
		Type:        "Action",
		Name:        name,
		Description: description,
	}
}

// NewCondition creates a leaf Condition node.
func NewCondition(name, description string) SerializableNode {
	return SerializableNode{
		Type:        "Condition",
		Name:        name,
		Description: description,
	}
}

// NewChainAction creates a ChainAction node with LLM chain configuration.
func NewChainAction(prompt string, maxTokens int) SerializableNode {
	return SerializableNode{
		Type: "ChainAction",
		Name: prompt,
		Metadata: map[string]any{
			"max_tokens": float64(maxTokens),
		},
	}
}

// NewStrategy creates a strategy path inside a StrategyRouter.
// Each strategy is a Sequence: Condition (gate) + Actions (execution).
func NewStrategy(name string, children ...SerializableNode) SerializableNode {
	return SerializableNode{
		Type:     "Sequence",
		Name:     name,
		Children: children,
	}
}

// NewStrategyRouter creates a Selector that tries each strategy path in order.
func NewStrategyRouter(strategies ...SerializableNode) SerializableNode {
	return SerializableNode{
		Type:     "Selector",
		Name:     "StrategyRouter",
		Children: strategies,
	}
}

// NewDecisionTree creates a deterministic branch selector. The runtime reads
// Metadata["key"] from Blackboard.ChainState and runs the first child whose
// metadata match/matches/branch equals that value. Use Metadata["default"] or a
// child with Metadata["default"] = true for fallback routing.
func NewDecisionTree(name, key string, children ...SerializableNode) SerializableNode {
	return SerializableNode{
		Type:     "DecisionTree",
		Name:     name,
		Children: children,
		Metadata: map[string]any{
			"key": key,
		},
	}
}

// NewOutcomeSelector creates a post-execution Selector: success check → self-correct → escalate.
func NewOutcomeSelector(children ...SerializableNode) SerializableNode {
	return SerializableNode{
		Type:     "Selector",
		Name:     "OutcomeSelector",
		Children: children,
	}
}

// NewReflect creates a standard ReflectOnOutcome action.
func NewReflect() SerializableNode {
	return NewAction("ReflectOnOutcome", "Generate reflection and validate output quality")
}

// NewAdapt creates a standard UpdateBehaviorTree action.
func NewAdapt() SerializableNode {
	return NewAction("UpdateBehaviorTree", "Adapt tree on 3+ consecutive failures")
}

// NewEscalate creates a standard EscalateToDeepSeek action.
func NewEscalate() SerializableNode {
	return NewAction("EscalateToDeepSeek", "Escalate to external LLM for difficult tasks")
}

// NewSelfCorrect creates a standard self-correction ChainAction.
func NewSelfCorrect(maxTokens int) SerializableNode {
	return NewChainAction(
		"llm_call:Self-correct the previous task. Task: {{.Task}}. Fix errors and produce a correct answer.",
		maxTokens,
	)
}

// NewDefaultOutcomeSelector creates the standard outcome selector used by most trees:
// WasSuccessful → SelfCorrect → Escalate.
func NewDefaultOutcomeSelector(maxTokens int) SerializableNode {
	return NewOutcomeSelector(
		NewCondition("WasSuccessful", "Exit if task succeeded and output is valid"),
		NewSelfCorrect(maxTokens),
		NewEscalate(),
	)
}
