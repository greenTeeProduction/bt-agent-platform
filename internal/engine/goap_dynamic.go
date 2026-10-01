package engine

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/goap"
	"github.com/nico/go-bt-evolve/internal/reliability"
	btcore "github.com/rvitorper/go-bt/core"
)

type goapRuntimeStep struct {
	plan    *goap.Plan
	index   int
	command btcore.Command[Blackboard]
}

func decodeGoapValue(raw any, out any) error {
	data, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(out)
}

// Metadata is applied when the selected node runs, never while unrelated
// branches are being built. Runtime observations always survive setup/replan.
func applyGoapMetadata(b *Blackboard, node *evolution.SerializableNode) error {
	if node.Name == "VerifyGoapGoal" && node.Metadata["goap_final_goal"] == nil {
		return fmt.Errorf("GOAP completion requires a declared final goal")
	}
	if b.ChainState == nil {
		b.ChainState = map[string]any{}
	}
	cs := b.ChainState
	for _, key := range []string{"goap_actions", "goap_goals", "goap_config", "goap_llm_prompts"} {
		if value, ok := node.Metadata[key]; ok && value != nil {
			var detached any
			if err := decodeGoapValue(value, &detached); err != nil {
				return err
			}
			cs[key] = detached
			if key == "goap_goals" {
				var goals []*goap.Goal
				if err := decodeGoapValue(value, &goals); err != nil {
					return err
				}
				var selected *goap.Goal
				for _, goal := range goals {
					if goal != nil && (selected == nil || goal.Priority > selected.Priority) {
						selected = goal
					}
				}
				if selected != nil {
					cs["goap_current_goal"] = selected
				}
			}
		}
	}
	if raw, ok := node.Metadata["goap_initial_state"]; ok {
		var initial goap.WorldState
		if err := decodeGoapValue(raw, &initial); err != nil {
			return err
		}
		state := goapWorldStateFrom(b).Clone()
		for key, value := range initial {
			if _, exists := state[key]; !exists {
				state[key] = value
			}
		}
		cs[goapWorldStateChainKey] = state
	}
	if raw, ok := node.Metadata["goap_final_goal"]; ok {
		var goal goap.Goal
		if err := decodeGoapValue(raw, &goal); err != nil {
			return err
		}
		if len(goal.Conditions) == 0 || !goapWorldStateFrom(b).Satisfies(goal.Conditions) {
			return fmt.Errorf("observed GOAP state does not satisfy the declared final goal")
		}
		b.Outcome = "success"
	}
	return nil
}

func goapNodeHasMetadata(node *evolution.SerializableNode) bool {
	switch node.Name {
	case "VerifyGoapGoal":
		return true
	case "SeedGoapState", "SetupGoapTools", "HasGoapGoal", "PlanGoapActions", "ExecuteGoapStep":
		return len(node.Metadata) > 0
	default:
		return false
	}
}

func setupObservedGoapTools(ctx *btcore.BTContext[Blackboard]) int {
	b := ctx.Blackboard
	if b.ChainState == nil {
		b.ChainState = map[string]any{}
	}
	cs := b.ChainState
	if actions, ok := cs["goap_actions"]; !ok || actions == nil {
		cs["goap_actions"] = goap.StandardActions()
	}
	if _, ok := cs["goap_goals"]; !ok {
		cs["goap_goals"] = []*goap.Goal{goap.NewGoal("task_completed", 1, goap.WorldState{"task_status": "completed"})}
	}
	if _, ok := cs["goap_config"]; !ok {
		cs["goap_config"] = goap.DefaultGOAPConfig()
	}
	state := goapWorldStateFrom(b).Clone()
	initial := goap.DefaultInitialState()
	initial["task"], initial["task_status"] = b.Task, "pending"
	for key, value := range initial {
		if _, ok := state[key]; !ok {
			state[key] = value
		}
	}
	cs[goapWorldStateChainKey] = state
	return 1
}

func executeObservedGoapStep(ctx *btcore.BTContext[Blackboard]) int {
	b := ctx.Blackboard
	fail := func(err error) int { return failGoapExecution(b, err) }
	if b.applyExecutionStop() {
		return -1
	}
	plan, ok := b.ChainState["goap_plan"].(*goap.Plan)
	if !ok || plan == nil || plan.Goal == nil {
		return fail(fmt.Errorf("GOAP execution requires its full planned capabilities and goal"))
	}
	idx, ok := b.ChainState["goap_step_index"].(int)
	if !ok || idx < 0 || idx > len(plan.Steps) {
		return fail(fmt.Errorf("invalid GOAP execution cursor"))
	}
	complete := func() int {
		if len(plan.Goal.Conditions) == 0 || !goapWorldStateFrom(b).Satisfies(plan.Goal.Conditions) {
			return fail(fmt.Errorf("observed GOAP facts do not satisfy the goal"))
		}
		b.Outcome = "success"
		return 1
	}
	if idx == len(plan.Steps) {
		return complete()
	}
	step := b.goapStepRuntime
	if step == nil || step.plan != plan || step.index != idx {
		var prompts map[string]string
		if raw, ok := b.ChainState["goap_llm_prompts"]; ok {
			if err := decodeGoapValue(raw, &prompts); err != nil {
				return fail(err)
			}
		}
		node := goap.CompileActionStep(idx, plan.Steps[idx], goap.CompileOptions{LLMPrompts: prompts, KnownAction: func(name string) bool { return GetAction(name) != nil }})
		definition := evolution.FromGoapNode(&node)
		if info := ValidateTreeFull(definition); !info.Valid() {
			return fail(fmt.Errorf("invalid GOAP executor: %v", info.Errors))
		}
		step = &goapRuntimeStep{plan: plan, index: idx, command: buildNode(definition, b, "")}
		b.goapStepRuntime = step
	}
	status := step.command.Run(ctx)
	if status != 1 {
		return status
	}
	results := getStepResults(b.ChainState)
	b.ChainState["goap_step_results"] = append(results, GoapStepResult{Step: idx + 1, Result: resolvedResult(b)})
	b.ChainState["goap_last_step_result"] = resolvedResult(b)
	b.ChainState["goap_executed_steps"] = append(getStringSlice(b.ChainState, "goap_executed_steps"), plan.Steps[idx].Name)
	b.ChainState["goap_step_index"] = idx + 1
	if idx+1 == len(plan.Steps) {
		return complete()
	}
	b.Outcome = "running"
	return 0
}

func failGoapExecution(b *Blackboard, err error) int {
	var stop error = &reliability.ExecutionStoppedError{Outcome: "failure", Err: err}
	for _, receipt := range b.EvidenceEffects() {
		if receipt.WriteCommitted {
			stop = &reliability.ExecutionUncertainError{Err: err}
			break
		}
	}
	b.stopExecution(err.Error(), stop)
	b.applyExecutionStop()
	if b.ChainState != nil {
		b.ChainState["goap_effect_error"] = err.Error()
	}
	return -1
}
