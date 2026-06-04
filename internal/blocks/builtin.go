package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

func builtinBlocks() []Block {
	return []Block{
		{
			ID:          "core:pre_gate",
			Name:        "PreGate",
			Description: "Input validation and default tool setup",
			Category:    CategoryCore,
			Mutable:     false,
			Version:     1,
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
			Description: "Agent chain with real tools for task execution",
			Category:    CategoryTool,
			Mutable:     true,
			Version:     1,
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
			Description: "Outcome routing: success, self-correct retry, escalation",
			Category:    CategoryRecovery,
			Mutable:     false,
			Version:     1,
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
									{Type: "Action", Name: "SelfCorrect", Description: "Fix and retry"},
								},
							},
							{Type: "Action", Name: "EscalateToDeepSeek", Description: "Escalate to external LLM"},
						},
					},
					{Type: "Action", Name: "UpdateBehaviorTree", Description: "Adapt on repeated failures"},
				},
			},
		},
		{
			ID:          "core:reflect_only",
			Name:        "ReflectOnly",
			Description: "Reflection without full outcome selector",
			Category:    CategoryRecovery,
			Mutable:     true,
			Version:     1,
			Tree: &evolution.SerializableNode{
				Type: "Sequence",
				Name: "ReflectOnly",
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "ReflectOnOutcome", Description: "Reflect on output quality"},
				},
			},
		},
	}
}

// DefaultTaskBlocks is the standard on-demand task pipeline block order.
var DefaultTaskBlocks = []string{
	"core:pre_gate",
	"core:tool_execution",
	"core:error_handling",
}
