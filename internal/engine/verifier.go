package engine

import (
	"fmt"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// Default validation limits (per Plan #1 acceptance criteria).
const (
	DefaultMaxDepth    = 16
	DefaultMaxNodes    = 200
	DefaultMaxParallel = 10
	DefaultMaxRetries  = 5
)

// ValidateTreeFull performs a 5-stage validation on a SerializableNode tree.
// It is deliberately backward-compatible: trees without typed edges are valid
// if their legacy node/action/condition names are known to the runtime.
func ValidateTreeFull(tree *evolution.SerializableNode) *evolution.NodeValidationInfo {
	info := &evolution.NodeValidationInfo{
		Errors:    []string{},
		EdgeTypes: []evolution.EdgeType{},
	}
	if tree == nil {
		info.Errors = append(info.Errors, "tree is nil")
		return info
	}
	info.Type = tree.Type

	// Cycle detection: track visited node names on the current ancestry path.
	// A logical cycle exists when EdgeReuses references a sibling/ancestor subtree
	// that contains the referencing node, or when an action node's children
	// contain a node with its own name (self-referential subtree).
	walkValidate(tree, info, 1, false, make(map[string]bool))
	return info
}

func walkValidate(node *evolution.SerializableNode, info *evolution.NodeValidationInfo, depth int, hasApprovalGate bool, visitedNames map[string]bool) {
	if node == nil {
		info.Errors = append(info.Errors, "nil node")
		return
	}

	// Cycle check: if this node's name is already in the ancestry path, we have a cycle.
	// Empty-name nodes are allowed (anonymous inner nodes).
	if node.Name != "" {
		if visitedNames[node.Name] {
			info.Errors = append(info.Errors, fmt.Sprintf("node %q: cycle detected — node name appears twice in ancestry path at depth %d", node.Name, depth))
			return
		}
		visitedNames[node.Name] = true
		defer func() { visitedNames[node.Name] = false }()
	}

	info.NodeCount++
	if depth > info.MaxDepth {
		info.MaxDepth = depth
	}
	if node.Type == "Parallel" || node.Type == "ReactiveParallel" {
		if len(node.Children) > info.ParallelWidth {
			info.ParallelWidth = len(node.Children)
		}
	}
	if node.MaxRetries > info.MaxRetries {
		info.MaxRetries = node.MaxRetries
	}
	if node.TimeoutMs > info.TimeoutMs {
		info.TimeoutMs = node.TimeoutMs
	}
	if node.Type == "HumanApprovalGate" {
		hasApprovalGate = true
		info.HasApprovalGate = true
	}

	if !evolution.KnownNodeTypes[node.Type] {
		info.Errors = append(info.Errors, fmt.Sprintf("node %q: unknown node type %q", node.Name, node.Type))
	}

	switch node.Type {
	case "Action":
		info.ActionName = node.Name
		if !isKnownActionName(node.Name) {
			info.Errors = append(info.Errors, fmt.Sprintf("node %q: unknown action %q", node.Name, node.Name))
		}
	case "Condition":
		if !isKnownConditionName(node.Name) {
			info.Errors = append(info.Errors, fmt.Sprintf("node %q: unknown condition %q", node.Name, node.Name))
		}
	case "Retry", "Repeater":
		if node.MaxRetries <= 0 {
			info.Errors = append(info.Errors, fmt.Sprintf("node %q: %s requires max_retries > 0", node.Name, node.Type))
		}
	}

	if sec := sideEffectClass(node); sec != "" && sec != "none" {
		info.SideEffectClass = sec
		if (sec == "destroy" || sec == "external") && !hasApprovalGate {
			info.Errors = append(info.Errors, fmt.Sprintf("node %q: side_effect_class %q requires HumanApprovalGate ancestor", node.Name, sec))
		}
	}

	for _, edge := range node.Edges {
		for _, err := range evolution.ValidateEdge(edge, len(node.Children)) {
			info.Errors = append(info.Errors, fmt.Sprintf("node %q: %s", node.Name, err))
		}
		info.EdgeTypes = append(info.EdgeTypes, edge.Type)
	}

	for i := range node.Children {
		walkValidate(&node.Children[i], info, depth+1, hasApprovalGate, visitedNames)
	}

	if info.MaxDepth > DefaultMaxDepth {
		// Keep only one aggregate error for this structural limit.
		addUniqueError(info, fmt.Sprintf("max depth %d exceeds limit %d", info.MaxDepth, DefaultMaxDepth))
	}
	if info.NodeCount > DefaultMaxNodes {
		addUniqueError(info, fmt.Sprintf("node count %d exceeds limit %d", info.NodeCount, DefaultMaxNodes))
	}
	if info.ParallelWidth > DefaultMaxParallel {
		addUniqueError(info, fmt.Sprintf("parallel width %d exceeds limit %d", info.ParallelWidth, DefaultMaxParallel))
	}
	if info.MaxRetries > DefaultMaxRetries {
		addUniqueError(info, fmt.Sprintf("max retries %d exceeds limit %d", info.MaxRetries, DefaultMaxRetries))
	}
}

func sideEffectClass(node *evolution.SerializableNode) string {
	if node.Metadata == nil {
		return ""
	}
	if sec, ok := node.Metadata["side_effect_class"].(string); ok {
		return sec
	}
	return ""
}

func addUniqueError(info *evolution.NodeValidationInfo, msg string) {
	for _, existing := range info.Errors {
		if existing == msg {
			return
		}
	}
	info.Errors = append(info.Errors, msg)
}

// computeTreeMetrics is kept for existing tests and callers. It returns depth
// as edge-depth (a leaf is 0), plus aggregate node count, max parallel width,
// max retries, max timeout, and the first side-effect class on this subtree.
func computeTreeMetrics(node *evolution.SerializableNode, info *evolution.NodeValidationInfo) (maxDepth, nodeCount, parallelWidth, maxRetries int, timeoutMs int64, sideEffectClass string) {
	if node == nil {
		return 0, 0, 0, 0, 0, ""
	}
	nodeCount = 1
	if len(node.Children) > 0 {
		childDepth, childCount, childPar, childRetry, childTimeout := computeSubtreeMetrics(node.Children)
		maxDepth = childDepth + 1
		nodeCount += childCount
		parallelWidth = childPar
		maxRetries = childRetry
		timeoutMs = childTimeout
	}
	if node.Type == "Parallel" || node.Type == "ReactiveParallel" {
		if len(node.Children) > parallelWidth {
			parallelWidth = len(node.Children)
		}
	}
	if node.MaxRetries > maxRetries {
		maxRetries = node.MaxRetries
	}
	if node.TimeoutMs > timeoutMs {
		timeoutMs = node.TimeoutMs
	}
	sideEffectClass = sideEffectClassOfSubtree(node)
	return maxDepth, nodeCount, parallelWidth, maxRetries, timeoutMs, sideEffectClass
}

func computeSubtreeMetrics(children []evolution.SerializableNode) (maxDepth, nodeCount, parallelWidth, maxRetries int, maxTimeout int64) {
	for i := range children {
		cd, cc, cp, cr, ct, _ := computeTreeMetrics(&children[i], nil)
		if cd > maxDepth {
			maxDepth = cd
		}
		nodeCount += cc
		if cp > parallelWidth {
			parallelWidth = cp
		}
		if cr > maxRetries {
			maxRetries = cr
		}
		if ct > maxTimeout {
			maxTimeout = ct
		}
	}
	return maxDepth, nodeCount, parallelWidth, maxRetries, maxTimeout
}

func sideEffectClassOfSubtree(node *evolution.SerializableNode) string {
	if sec := sideEffectClass(node); sec != "" {
		return sec
	}
	for i := range node.Children {
		if sec := sideEffectClassOfSubtree(&node.Children[i]); sec != "" {
			return sec
		}
	}
	return ""
}
