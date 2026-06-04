package evolution

import (
	"encoding/json"
)

// EdgeType represents the type of relationship between behavior tree nodes.
type EdgeType string

const (
	EdgeChild       EdgeType = "child"        // Standard BT child relationship (default)
	EdgeFallback    EdgeType = "fallback"     // Fallback/alternative path
	EdgeGuard       EdgeType = "guard"        // Precondition guard; condition must pass before executing child
	EdgeEffect      EdgeType = "effect"       // Postcondition effect (writes to blackboard/world)
	EdgeInterrupt   EdgeType = "interrupt"    // Event-driven abort trigger
	EdgeDataflow    EdgeType = "dataflow"     // Blackboard variable read/write dependency
	EdgeQualityGate EdgeType = "quality_gate" // Validation gate that must pass
	EdgeRecovery    EdgeType = "recovery"     // Recovery/fallback path after failure
	EdgeApproval    EdgeType = "approval"     // Human approval required
	EdgeEvolvesFrom EdgeType = "evolves_from" // Evolution lineage metadata
	EdgeReuses      EdgeType = "reuses"       // Shared subtree reference
)

// TypedEdge represents a typed relationship between behavior tree nodes.
// This enables expressing preconditions, postconditions, side-effect classifications,
// and validating trees before execution.
type TypedEdge struct {
	Type       EdgeType          `json:"type,omitempty"`        // Edge type (default: child)
	ChildIndex int               `json:"child_index,omitempty"` // Index into Children array (-1 = all)
	Label      string            `json:"label,omitempty"`       // Human-readable label
	Condition  string            `json:"condition,omitempty"`   // Optional condition expression
	Effect     string            `json:"effect,omitempty"`      // Optional effect description
	Blackboard map[string]string `json:"blackboard,omitempty"`  // Blackboard key mappings
	Priority   int               `json:"priority,omitempty"`    // Ordering priority (lower = higher)
	Weight     float64           `json:"weight,omitempty"`      // Utility weight for scoring
}

// NodeValidationInfo contains metadata about a node for validation purposes.
type NodeValidationInfo struct {
	Type            string     // Node type (Sequence, Selector, Action, Condition, etc.)
	ActionName      string     // For Action nodes, the registered action name
	MaxDepth        int        // Max tree depth
	NodeCount       int        // Total node count
	ParallelWidth   int        // Max parallel branches
	MaxRetries      int        // Max retries configured
	TimeoutMs       int64      // Timeout configured
	SideEffectClass string     // "none", "local", "network", "filesystem", "destroy", "external"
	HasApprovalGate bool       // Has a HumanApprovalGate ancestor
	EdgeTypes       []EdgeType // Used edge types
	Errors          []string   // Validation errors
}

// Valid returns true if the tree passes all validation checks.
func (info *NodeValidationInfo) Valid() bool {
	return len(info.Errors) == 0
}

// KnownNodeTypes contains all valid behavior tree node types.
// This list is used by the verifier to validate tree schemas.
var KnownNodeTypes = map[string]bool{
	// Composite nodes
	"Sequence": true,
	"Selector": true,
	"Parallel": true,
	// Decorator nodes
	"Retry":          true,
	"RateLimit":      true,
	"Timeout":        true,
	"Budget":         true,
	"CircuitBreaker": true,
	"Inverter":       true,
	"Succeeder":      true,
	"Repeater":       true,
	"Runner":         true,
	// Leaf nodes
	"Action":    true,
	"Condition": true,
	// Chain nodes (langchaingo integration)
	"ChainAction": true,
	// Specialized nodes
	"StrategyRouter":   true,
	"Monitor":          true,
	"UtilitySelector":  true, // Plan #2
	"DecisionTree":     true, // Deterministic ChainState/source-based branching
	"PlannerNode":      true, // Plan #2 extension
	"ReactiveParallel": true, // Plan #3
	"AbortOnEvent":     true, // Plan #3
	// Domain-specific
	"HumanApprovalGate": true,
	"QualityGate":       true,
}

