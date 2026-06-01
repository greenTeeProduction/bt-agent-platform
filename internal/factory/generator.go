package factory

import (
	"fmt"

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

	// 1. PreGate: validate conditions before execution
	if len(spec.PreChecks) > 0 {
		preNodes := make([]evolution.SerializableNode, len(spec.PreChecks))
		for i, c := range spec.PreChecks {
			preNodes[i] = evolution.SerializableNode{
				Type:        "Condition",
				Name:        c.Name,
				Description: c.Description,
			}
		}
		children = append(children, evolution.SerializableNode{
			Type:     "Sequence",
			Name:     "PreGate",
			Children: preNodes,
		})
	}

	// 2. StrategyRouter: a Selector that tries each strategy path
	strategyKids := g.buildStrategyPaths(spec)
	children = append(children, evolution.SerializableNode{
		Type:     "Selector",
		Name:     "StrategyRouter",
		Children: strategyKids,
	})

	// 3. ReflectOnOutcome (always)
	children = append(children, evolution.SerializableNode{
		Type:        "Action",
		Name:        "ReflectOnOutcome",
		Description: "Generate reflection: what went well, what to improve",
	})

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
			Type: "Retry",
			Name: "RetrySelfCorrect",
			Children: []evolution.SerializableNode{{
				Type:        "Action",
				Name:        spec.SelfCorrect.Name,
				Description: spec.SelfCorrect.Description,
			}},
			MaxRetries: 3,
		})
	}

	if spec.Fallback != nil {
		outcomeKids = append(outcomeKids, evolution.SerializableNode{
			Type:        "Action",
			Name:        spec.Fallback.Name,
			Description: spec.Fallback.Description,
		})
	} else {
		// Default escalation
		outcomeKids = append(outcomeKids, evolution.SerializableNode{
			Type:        "Action",
			Name:        "EscalateToDeepSeek",
			Description: "Escalate to external LLM for difficult tasks",
		})
	}

	children = append(children, evolution.SerializableNode{
		Type:     "Selector",
		Name:     "OutcomeSelector",
		Children: outcomeKids,
	})

	// 5. UpdateBehaviorTree (adapt on failures)
	children = append(children, evolution.SerializableNode{
		Type:        "Action",
		Name:        "UpdateBehaviorTree",
		Description: "Adapt tree on 3+ consecutive failures",
	})

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

// buildStrategyPaths converts strategy_path into a Selector's children.
// Pattern: condition → action pairs become Sequences. Standalone actions become Sequences of one.
func (g *Generator) buildStrategyPaths(spec *TreeSpec) []evolution.SerializableNode {
	var paths []evolution.SerializableNode

	var currentKids []evolution.SerializableNode

	for _, node := range spec.StrategyPath {
		switch node.Type {
		case "Condition":
			// If we have pending actions, close the current Sequence and start a new one
			if len(currentKids) > 0 {
				paths = append(paths, evolution.SerializableNode{
					Type:     "Sequence",
					Name:     fmt.Sprintf("Path_%d", len(paths)+1),
					Children: currentKids,
				})
				currentKids = nil
			}
			currentKids = append(currentKids, evolution.SerializableNode{
				Type:        "Condition",
				Name:        node.Name,
				Description: node.Description,
			})
		case "Action":
			currentKids = append(currentKids, evolution.SerializableNode{
				Type:        "Action",
				Name:        node.Name,
				Description: node.Description,
			})
		}
	}

	// Close final sequence
	if len(currentKids) > 0 {
		paths = append(paths, evolution.SerializableNode{
			Type:     "Sequence",
			Name:     fmt.Sprintf("Path_%d", len(paths)+1),
			Children: currentKids,
		})
	}

	// Always add a fallback execution path (AnalyzeTask → ExecutePlan)
	paths = append(paths, evolution.SerializableNode{
		Type: "Sequence",
		Name: "FallbackExecution",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "AnalyzeTask", Description: "LLM: analyze task complexity + intent"},
			{Type: "Action", Name: "ExecutePlan", Description: "LLM: generate and execute plan"},
		},
	})

	return paths
}
