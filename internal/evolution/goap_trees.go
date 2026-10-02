package evolution

import (
	"bytes"
	"encoding/json"
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
	valid := true
	for pair := range strings.SplitSeq(postconditions, ",") {
		parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			valid = false
			break
		}
		key, text := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if _, exists := pcMap[key]; exists || text == "" {
			valid = false
			break
		}
		value, err := decodeContractValue(json.RawMessage(text))
		if err != nil || !json.Valid([]byte(text)) {
			value = text
		}
		pcMap[key] = value
	}
	var declaration any = pcMap
	if !valid {
		// Keep malformed declarations visible to validation; never silently
		// drop a requested fact and turn a broken gate into a weaker one.
		declaration = postconditions
	}

	return &SerializableNode{
		Type:        "CheckpointVerifier",
		Name:        tree.Name + "_Verified",
		Description: "Checkpoint verifier: re-run " + tree.Name + " until the blackboard postconditions hold, up to the retry limit",
		MaxRetries:  maxRetries,
		Metadata: map[string]any{
			"postconditions": declaration,
			"state_key":      "goap_world_state",
		},
		Children: []SerializableNode{*tree},
	}
}

// GOAPPlanningTree returns a behavior tree that uses GOAP (Goal-Oriented
// Action Planning) to plan and execute multi-step tasks.
//
// The memory sequence retains the declared capabilities and goal while each
// GoapStep checks fresh observations. Built-in action declarations require
// capability adapters; generated prose alone cannot establish their effects.
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
		Type:        string(node.Type),
		Name:        node.Name,
		Description: goapNodeDescriptions[node.Name],
		Children:    convertGoapChildren(node.Children),
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
		Type:        string(node.Type),
		Name:        node.Name,
		Description: goapNodeDescriptions[node.Name],
		Children:    convertGoapChildren(node.Children),
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
		Type:        string(node.Type),
		Name:        node.Name,
		Description: goapNodeDescriptions[node.Name],
		Children:    convertGoapChildren(node.Children),
	}
}

// FromGoapNode converts a goap-package tree (goap.SerializableNode mirrors
// this package's type to avoid an import cycle) into an evolution tree,
// ready for engine building, validation, and persistence. Used by the
// plan→BT compiler consumers (ADR-133 Phase 3).
func FromGoapNode(node *goap.SerializableNode) *SerializableNode {
	if node == nil {
		return nil
	}
	converted := convertGoapChildren([]goap.SerializableNode{*node})
	return &converted[0]
}

// goapNodeDescriptions documents the fixed node names goap.BuildSerializableTree
// produces. These descriptions fill missing descriptions on conversion.
var goapNodeDescriptions = map[string]string{
	"SetupGoapTools":     "Initialize declared GOAP capabilities without discarding observed state",
	"GOAP_Root":          "GOAP A* planning pipeline: plan a multi-step action sequence, execute it, and reflect on the outcome",
	"HasGoapGoal":        "Detect whether the task requires multi-step planning and a GOAP goal can be derived from it",
	"PlanGoapActions":    "Run the A* planner over the configured actions to find an optimal step sequence toward the goal",
	"GoapStrategyRouter": "Execute the planned steps, falling back to a partial-result path if execution fails",
	"GoapExecutePath":    "Execute the next planned GOAP step and continue while steps remain",
	"ExecuteGoapStep":    "Execute and independently check the next GOAP capability step",
	"HasMoreGoapSteps":   "Detect whether the computed plan has remaining unexecuted steps",
	"GoapFallback":       "Report failure when GOAP execution cannot proceed",
	"ReflectGoapOutcome": "Finalize the outcome and result of the GOAP planning run",
}

// goapNodeGuards is the machine-readable counterpart to goapNodeDescriptions:
// the description is prose, but engine/typed_edges.go and
// engine/utility_selector.go gate execution on TypedEdge.Condition, and
// ValidateEdge rejects a guard edge with a blank Condition. Default guard
// metadata for fixed Condition names is attached only when absent, keeping the
// converted trees compliant with the domains package's condition-coverage
// convention (every Condition node carries a labelled guard edge).
var goapNodeGuards = map[string]TypedEdge{
	"HasGoapGoal": {
		Type:       EdgeGuard,
		Label:      "has-goap-goal",
		Condition:  "task requires multi-step planning and a GOAP goal can be derived from it",
		ChildIndex: -1,
	},
	"HasMoreGoapSteps": {
		Type:       EdgeGuard,
		Label:      "has-more-goap-steps",
		Condition:  "the computed GOAP plan has remaining unexecuted steps",
		ChildIndex: -1,
	},
}

func convertGoapChildren(children []goap.SerializableNode) []SerializableNode {
	if len(children) == 0 {
		return nil
	}
	result := make([]SerializableNode, len(children))
	for i, c := range children {
		var node SerializableNode
		data, err := json.Marshal(c)
		if err != nil {
			node.Type = "InvalidGOAPDefinition"
		} else {
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.UseNumber()
			if err = decoder.Decode(&node); err != nil {
				node.Type = "InvalidGOAPDefinition"
			}
		}
		if node.Description == "" {
			node.Description = goapNodeDescriptions[c.Name]
		}
		if guard, ok := goapNodeGuards[c.Name]; ok && len(node.Edges) == 0 {
			node.Edges = []TypedEdge{guard}
		}
		if len(c.Children) > 0 {
			node.Children = convertGoapChildren(c.Children)
		}
		result[i] = node
	}
	return result
}
