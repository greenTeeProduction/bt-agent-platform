package engine

import (
	"strings"
)

// ParamAction is a GOAP action with runtime parameter binding.
// The ActionFunc, Preconditions, and Effects fields all support
// template substitution using ${param_name} syntax.
type ParamAction struct {
	GOAPAction
	Params map[string]string // parameter name -> default value
}

// Bind replaces template variables with concrete values from bindings.
func (pa *ParamAction) Bind(bindings map[string]string) GOAPAction {
	bound := GOAPAction{
		Name:       bindTemplate(pa.Name, bindings),
		Cost:       pa.Cost,
		ActionFunc: bindTemplate(pa.ActionFunc, bindings),
	}

	bound.Preconditions = make(map[string]bool, len(pa.Preconditions))
	for k, v := range pa.Preconditions {
		bound.Preconditions[bindTemplate(k, bindings)] = v
	}

	bound.Effects = make(map[string]bool, len(pa.Effects))
	for k, v := range pa.Effects {
		bound.Effects[bindTemplate(k, bindings)] = v
	}

	return bound
}

// bindTemplate replaces ${key} patterns with their values.
func bindTemplate(tmpl string, bindings map[string]string) string {
	result := tmpl
	for k, v := range bindings {
		result = strings.ReplaceAll(result, "${"+k+"}", v)
	}
	return result
}

// ParamPlannerNode extends PlannerNode with parameterized actions.
// It resolves parameters from the Blackboard before planning.
type ParamPlannerNode struct {
	PlannerNode
	Templates []ParamAction
}

// PlanWithContext resolves template parameters from bindings and plans.
func (pp *ParamPlannerNode) PlanWithContext(worldState map[string]bool, bindings map[string]string) Plan {
	pp.Actions = make([]GOAPAction, len(pp.Templates))
	for i, tmpl := range pp.Templates {
		pp.Actions[i] = tmpl.Bind(bindings)
	}

	return pp.Plan(worldState)
}
