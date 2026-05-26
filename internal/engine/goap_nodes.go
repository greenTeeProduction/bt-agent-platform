package engine

import (
	btcore "github.com/rvitorper/go-bt/core"
	"github.com/nico/go-bt-evolve/internal/goap"
)

// registerGoapNodes registers all GOAP-related conditions and actions.
func registerGoapNodes() {
	// --- Conditions ---

	// HasGoapGoal checks whether the blackboard has a GOAP plan ready.
	conditionRegistry["HasGoapGoal"] = func(b *Blackboard) bool {
		cs := b.ChainState
		if cs == nil {
			return false
		}
		// Check if we have goals configured
		if _, ok := cs["goap_goals"]; !ok {
			return false
		}
		// Check if the task is suitable for GOAP planning
		if b.Task == "" {
			return false
		}
		// Build a goal from the task if not already present
		if _, ok := cs["goap_current_goal"]; !ok {
			goal := goap.BuildGoalFromTask(b.Task)
			cs["goap_current_goal"] = goal
		}
		return true
	}

	// HasMoreGoapSteps checks if there are remaining plan steps.
	conditionRegistry["HasMoreGoapSteps"] = func(b *Blackboard) bool {
		cs := b.ChainState
		if cs == nil {
			return false
		}
		idx, ok := cs["goap_step_index"]
		if !ok {
			return false
		}
		steps, ok := cs["goap_steps"]
		if !ok {
			return false
		}
		stepSlice, ok := steps.([]string)
		if !ok {
			return false
		}
		currentIdx, ok := idx.(int)
		if !ok {
			return false
		}
		return currentIdx < len(stepSlice)
	}

	// --- Actions ---

	// PlanGoapActions runs the GOAP planner and stores the plan on the blackboard.
	actionRegistry["PlanGoapActions"] = func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		cs := b.ChainState
		if cs == nil {
			cs = make(map[string]interface{})
			b.ChainState = cs
		}

		// Extract actions and config from metadata
		actionsRaw, ok := cs["goap_actions"]
		if !ok {
			b.Outcome = "failure"
			b.Result = "no actions configured for GOAP planner"
			return -1
		}

		var plannerActions []goap.Action
		// Support both []goap.Action and []interface{} from JSON deserialization
		switch v := actionsRaw.(type) {
		case []goap.Action:
			plannerActions = v
		case []interface{}:
			for _, a := range v {
				if m, ok := a.(map[string]interface{}); ok {
					action := goap.Action{
						Name: stringField(m, "name"),
						Cost: floatField(m, "cost", 1.0),
					}
					if pre, ok := m["preconditions"].(map[string]interface{}); ok {
						action.Preconditions = goap.WorldState(worldStateFromMap(pre))
					}
					if eff, ok := m["effects"].(map[string]interface{}); ok {
						action.Effects = goap.WorldState(worldStateFromMap(eff))
					}
					plannerActions = append(plannerActions, action)
				}
			}
		}

		if len(plannerActions) == 0 {
			b.Outcome = "failure"
			b.Result = "no valid actions for GOAP planner"
			return -1
		}

		// Build planner
		config := goap.DefaultGOAPConfig()
		if cfgRaw, ok := cs["goap_config"]; ok {
			if cfg, ok := cfgRaw.(goap.GOAPTreeConfig); ok {
				config = cfg
			}
		}
		planner := goap.NewPlanner(plannerActions, config.MaxPlannerDepth, config.MaxPlannerNodes)

		// Get or create world state
		var worldState goap.WorldState
		if wsRaw, ok := cs["goap_world_state"]; ok {
			if ws, ok := wsRaw.(goap.WorldState); ok {
				worldState = ws
			}
		}
		if worldState == nil {
			worldState = make(goap.WorldState)
			// Initialize from task
			worldState["task"] = b.Task
			worldState["has_result"] = false
			worldState["task_status"] = "pending"
		}

		// Get goal
		var goal *goap.Goal
		if gRaw, ok := cs["goap_current_goal"]; ok {
			if g, ok := gRaw.(*goap.Goal); ok {
				goal = g
			}
		}
		if goal == nil {
			goal = goap.BuildGoalFromTask(b.Task)
		}

		// Plan
		plan := planner.Plan(worldState, goal)
		if plan == nil {
			b.Outcome = "failure"
			b.Result = "GOAP planner could not find a plan"
			cs["goap_plan_found"] = false
			return -1
		}

		cs["goap_plan_found"] = true
		cs["goap_plan"] = plan
		cs["goap_steps"] = planStepsToStrings(plan)
		cs["goap_step_index"] = 0
		cs["goap_world_state"] = worldState
		cs["goap_planned_goal"] = plan.Goal.Name
		b.Plan = plan.String()
		b.Outcome = "success"

		return 1
	}

	// ExecuteGoapStep executes the next step in the GOAP plan.
	actionRegistry["ExecuteGoapStep"] = func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		cs := b.ChainState
		if cs == nil {
			b.Outcome = "failure"
			return -1
		}

		idxRaw, ok := cs["goap_step_index"]
		if !ok {
			b.Outcome = "failure"
			return -1
		}
		idx := idxRaw.(int)

		stepsRaw, ok := cs["goap_steps"]
		if !ok {
			b.Outcome = "failure"
			return -1
		}
		steps := stepsRaw.([]string)

		if idx >= len(steps) {
			b.Outcome = "success"
			b.Result = "all GOAP steps completed"
			return 1
		}

		stepName := steps[idx]

		// Execute step via LLM if available
		if b.LLM != nil {
			prompt := buildGoapStepPrompt(b.Task, stepName, cs)
			result, err := b.LLM.Generate(prompt)
			if err != nil {
				b.Outcome = "failure"
				b.Result = "GOAP step " + stepName + " failed: " + err.Error()
				return -1
			}
			cs["goap_last_step_result"] = result
		} else {
			cs["goap_last_step_result"] = "step " + stepName + " marked complete (no LLM)"
		}

		// Update world state based on the plan step effects
		if plan, ok := cs["goap_plan"]; ok {
			if p, ok := plan.(*goap.Plan); ok && idx < len(p.Steps) {
				ws, ok := cs["goap_world_state"].(goap.WorldState)
				if !ok {
					ws = make(goap.WorldState)
				}
				for k, v := range p.Steps[idx].Effects {
					ws[k] = v
				}
				cs["goap_world_state"] = ws
			}
		}

		cs["goap_step_index"] = idx + 1
		cs["goap_executed_steps"] = append(getStringSlice(cs, "goap_executed_steps"), stepName)
		b.Outcome = "running"

		return 1
	}

	// GoapFallback handles the case where GOAP execution fails.
	actionRegistry["GoapFallback"] = func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		b.Outcome = "partial"
		b.Result = "GOAP execution failed, falling back to default behavior"
		return 1
	}

	// ReflectGoapOutcome reflects on the GOAP execution outcome.
	actionRegistry["ReflectGoapOutcome"] = func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		cs := b.ChainState

		planFound := false
		if cs != nil {
			if pf, ok := cs["goap_plan_found"]; ok {
				planFound = pf.(bool)
			}
		}

		if b.Outcome == "success" && planFound {
			b.Outcome = "success"
			b.Result = "GOAP plan executed successfully"
		}

		return 1
	}
}

// planStepsToStrings extracts step names from a plan.
func planStepsToStrings(plan *goap.Plan) []string {
	steps := make([]string, len(plan.Steps))
	for i, s := range plan.Steps {
		steps[i] = s.Name
	}
	return steps
}

// getStringSlice safely gets a string slice from chain state.
func getStringSlice(cs map[string]interface{}, key string) []string {
	if raw, ok := cs[key]; ok {
		if s, ok := raw.([]string); ok {
			return s
		}
	}
	return []string{}
}

// buildGoapStepPrompt creates an LLM prompt for executing a GOAP step.
func buildGoapStepPrompt(task, stepName string, cs map[string]interface{}) string {
	prompt := "You are executing a GOAP (Goal-Oriented Action Planning) step.\n"
	prompt += "Task: " + task + "\n"
	prompt += "Step: " + stepName + "\n\n"
	prompt += "Execute this step and return only the result. Be concise.\n"
	return prompt
}

func worldStateFromMap(m map[string]interface{}) map[string]interface{} {
	return m
}

func stringField(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func floatField(m map[string]interface{}, key string, def float64) float64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		}
	}
	return def
}
