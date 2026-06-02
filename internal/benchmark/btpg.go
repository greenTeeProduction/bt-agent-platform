package benchmark

import (
	"fmt"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
)

// BTPGMetrics holds behavior tree quality metrics adapted from the
// Behavior Tree Planning Gym (BTPG) paper for service robot evaluation.
type BTPGMetrics struct {
	NodeCount          int     `json:"node_count"`          // total nodes in tree
	Depth              int     `json:"depth"`               // max depth
	BranchingFactor    float64 `json:"branching_factor"`    // avg children per composite node
	SuccessRate        float64 `json:"success_rate"`        // task completion rate
	PlanningEfficiency float64 `json:"planning_efficiency"` // success / node_count (higher is better)
	RobustnessScore    float64 `json:"robustness_score"`    // handles edge cases (0.0 - 1.0)
}

// BTPGTaskResult holds per-task evaluation results.
type BTPGTaskResult struct {
	Task         string `json:"task"`
	Success      bool   `json:"success"`
	NodesVisited int    `json:"nodes_visited"`
	Path         string `json:"path"`
	Output       string `json:"output"`
}

// BTPGResult aggregates BTPG evaluation results.
type BTPGResult struct {
	Metrics BTPGMetrics      `json:"metrics"`
	PerTask []BTPGTaskResult `json:"per_task"`
}

// EvaluateBTPG runs service-robot-style tasks through the tree and computes
// BTPG quality metrics. For each task it runs the tree, measures success,
// counts nodes visited, and tracks the execution path. Edge-case tasks
// (very short or ambiguous) contribute to the robustness score.
func EvaluateBTPG(tree *evolution.SerializableNode, tasks []string, llm llm.LLM) *BTPGResult {
	var results []BTPGTaskResult
	successes := 0
	edgeSuccesses := 0
	edgeTotal := 0
	quality := BTPGQualityScore(tree)

	for _, task := range tasks {
		bb := &engine.Blackboard{
			Task: task,
			LLM:  llm,
		}
		bt := engine.BuildTree(tree, bb)
		output := engine.RunTask(bb, bt)

		success := bb.Outcome == "success"
		if success {
			successes++
		}
		path := detectPath(output, bb)

		// Count nodes visited: use tree's node count as approximation
		nodesVisited := quality.NodeCount
		if !success {
			// Partial traversal may visit fewer nodes
			nodesVisited = max1(nodesVisited / 2)
		}

		results = append(results, BTPGTaskResult{
			Task:         task,
			Success:      success,
			NodesVisited: nodesVisited,
			Path:         path,
			Output:       output,
		})

		// Edge case detection: short or ambiguous tasks
		if isEdgeCaseTask(task) {
			edgeTotal++
			if success {
				edgeSuccesses++
			}
		}
	}

	n := len(tasks)
	if n == 0 {
		return &BTPGResult{Metrics: quality, PerTask: results}
	}

	successRate := float64(successes) / float64(n)
	quality.SuccessRate = successRate

	// Planning efficiency: success / node_count
	if quality.NodeCount > 0 {
		quality.PlanningEfficiency = successRate / float64(quality.NodeCount)
	}

	// Robustness: how well the tree handles edge cases
	if edgeTotal > 0 {
		quality.RobustnessScore = float64(edgeSuccesses) / float64(edgeTotal)
	} else {
		quality.RobustnessScore = successRate
	}

	return &BTPGResult{
		Metrics: quality,
		PerTask: results,
	}
}

// isEdgeCaseTask identifies ambiguous, very short, or unusual tasks that
// test a tree's robustness.
func isEdgeCaseTask(task string) bool {
	return len(task) < 15 || containsStr(task, "??") || containsStr(task, "!!!!")
}

// BTPGQualityScore performs static analysis of a behavior tree (no LLM needed).
// Computes node count, maximum depth, and average branching factor.
func BTPGQualityScore(tree *evolution.SerializableNode) BTPGMetrics {
	if tree == nil {
		return BTPGMetrics{}
	}

	nodeCount := evolution.CountNodes(tree)
	depth := evolution.MaxDepth(tree, 0)
	bf := avgBranchingFactor(tree)

	return BTPGMetrics{
		NodeCount:       nodeCount,
		Depth:           depth,
		BranchingFactor: bf,
	}
}

// avgBranchingFactor computes the average number of children per composite node.
// Only Sequence and Selector types are considered composites.
func avgBranchingFactor(node *evolution.SerializableNode) float64 {
	var totalChildren, compositeCount int
	var walk func(n *evolution.SerializableNode)
	walk = func(n *evolution.SerializableNode) {
		if n == nil {
			return
		}
		if n.Type == "Sequence" || n.Type == "Selector" {
			compositeCount++
			totalChildren += len(n.Children)
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(node)

	if compositeCount == 0 {
		return 0
	}
	return float64(totalChildren) / float64(compositeCount)
}

// BTPGTreeSummary returns a human-readable summary of BTPG metrics.
func BTPGTreeSummary(tree *evolution.SerializableNode) string {
	m := BTPGQualityScore(tree)
	return fmt.Sprintf("BTPG: nodes=%d depth=%d branchFactor=%.2f",
		m.NodeCount, m.Depth, m.BranchingFactor)
}

// BuiltinBTPGTasks returns 8 service-robot-style tasks representative of
// the Behavior Tree Planning Gym domain.
func BuiltinBTPGTasks() []string {
	return []string{
		"bring me a coffee",
		"clean the living room",
		"set the table for dinner",
		"find my keys",
		"water the plants",
		"take out the trash",
		"feed the cat",
		"turn off all lights",
	}
}
