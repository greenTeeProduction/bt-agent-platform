package research

import "github.com/nico/go-bt-evolve/internal/evolution"

// DeepResearchTree — updated with agent and tool nodes.
//
// Phase 1: Validate & setup tools
// Phase 2: Clarify ambiguous queries
// Phase 3: Agent-based iterative research (search → read → analyze → cross-ref → gap-detect)
// Phase 4: Synthesize structured report via refine chain
// Phase 5: Quality gate
func DeepResearchTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "DeepResearch_Main",
		Children: []evolution.SerializableNode{
			// Phase 1: Validate and setup
			{
				Type: "Sequence",
				Name: "PreGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput", Description: "Non-empty query"},
					{Type: "Condition", Name: "IsResearchQuery", Description: "Detect research / investigate / analyze / what is / how does keywords"},
					{Type: "Action", Name: "SetupResearchTools", Description: "Populate bb.ChainTools with web_search, knowledge_graph, calculator"},
				},
			},
			// Phase 2: Clarify ambiguous queries
			{
				Type: "Selector",
				Name: "ClarificationGate",
				Children: []evolution.SerializableNode{
					{
						Type: "Sequence",
						Name: "NeedsClarification",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "IsAmbiguousQuery"},
							{Type: "Action", Name: "AskClarifyingQuestions"},
							{Type: "Action", Name: "RefineQueryWithAnswers"},
						},
					},
					{Type: "Action", Name: "ProceedDirectly"},
				},
			},
			// Phase 3: Agent-based iterative research
			// Replaces: DecomposeQuery → AssessComplexity → SearchBroad → Filter → Extract → CrossRef → GapDetect
			{
				Type: "ChainAction",
				Name: "agent:Research the following topic thoroughly. Search for information, analyze findings, cross-reference facts, and identify any gaps. Produce a comprehensive set of findings with sources.",
				Metadata: map[string]any{
					"max_tokens":  float64(15),
					"system_msg": "You are a deep research agent. Use web_search to find information, then analyze and cross-reference. Track your sources. Flag any gaps or uncertainties.",
					"tools":      []any{"web_search"},
				},
			},
			// Phase 4: Synthesize structured report via refine chain
			{
				Type: "ChainAction",
				Name: "refine:Synthesize the research findings into a structured report. Include: Executive Summary, Background, Findings (with citations), Analysis, Conclusion, and Sources. Use the findings from the previous step.",
				Metadata: map[string]any{
					"max_tokens": float64(2048),
				},
			},
			// Phase 5: Quality gate
			{
				Type: "Sequence",
				Name: "QualityGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "CheckSourceCount", Description: "≥2 independent sources per major claim"},
					{Type: "Condition", Name: "CheckCoverageCompleteness", Description: "All sub-questions addressed"},
					{Type: "Action", Name: "FlagRemainingGaps", Description: "Note limitations or areas for further research"},
				},
			},
			// Reflect and self-correct
			{
				Type: "Selector",
				Name: "OutcomeSelector",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "WasSuccessful"},
					{
						Type: "ChainAction",
						Name: "agent:Self-correct the research report. Identify weaknesses, fill gaps, and produce an improved version.",
						Metadata: map[string]any{
							"max_tokens": float64(5),
						},
					},
				},
			},
			{Type: "Action", Name: "UpdateBehaviorTree"},
		},
	}
}

// QuickResearchTree — single-pass agent research for fast answers.
func QuickResearchTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "QuickResearch_Main",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence", Name: "PreGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput"},
					{Type: "Condition", Name: "IsResearchQuery"},
					{Type: "Action", Name: "SetupResearchTools"},
				},
			},
			// Agent-based single-pass research (replaces Decompose → Search → Filter → Extract → Structure → Cite)
			{
				Type: "ChainAction",
				Name: "agent:Research {{.Task}} and provide a concise structured answer with sources.",
				Metadata: map[string]any{
					"max_tokens":  float64(8),
					"system_msg": "You are a quick research agent. Search for information, synthesize findings, and provide a structured answer with citations. Be concise.",
					"tools":      []any{"web_search"},
				},
			},
			// Refine for quality
			{
				Type: "ChainAction",
				Name: "refine:Improve the answer: make it clearer, verify facts, add missing context.",
				Metadata: map[string]any{
					"max_tokens": float64(1024),
				},
			},
			{
				Type: "Selector", Name: "OutcomeSelector",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "WasSuccessful"},
					{
						Type: "ChainAction",
						Name: "agent:Fix any issues with the research and produce a corrected answer.",
						Metadata: map[string]any{"max_tokens": float64(3)},
					},
				},
			},
			{Type: "Action", Name: "UpdateBehaviorTree"},
		},
	}
}

// ResearchTrees returns all research tree variants.
func ResearchTrees() map[string]*evolution.SerializableNode {
	return map[string]*evolution.SerializableNode{
		"deep_research":  DeepResearchTree(),
		"quick_research": QuickResearchTree(),
	}
}

// Descriptions maps research tree names to descriptions.
var Descriptions = map[string]string{
	"deep_research":  "Agent-based deep research: agent loop with web_search for iterative search → refine for structured report. 15-iteration cap, quality gate.",
	"quick_research": "Single-pass agent research: web_search → synthesize → refine. Fast, cited answers.",
}
