package goap

import (
	"fmt"
	"sync"
	"time"
)

// ActionFunc is the execution function for a GOAP action.
// It receives the current world state and returns the new state or an error.
type ActionFunc func(state WorldState) (WorldState, error)

// ActionRegistry maps action names to their execution functions.
type ActionRegistry map[string]ActionFunc

// AgentState represents the current state of a GOAP agent.
type AgentState string

const (
	AgentIdle       AgentState = "idle"
	AgentPlanning   AgentState = "planning"
	AgentExecuting  AgentState = "executing"
	AgentSucceeded  AgentState = "succeeded"
	AgentFailed     AgentState = "failed"
	AgentReplanning AgentState = "replanning"
)

// AgentRun tracks a single execution of a plan.
type AgentRun struct {
	Goal        *Goal         `json:"goal"`
	Plan        *Plan         `json:"plan"`
	StartState  WorldState    `json:"start_state"`
	EndState    WorldState    `json:"end_state"`
	StepsTaken  []string      `json:"steps_taken"`
	Status      AgentState    `json:"status"`
	Error       string        `json:"error,omitempty"`
	Duration    time.Duration `json:"duration"`
	ReplansUsed int           `json:"replans_used"`
}

// Agent executes GOAP plans with monitoring and replanning.
type Agent struct {
	mu         sync.RWMutex
	Planner    *Planner
	Registry   ActionRegistry
	WorldState WorldState
	Goals      []*Goal
	CurrentRun *AgentRun
	State      AgentState
	History    []*AgentRun
	MaxReplans int // maximum replanning attempts (default 3)
	Callbacks  AgentCallbacks
}

// AgentCallbacks are invoked during plan execution.
type AgentCallbacks struct {
	OnPlanFound    func(plan *Plan)
	OnStepStart    func(step int, action *Action)
	OnStepComplete func(step int, action *Action, err error)
	OnReplan       func(oldPlan *Plan, newPlan *Plan, reason string)
	OnComplete     func(run *AgentRun)
	OnError        func(run *AgentRun, err error)
}

// NewAgent creates a new GOAP agent.
func NewAgent(planner *Planner, registry ActionRegistry) *Agent {
	return &Agent{
		Planner:    planner,
		Registry:   registry,
		WorldState: make(WorldState),
		Goals:      nil,
		State:      AgentIdle,
		History:    make([]*AgentRun, 0),
		MaxReplans: 3,
	}
}

// SetGoals sets the agent's goals, sorted by priority.
func (a *Agent) SetGoals(goals ...*Goal) {
	a.Goals = goals
}

// AddGoal appends a goal.
func (a *Agent) AddGoal(goal *Goal) {
	a.Goals = append(a.Goals, goal)
}

// SetState sets a world state variable.
func (a *Agent) SetState(key string, value interface{}) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.WorldState[key] = value
}

// GetState reads a world state variable.
func (a *Agent) GetState(key string) (interface{}, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	v, ok := a.WorldState[key]
	return v, ok
}

// Run executes the agent: plan, execute, monitor, replan if needed.
func (a *Agent) Run() *AgentRun {
	a.State = AgentPlanning
	startTime := time.Now()

	plan := a.Planner.FindBestPlan(a.WorldState, a.Goals)
	if plan == nil {
		run := &AgentRun{
			StartState: a.WorldState.Clone(),
			Status:     AgentFailed,
			Error:      "no plan found",
			Duration:   time.Since(startTime),
		}
		a.CurrentRun = run
		a.History = append(a.History, run)
		a.State = AgentFailed
		if a.Callbacks.OnError != nil {
			a.Callbacks.OnError(run, fmt.Errorf("no plan found"))
		}
		return run
	}

	if a.Callbacks.OnPlanFound != nil {
		a.Callbacks.OnPlanFound(plan)
	}

	run := a.executePlan(plan, startTime)
	a.CurrentRun = run
	a.History = append(a.History, run)
	return run
}

