package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// HermesSelfEvolutionTree is a meta-cognitive behavior tree for Hermes Agent
// to monitor, evaluate, and improve its own performance.
//
// Strategy paths:
//  1. Self-Monitor: periodic check of performance
//  2. Skill Evolution: update skills based on failure patterns
//  3. Strategy Optimization: improve workflows
//  4. Model/Tool Tuning: optimize model and tool selection
//  5. Knowledge Synthesis: consolidate learnings
func HermesSelfEvolutionTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type:        "Sequence",
		Name:        "HermesEvolve_Main",
		Description: "Meta-cognitive loop that analyzes and improves Hermes Agent's own performance",
		Children: []evolution.SerializableNode{
			// PreGate: validate input and setup tools
			{
				Type:        "Sequence",
				Name:        "PreGate",
				Description: "Input validation gate that must pass before the evolution cycle runs",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput", Description: "Check input is non-empty"},
					{Type: "Action", Name: "SetupDefaultTools", Description: "Populate tools for analysis and skill management"},
				},
			},
			// StrategyRouter: choose evolution strategy based on context
			{
				Type:        "Selector",
				Name:        "EvolutionRouter",
				Description: "Choose the evolution strategy that matches the detected issue",
				Children: []evolution.SerializableNode{
					// Path 1: Self-Monitor — periodic performance check
					{
						Type:        "Sequence",
						Name:        "SelfMonitorPath",
						Description: "Periodic review of recent session outcomes",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsPeriodicCheck", Description: "Trigger every N tasks or on performance drop"},
							{
								Type:        "ChainAction",
								Name:        "llm_call:Review the last 10 Hermes Agent sessions. Identify patterns: what tasks succeeded? What failed? Are there recurring error types? Categorize each failure as skill_gap, tool_misuse, model_limitation, or workflow_inefficiency. Output structured findings.",
								Description: "LLM step that categorizes recent failures by root cause",
								Metadata: map[string]any{
									"max_tokens": float64(10),
									"system_msg": "You are a self-monitoring agent for Hermes Agent. Analyze session outcomes critically but constructively.",
								},
							},
							{
								Type:        "ChainAction",
								Name:        "llm_call:Based on the failure patterns identified, prioritize the top 3 improvements that would have the biggest impact. Consider: frequency of occurrence, severity of failure, ease of fix. Output: ranked list with justification.",
								Description: "LLM step that ranks the top improvements by impact",
								Metadata:    map[string]any{"max_tokens": float64(5)},
							},
						},
					},
					// Path 2: Skill Evolution — update skills
					{
						Type:        "Sequence",
						Name:        "SkillEvolutionPath",
						Description: "Update skills to close detected gaps",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "HasSkillGaps", Description: "Detected missing or outdated skills"},
							{
								Type:        "ChainAction",
								Name:        "llm_call:Analyze the current skill set of Hermes Agent. Which skills are effective? Which are outdated? What new skills would address the identified failure patterns? Propose specific skill updates with concrete improvements.",
								Description: "LLM step that proposes concrete skill updates",
								Metadata: map[string]any{
									"max_tokens": float64(8),
									"system_msg": "You are a skill architect. Design SKILL.md updates that are specific, actionable, and testable.",
								},
							},
							{
								Type:        "ChainAction",
								Name:        "llm_call:Validate the proposed skill changes against the failure patterns they're meant to fix. For each proposed change, explain: what specific failure it prevents, how it changes Hermes behavior, and how to verify the fix works.",
								Description: "LLM step that validates proposed skill changes against the failures they fix",
								Metadata:    map[string]any{"max_tokens": float64(5)},
							},
						},
					},
					// Path 3: Strategy Optimization — improve workflows
					{
						Type:        "Sequence",
						Name:        "StrategyOptPath",
						Description: "Streamline inefficient workflows",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "HasWorkflowInefficiencies", Description: "Detected redundant steps or suboptimal patterns"},
							{
								Type:        "ChainAction",
								Name:        "llm_call:Analyze Hermes Agent's recent workflows. Look for: redundant tool calls, unnecessarily verbose responses, suboptimal model choices for task types, missed opportunities to delegate or parallelize. Propose concrete workflow optimizations.",
								Description: "LLM step that proposes workflow optimizations",
								Metadata:    map[string]any{"max_tokens": float64(8)},
							},
							{
								Type:        "ChainAction",
								Name:        "llm_call:For each optimization proposed, estimate the time/cost savings and the risk of regression. Prioritize low-risk, high-impact changes. Output an implementation plan ordered by ROI.",
								Description: "LLM step that ranks optimizations by ROI and regression risk",
								Metadata:    map[string]any{"max_tokens": float64(5)},
							},
						},
					},
					// Path 4: Model & Tool Tuning
					{
						Type:        "Sequence",
						Name:        "ModelTuningPath",
						Description: "Tune model and tool selection",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "HasModelToolIssues", Description: "Model selection or tool configuration issues detected"},
							{
								Type:        "ChainAction",
								Name:        "llm_call:Evaluate Hermes Agent's model and tool usage. Which models perform best for which task types? Are tools being used correctly? Are there configuration issues (timeouts, rate limits, missing tools)? Recommend specific changes.",
								Description: "LLM step that evaluates model and tool usage and recommends changes",
								Metadata:    map[string]any{"max_tokens": float64(8)},
							},
						},
					},
					// Path 5: Knowledge Synthesis — default fallback
					{
						Type:        "Sequence",
						Name:        "KnowledgeSynthesisPath",
						Description: "Default fallback that consolidates recent learnings",
						Children: []evolution.SerializableNode{
							{
								Type:        "ChainAction",
								Name:        "llm_call:Synthesize all recent learnings about Hermes Agent's performance. What patterns emerge across different tasks? What capabilities have improved? What new weaknesses have appeared? Produce a comprehensive self-assessment with actionable recommendations.",
								Description: "LLM step that produces a comprehensive self-assessment",
								Metadata:    map[string]any{"max_tokens": float64(10)},
							},
							{
								Type:        "ChainAction",
								Name:        "llm_call:Based on the self-assessment, update Hermes Agent's persistent memory with durable facts: what models work best for what, preferred workflows, common pitfalls to avoid, successful patterns to repeat. Save only facts that will remain useful across future sessions.",
								Description: "LLM step that saves durable facts to persistent memory",
								Metadata:    map[string]any{"max_tokens": float64(5)},
							},
						},
					},
				},
			},
			// Reflection and quality gate
			{
				Type:        "Action",
				Name:        "ReflectOnOutcome",
				Description: "What went well? What to improve in the evolution process?",
			},
			{
				Type:        "ChainAction",
				Name:        "llm_call:Generate a Self-Evolution Report summarizing: what was analyzed, what improvements were identified, what changes are recommended, and what the expected impact is. Include confidence levels for each recommendation.",
				Description: "LLM step that generates the Self-Evolution Report",
				Metadata: map[string]any{
					"max_tokens": float64(8),
					"system_msg": "You are a meta-agent producing a self-evolution report for Hermes Agent. Be specific, actionable, and honest about uncertainties.",
				},
			},
			// Outcome handling with agent-based self-correction
			{
				Type:        "Selector",
				Name:        "OutcomeSelector",
				Description: "Confirm success or fall through to self-correction",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "WasSuccessful", Description: "Evolution cycle completed successfully"},
					{
						Type:        "ChainAction",
						Name:        "llm_call:Self-correct the evolution process. What went wrong in the self-analysis? Fix any errors in reasoning and produce a corrected assessment.",
						Description: "LLM step that corrects errors in the self-analysis",
						Metadata:    map[string]any{"max_tokens": float64(5)},
					},
				},
			},
		},
	}
}
