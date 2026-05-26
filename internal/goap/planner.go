package goap

import (
	"container/heap"
	"fmt"
)

// plannerNode is an A* search node.
type plannerNode struct {
	state    WorldState
	actions  []Action // path of actions taken to reach this state
	cost     float64  // g(n) — accumulated cost
	heuristic float64 // h(n) — estimated cost to goal
}

// plannerNodeHeap implements container/heap.Interface for A* priority queue.
type plannerNodeHeap []*plannerNode

func (h plannerNodeHeap) Len() int           { return len(h) }
func (h plannerNodeHeap) Less(i, j int) bool  { return h[i].cost+h[i].heuristic < h[j].cost+h[j].heuristic }
func (h plannerNodeHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }

func (h *plannerNodeHeap) Push(x interface{}) {
	*h = append(*h, x.(*plannerNode))
}

func (h *plannerNodeHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// Planner performs A* search to find optimal action sequences.
type Planner struct {
	Actions          []Action     `json:"actions"`          // available actions
	MaxDepth         int          `json:"max_depth"`        // search depth limit
	MaxNodes         int          `json:"max_nodes"`        // expanded nodes limit
	stats            PlannerStats `json:"-"`
}

// PlannerStats tracks planner performance.
type PlannerStats struct {
	NodesExpanded   int   `json:"nodes_expanded"`
	NodesGenerated  int   `json:"nodes_generated"`
	PlansFound      int   `json:"plans_found"`
	MaxDepthReached int   `json:"max_depth_reached"`
	LastDuration    int64 `json:"last_duration_ms"`
}

// DefaultPlanner creates a planner with sensible defaults.
func DefaultPlanner(actions []Action) *Planner {
	return &Planner{
		Actions:  actions,
		MaxDepth: 50,
		MaxNodes: 10000,
	}
}

// NewPlanner creates a planner with custom limits.
func NewPlanner(actions []Action, maxDepth, maxNodes int) *Planner {
	return &Planner{
		Actions:  actions,
		MaxDepth: maxDepth,
		MaxNodes: maxNodes,
	}
}

// Plan finds the lowest-cost sequence of actions to reach a goal from the current state.
// Returns nil if no plan exists within search limits.
func (p *Planner) Plan(current WorldState, goal *Goal) *Plan {
	p.stats = PlannerStats{}

	if current.Satisfies(goal.Conditions) {
		p.stats.PlansFound = 1
		return &Plan{Goal: goal, Steps: nil, Cost: 0}
	}

	// A* search
	visited := make(map[string]bool)

	pq := &plannerNodeHeap{}
	heap.Init(pq)

	startNode := &plannerNode{
		state:    current.Clone(),
		actions:  nil,
		cost:     0,
		heuristic: p.heuristic(current, goal),
	}
	heap.Push(pq, startNode)
	p.stats.NodesGenerated++

	for pq.Len() > 0 {
		if p.stats.NodesExpanded >= p.MaxNodes {
			break
		}

		node := heap.Pop(pq).(*plannerNode)
		p.stats.NodesExpanded++

		depth := len(node.actions)
		if depth > p.stats.MaxDepthReached {
			p.stats.MaxDepthReached = depth
		}

		// Goal test
		if node.state.Satisfies(goal.Conditions) {
			p.stats.PlansFound = 1
			return &Plan{
				Goal:  goal,
				Steps: node.actions,
				Cost:  node.cost,
			}
		}

		// Depth limit
		if depth >= p.MaxDepth {
			continue
		}

		// State key for visited set
		stateKey := node.state.String()
		if visited[stateKey] {
			continue
		}
		visited[stateKey] = true

		// Generate successors
		for _, action := range p.Actions {
			if node.state.MeetsPreconditions(action.Preconditions) {
				newState := node.state.Apply(action.Effects)

				// Avoid cycles: if applying this action produces the same state, skip
				if newState.Equals(node.state) {
					continue
				}

				newActions := make([]Action, len(node.actions)+1)
				copy(newActions, node.actions)
				newActions[depth] = action

				newNode := &plannerNode{
					state:     newState,
					actions:   newActions,
					cost:      node.cost + action.Cost,
					heuristic: p.heuristic(newState, goal),
				}

				heap.Push(pq, newNode)
				p.stats.NodesGenerated++
			}
		}
	}

	return nil
}

// FindBestPlan evaluates multiple goals and returns the plan with the best priority/cost ratio.
func (p *Planner) FindBestPlan(current WorldState, goals []*Goal) *Plan {
	var bestPlan *Plan
	var bestScore float64

	for _, goal := range goals {
		plan := p.Plan(current, goal)
		if plan == nil {
			continue
		}
		// Score: higher priority + lower cost = better
		costFactor := 1.0
		if plan.Cost > 0 {
			costFactor = 1.0 / plan.Cost
		}
		score := goal.Priority * costFactor * 10.0

		if score > bestScore {
			bestScore = score
			bestPlan = plan
		}
	}

	return bestPlan
}

// Stats returns planner statistics from the last Plan() call.
func (p *Planner) Stats() PlannerStats {
	return p.stats
}

// heuristic estimates the cost from a state to the goal.
// Uses the count of unsatisfied goal conditions as an admissible heuristic.
func (p *Planner) heuristic(state WorldState, goal *Goal) float64 {
	unsatisfied := 0
	for k, want := range goal.Conditions {
		have, ok := state[k]
		if !ok || have != want {
			unsatisfied++
		}
	}
	return float64(unsatisfied)
}

// PlanMultiple finds the best plan across multiple goals.
// This is a convenience wrapper around FindBestPlan.
func PlanMultiple(state WorldState, goals []*Goal, actions []Action) *Plan {
	planner := DefaultPlanner(actions)
	return planner.FindBestPlan(state, goals)
}

// ValidatePlan checks whether a plan is executable from the given state.
func ValidatePlan(plan *Plan, state WorldState) error {
	if plan == nil {
		return fmt.Errorf("plan is nil")
	}
	if len(plan.Steps) == 0 {
		return nil // trivially satisfied
	}

	current := state
	for i, step := range plan.Steps {
		if !current.MeetsPreconditions(step.Preconditions) {
			return fmt.Errorf("step %d (%q): preconditions not met at %v",
				i+1, step.Name, current)
		}
		current = current.Apply(step.Effects)
	}

	// Verify final state satisfies goal
	if !current.Satisfies(plan.Goal.Conditions) {
		return fmt.Errorf("final state %v does not satisfy goal %q", current, plan.Goal.Name)
	}
	return nil
}
