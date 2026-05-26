package goap

import (
	"encoding/json"
	"fmt"
	"strings"
)

// --- Blackboard Integration ---

// Blackboard is the interface that internal/engine.Blackboard must satisfy for GOAP.
// This avoids circular imports.
type Blackboard interface {
	GetTask() string
	GetPlan() string
	GetResult() string
	GetOutcome() string
	SetOutcome(string)
	SetPlan(string)
	SetResult(string)
	GetChainState() map[string]interface{}
}

// BlackboardBridge connects a GOAP agent to a BT blackboard.
// It maps WorldState to/from the blackboard's chain state and task/plan/result fields.
type BlackboardBridge struct {
	Agent      *Agent
	Blackboard Blackboard
	LLMActions map[string]string // action name -> LLM prompt template
}

// NewBlackboardBridge creates a bridge between a GOAP agent and a BT blackboard.
func NewBlackboardBridge(agent *Agent, bb Blackboard) *BlackboardBridge {
	return &BlackboardBridge{
		Agent:      agent,
		Blackboard: bb,
		LLMActions: make(map[string]string),
	}
}

// RegisterLLMAction maps a GOAP action name to an LLM prompt template.
// When the action executes, the LLM is called with this prompt.
// Template variables: {{.State}}, {{.Goal}}, {{.ActionName}}
func (b *BlackboardBridge) RegisterLLMAction(name, promptTemplate string) {
	b.LLMActions[name] = promptTemplate
}

// SyncFromBB reads the blackboard's state into the GOAP agent's WorldState.
func (b *BlackboardBridge) SyncFromBB() {
	// Read chain state
	cs := b.Blackboard.GetChainState()
	if cs == nil {
		return
	}

	// Map known keys
	keyMap := map[string]string{
		"goap_state":     "goap_state",
		"goal_priority":  "goal_priority",
		"task_type":      "task_type",
		"task_status":    "task_status",
		"has_resources":  "has_resources",
		"has_plan":       "has_plan",
		"has_result":     "has_result",
	}

	for bbKey, wsKey := range keyMap {
		if v, ok := cs[bbKey]; ok {
			b.Agent.SetState(wsKey, v)
		}
	}
}

// SyncToBB writes the GOAP agent's WorldState into the blackboard.
func (b *BlackboardBridge) SyncToBB() {
	cs := b.Blackboard.GetChainState()
	if cs == nil {
		return
	}
	// Write all world state to chain state
	for k, v := range b.Agent.WorldState {
		cs[k] = v
	}
}

// PlanAndSync runs the planner and writes the plan to the blackboard.
func (b *BlackboardBridge) PlanAndSync() *AgentRun {
	b.SyncFromBB()
	run := b.Agent.Run()

	if run.Plan != nil {
		b.Blackboard.SetPlan(run.Plan.String())
	}

	cs := b.Blackboard.GetChainState()
	if cs != nil {
		cs["goap_run"] = run
		cs["goap_status"] = string(run.Status)
		cs["goap_steps"] = run.StepsTaken
	}
	b.SyncToBB()

	if run.Status == AgentSucceeded {
		b.Blackboard.SetOutcome("success")
		b.Blackboard.SetResult(run.Plan.String())
	} else {
		b.Blackboard.SetOutcome("failure")
		b.Blackboard.SetResult(fmt.Sprintf("GOAP failed: %s", run.Error))
	}

	return run
}

// --- BT Node Builders ---

// BTNodeType represents the type of BT node to create.
type BTNodeType string

const (
	NodeSequence  BTNodeType = "Sequence"
	NodeSelector  BTNodeType = "Selector"
	NodeCondition BTNodeType = "Condition"
	NodeAction    BTNodeType = "Action"
)

