package evolution

import (
	"strings"

	"github.com/nico/go-bt-evolve/internal/goap"
)

// WrapWithCheckpointVerifier wraps a SerializableNode tree with a CheckpointVerifier
// decorator node. The verifier snapshots world state before child execution and
// validates postconditions after. On mismatch or failure, it restores state and retries.
//
// Parameters:
//   - tree: the subtree to wrap
//   - maxRetries: maximum retry attempts (defaults to 3 in the engine if <= 0)
//   - postconditions: comma-separated key=value pairs, e.g. "has_result=true,task_status=completed"
func WrapWithCheckpointVerifier(tree *SerializableNode, maxRetries int, postconditions string) *SerializableNode {
	pcMap := make(map[string]any)
	if postconditions != "" {
		for _, pair := range strings.Split(postconditions, ",") {
			parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
			if len(parts) == 2 {
				// Parse boolean values; non-"true" values default to true for string
				// postconditions like "task_status=completed" that represent factual states.
				pcMap[parts[0]] = parts[1] == "true"
			}
		}
	}

	return &SerializableNode{
		Type:       "CheckpointVerifier",
		Name:       tree.Name + "_Verified",
		MaxRetries: maxRetries,
		Metadata: map[string]any{
			"postconditions": pcMap,
		},
		Children: []SerializableNode{*tree},
	}
}

// GOAPPlanningTree returns a behavior tree that uses GOAP (Goal-Oriented
// Action Planning) to plan and execute multi-step tasks.
//
// This tree is ideal for complex tasks that require sequential planning:
// build pipelines, deployment sequences, research workflows, multi-phase
// operations where the order of steps matters and preconditions must be satisfied.
//
// Structure:
//
//	Sequence: GOAP_Root
//	  HasGoapGoal         ← condition: detects if task needs multi-step planning
//	  PlanGoapActions     ← action: runs A* planner to find optimal action sequence
//	  GoapStrategyRouter  ← selector: try execution, fallback on failure
//	    GoapExecutePath   ← sequence: execute each step via LLM
//	    GoapFallback      ← fallback: mark partial and continue
//	  ReflectGoapOutcome  ← action: finalize outcome and result
func GOAPPlanningTree() *SerializableNode {
	def := goap.GOAPTreeDefinition{
		Name:        "goap_planning",
		Description: "GOAP multi-step action planning tree",
		Goals:       nil, // goals are derived from task text
		Actions:     goap.StandardActions(),
		Config:      goap.DefaultGOAPConfig(),
	}

	node := goap.BuildSerializableTree(def)
	return &SerializableNode{
		Type:     string(node.Type),
		Name:     node.Name,
		Children: convertGoapChildren(node.Children),
	}
}

// GOAPResearchTree returns a research-specific GOAP tree for multi-phase
// investigation tasks (literature review → hypothesis → experiment → conclusion).
func GOAPResearchTree() *SerializableNode {
	actions := []goap.Action{
		{Name: "literature_review", Cost: 2.0,
			Preconditions: goap.WorldState{"has_research_plan": false},
			Effects:       goap.WorldState{"has_research_plan": true}},
		{Name: "formulate_hypothesis", Cost: 1.5,
			Preconditions: goap.WorldState{"has_research_plan": true},
			Effects:       goap.WorldState{"has_hypothesis": true}},
		{Name: "design_experiment", Cost: 2.0,
			Preconditions: goap.WorldState{"has_hypothesis": true},
			Effects:       goap.WorldState{"has_experiment_design": true}},
		{Name: "run_experiment", Cost: 3.0,
			Preconditions: goap.WorldState{"has_experiment_design": true},
			Effects:       goap.WorldState{"has_experiment_results": true}},
		{Name: "analyze_results", Cost: 2.0,
			Preconditions: goap.WorldState{"has_experiment_results": true},
			Effects:       goap.WorldState{"has_analysis": true}},
		{Name: "draw_conclusions", Cost: 1.0,
			Preconditions: goap.WorldState{"has_analysis": true},
			Effects:       goap.WorldState{"has_result": true, "task_status": "completed"}},
	}

	def := goap.GOAPTreeDefinition{
		Name:        "goap_research",
		Description: "GOAP multi-phase research pipeline",
		Actions:     actions,
		Config:      goap.DefaultGOAPConfig(),
	}

	node := goap.BuildSerializableTree(def)
	return &SerializableNode{
		Type:     string(node.Type),
		Name:     node.Name,
		Children: convertGoapChildren(node.Children),
	}
}

// GOAPDevOpsTree returns a DevOps-specific GOAP tree for CI/CD pipeline tasks.
func GOAPDevOpsTree() *SerializableNode {
	actions := []goap.Action{
		{Name: "checkout_code", Cost: 1.0,
			Preconditions: goap.WorldState{"has_code": false},
			Effects:       goap.WorldState{"has_code": true}},
		{Name: "run_linter", Cost: 2.0,
			Preconditions: goap.WorldState{"has_code": true, "linted": false},
			Effects:       goap.WorldState{"linted": true}},
		{Name: "run_tests", Cost: 3.0,
			Preconditions: goap.WorldState{"linted": true, "tested": false},
			Effects:       goap.WorldState{"tested": true}},
		{Name: "build_artifact", Cost: 2.0,
			Preconditions: goap.WorldState{"tested": true, "built": false},
			Effects:       goap.WorldState{"built": true}},
		{Name: "deploy_staging", Cost: 2.0,
			Preconditions: goap.WorldState{"built": true, "deployed": false},
			Effects:       goap.WorldState{"deployed": true}},
		{Name: "run_smoke_tests", Cost: 1.5,
			Preconditions: goap.WorldState{"deployed": true, "smoke_tested": false},
			Effects:       goap.WorldState{"smoke_tested": true, "has_result": true, "task_status": "completed"}},
	}

	def := goap.GOAPTreeDefinition{
		Name:        "goap_devops",
		Description: "GOAP CI/CD pipeline execution",
		Actions:     actions,
		Config:      goap.DefaultGOAPConfig(),
	}

	node := goap.BuildSerializableTree(def)
	return &SerializableNode{
		Type:     string(node.Type),
		Name:     node.Name,
		Children: convertGoapChildren(node.Children),
	}
}

func convertGoapChildren(children []goap.SerializableNode) []SerializableNode {
	result := make([]SerializableNode, len(children))
	for i, c := range children {
		node := SerializableNode{
			Type:     string(c.Type),
			Name:     c.Name,
			Metadata: c.Metadata,
		}
		if len(c.Children) > 0 {
			node.Children = convertGoapChildren(c.Children)
		}
		result[i] = node
	}
	return result
}
