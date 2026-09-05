// Package research provides behavior trees for deep and quick research
// with agent-powered iterative analysis. Trees orchestrate web search,
// source evaluation, cross-referencing, and structured report generation
// in a multi-phase pipeline.
package evolution

// DeepResearchTree — updated with agent and tool nodes.
//
// Phase 1: Validate & setup tools
// Phase 2: Clarify ambiguous queries
// Phase 3: Agent-based iterative research (search → read → analyze → cross-ref → gap-detect)
// Phase 4: Synthesize structured report via refine chain
// Phase 5: Quality gate
func DeepResearchTree() *SerializableNode {
	return &SerializableNode{
		Type: "Sequence",
		Name: "DeepResearch_Main",
		Children: []SerializableNode{
			// Phase 1: Validate and setup
			{
				Type: "Sequence",
				Name: "PreGate",
				Children: []SerializableNode{
					{Type: "Condition", Name: "ValidateInput", Description: "Non-empty query"},
					{Type: "Condition", Name: "IsResearchQuery", Description: "Detect research / investigate / analyze / what is / how does keywords"},
					{Type: "Action", Name: "SetupResearchTools", Description: "Populate bb.ChainTools with web_search, graphify, calculator"},
				},
			},
			// Phase 2: Clarify ambiguous queries
			{
				Type: "Selector",
				Name: "ClarificationGate",
				Children: []SerializableNode{
					{
						Type: "Sequence",
						Name: "NeedsClarification",
						Children: []SerializableNode{
							{Type: "Condition", Name: "IsAmbiguousQuery"},
							{Type: "Action", Name: "AskClarifyingQuestions"},
							{Type: "Action", Name: "RefineQueryWithAnswers"},
						},
					},
					{Type: "Action", Name: "ProceedDirectly"},
				},
			},
			// Phase 3: Agent-based iterative research
			{
				Type: "ChainAction",
				Name: "agent:Research the following topic thoroughly: {{.Task}}. Search for information, analyze findings, cross-reference facts, and identify any gaps. Produce a comprehensive set of findings with sources.",
				Metadata: map[string]any{
					"max_iterations": float64(10),
					"system_msg":     "You are a deep research agent. Use web_search to search the web, http_get to fetch pages, file_read to check local knowledge, and calculator for math. Track your sources. Flag any gaps or uncertainties. When done, produce a Final Answer with ALL findings and source citations.",
					"tools":          []any{"web_search", "http_get", "file_read", "calculator"},
				},
			},
			// Phase 4: Synthesize structured report from findings
			{
				Type: "ChainAction",
				Name: "llm_call:Synthesize the following research findings into a comprehensive structured report. Include: Executive Summary, Background, Findings (with citations), Analysis, Conclusion, and Sources.\n\nRESEARCH FINDINGS:\n{{.Result}}\n\nTASK: {{.Task}}",
				Metadata: map[string]any{
					"max_tokens": float64(2048),
				},
			},
			// Phase 5: Quality gate
			{
				Type: "Sequence",
				Name: "QualityGate",
				Children: []SerializableNode{
					{Type: "Condition", Name: "CheckSourceCount", Description: "≥2 independent sources per major claim"},
					{Type: "Condition", Name: "CheckCoverageCompleteness", Description: "All sub-questions addressed"},
					{Type: "Action", Name: "FlagRemainingGaps", Description: "Note limitations or areas for further research"},
				},
			},
			{Type: "Action", Name: "UpdateBehaviorTree"},
		},
	}
}

// QuickResearchTree — single-pass agent research for fast answers.
func QuickResearchTree() *SerializableNode {
	return &SerializableNode{
		Type: "Sequence",
		Name: "QuickResearch_Main",
		Children: []SerializableNode{
			{
				Type: "Sequence", Name: "PreGate",
				Children: []SerializableNode{
					{Type: "Condition", Name: "ValidateInput"},
					{Type: "Condition", Name: "IsResearchQuery"},
					{Type: "Action", Name: "SetupResearchTools"},
				},
			},
			// Agent-based single-pass research (replaces Decompose → Search → Filter → Extract → Structure → Cite)
			{
				Type: "ChainAction",
				Name: "llm_call:Research {{.Task}} and provide a concise structured answer with sources.",
				Metadata: map[string]any{
					"max_tokens": float64(1024),
					"system_msg": "You are a quick research agent. Search for information, synthesize findings, and provide a structured answer with citations. Be concise.",
					"tools":      []any{"web_search"},
				},
			},
			// Refine moved into a single llm_call: pass previous output via {{.Result}}
			{
				Type: "ChainAction",
				Name: "llm_call:Improve the following answer — make it clearer, verify facts, add missing context.\n\nANSWER TO IMPROVE:\n{{.Result}}",
				Metadata: map[string]any{
					"max_tokens": float64(1024),
				},
			},
			// Self-reflection removed — was corrupting output with meta-critique instead of research.
			{Type: "Action", Name: "UpdateBehaviorTree"},
		},
	}
}

// ResearchTrees returns all research tree variants.
func ResearchTrees() map[string]*SerializableNode {
	return map[string]*SerializableNode{
		"deep_research":  DeepResearchTree(),
		"quick_research": QuickResearchTree(),
	}
}

// Descriptions maps research tree names to descriptions.
var Descriptions = map[string]string{
	"deep_research":  "Agent-based deep research: agent loop with web_search for iterative search → refine for structured report. 15-iteration cap, quality gate.",
	"quick_research": "Single-pass agent research: web_search → synthesize → refine. Fast, cited answers.",
}
