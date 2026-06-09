package engine

import (
	"container/heap"
	"fmt"
	"sort"
	"strings"

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

// intSliceFromInterface converts various input types to []int.
// Handles: nil, []float64, []interface{} with float64 or int elements.
func intSliceFromInterface(v interface{}) []int {
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
	case []interface{}:
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
// Standalone PlannerNode — A* GOAP planning
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
	MaxDepth int    // max plan depth (default 5)
	Mode     string // "greedy" or "search" (A*)
}

// Plan represents a computed action sequence.
type Plan struct {
	Actions  []string
	Cost     float64
	Depth    int
	Complete bool
}

// plannerState is an A* search state.
type plannerState struct {
	WorldState map[string]bool
	Actions    []string
	Cost       float64
	Depth      int
	Heuristic  float64
	index      int // for heap
}

type plannerHeap []*plannerState

func (h plannerHeap) Len() int           { return len(h) }
func (h plannerHeap) Less(i, j int) bool { return h[i].Cost+h[i].Heuristic < h[j].Cost+h[j].Heuristic }
func (h plannerHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i]; h[i].index = i; h[j].index = j }
func (h *plannerHeap) Push(x any) {
	n := len(*h)
	item := x.(*plannerState)
	item.index = n
	*h = append(*h, item)
}
func (h *plannerHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[:n-1]
	return item
}

// Plan searches for an action sequence from the current world state to the goal.
func (p *PlannerNode) Plan(worldState map[string]bool) Plan {
	if p.MaxDepth == 0 {
		p.MaxDepth = 5
	}
	if p.Mode == "" {
		p.Mode = "search"
	}
	if p.Mode == "greedy" {
		return p.greedyPlan(worldState)
	}
	return p.aStarPlan(worldState)
}

func (p *PlannerNode) goalSatisfied(state map[string]bool) bool {
	for k, v := range p.Goal.Conditions {
		if state[k] != v {
			return false
		}
	}
	return true
}

func (p *PlannerNode) applicableActions(state map[string]bool) []GOAPAction {
	var applicable []GOAPAction
	for _, a := range p.Actions {
		ok := true
		for k, v := range a.Preconditions {
			if state[k] != v {
				ok = false
				break
			}
		}
		if ok {
			applicable = append(applicable, a)
		}
	}
	sort.Slice(applicable, func(i, j int) bool { return applicable[i].Cost < applicable[j].Cost })
	return applicable
}

func (p *PlannerNode) applyAction(state map[string]bool, action GOAPAction) map[string]bool {
	newState := make(map[string]bool, len(state)+len(action.Effects))
	for k, v := range state {
		newState[k] = v
	}
	for k, v := range action.Effects {
		newState[k] = v
	}
	return newState
}

func (p *PlannerNode) heuristic(state map[string]bool) float64 {
	unsatisfied := 0.0
	for k, v := range p.Goal.Conditions {
		if state[k] != v {
			unsatisfied++
		}
	}
	return unsatisfied
}

func (p *PlannerNode) greedyPlan(worldState map[string]bool) Plan {
	state := p.copyState(worldState)
	var actions []string
	cost := 0.0
	for depth := 0; depth < p.MaxDepth; depth++ {
		if p.goalSatisfied(state) {
			return Plan{Actions: actions, Cost: cost, Depth: depth, Complete: true}
		}
		applicable := p.applicableActions(state)
		if len(applicable) == 0 {
			break
		}
		best := applicable[0]
		state = p.applyAction(state, best)
		actions = append(actions, best.ActionFunc)
		cost += best.Cost
	}
	return Plan{Actions: actions, Cost: cost, Depth: len(actions), Complete: p.goalSatisfied(state)}
}

func (p *PlannerNode) aStarPlan(worldState map[string]bool) Plan {
	h := &plannerHeap{}
	heap.Init(h)
	start := &plannerState{
		WorldState: p.copyState(worldState),
		Cost:       0,
		Depth:      0,
		Heuristic:  p.heuristic(worldState),
	}
	heap.Push(h, start)
	visited := make(map[string]bool)
	for h.Len() > 0 {
		current := heap.Pop(h).(*plannerState)
		stateKey := p.stateKey(current.WorldState)
		if visited[stateKey] {
			continue
		}
		visited[stateKey] = true
		if p.goalSatisfied(current.WorldState) {
			return Plan{Actions: current.Actions, Cost: current.Cost, Depth: current.Depth, Complete: true}
		}
		if current.Depth >= p.MaxDepth {
			continue
		}
		for _, action := range p.applicableActions(current.WorldState) {
			newState := p.applyAction(current.WorldState, action)
			newActions := make([]string, len(current.Actions)+1)
			copy(newActions, current.Actions)
			newActions[len(current.Actions)] = action.ActionFunc
			next := &plannerState{
				WorldState: newState,
				Actions:    newActions,
				Cost:       current.Cost + action.Cost,
				Depth:      current.Depth + 1,
				Heuristic:  p.heuristic(newState),
			}
			heap.Push(h, next)
		}
	}
	return Plan{Complete: false}
}

func (p *PlannerNode) copyState(state map[string]bool) map[string]bool {
	out := make(map[string]bool, len(state))
	for k, v := range state {
		out[k] = v
	}
	return out
}

func (p *PlannerNode) stateKey(state map[string]bool) string {
	keys := make([]string, 0, len(state))
	for k, v := range state {
		keys = append(keys, fmt.Sprintf("%s=%v", k, v))
	}
	sort.Strings(keys)
	return strings.Join(keys, "|")
}