// ValidateEdge validates a single TypedEdge against the tree structure.
func ValidateEdge(edge TypedEdge, childrenCount int) []string {
	var errors []string

	// Validate child index
	if edge.ChildIndex != -1 {
		if edge.ChildIndex < 0 || edge.ChildIndex >= childrenCount {
			errors = append(errors, "edge child_index out of range")
		}
	}

	// Validate edge-type specific constraints
	switch edge.Type {
	case EdgeGuard:
		if edge.Condition == "" {
			errors = append(errors, "guard edges must have a condition")
		}
	case EdgeEffect:
		if edge.Effect == "" {
			errors = append(errors, "effect edges must have an effect description")
		}
	case EdgeInterrupt:
		if edge.Label == "" {
			errors = append(errors, "interrupt edges must have an event label")
		}
	case EdgeDataflow:
		if len(edge.Blackboard) == 0 {
			errors = append(errors, "dataflow edges must specify blackboard keys")
		}
	case EdgeQualityGate:
		if edge.Label == "" {
			errors = append(errors, "quality gate edges must have a validation label")
		}
	}

	return errors
}

// IsDefaultEdge returns true if this edge represents the default child relationship.
func (e TypedEdge) IsDefaultEdge() bool {
	return e.Type == EdgeChild || e.Type == ""
}

// GetChildIndex returns the effective child index for this edge.
// Returns -1 for "all children" and validates bounds.
func (e TypedEdge) GetChildIndex(childrenCount int) (int, bool) {
	if e.ChildIndex == -1 {
		return -1, true
	}
	if e.ChildIndex < 0 || e.ChildIndex >= childrenCount {
		return e.ChildIndex, false
	}
	return e.ChildIndex, true
}

// MarshalJSON handles the omitempty behavior for TypedEdge.
func (e TypedEdge) MarshalJSON() ([]byte, error) {
	// Define a local type to handle omitempty properly
	type Alias TypedEdge
	aux := &struct {
		Type       *EdgeType          `json:"type,omitempty"`
		ChildIndex *int               `json:"child_index,omitempty"`
		Label      *string            `json:"label,omitempty"`
		Condition  *string            `json:"condition,omitempty"`
		Effect     *string            `json:"effect,omitempty"`
		Blackboard *map[string]string `json:"blackboard,omitempty"`
		Priority   *int               `json:"priority,omitempty"`
		Weight     *float64           `json:"weight,omitempty"`
	}{
		Type:       (*EdgeType)(&e.Type),
		ChildIndex: &e.ChildIndex,
		Label:      &e.Label,
		Condition:  &e.Condition,
		Effect:     &e.Effect,
		Blackboard: &e.Blackboard,
		Priority:   &e.Priority,
		Weight:     &e.Weight,
	}
	return json.Marshal(aux)
}

// Validate performs basic validation on a SerializableNode tree.
// Returns nil if the tree is valid, or a list of validation errors.
// This is a lightweight check suitable for callers that don't import the engine package;
// it validates node types, edges, and cycles but not depth, node count, or parallel width.
func (n *SerializableNode) Validate() []string {
	var errors []string
	visited := make(map[string]bool)
	n.validateRecursive(&errors, visited)
	return errors
}

// validateRecursive is the internal recursive helper for Validate() with cycle detection.
func (n *SerializableNode) validateRecursive(errors *[]string, visited map[string]bool) {
	if n == nil {
		return
	}

	// Cycle detection
	if n.Name != "" {
		if visited[n.Name] {
			*errors = append(*errors, "node "+n.Name+": cycle detected — duplicate name in ancestry path")
			return
		}
		visited[n.Name] = true
		defer func() { visited[n.Name] = false }()
	}

	// Check node type
	if !KnownNodeTypes[n.Type] {
		*errors = append(*errors, "node "+n.Name+": unknown node type "+n.Type)
	}

	// Check edges
	for _, edge := range n.Edges {
		edgeErrs := ValidateEdge(edge, len(n.Children))
		for _, err := range edgeErrs {
			*errors = append(*errors, "node "+n.Name+": "+err)
		}
	}

	// Recursively validate children
	for i := range n.Children {
		n.Children[i].validateRecursive(errors, visited)
	}
}
