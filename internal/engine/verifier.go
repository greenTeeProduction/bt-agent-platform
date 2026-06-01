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
// Returns a ValidationResult containing all errors and validation metadata.
func ValidateTreeFull(tree *evolution.SerializableNode) *evolution.NodeValidationInfo {
	info := &evolution.NodeValidationInfo{
		Type:      tree.Type,
		Errors:    []string{},
		EdgeTypes: []evolution.EdgeType{},
	}

	// Stage 1 — Schema validation
	// (JSON/YAML parsing already done by outer layers; structure is assumed valid here)

	// Stage 2 — Node Types
	if !evolution.KnownNodeTypes[tree.Type] {
		info.Errors = append(info.Errors, fmt.Sprintf("unknown node type: %s", tree.Type))
	}

	// Stage 3 — Action Names (for Action nodes)
	if tree.Type == "Action" {
		info.ActionName = tree.Name
		// Note: registry check happens at build time; here we just record
	}

	// Stage 4 — Structural Constraints
	info.MaxDepth, info.NodeCount, info.ParallelWidth, info.MaxRetries, info.TimeoutMs, info.SideEffectClass =
		computeTreeMetrics(tree, info)

	if info.MaxDepth > DefaultMaxDepth {
		info.Errors = append(info.Errors, fmt.Sprintf("max depth %d exceeds limit %d", info.MaxDepth, DefaultMaxDepth))
	}
	if info.NodeCount > DefaultMaxNodes {
		info.Errors = append(info.Errors, fmt.Sprintf("node count %d exceeds limit %d", info.NodeCount, DefaultMaxNodes))
	}
	if info.ParallelWidth > DefaultMaxParallel {
		info.Errors = append(info.Errors, fmt.Sprintf("parallel width %d exceeds limit %d", info.ParallelWidth, DefaultMaxParallel))
	}
	if info.MaxRetries > DefaultMaxRetries {
		info.Errors = append(info.Errors, fmt.Sprintf("max retries %d exceeds limit %d", info.MaxRetries, DefaultMaxRetries))
	}

	// Stage 5 — Edge Validation
	for _, edge := range tree.Edges {
		errors := evolution.ValidateEdge(edge, len(tree.Children))
		info.Errors = append(info.Errors, errors...)
		info.EdgeTypes = append(info.EdgeTypes, edge.Type)
	}

	return info
}

// computeTreeMetrics walks the tree and computes structural constraints.
// Returns maxDepth, nodeCount, maxParallelWidth, maxRetries, maxTimeoutMs, sideEffectClass.
func computeTreeMetrics(node *evolution.SerializableNode, info *evolution.NodeValidationInfo) (maxDepth, nodeCount, parallelWidth, maxRetries int, timeoutMs int64, sideEffectClass string) {
	nodeCount = 1

	// Track max depth
	if len(node.Children) > 0 {
		childDepth, childCount, childPar, childRetry, childTimeout := computeSubtreeMetrics(node.Children)
		if childDepth+1 > maxDepth {
			maxDepth = childDepth + 1
		}
		nodeCount += childCount
		if childPar > parallelWidth {
			parallelWidth = childPar
		}
		if childRetry > maxRetries {
			maxRetries = childRetry
		}
		if childTimeout > timeoutMs {
			timeoutMs = childTimeout
		}
	}

	// Track parallel width
	if node.Type == "Parallel" {
		if len(node.Children) > parallelWidth {
			parallelWidth = len(node.Children)
		}
	}

	// Track retries
	if node.MaxRetries > maxRetries {
		maxRetries = node.MaxRetries
	}

	// Track timeout
	if node.TimeoutMs > timeoutMs {
		timeoutMs = node.TimeoutMs
	}

	// Infer side-effect class from metadata or node name
	if node.Metadata != nil {
		if sec, ok := node.Metadata["side_effect_class"].(string); ok {
			sideEffectClass = sec
		}
	}

	return maxDepth, nodeCount, parallelWidth, maxRetries, timeoutMs, sideEffectClass
}

// computeSubtreeMetrics computes metrics for a subtree.
func computeSubtreeMetrics(children []evolution.SerializableNode) (maxDepth, nodeCount, parallelWidth, maxRetries int, maxTimeout int64) {
	for _, child := range children {
		cd, cc, cp, cr, ct, _ := computeTreeMetrics(&child, nil)
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
