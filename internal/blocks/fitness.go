package blocks

import (
	"strings"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/metrics"
)

// CollectBlockIDs walks a tree and returns unique block_id metadata values.
func CollectBlockIDs(tree *evolution.SerializableNode) []string {
	seen := make(map[string]bool)
	var walk func(*evolution.SerializableNode)
	walk = func(n *evolution.SerializableNode) {
		if n == nil {
			return
		}
		if n.Metadata != nil {
			if id, ok := n.Metadata["block_id"].(string); ok && id != "" {
				seen[id] = true
			}
		}
		if id := BlockIDFromNode(n); n.Type == "SubTreeRef" && id != "" {
			seen[id] = true
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(tree)
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	return out
}

// ScoreFromBlackboard derives a 0–100 fitness score from execution state.
func ScoreFromBlackboard(outcome string, qualityScore float64, success bool) float64 {
	score := qualityScore * 100
	if score <= 0 {
		if success || strings.EqualFold(outcome, "success") || strings.EqualFold(outcome, "completed") {
			score = 75
		} else {
			score = 25
		}
	}
	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}
	return score
}

// RecordTaskBlockFitness records per-block fitness gauges after a task run.
func RecordTaskBlockFitness(tree *evolution.SerializableNode, agent, outcome string, qualityScore float64, success bool) {
	ids := CollectBlockIDs(tree)
	if len(ids) == 0 {
		return
	}
	score := ScoreFromBlackboard(outcome, qualityScore, success)
	if agent == "" {
		agent = "default"
	}
	for _, id := range ids {
		metrics.RecordBlockFitness(id, agent, score)
	}
}

// FitnessRanking returns block ids sorted by recorded fitness (highest first).
func FitnessRanking() []string {
	return metrics.BlockFitnessRanking()
}
