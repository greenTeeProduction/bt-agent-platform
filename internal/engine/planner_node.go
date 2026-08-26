package engine

import (
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/goap"
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
		case []any:
			var goals []GoalDefinition
			for _, item := range g {
				if m, ok := item.(map[string]any); ok {
					goal := GoalDefinition{
						Name:        stringFromMap(m, "name"),
						Priority:    floatFromMap(m, "priority"),
						Description: stringFromMap(m, "description"),
					}
					if pre, ok := m["preconditions"].([]any); ok {
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

func stringFromMap(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func floatFromMap(m map[string]any, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0.5
}

// intSliceFromInterface converts various input types to []int.
// Handles: nil, []float64, []interface{} with float64 or int elements.
func intSliceFromInterface(v any) []int {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case []float64:
		out := make([]int, len(val))
		for i, f := range val {
			out[i] = int(f)
		}
		return out
	case []any:
		if len(val) == 0 {
			return []int{}
		}
		var out []int
		for _, item := range val {
			switch n := item.(type) {
			case float64:
				out = append(out, int(n))
			case int:
				out = append(out, n)
				// skip non-numeric types (strings, bools, etc.)
			}
		}
		return out
	default:
		return nil
	}
}

// ============================================================================
// Standalone PlannerNode — delegates to the internal/goap A* planner
// ============================================================================

// GOAPAction represents a single action in the GOAP planning domain.
type GOAPAction struct {
	Name          string
	Cost          float64
	Preconditions map[string]bool // what must be true to execute
	Effects       map[string]bool // what changes after execution
	ActionFunc    string          // registered engine action name
}

// GOAPGoal represents a desired world state.
type GOAPGoal struct {
	Name       string
	Priority   float64
	Conditions map[string]bool // desired world state
}

// PlannerNode implements Goal-Oriented Behavior Tree (GOBT) planning.
type PlannerNode struct {
	Goal     GOAPGoal
	Actions  []GOAPAction
	MaxDepth int // max plan depth (default 5)
	// Mode is retained for API compatibility; both "greedy" and "search"
	// now run the unified internal/goap A* planner (ADR-133 Phase 6), which
	// finds a complete plan whenever the old greedy walk did.
	Mode string
}

// Plan represents a computed action sequence.
type Plan struct {
	Actions  []string
	Cost     float64
	Depth    int
	Complete bool
}

// plannerNodeMaxNodes bounds the A* search; matches goap.DefaultGOAPConfig.
const plannerNodeMaxNodes = 10000

// Plan searches for an action sequence from the current world state to the
// goal by delegating to internal/goap's A* planner — the platform's single
// GOAP search implementation (ADR-133 Phase 6 removed the duplicate here).
//
// Semantics note: the engine's bool-typed domain historically treated a
// missing key as false, while goap.WorldState.Satisfies requires the key to
// be present. Every key referenced by the goal, an action, or the initial
// state is therefore seeded to false before planning, which preserves the
// historical behavior exactly.
func (p *PlannerNode) Plan(worldState map[string]bool) Plan {
	maxDepth := p.MaxDepth
	if maxDepth == 0 {
		maxDepth = 5
	}

	keys := make(map[string]struct{})
	collect := func(m map[string]bool) {
		for k := range m {
			keys[k] = struct{}{}
		}
	}
	collect(p.Goal.Conditions)
	collect(worldState)

	actionFunc := make(map[string]string, len(p.Actions))
	actions := make([]goap.Action, len(p.Actions))
	for i, a := range p.Actions {
		collect(a.Preconditions)
		collect(a.Effects)
		actions[i] = goap.Action{
			Name:          a.Name,
			Cost:          a.Cost,
			Preconditions: boolConditions(a.Preconditions),
			Effects:       boolConditions(a.Effects),
		}
		actionFunc[a.Name] = a.ActionFunc
	}

	initial := make(goap.WorldState, len(keys))
	for k := range keys {
		initial[k] = false
	}
	for k, v := range worldState {
		initial[k] = v
	}

	planner := goap.NewPlanner(actions, maxDepth, plannerNodeMaxNodes)
	result := planner.Plan(initial, &goap.Goal{
		Name:       p.Goal.Name,
		Priority:   p.Goal.Priority,
		Conditions: boolConditions(p.Goal.Conditions),
	})
	if result == nil {
		return Plan{Complete: false}
	}

	plan := Plan{Cost: result.Cost, Depth: len(result.Steps), Complete: true}
	for _, step := range result.Steps {
		plan.Actions = append(plan.Actions, actionFunc[step.Name])
	}
	return plan
}

// boolConditions lifts the engine's bool-typed condition maps into
// goap.WorldState.
func boolConditions(m map[string]bool) goap.WorldState {
	ws := make(goap.WorldState, len(m))
	for k, v := range m {
		ws[k] = v
	}
	return ws
}
