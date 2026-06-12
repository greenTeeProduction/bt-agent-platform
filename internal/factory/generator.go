package factory

import (
	"fmt"
	"strings"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// GeneratedAgent is a runnable agent built from a skill.
type GeneratedAgent struct {
	Name     string
	Spec     *TreeSpec
	SerTree  *evolution.SerializableNode
	Runnable btcore.Command[engine.Blackboard]
}

// Generator converts a TreeSpec into a SerializableNode + go-bt runnable tree.
type Generator struct{}

// NewGenerator creates a BT generator.
func NewGenerator() *Generator {
	return &Generator{}
}

func generatedFallbackChainAction() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "ChainAction",
		Name:        "agent:{{.Task}}",
		Description: "Execute generated-agent fallback path with real tool-capable ChainAction instead of AnalyzeTask/ExecutePlan stubs",
		Metadata: map[string]any{
			"system_msg": "You are a generated BT agent fallback executor. Analyze the task, use available tools when needed, and produce a concrete final result. Do not return placeholder text.",
			"tools":      []any{"web_search", "file_ops", "code_exec"},
			"max_tokens": float64(1024),
		},
	}
}

func generatedStepChainAction(step TreeNode) evolution.SerializableNode {
	desc := strings.TrimSpace(step.Description)
	if desc == "" {
		desc = step.Name
	}
	return evolution.SerializableNode{
		Type:        "ChainAction",
		Name:        fmt.Sprintf("agent:%s\n\nTask: {{.Task}}\nPrevious result: {{.Result}}", desc),
		Description: desc,
		Metadata: map[string]any{
			"system_msg": "You are executing one generated behavior-tree step. Use available tools when needed and return concrete, verifiable output. Do not fabricate tool results.",
			"tools":      []any{"web_search", "file_ops", "code_exec"},
			"max_tokens": float64(1024),
		},
	}
}

func generatedSelfCorrectChainAction(step TreeNode) evolution.SerializableNode {
	desc := strings.TrimSpace(step.Description)
	if desc == "" {
		desc = "Self-correct the previous output"
	}
	return evolution.SerializableNode{
		Type:        "ChainAction",
		Name:        fmt.Sprintf("llm_call:%s. Task: {{.Task}} Previous output: {{.Result}}", desc),
		Description: desc,
		Metadata: map[string]any{
			"max_tokens": float64(1024),
		},
	}
}

func generatedFallbackChainActionFromStep(step TreeNode) evolution.SerializableNode {
	desc := strings.TrimSpace(step.Description)
	if desc == "" {
		desc = "Handle the task using fallback execution"
	}
	return evolution.SerializableNode{
		Type:        "ChainAction",
		Name:        fmt.Sprintf("agent:%s\n\nTask: {{.Task}}\nPrevious output: {{.Result}}", desc),
		Description: desc,
		Metadata: map[string]any{
			"system_msg": "You are the fallback path for a generated BT agent. Recover from prior failure and produce a concrete final result using available tools when needed.",
			"tools":      []any{"web_search", "file_ops", "code_exec"},
			"max_tokens": float64(1024),
		},
	}
}

// Generate produces a GeneratedAgent from a TreeSpec and shared blackboard.
func (g *Generator) Generate(spec *TreeSpec, name string, bb *engine.Blackboard) (*GeneratedAgent, error) {
	serTree := g.buildSerializable(spec, name)

	runnable := engine.BuildTree(serTree, bb)

	return &GeneratedAgent{
		Name:     name,
		Spec:     spec,
		SerTree:  serTree,
		Runnable: runnable,
	}, nil
}

