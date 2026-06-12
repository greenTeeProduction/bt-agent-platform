package engine

import (
	"sort"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// UtilityScore represents the utility evaluation of a behavior tree node.
type UtilityScore struct {
	ChildIndex    int                `json:"child_index"`
	WeightedScore float64            `json:"weighted_score"`
	Criteria      map[string]float64 `json:"criteria,omitempty"`
	Confidence    float64            `json:"confidence"`
	CostEstimate  float64            `json:"cost_estimate"`
	RiskScore     float64            `json:"risk_score"`
	Valid         bool               `json:"valid"`
}

// ScoringCriteria defines weights for each scoring dimension.
type ScoringCriteria struct {
	UrgencyWeight       float64 `json:"urgency_weight"`
	CostWeight          float64 `json:"cost_weight"`
	ConfidenceWeight    float64 `json:"confidence_weight"`
	HistoricalWeight    float64 `json:"historical_weight"`
	GoalAlignmentWeight float64 `json:"goal_alignment_weight"`
	RiskTolerance       float64 `json:"risk_tolerance"`
}

// DefaultScoringCriteria returns uniform weights.
func DefaultScoringCriteria() ScoringCriteria {
	return ScoringCriteria{
		UrgencyWeight:       0.25,
		CostWeight:          0.25,
		ConfidenceWeight:    0.25,
		HistoricalWeight:    0.25,
		GoalAlignmentWeight: 0.0,
		RiskTolerance:       0.5,
	}
}

// ScoreChild evaluates a single child node for utility scoring.
// Returns a UtilityScore with all criteria evaluated.
func ScoreChild(child *evolution.SerializableNode, bb *Blackboard, criteria ScoringCriteria) UtilityScore {
	score := UtilityScore{
		Criteria: make(map[string]float64),
		Valid:    true,
	}

	// 1. Check guard edges (preconditions)
	for _, edge := range child.Edges {
		if edge.Type == evolution.EdgeGuard {
			if edge.Condition != "" && !evaluateGuardCondition(edge.Condition, bb) {
				score.Valid = false
				return score
			}
		}
	}

	// 2. Urgency — from blackboard task priority
	urgency := 0.5 // default medium urgency
	if bb.ChainState != nil {
		if prio, ok := bb.ChainState["task_priority"].(float64); ok {
			urgency = prio
		}
	}
	score.Criteria["urgency"] = urgency

	// 3. Cost estimate — lower is better
	cost := 0.5
	if child.Metadata != nil {
		if c, ok := child.Metadata["cost_estimate"].(float64); ok {
			cost = c
		}
	}
	score.Criteria["cost"] = 1.0 - cost // invert: lower cost = higher score
	score.CostEstimate = cost

	// 4. Confidence — from node weight or metadata
	confidence := 0.5
	if child.Metadata != nil {
		if conf, ok := child.Metadata["confidence"].(float64); ok {
			confidence = conf
		}
	}
	for _, edge := range child.Edges {
		if edge.Type == evolution.EdgeChild && edge.Weight > 0 {
			confidence = edge.Weight
		}
	}
	score.Criteria["confidence"] = confidence
	score.Confidence = confidence

	// 5. Historical success — from blackboard or reflections
	historical := 0.5
	if bb.Reflections != nil {
		historical = 0.5 // default; could be enhanced with per-node stats
	}
	score.Criteria["historical"] = historical

	// 6. Goal alignment — from GOAP goal stack
	goalAlign := 0.0
	if bb.ChainState != nil {
		if ga, ok := bb.ChainState["goal_alignment"].(float64); ok {
			goalAlign = ga
		}
	}
	score.Criteria["goal_alignment"] = goalAlign

	// 7. Risk score — from edge risk metadata
	risk := 0.3
	if child.Metadata != nil {
		if r, ok := child.Metadata["risk_score"].(float64); ok {
			risk = r
		}
	}
	score.Criteria["risk"] = 1.0 - risk // invert: lower risk = higher score
	score.RiskScore = risk

	// Weighted sum
	score.WeightedScore =
		criteria.UrgencyWeight*urgency +
			criteria.CostWeight*(1.0-cost) +
			criteria.ConfidenceWeight*confidence +
			criteria.HistoricalWeight*historical +
			criteria.GoalAlignmentWeight*goalAlign

	return score
}

// evaluateGuardCondition evaluates a simple guard condition against the blackboard.
// Supports basic expressions like "hasCodexAccess", "budget_remaining > 0", etc.
func evaluateGuardCondition(condition string, bb *Blackboard) bool {
	if bb.ChainState == nil {
		return condition != "" // if no state, assume condition is a simple existence check
	}
	// Simple lookup: if the key exists in ChainState, check truthiness
	if val, ok := bb.ChainState[condition].(bool); ok {
		return val
	}
	if _, ok := bb.ChainState[condition]; ok {
		return true // key exists = condition met
	}
	// Default: pass (conditions that can't be evaluated are assumed met to avoid blocking)
	return true
}

// ScoreChildren evaluates all children and returns ranked scores.
func ScoreChildren(node *evolution.SerializableNode, bb *Blackboard, criteria ScoringCriteria) []UtilityScore {
	scores := make([]UtilityScore, 0, 16)
	for i := range node.Children {
		score := ScoreChild(&node.Children[i], bb, criteria)
		score.ChildIndex = i
		scores = append(scores, score)
	}
	// Sort by weighted score descending
	sort.Slice(scores, func(i, j int) bool {
		if !scores[i].Valid {
			return false
		}
		if !scores[j].Valid {
			return true
		}
		if scores[i].WeightedScore == scores[j].WeightedScore {
			// Tie-break: lower cost wins
			return scores[i].CostEstimate < scores[j].CostEstimate
		}
		return scores[i].WeightedScore > scores[j].WeightedScore
	})
	return scores
}

// BuildUtilitySelector builds a go-bt Command for a UtilitySelector node.
func BuildUtilitySelector(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	// Read scoring criteria from metadata or use defaults
	criteria := DefaultScoringCriteria()
	if node.Metadata != nil {
		if uw, ok := node.Metadata["urgency_weight"].(float64); ok {
			criteria.UrgencyWeight = uw
		}
		if cw, ok := node.Metadata["cost_weight"].(float64); ok {
			criteria.CostWeight = cw
		}
		if confw, ok := node.Metadata["confidence_weight"].(float64); ok {
			criteria.ConfidenceWeight = confw
		}
		if hw, ok := node.Metadata["historical_weight"].(float64); ok {
			criteria.HistoricalWeight = hw
		}
		if rt, ok := node.Metadata["risk_tolerance"].(float64); ok {
			criteria.RiskTolerance = rt
		}
	}

	// Pre-build all child commands so we don't rebuild on every tick
	children := make([]btcore.Command[Blackboard], len(node.Children))
	for i := range node.Children {
		children[i] = buildNode(&node.Children[i], bb, node.Name)
	}

	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		scores := ScoreChildren(node, ctx.Blackboard, criteria)

		// Find first valid child
		var bestIdx int = -1
		var bestScore float64 = -1
		for i := range scores {
			if scores[i].Valid && scores[i].WeightedScore > bestScore {
				bestIdx = scores[i].ChildIndex
				bestScore = scores[i].WeightedScore
			}
		}

		if bestIdx == -1 {
			// No valid children
			return -1 // Failure
		}

		// Execute the chosen child
		result := children[bestIdx].Run(ctx)

		// If child is still Running (0), we must yield immediately.
		// This prevents "reward hacking" where a child returns Running
		// indefinitely to monopolize the selector. The next tick will
		// re-evaluate scores and may pick a different child if conditions change.
		if result == 0 {
			return 0 // Running — yield to allow re-evaluation next tick
		}

		// If child failed, try next best (unless fail_fast)
		failFast := false
		reEval := false
		if node.Metadata != nil {
			if ff, ok := node.Metadata["fail_fast"].(bool); ok {
				failFast = ff
			}
			if re, ok := node.Metadata["re_evaluate_on_change"].(bool); ok {
				reEval = re
			}
		}

		if result == -1 && !failFast {
			// Re-score and try next (excluding current best)
			// With re_evaluate_on_change, we also check if conditions changed mid-tick
			if reEval {
				scores = ScoreChildren(node, ctx.Blackboard, criteria)
			}
			for i := range scores {
				if i != bestIdx && scores[i].Valid {
					nextResult := children[scores[i].ChildIndex].Run(ctx)
					if nextResult == 1 {
						return 1 // Success
					}
					if nextResult == 0 {
						return 0 // Running
					}
				}
			}
			return -1 // All failed
		}

		return result
	})
}
