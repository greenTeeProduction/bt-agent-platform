package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

// ReliabilitySpec configures node-level timeout, retry, and circuit-breaker behavior.
type ReliabilitySpec struct {
	TimeoutMs      int64
	MaxRetries     int
	CBThreshold    int
	CBCooldownSecs int
	Graceful       bool // attach Selector fallbacks for common error classes
}

// Default specs per block kind
var (
	SpecPreGate = ReliabilitySpec{
		TimeoutMs:  30_000,
		MaxRetries: 1,
		Graceful:   true,
	}
	SpecToolExecution = ReliabilitySpec{
		TimeoutMs:      300_000, // 5 min for agent+tools
		MaxRetries:     2,
		CBThreshold:    3,
		CBCooldownSecs: 120,
		Graceful:       true,
	}
	SpecErrorHandling = ReliabilitySpec{
		TimeoutMs:      180_000,
		MaxRetries:     1,
		CBThreshold:    5,
		CBCooldownSecs: 60,
		Graceful:       true,
	}

	SpecPlan = ReliabilitySpec{
		TimeoutMs:  60_000,
		MaxRetries: 1,
		Graceful:   true,
	}
	SpecRAGGate = ReliabilitySpec{
		TimeoutMs:  90_000,
		MaxRetries: 2,
		Graceful:   true,
	}
	SpecClarifyGate = ReliabilitySpec{
		TimeoutMs:  30_000,
		MaxRetries: 0,
		Graceful:   true,
	}
	SpecQualityGate = ReliabilitySpec{
		TimeoutMs:  30_000,
		MaxRetries: 1,
		Graceful:   true,
	}
	SpecReflect = ReliabilitySpec{
		TimeoutMs:  60_000,
		MaxRetries: 1,
		Graceful:   true,
	}
)

// WrapReliable wraps a subtree with Timeout → Retry → CircuitBreaker (inner to outer)
// and optional graceful Selector fallbacks for timeout/transient/validation/circuit-open.
func WrapReliable(blockName string, child evolution.SerializableNode, spec ReliabilitySpec) evolution.SerializableNode {
	inner := child

	if spec.CBThreshold > 0 {
		inner = evolution.SerializableNode{
			Type:       "CircuitBreaker",
			Name:       blockName + "_CB",
			MaxRetries: spec.CBThreshold,
			Metadata: map[string]any{
				"cooldown_secs": float64(spec.CBCooldownSecs),
			},
			Children: []evolution.SerializableNode{inner},
		}
	}
	if spec.MaxRetries > 0 {
		inner = evolution.SerializableNode{
			Type:       "Retry",
			Name:       blockName + "_Retry",
			MaxRetries: spec.MaxRetries,
			Children:   []evolution.SerializableNode{inner},
		}
	}
	if spec.TimeoutMs > 0 {
		inner = evolution.SerializableNode{
			Type:      "Timeout",
			Name:      blockName + "_Timeout",
			TimeoutMs: spec.TimeoutMs,
			Children:  []evolution.SerializableNode{inner},
		}
	}

	if !spec.Graceful {
		return evolution.SerializableNode{
			Type:     "Sequence",
			Name:     blockName + "_Reliable",
			Children: []evolution.SerializableNode{inner},
		}
	}

	return evolution.SerializableNode{
		Type: "Selector",
		Name: blockName + "_Reliable",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence",
				Name: blockName + "_Primary",
				Children: []evolution.SerializableNode{
					{
						Type: "Action",
						Name: "SetActiveCircuit",
						Metadata: map[string]any{
							"pending_cb": blockName + "_CB",
						},
					},
					{Type: "Condition", Name: "CircuitAllows", Description: "Circuit closed or half-open"},
					inner,
				},
			},
			seqFallback(blockName+"_OnCircuitOpen",
				cond("CircuitOpen", "Circuit breaker open"),
				act("HandleCircuitOpen", "Degrade gracefully when circuit is open"),
			),
			seqFallback(blockName+"_OnTimeout",
				cond("LastErrorIsTimeout", "Previous node timed out"),
				act("HandleTimeoutError", "Preserve partial result after timeout"),
			),
			seqFallback(blockName+"_OnTransient",
				cond("LastErrorIsTransient", "Network/LLM transient error"),
				act("HandleTransientError", "Degrade without hard failure"),
			),
			seqFallback(blockName+"_OnValidation",
				cond("LastErrorIsValidation", "Input/auth validation error"),
				act("HandleValidationError", "Fail fast with clear validation message"),
			),
		},
	}
}

func seqFallback(name string, children ...evolution.SerializableNode) evolution.SerializableNode {
	return evolution.SerializableNode{Type: "Sequence", Name: name, Children: children}
}

func cond(name, desc string) evolution.SerializableNode {
	return evolution.SerializableNode{Type: "Condition", Name: name, Description: desc}
}

func act(name, desc string) evolution.SerializableNode {
	return evolution.SerializableNode{Type: "Action", Name: name, Description: desc}
}

// ApplyReliability wraps a block's tree with the given spec (mutates copy on register).
func ApplyReliability(b *Block, spec ReliabilitySpec) {
	if b == nil || b.Tree == nil {
		return
	}
	wrapped := WrapReliable(b.Name, *cloneTree(b.Tree), spec)
	b.Tree = &wrapped
}
