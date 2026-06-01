package engine

import (
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// GoalDefinition represents a goal the PlannerNode can pursue.
type GoalDefinition struct {
	Name          string   `json:"name"`
	Priority      float64  `json:"priority"`
	Description   string   `json:"description,omitempty"`
	Preconditions []string `json:"preconditions,omitempty"`
}

// BuildPlannerNode builds a go-bt Command for a PlannerNode.
// A PlannerNode extends UtilitySelector with GOAP-style goal management:
//   - maintains a goal stack read from blackboard
//   - on child failure, pops the current goal and re-evaluates
//   - on external events, may push new goals with higher priority
func BuildPlannerNode(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	maxGoalDepth := 5
	if node.Metadata != nil {
		if d, ok := node.Metadata["max_goal_depth"].(float64); ok {
			maxGoalDepth = int(d)
		}
	}

	// Read goals from metadata
	goals := readGoals(node, bb)

	// Build the underlying UtilitySelector for action selection
	utilCmd := BuildUtilitySelector(node, bb)

	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		// Initialize goal stack if empty
		if ctx.Blackboard.ChainState == nil {
			ctx.Blackboard.ChainState = make(map[string]any)
		}
		stack, _ := ctx.Blackboard.ChainState["goal_stack"].([]GoalDefinition)
		if len(stack) == 0 && len(goals) > 0 {
			stack = goals
		}

		// Track stack depth limit
		if len(stack) > maxGoalDepth {
			stack = stack[:maxGoalDepth]
		}

		// Execute the highest-priority goal's action
		result := utilCmd.Run(ctx)

		if result == -1 && len(stack) > 0 {
			// Current goal failed — pop and try next
			stack = stack[1:]
			ctx.Blackboard.ChainState["goal_stack"] = stack
			if len(stack) > 0 {
				return utilCmd.Run(ctx)
			}
		}

		if result == 1 && len(stack) > 0 {
			// Current goal succeeded — pop and record
			completed := stack[0]
			stack = stack[1:]
			if completedGoals, ok := ctx.Blackboard.ChainState["completed_goals"].([]string); ok {
				ctx.Blackboard.ChainState["completed_goals"] = append(completedGoals, completed.Name)
			} else {
				ctx.Blackboard.ChainState["completed_goals"] = []string{completed.Name}
			}
			ctx.Blackboard.ChainState["goal_stack"] = stack
		}

		return result
	})
}

// readGoals extracts goal definitions from node metadata.
func readGoals(node *evolution.SerializableNode, bb *Blackboard) []GoalDefinition {
	if node.Metadata == nil {
		return nil
	}

	if raw, ok := node.Metadata["goals"]; ok {
		switch g := raw.(type) {
		case []interface{}:
			var goals []GoalDefinition
			for _, item := range g {
				if m, ok := item.(map[string]interface{}); ok {
					goal := GoalDefinition{
						Name:        stringFromMap(m, "name"),
						Priority:    floatFromMap(m, "priority"),
						Description: stringFromMap(m, "description"),
					}
					if pre, ok := m["preconditions"].([]interface{}); ok {
						for _, p := range pre {
							if s, ok := p.(string); ok {
								goal.Preconditions = append(goal.Preconditions, s)
							}
						}
					}
					goals = append(goals, goal)
				}
			}
			return goals
		}
	}

	// Fallback: read from ChainState
	if bb.ChainState != nil {
		if goalsRaw, ok := bb.ChainState["goals"].([]GoalDefinition); ok {
			return goalsRaw
		}
	}

	return nil
}

func stringFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func floatFromMap(m map[string]interface{}, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0.5
}