// buildSerializable converts a TreeSpec into a SerializableNode tree.
func (g *Generator) buildSerializable(spec *TreeSpec, name string) *evolution.SerializableNode {
	var children []evolution.SerializableNode

	// 1. PreGate: generated skills use known runtime nodes only. LLM-derived
	// checks are advisory descriptions, not invented Condition handler names.
	if len(spec.PreChecks) > 0 {
		children = append(children, evolution.SerializableNode{
			Type: "Sequence",
			Name: "PreGate",
			Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "ValidateInput", Description: "Generated agents require a non-empty task"},
				{Type: "Action", Name: "SetupDefaultTools", Description: "Populate default tools for generated ChainAction nodes"},
			},
		})
	}

	// 2. StrategyRouter: deterministic DecisionTree that routes on ChainState["route"].
	// If no route is set, default to the first generated path.
	strategyKids := g.buildStrategyPaths(spec)
	defaultBranch := "fallback"
	if len(strategyKids) > 0 {
		if branch, ok := strategyKids[0].Metadata["branch"].(string); ok {
			defaultBranch = branch
		}
	}
	children = append(children,
		evolution.SerializableNode{
			Type: "DecisionTree",
			Name: "StrategyRouter",
			Metadata: map[string]any{
				"key":     "route",
				"default": defaultBranch,
			},
			Children: strategyKids,
		},
		evolution.SerializableNode{
			Type:        "Action",
			Name:        "ReflectOnOutcome",
			Description: "Generate reflection: what went well, what to improve",
		},
	)

	// 4. OutcomeSelector: success detection + self-correct + escalation
	outcomeKids := []evolution.SerializableNode{
		{
			Type:        "Condition",
			Name:        "WasSuccessful",
			Description: "Exit if task succeeded",
		},
	}

	if spec.SelfCorrect != nil {
		outcomeKids = append(outcomeKids, evolution.SerializableNode{
			Type:       "Retry",
			Name:       "RetrySelfCorrect",
			Children:   []evolution.SerializableNode{generatedSelfCorrectChainAction(*spec.SelfCorrect)},
			MaxRetries: 3,
		})
	}

	if spec.Fallback != nil {
		outcomeKids = append(outcomeKids, generatedFallbackChainActionFromStep(*spec.Fallback))
	} else {
		// Default escalation
		outcomeKids = append(outcomeKids, evolution.SerializableNode{
			Type:        "Action",
			Name:        "EscalateToDeepSeek",
			Description: "Escalate to external LLM for difficult tasks",
		})
	}

	// 4. OutcomeSelector + 5. UpdateBehaviorTree
	children = append(children,
		evolution.SerializableNode{
			Type:     "Selector",
			Name:     "OutcomeSelector",
			Children: outcomeKids,
		},
		evolution.SerializableNode{
			Type:        "Action",
			Name:        "UpdateBehaviorTree",
			Description: "Adapt tree on 3+ consecutive failures",
		},
	)

	rootType := spec.RootType
	if rootType == "" {
		rootType = "Sequence"
	}
	rootName := spec.RootName
	if rootName == "" {
		rootName = fmt.Sprintf("%s_Main", name)
	}

	return &evolution.SerializableNode{
		Type:     rootType,
		Name:     rootName,
		Children: children,
	}
}

// buildStrategyPaths converts strategy_path into DecisionTree children.
// Condition nodes define branch labels; Action nodes compile to executable ChainAction nodes.
func (g *Generator) buildStrategyPaths(spec *TreeSpec) []evolution.SerializableNode {
	var paths []evolution.SerializableNode

	var currentKids []evolution.SerializableNode
	var currentBranch string

	for _, node := range spec.StrategyPath {
		switch node.Type {
		case "Condition":
			if len(currentKids) > 0 {
				paths = append(paths, evolution.SerializableNode{
					Type:     "Sequence",
					Name:     fmt.Sprintf("Path_%d", len(paths)+1),
					Children: currentKids,
					Metadata: map[string]any{"branch": currentBranch, "match": currentBranch},
				})
				currentKids = nil
			}
			currentBranch = node.Name
		case "Action":
			if currentBranch == "" {
				currentBranch = fmt.Sprintf("path_%d", len(paths)+1)
			}
			currentKids = append(currentKids, generatedStepChainAction(node))
		}
	}

	if len(currentKids) > 0 {
		paths = append(paths, evolution.SerializableNode{
			Type:     "Sequence",
			Name:     fmt.Sprintf("Path_%d", len(paths)+1),
			Children: currentKids,
			Metadata: map[string]any{"branch": currentBranch, "match": currentBranch},
		})
	}

	// Always add a real fallback execution path via ChainAction.
	// Do not emit AnalyzeTask → ExecutePlan stubs: they only produce placeholder output.
	paths = append(paths, evolution.SerializableNode{
		Type:     "Sequence",
		Name:     "FallbackExecution",
		Children: []evolution.SerializableNode{generatedFallbackChainAction()},
		Metadata: map[string]any{"branch": "fallback", "default": true},
	})

	return paths
}
