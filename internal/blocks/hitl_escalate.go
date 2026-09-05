package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

// HITLEscalateBlock escalates when failures accumulate or approval expired.
func HITLEscalateBlock() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "Sequence",
		Name:        "HITLEscalate",
		Description: "Escalate to operator on repeated HITL failure",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "PersistentFailures", Description: "Failure threshold met"},
			{Type: "Action", Name: "EscalateToOperator", Description: "Notify operator"},
			{Type: "Action", Name: "EscalateHITL", Description: "Mark HITL request escalated"},
		},
	}
}
