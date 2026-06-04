package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

func builtinBlocks() []Block {
	blocks := []Block{
		{
			ID:          "core:pre_gate",
			Name:        "PreGate",
			Description: "Input validation and default tool setup (timeout + graceful validation handling)",
			Category:    CategoryCore,
			Mutable:     false,
			Version:     2,
			Tree: &evolution.SerializableNode{
				Type: "Sequence",
				Name: "PreGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput", Description: "Non-empty task"},
					{Type: "Condition", Name: "HasClearTask", Description: "Task has clear goal"},
					{Type: "Action", Name: "SetupDefaultTools", Description: "Populate bb.ChainTools"},
				},
			},
		},
		{
			ID:          "core:tool_execution",
			Name:        "ToolExecution",
			Description: "Reliable agent execution: CB, retry, 5m timeout, graceful transient/timeout recovery",
			Category:    CategoryTool,
			Mutable:     true,
			Version:     2,
			Tree: &evolution.SerializableNode{
				Type: "Sequence",
				Name: "ToolExecution",
				Children: []evolution.SerializableNode{
					{
						Type:        "ChainAction",
						Name:        "agent:Execute the task using available tools.\n\nTask: {{.Task}}\nPlan: {{.Plan}}",
						Description: "Tool-capable ReAct execution",
						Metadata: map[string]any{
							"system_msg": "You are a tool execution agent. Use file_read, shell_exec, and http_get when needed. Produce concrete results.",
							"tools":      []any{"file_read", "shell_exec", "http_get"},
							"max_tokens": float64(2048),
						},
					},
				},
			},
		},
		{
			ID:          "core:error_handling",
			Name:        "ErrorHandling",
			Description: "Reliable outcome routing with timeout-wrapped self-correct and escalation",
			Category:    CategoryRecovery,
			Mutable:     false,
			Version:     2,
			Tree: &evolution.SerializableNode{
				Type: "Sequence",
				Name: "ErrorHandling",
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "ReflectOnOutcome", Description: "Reflect on output quality"},
					{
						Type: "Selector",
						Name: "OutcomeSelector",
						Children: []evolution.SerializableNode{
							{Type: "Action", Name: "MarkSuccessful", Description: "Mark task successful"},
							{
								Type:       "Retry",
								Name:       "RetrySelfCorrect",
								MaxRetries: 3,
								Children: []evolution.SerializableNode{
									{
										Type:      "Timeout",
										Name:      "SelfCorrect_Timeout",
										TimeoutMs: 120_000,
										Children: []evolution.SerializableNode{
											{Type: "Action", Name: "SelfCorrect", Description: "Fix and retry"},
										},
									},
								},
							},
							{
								Type:      "Timeout",
								Name:      "Escalate_Timeout",
								TimeoutMs: 180_000,
								Children: []evolution.SerializableNode{
									{Type: "Action", Name: "EscalateToDeepSeek", Description: "Escalate to external LLM"},
								},
							},
						},
					},
					{Type: "Action", Name: "UpdateBehaviorTree", Description: "Adapt on repeated failures"},
				},
			},
		},
		{
			ID:          "core:human_gate",
			Name:        "HumanGate",
			Description: "Human-in-the-loop checkpoint before risky execution",
			Category:    CategoryCore,
			Mutable:     false,
			Version:     1,
			Tree: func() *evolution.SerializableNode {
				n := HumanGateBlock("HumanGate", "Review the task and approve before the agent executes tools or makes external changes.")
				return &n
			}(),
		},

		{
			ID:          "core:plan",
			Name:        "Plan",
			Description: "Assess complexity and generate execution plan",
			Category:    CategoryCore,
			Mutable:     false,
			Version:     1,
			Tree: func() *evolution.SerializableNode {
				n := PlanBlock()
				return &n
			}(),
		},
		{
			ID:          "core:rag_gate",
			Name:        "RAGGate",
			Description: "Knowledge graph / cache lookup before expensive LLM calls",
			Category:    CategoryCore,
			Mutable:     true,
			Version:     1,
			Tree: func() *evolution.SerializableNode {
				n := RAGGateBlock()
				return &n
			}(),
		},
		{
			ID:          "core:clarify_gate",
			Name:        "ClarifyGate",
			Description: "Ask clarifying questions when the task is ambiguous",
			Category:    CategoryCore,
			Mutable:     false,
			Version:     1,
			Tree: func() *evolution.SerializableNode {
				n := ClarifyGateBlock()
				return &n
			}(),
		},
		{
			ID:          "core:quality_gate",
			Name:        "QualityGate",
			Description: "Validate output quality before marking success",
			Category:    CategoryCore,
			Mutable:     false,
			Version:     1,
			Tree: func() *evolution.SerializableNode {
				n := QualityGateBlock()
				return &n
			}(),
		},
		{
			ID:          "core:strategy_router",
			Name:        "StrategyRouter",
			Description: "Reusable intent router shell for ComposeSpec.Middle",
			Category:    CategoryCore,
			Mutable:     true,
			Version:     1,
			Tree: func() *evolution.SerializableNode {
				n := StrategyRouterBlock()
				return &n
			}(),
		},
		{
			ID:          "core:reflect_only",
			Name:        "ReflectOnly",
			Description: "Reflection with timeout guard",
			Category:    CategoryRecovery,
			Mutable:     true,
			Version:     2,
			Tree: &evolution.SerializableNode{
				Type: "Sequence",
				Name: "ReflectOnly",
				Children: []evolution.SerializableNode{
					{
						Type:      "Timeout",
						Name:      "Reflect_Timeout",
						TimeoutMs: 60_000,
						Children: []evolution.SerializableNode{
							{Type: "Action", Name: "ReflectOnOutcome", Description: "Reflect on output quality"},
						},
					},
				},
			},
		},
	}
	for i := range blocks {
		switch blocks[i].ID {
		case "core:pre_gate":
			ApplyReliability(&blocks[i], SpecPreGate)
		case "core:tool_execution":
			ApplyReliability(&blocks[i], SpecToolExecution)
		case "core:error_handling":
			ApplyReliability(&blocks[i], SpecErrorHandling)

		case "core:plan":
			ApplyReliability(&blocks[i], SpecPlan)
		case "core:rag_gate":
			ApplyReliability(&blocks[i], SpecRAGGate)
		case "core:clarify_gate":
			ApplyReliability(&blocks[i], SpecClarifyGate)
		case "core:quality_gate":
			ApplyReliability(&blocks[i], SpecQualityGate)
		case "core:reflect_only":
			ApplyReliability(&blocks[i], SpecReflect)
		}
	}
	return blocks
}

// DefaultTaskBlocks is the standard on-demand task pipeline block order.
var DefaultTaskBlocks = []string{
	"core:pre_gate",
	"core:tool_execution",
	"core:error_handling",
}
