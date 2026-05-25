package langagent

import (
	"context"
	"fmt"
	"strings"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/factory"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/reflection"

	"github.com/tmc/langchaingo/agents"
	"github.com/tmc/langchaingo/chains"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/prompts"
	"github.com/tmc/langchaingo/tools"
)

// EvolvedAgent is a langchain agent wired to the evolved BT framework.
type EvolvedAgent struct {
	Agent    agents.Agent
	Executor *agents.Executor
	Tools    []tools.Tool
	BB       *engine.Blackboard
}

// Config holds all dependencies needed to create the evolved agent.
type Config struct {
	LLMClient    llm.LLM
	LangLLM      llms.Model
	RefStore     *reflection.Store
	TreeStore    *evolution.TreeStore
	AgentFactory *factory.AgentFactory
	RunTaskFn    func(task string) string
	BB           *engine.Blackboard
}

// NewEvolvedAgent creates a langchain ReAct agent with BT-framework tools.
func NewEvolvedAgent(cfg Config) (*EvolvedAgent, error) {
	if cfg.LangLLM == nil {
		return nil, fmt.Errorf("LangLLM is required")
	}

	// Build tools
	agentTools := []tools.Tool{
		NewRunTaskTool(cfg.BB, cfg.RunTaskFn),
		NewReflectTool(cfg.BB),
		NewFitnessTool(cfg.RefStore, cfg.TreeStore),
		NewEvolveTool(cfg.RefStore, cfg.TreeStore),
		NewGetTreeTool(cfg.TreeStore),
		NewGetReflectionsTool(cfg.RefStore),
	}
	if cfg.AgentFactory != nil {
		agentTools = append(agentTools, NewCreateAgentTool(cfg.AgentFactory))
	}

	// Build the evolution-aware prompt template
	prompt := buildEvolvedPrompt(agentTools)

	// Create the OneShotAgent (ReAct)
	agent := agents.NewOneShotAgent(
		cfg.LangLLM,
		agentTools,
		agents.WithPrompt(prompt),
	)

	// Create executor
	executor := agents.NewExecutor(
		agent,
		agents.WithMaxIterations(10),
		agents.WithReturnIntermediateSteps(),
	)

	return &EvolvedAgent{
		Agent:    agent,
		Executor: executor,
		Tools:    agentTools,
		BB:       cfg.BB,
	}, nil
}

// Run executes a task through the langchain agent and auto-reflects.
func (ea *EvolvedAgent) Run(ctx context.Context, task string) (string, error) {
	result, err := chains.Call(ctx, ea.Executor, map[string]any{"input": task})
	if err != nil {
		return "", fmt.Errorf("agent call: %w", err)
	}

	output, _ := result["output"].(string)

	// Auto-evolve if failure count >= 3
	failures := 0
	if ea.BB != nil && ea.BB.Reflections != nil {
		failures = ea.BB.Reflections.CountFailures()
	}
	if failures >= 3 && ea.BB != nil && ea.BB.TreeStore != nil {
		tree, err := ea.BB.TreeStore.Load()
		if err == nil && tree != nil {
			ops := []evolution.MutationOp{
				{Operation: "wrap_retry", Target: "AnalyzeTask"},
				{Operation: "increase_retries", Target: "RetrySelfCorrect"},
			}
			if evolution.ApplyMutations(tree, ops) > 0 {
				_ = ea.BB.TreeStore.Save(tree)
			}
		}
	}

	return output, nil
}

// buildEvolvedPrompt creates prompt with tool_names/tool_descriptions as PartialVariables.
func buildEvolvedPrompt(agentTools []tools.Tool) prompts.PromptTemplate {
	prefix := `You are an evolved AI agent powered by a behavior tree framework. Your behavior tree mirrors the Rust BT framework with self-improvement, reflection, and evolution capabilities.

Your behavior tree structure:
1. PreGate: validates input before execution
2. StrategyRouter: selects the best strategy (knowledge, cache, or execution path)
3. ReflectOnOutcome: analyzes what went well and what could improve
4. OutcomeSelector: checks success, retries on failure, escalates if needed  
5. UpdateBehaviorTree: evolves the tree when failures accumulate

WORKFLOW:
- Use bt_run_task to execute tasks through the behavior tree
- After execution, use bt_reflect to analyze and learn from outcomes
- Check bt_get_fitness periodically to monitor your success rate
- When you've had 3+ failures, use bt_evolve to mutate and improve the tree
- Use bt_get_reflections to learn from past experiences
- Use bt_get_tree to inspect your decision structure

You have access to the following tools:`

	instructions := `Use the following format:
Question: the input question you must answer
Thought: you should always think about what to do
Action: the action to take, should be one of [ {{.tool_names}} ]
Action Input: the input to the action
Observation: the result of the action
... (this Thought/Action/Action Input/Observation can repeat N times)
Thought: I now know the final answer
Final Answer: the final answer to the original input question`

	suffix := `Begin!

Question: {{.input}}
{{.agent_scratchpad}}`

	template := strings.Join([]string{prefix, instructions, suffix}, "\n\n")

	return prompts.PromptTemplate{
		Template:       template,
		TemplateFormat: prompts.TemplateFormatGoTemplate,
		InputVariables: []string{"input", "agent_scratchpad"},
		PartialVariables: map[string]any{
			"tool_names":        toolNames(agentTools),
			"tool_descriptions": toolDescriptions(agentTools),
		},
	}
}

func toolNames(tools []tools.Tool) string {
	var b strings.Builder
	for i, t := range tools {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(t.Name())
	}
	return b.String()
}

func toolDescriptions(tools []tools.Tool) string {
	var b strings.Builder
	for _, t := range tools {
		b.WriteString(fmt.Sprintf("- %s: %s\n", t.Name(), t.Description()))
	}
	return b.String()
}
