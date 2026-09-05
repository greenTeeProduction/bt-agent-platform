// Package goap implements Goal-Oriented Action Planning: A* search-based
// deliberative planning that finds action sequences to achieve goal states.
//
// GOAP complements behavior trees by providing deliberate long-horizon planning
// while BTs handle reactive execution and intermediate decision-making.
//
// Core concepts:
//   - WorldState: key-value map representing the current world
//   - Goal: desired world state (subset of keys that must match)
//   - Action: operator with preconditions (must be true) and effects (what changes)
//   - Planner: A* search to find optimal action sequence from current state to goal
//   - Agent: executes plans, monitors for failure, triggers replanning
package goap

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// WorldState is a key-value representation of the agent's world.
// Keys are strings, values can be any comparable type.
type WorldState map[string]any

// Goal represents a desired world state. Only the specified keys must match;
// other keys in the world state are ignored during goal satisfaction checks.
type Goal struct {
	Name       string     `json:"name"`
	Priority   float64    `json:"priority"`   // 0-1, higher = more important
	Conditions WorldState `json:"conditions"` // must all be satisfied
	Deadline   int        `json:"deadline"`   // optional step deadline (0 = none)
}

// Action is an operator that transforms the world state.
// It has preconditions (must be true to execute) and effects (what changes).
type Action struct {
	Name          string         `json:"name"`
	Cost          float64        `json:"cost"`               // execution cost (1.0 default)
	Preconditions WorldState     `json:"preconditions"`      // must all match
	Effects       WorldState     `json:"effects"`            // state changes after execution
	Metadata      map[string]any `json:"metadata,omitempty"` // arbitrary data
}

// Plan is an ordered sequence of actions to achieve a goal.
type Plan struct {
	Goal  *Goal    `json:"goal"`
	Steps []Action `json:"steps"`
	Cost  float64  `json:"cost"`
}

// String returns a human-readable plan representation.
func (p *Plan) String() string {
	if len(p.Steps) == 0 {
		return fmt.Sprintf("Plan for %q: (empty, trivially satisfied)", p.Goal.Name)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Plan for %q (cost=%.1f, %d steps):\n", p.Goal.Name, p.Cost, len(p.Steps))
	for i, step := range p.Steps {
		fmt.Fprintf(&sb, "  %d. %s (cost=%.1f)\n", i+1, step.Name, step.Cost)
	}
	return sb.String()
}

// Clone creates a deep copy of a WorldState.
func (ws WorldState) Clone() WorldState {
	clone := make(WorldState, len(ws))
	maps.Copy(clone, ws)
	return clone
}

// Satisfies checks if this world state satisfies all goal conditions.
func (ws WorldState) Satisfies(goal WorldState) bool {
	for k, want := range goal {
		have, ok := ws[k]
		if !ok || have != want {
			return false
		}
	}
	return true
}

// MeetsPreconditions checks if this world state meets all action preconditions.
func (ws WorldState) MeetsPreconditions(pre WorldState) bool {
	return ws.Satisfies(pre) // Same logic — all keys must match
}

// Apply applies an action's effects to produce a new WorldState.
// Returns a clone with effects applied (original is not modified).
func (ws WorldState) Apply(effects WorldState) WorldState {
	result := ws.Clone()
	maps.Copy(result, effects)
	return result
}

// Equals checks if two world states are identical.
func (ws WorldState) Equals(other WorldState) bool {
	if len(ws) != len(other) {
		return false
	}
	for k, v := range ws {
		if other[k] != v {
			return false
		}
	}
	return true
}

// String returns a sorted representation of the world state.
func (ws WorldState) String() string {
	keys := slices.Sorted(maps.Keys(ws))
	var sb strings.Builder
	sb.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%s: %v", k, ws[k])
	}
	sb.WriteString("}")
	return sb.String()
}

// IsSatisfied checks if this goal is satisfied by the given world state.
func (g *Goal) IsSatisfied(state WorldState) bool {
	return state.Satisfies(g.Conditions)
}

// NewGoal creates a new goal with the given name, priority, and conditions.
func NewGoal(name string, priority float64, conditions WorldState) *Goal {
	return &Goal{Name: name, Priority: priority, Conditions: conditions}
}

// NewAction creates a new action with the given name, cost, preconditions, and effects.
func NewAction(name string, cost float64, pre, effects WorldState) Action {
	return Action{Name: name, Cost: cost, Preconditions: pre, Effects: effects}
}