// SerializableNode mirrors engine.SerializableNode to avoid import cycles.
type SerializableNode struct {
	Type     BTNodeType         `json:"type"`
	Name     string             `json:"name"`
	Children []SerializableNode `json:"children,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// GOAPTreeDefinition is a complete behavior tree that integrates GOAP planning.
type GOAPTreeDefinition struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Goals       []*Goal            `json:"goals"`
	Actions     []Action           `json:"actions"`
	LLMPrompts  map[string]string  `json:"llm_prompts,omitempty"` // action -> prompt template
	Config      GOAPTreeConfig     `json:"config"`
}

// GOAPTreeConfig configures the GOAP-BT integration.
type GOAPTreeConfig struct {
	MaxPlannerDepth int  `json:"max_planner_depth"`
	MaxPlannerNodes int  `json:"max_planner_nodes"`
	MaxReplans      int  `json:"max_replans"`
	FallbackOnFail  bool `json:"fallback_on_fail"` // allow fallback if GOAP fails
}

// DefaultGOAPConfig returns a sensible default configuration.
func DefaultGOAPConfig() GOAPTreeConfig {
	return GOAPTreeConfig{
		MaxPlannerDepth: 50,
		MaxPlannerNodes: 10000,
		MaxReplans:      3,
		FallbackOnFail:  true,
	}
}

// BuildSerializableTree creates a BT structure that wraps GOAP planning.
// The tree structure:
//
//	Sequence: GOAP_Root
//	  Condition: HasGoapGoal           ← triggers only if goals are set
//	  Action:    PlanGoapActions       ← runs the planner
//	  Selector:  GoapStrategyRouter
//	    Sequence: GoapExecutePath      ← executes the plan step by step
//	      Action: ExecuteNextGoapStep
//	      Condition: HasMoreGoapSteps
//	    Action: GoapFallback           ← fallback if execution fails
//	  Action:    ReflectGoapOutcome    ← post-execution reflection
func BuildSerializableTree(def GOAPTreeDefinition) SerializableNode {
	return SerializableNode{
		Type: "Sequence",
		Name: "GOAP_Root",
		Children: []SerializableNode{
			{
				Type: "Condition",
				Name: "HasGoapGoal",
				Metadata: map[string]interface{}{
					"goap_goals": def.Goals,
					"goap_actions": def.Actions,
					"goap_config": def.Config,
					"goap_llm_prompts": def.LLMPrompts,
				},
			},
			{
				Type: "Action",
				Name: "PlanGoapActions",
				Metadata: map[string]interface{}{
					"goap_actions": def.Actions,
					"goap_config": def.Config,
				},
			},
			{
				Type: "Selector",
				Name: "GoapStrategyRouter",
				Children: []SerializableNode{
					{
						Type: "Sequence",
						Name: "GoapExecutePath",
						Children: []SerializableNode{
							{
								Type: "Action",
								Name: "ExecuteGoapStep",
								Metadata: map[string]interface{}{
									"goap_llm_prompts": def.LLMPrompts,
								},
							},
							{
								Type: "Condition",
								Name: "HasMoreGoapSteps",
							},
						},
					},
					{
						Type: "Action",
						Name: "GoapFallback",
					},
				},
			},
			{
				Type: "Action",
				Name: "ReflectGoapOutcome",
			},
		},
	}
}

// --- JSON Conversion Helpers ---

// ToJSON serializes the tree definition to JSON.
func (def GOAPTreeDefinition) ToJSON() ([]byte, error) {
	return json.MarshalIndent(def, "", "  ")
}

// FromJSON deserializes a tree definition from JSON.
func FromJSON(data []byte) (*GOAPTreeDefinition, error) {
	var def GOAPTreeDefinition
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	if def.Config.MaxPlannerDepth == 0 {
		def.Config = DefaultGOAPConfig()
	}
	return &def, nil
}

// --- Domain-Specific Helpers ---

// BuildGoalFromTask creates a goal from a natural language task description.
// Simple heuristic: extracts key phrases and maps to goal conditions.
func BuildGoalFromTask(task string) *Goal {
	lower := strings.ToLower(task)

	conditions := make(WorldState)

	if strings.Contains(lower, "build") || strings.Contains(lower, "create") || strings.Contains(lower, "implement") {
		conditions["task_type"] = "build"
		conditions["has_result"] = true
	}
	if strings.Contains(lower, "test") || strings.Contains(lower, "verify") || strings.Contains(lower, "validate") {
		conditions["task_type"] = "test"
		conditions["has_result"] = true
	}
	if strings.Contains(lower, "deploy") || strings.Contains(lower, "release") || strings.Contains(lower, "ship") {
		conditions["task_type"] = "deploy"
		conditions["has_result"] = true
	}
	if strings.Contains(lower, "research") || strings.Contains(lower, "analyze") || strings.Contains(lower, "investigate") {
		conditions["task_type"] = "research"
		conditions["has_result"] = true
	}
	if strings.Contains(lower, "fix") || strings.Contains(lower, "debug") || strings.Contains(lower, "resolve") {
		conditions["task_type"] = "fix"
		conditions["has_result"] = true
	}

	// Default goal: task completed
	if len(conditions) == 0 {
		conditions["task_type"] = "general"
		conditions["task_status"] = "completed"
	}

	// Always want a completed status
	if _, ok := conditions["has_result"]; !ok {
		conditions["task_status"] = "completed"
	}

	return &Goal{
		Name:       extractGoalName(task),
		Priority:   0.5,
		Conditions: conditions,
	}
}

func extractGoalName(task string) string {
	if len(task) > 40 {
		return task[:40] + "..."
	}
	return task
}

// StandardActions returns a set of standard GOAP actions for task execution.
func StandardActions() []Action {
	return []Action{
		{
			Name:          "analyze_requirements",
			Cost:          1.0,
			Preconditions: WorldState{"has_result": false},
			Effects:       WorldState{"has_analysis": true},
		},
		{
			Name:          "gather_resources",
			Cost:          1.0,
			Preconditions: WorldState{"has_analysis": true},
			Effects:       WorldState{"has_resources": true},
		},
		{
			Name:          "execute_build",
			Cost:          2.0,
			Preconditions: WorldState{"has_resources": true, "task_type": "build"},
			Effects:       WorldState{"has_result": true, "task_status": "completed"},
		},
		{
			Name:          "execute_test",
			Cost:          1.5,
			Preconditions: WorldState{"has_resources": true, "task_type": "test"},
			Effects:       WorldState{"has_result": true, "task_status": "completed"},
		},
		{
			Name:          "execute_deploy",
			Cost:          3.0,
			Preconditions: WorldState{"has_resources": true, "task_type": "deploy"},
			Effects:       WorldState{"has_result": true, "task_status": "completed"},
		},
		{
			Name:          "execute_research",
			Cost:          2.0,
			Preconditions: WorldState{"has_resources": true, "task_type": "research"},
			Effects:       WorldState{"has_result": true, "task_status": "completed"},
		},
		{
			Name:          "execute_fix",
			Cost:          2.5,
			Preconditions: WorldState{"has_resources": true, "task_type": "fix"},
			Effects:       WorldState{"has_result": true, "task_status": "completed"},
		},
		{
			Name:          "execute_general",
			Cost:          1.0,
			Preconditions: WorldState{"task_type": "general"},
			Effects:       WorldState{"has_result": true, "task_status": "completed"},
		},
		{
			Name:          "verify_completion",
			Cost:          0.5,
			Preconditions: WorldState{"has_result": true},
			Effects:       WorldState{"task_status": "completed", "has_verification": true},
		},
	}
}

// SetupStandardRegistry creates an action registry with standard actions that use LLM for execution.
func SetupStandardRegistry(executor func(actionName string, state WorldState) (WorldState, error)) ActionRegistry {
	standardActions := StandardActions()
	registry := make(ActionRegistry, len(standardActions))
	for _, action := range standardActions {
		name := action.Name
		registry[name] = func(state WorldState) (WorldState, error) {
			return executor(name, state)
		}
	}
	return registry
}