func (a *Agent) executePlan(plan *Plan, startTime time.Time) *AgentRun {
	a.State = AgentExecuting

	run := &AgentRun{
		Goal:       plan.Goal,
		Plan:       plan,
		StartState: a.WorldState.Clone(),
		StepsTaken: make([]string, 0),
		Status:     AgentExecuting,
	}

	currentState := a.WorldState.Clone()
	replans := 0

	for replans <= a.MaxReplans {
		// Execute each step
		for i := range plan.Steps {
			action := &plan.Steps[i]

			if a.Callbacks.OnStepStart != nil {
				a.Callbacks.OnStepStart(i, action)
			}

			// Check preconditions still hold
			if !currentState.MeetsPreconditions(action.Preconditions) {
				// State changed — preconditions no longer met, need replan
				if replans < a.MaxReplans {
					a.State = AgentReplanning
					replans++
					newPlan := a.Planner.Plan(currentState, plan.Goal)
					if newPlan == nil {
						run.Status = AgentFailed
						run.Error = fmt.Sprintf("replan %d failed: no plan from state", replans)
						run.Duration = time.Since(startTime)
						a.State = AgentFailed
						return run
					}
					if a.Callbacks.OnReplan != nil {
						a.Callbacks.OnReplan(plan, newPlan, "precondition failure")
					}
					plan = newPlan
					goto retryPlan
				}
				run.Status = AgentFailed
				run.Error = fmt.Sprintf("step %d preconditions not met, max replans reached", i)
				run.Duration = time.Since(startTime)
				a.State = AgentFailed
				return run
			}

			// Execute the action
			fn, ok := a.Registry[action.Name]
			if !ok {
				run.Status = AgentFailed
				run.Error = fmt.Sprintf("action %q not found in registry", action.Name)
				run.Duration = time.Since(startTime)
				a.State = AgentFailed
				return run
			}

			newState, err := fn(currentState)
			if err != nil {
				if a.Callbacks.OnStepComplete != nil {
					a.Callbacks.OnStepComplete(i, action, err)
				}
				if replans < a.MaxReplans {
					a.State = AgentReplanning
					replans++
					newPlan := a.Planner.Plan(currentState, plan.Goal)
					if newPlan == nil {
						run.Status = AgentFailed
						run.Error = fmt.Sprintf("replan %d failed: no plan from state after action error", replans)
						run.Duration = time.Since(startTime)
						a.State = AgentFailed
						return run
					}
					if a.Callbacks.OnReplan != nil {
						a.Callbacks.OnReplan(plan, newPlan, err.Error())
					}
					plan = newPlan
					goto retryPlan
				}
				run.Status = AgentFailed
				run.Error = fmt.Sprintf("action %q failed: %v", action.Name, err)
				run.Duration = time.Since(startTime)
				a.State = AgentFailed
				return run
			}

			currentState = newState
			run.StepsTaken = append(run.StepsTaken, action.Name)

			if a.Callbacks.OnStepComplete != nil {
				a.Callbacks.OnStepComplete(i, action, nil)
			}

			// Update agent's world state
			a.mu.Lock()
			for k, v := range action.Effects {
				a.WorldState[k] = v
			}
			a.mu.Unlock()
		}

		// All steps done — check if goal satisfied
		if currentState.Satisfies(plan.Goal.Conditions) {
			run.Status = AgentSucceeded
			run.EndState = currentState
			run.ReplansUsed = replans
			run.Duration = time.Since(startTime)
			a.State = AgentSucceeded
			if a.Callbacks.OnComplete != nil {
				a.Callbacks.OnComplete(run)
			}
			return run
		}

		// Goal not satisfied after plan execution — replan
		if replans < a.MaxReplans {
			a.State = AgentReplanning
			replans++
			newPlan := a.Planner.Plan(currentState, plan.Goal)
			if newPlan == nil {
				run.Status = AgentFailed
				run.Error = fmt.Sprintf("replan %d failed: no plan", replans)
				run.Duration = time.Since(startTime)
				a.State = AgentFailed
				return run
			}
			if a.Callbacks.OnReplan != nil {
				a.Callbacks.OnReplan(plan, newPlan, "goal not satisfied after execution")
			}
			plan = newPlan
			continue
		}

		run.Status = AgentFailed
		run.Error = "goal not satisfied, max replans reached"
		run.Duration = time.Since(startTime)
		a.State = AgentFailed
		return run

	retryPlan:
		// Reset loop for new plan
	}

	// Should not reach here
	run.Status = AgentFailed
	run.Error = "max replans reached"
	run.Duration = time.Since(startTime)
	a.State = AgentFailed
	return run
}

// LatestRun returns the most recent agent run.
func (a *Agent) LatestRun() *AgentRun {
	return a.CurrentRun
}

// HistoryRuns returns all agent runs.
func (a *Agent) HistoryRuns() []*AgentRun {
	return a.History
}

// Summary returns a human-readable agent state summary.
func (a *Agent) Summary() string {
	state := string(a.State)
	goals := len(a.Goals)
	history := len(a.History)
	lastResult := "none"
	if a.CurrentRun != nil {
		lastResult = string(a.CurrentRun.Status)
	}
	return fmt.Sprintf("GOAP Agent [%s] goals=%d history=%d last=%s", state, goals, history, lastResult)
}
