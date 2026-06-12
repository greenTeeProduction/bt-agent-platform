package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"

	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// ChainConfig describes a langchain chain invocation from a BT node.
// This is the configuration embedded in SerializableNode for "ChainAction" type nodes.
type ChainConfig struct {
	ChainType string            `json:"chain_type"` // llm_call, rag_query, tool_call, conversation, structured_output
	Prompt    string            `json:"prompt"`     // prompt template (supports {{.Task}}, {{.Plan}}, {{.Result}})
	SystemMsg string            `json:"system_msg"` // system message for chat models
	ModelName string            `json:"model_name"` // override model (empty = use blackboard default)
	Tools     []string          `json:"tools"`      // tool names to expose (from bb.ChainTools)
	Params    map[string]string `json:"params"`     // additional chain parameters
	MaxTokens int               `json:"max_tokens"` // max output tokens
	Stream    bool              `json:"stream"`     // enable streaming via callbacks
}

// ChainKind categorizes the type of langchain integration.
type ChainKind string

const (
	ChainLLMCall          ChainKind = "llm_call"
	ChainRAGQuery         ChainKind = "rag_query"
	ChainToolCall         ChainKind = "tool_call"
	ChainConversation     ChainKind = "conversation"
	ChainStructuredOutput ChainKind = "structured_output"
	ChainRetrievalQA      ChainKind = "retrieval_qa"
	ChainMapReduce        ChainKind = "map_reduce"
	ChainRefine           ChainKind = "refine"
	ChainAgent            ChainKind = "agent"       // ReAct agent loop with tool use
	ChainToolAction       ChainKind = "tool_action" // direct tool invocation without agent loop
)

// BuildChainAction creates a BT action node that executes a langchain chain via the blackboard.
func BuildChainAction(cfg ChainConfig, bb *Blackboard) *btleaf.Action[Blackboard] {
	fn := buildChainActionFn(cfg, bb)
	return btleaf.NewAction(fn)
}

// buildChainActionFn creates the inner action function with panic recovery.
func buildChainActionFn(cfg ChainConfig, bb *Blackboard) func(*btcore.BTContext[Blackboard]) int {
	return func(_ *btcore.BTContext[Blackboard]) (result int) {
		start := time.Now()
		defer func() { bb.DurationMs = time.Since(start).Milliseconds() }()

		// Panic recovery: chain actions call LLMs and external tools — assume they WILL panic.
		// Recover, log, and return failure so the BT's retry/escalation logic can handle it.
		defer func() {
			if r := recover(); r != nil {
				bb.Outcome = "chain_panic"
				bb.Result = fmt.Sprintf("PANIC in chain '%s': %v", cfg.ChainType, r)
				result = -1
			}
		}()

		switch ChainKind(cfg.ChainType) {
		case ChainLLMCall:
			return execLLMCall(cfg, bb)
		case ChainRAGQuery:
			return execRAGQuery(cfg, bb)
		case ChainToolCall:
			return execToolCall(cfg, bb)
		case ChainConversation:
			return execConversation(cfg, bb)
		case ChainStructuredOutput:
			return execStructuredOutput(cfg, bb)
		case ChainRetrievalQA:
			return execRetrievalQA(cfg, bb)
		case ChainMapReduce:
			return execMapReduce(cfg, bb)
		case ChainRefine:
			return execRefine(cfg, bb)
		case ChainAgent:
			return execAgent(cfg, bb)
		case ChainToolAction:
			return execToolAction(cfg, bb)
		default:
			bb.Outcome = "chain_failed"
			bb.Result = fmt.Sprintf("unknown chain type: %s", cfg.ChainType)
			return -1
		}
	}
}

// --- Chain executors ---

func execLLMCall(cfg ChainConfig, bb *Blackboard) int {
	prompt := expandTemplate(cfg.Prompt, bb)
	if bb.LLM == nil {
		// Template-only mode: return the expanded prompt with data filled in.
		// Data-gathering actions (ReadGraphReport, ReadGitHistory, etc.) populate
		// bb.CachedResult and bb.ChainState before the chain runs.
		bb.Outcome = "template_only"
		bb.Result = generateTemplateOutput(prompt, bb)
		return 1
	}
	result, err := bb.LLM.Generate(prompt)
	if err != nil {
		bb.Outcome = "chain_failed"
		bb.Result = fmt.Sprintf("LLM error: %v", err)
		return -1
	}
	bb.Outcome = "chain_success"
	bb.Result = result
	bb.Results = append(bb.Results, result)
	return 1
}

// generateTemplateOutput produces a structured markdown section from the
// expanded chain prompt when no LLM is available. It extracts the section
// purpose from the prompt and formats the available data.
func generateTemplateOutput(prompt string, bb *Blackboard) string {
	var sb strings.Builder

	// Extract section title from prompt (first line up to newline or period)
	title := "Arc42 Section"
	if idx := strings.Index(prompt, "arc42 Section"); idx >= 0 {
		end := strings.Index(prompt[idx:], "\n")
		if end < 0 {
			end = strings.Index(prompt[idx:], " —")
		}
		if end > 0 {
			title = strings.TrimSpace(prompt[idx : idx+end])
		}
	}
	sb.WriteString(fmt.Sprintf("# %s\n\n", title))

	// Add available data from chain state
	if bb.CachedResult != "" && bb.CachedResult != prompt {
		// Truncate very long cached results
		truncated := bb.CachedResult
		if len(truncated) > 500 {
			truncated = truncated[:500] + "\n... (truncated)"
		}
		sb.WriteString("## Source Data\n\n```\n")
		sb.WriteString(truncated)
		sb.WriteString("\n```\n\n")
	}

	if bb.ChainState != nil {
		sb.WriteString("## Context\n\n")
		for k, v := range bb.ChainState {
			valStr := fmt.Sprintf("%v", v)
			if len(valStr) > 300 {
				valStr = valStr[:300] + "..."
			}
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", k, valStr))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Generated Content\n\n")
	sb.WriteString("*Auto-generated from codebase introspection. Run with `--llm=deepseek` for LLM-generated prose.*\n")
	return sb.String()
}

func execRAGQuery(cfg ChainConfig, bb *Blackboard) int {
	// Uses knowledge graph results from bb.KgResults as context
	query := expandTemplate(cfg.Prompt, bb)
	context := bb.KgResults
	if context == "" {
		context = bb.CachedResult
	}
	prompt := fmt.Sprintf(`Answer the question using ONLY the provided context.
If the context doesn't contain the answer, say "I don't have enough information."

CONTEXT:
%s

QUESTION:
%s

Answer:`, context, query)

	if bb.LLM == nil {
		bb.Outcome = "chain_failed"
		bb.Result = "no LLM available for RAG"
		return -1
	}
	result, err := bb.LLM.Generate(prompt)
	if err != nil {
		bb.Outcome = "chain_failed"
		bb.Result = fmt.Sprintf("RAG error: %v", err)
		return -1
	}
	bb.Outcome = "chain_success"
	bb.Result = result
	bb.Results = append(bb.Results, result)
	return 1
}

func execToolCall(cfg ChainConfig, bb *Blackboard) int {
	// Tool-calling via langchain: describe available tools and let LLM choose
	prompt := expandTemplate(cfg.Prompt, bb)

	toolDesc := "Available tools:\n"
	if cfg.Tools != nil {
		for _, t := range cfg.Tools {
			toolDesc += fmt.Sprintf("- %s\n", t)
		}
	} else if bb.ChainTools != nil {
		for _, t := range bb.ChainTools {
			toolDesc += fmt.Sprintf("- %v\n", t)
		}
	}

	fullPrompt := fmt.Sprintf(`You have access to the following tools:
%s

Using these tools, complete this task:
%s

If you need to use a tool, respond with: TOOL: <tool_name>
Otherwise, respond directly.`, toolDesc, prompt)

	if bb.LLM == nil {
		bb.Outcome = "chain_failed"
		return -1
	}
	result, err := bb.LLM.Generate(fullPrompt)
	if err != nil {
		bb.Outcome = "chain_failed"
		return -1
	}
	bb.Outcome = "chain_success"
	bb.Result = result
	bb.Results = append(bb.Results, result)
	return 1
}

func execConversation(cfg ChainConfig, bb *Blackboard) int {
	// Conversation chain with memory — uses ChainMemory for context
	userMsg := expandTemplate(cfg.Prompt, bb)

	// Build conversation context from memory
	history := ""
	if mem, ok := bb.ChainMemory.(fmt.Stringer); ok {
		history = mem.String()
	}

	prompt := fmt.Sprintf(`%s

Previous conversation:
%s

User: %s
Assistant:`, cfg.SystemMsg, history, userMsg)

	if bb.LLM == nil {
		bb.Outcome = "chain_failed"
		return -1
	}
	result, err := bb.LLM.Generate(prompt)
	if err != nil {
		bb.Outcome = "chain_failed"
		return -1
	}

	// Update conversation memory (store as simple string append for now)
	bb.ChainState["conv_history"] = history + fmt.Sprintf("User: %s\nAssistant: %s\n", userMsg, result)
	bb.Outcome = "chain_success"
	bb.Result = result
	bb.Results = append(bb.Results, result)
	return 1
}

func execStructuredOutput(cfg ChainConfig, bb *Blackboard) int {
	// Structured output: prompt + JSON schema constraint
	prompt := expandTemplate(cfg.Prompt, bb)

	// Add JSON output instruction
	schemaDesc := ""
	if schema, ok := cfg.Params["json_schema"]; ok {
		schemaDesc = fmt.Sprintf("\nRespond ONLY with valid JSON matching this schema:\n%s\n", schema)
	}

	fullPrompt := fmt.Sprintf(`%s
%s
Respond in valid JSON format only, no other text.`, prompt, schemaDesc)

	if bb.LLM == nil {
		bb.Outcome = "chain_failed"
		return -1
	}
	result, err := bb.LLM.Generate(fullPrompt)
	if err != nil {
		bb.Outcome = "chain_failed"
		return -1
	}
	bb.Outcome = "chain_success"
	bb.Result = result
	bb.Results = append(bb.Results, result)
	return 1
}

func execRetrievalQA(cfg ChainConfig, bb *Blackboard) int {
	// Full RetrievalQA chain: retrieves from knowledge sources then answers
	query := expandTemplate(cfg.Prompt, bb)

	// Phase 1: retrieval (uses kg results or cached)
	retrieved := bb.KgResults
	if retrieved == "" {
		retrieved = bb.CachedResult
	}
	if retrieved == "" && bb.LLM != nil {
		// Attempt retrieval via LLM
		retrievalPrompt := fmt.Sprintf("Search for information about: %s\nProvide relevant facts only.", query)
		r, _ := bb.LLM.Generate(retrievalPrompt)
		retrieved = r
	}

	// Phase 2: QA with retrieved context
	qaPrompt := fmt.Sprintf(`Based on the following information, answer the question.

RETRIEVED INFORMATION:
%s

QUESTION:
%s

Provide a comprehensive answer. If the information is insufficient, state what's missing.`, retrieved, query)

	if bb.LLM == nil {
		bb.Outcome = "chain_failed"
		return -1
	}
	result, err := bb.LLM.Generate(qaPrompt)
	if err != nil {
		bb.Outcome = "chain_failed"
		return -1
	}
	bb.Outcome = "chain_success"
	bb.Result = result
	bb.Results = append(bb.Results, result)
	return 1
}

func execMapReduce(cfg ChainConfig, bb *Blackboard) int {
	// Map-Reduce: split task into subtasks (map), process each, combine results (reduce)
	task := expandTemplate(cfg.Prompt, bb)

	if bb.LLM == nil {
		bb.Outcome = "chain_failed"
		return -1
	}

	// Map phase: decompose
	mapPrompt := fmt.Sprintf("Break down this task into 3-5 independent subtasks:\n%s\n\nSubtasks (one per line, numbered):", task)
	subtasks, err := bb.LLM.Generate(mapPrompt)
	if err != nil {
		bb.Outcome = "chain_failed"
		return -1
	}

	// Process each subtask (simplified: process first 2 for speed)
	lines := splitLines(subtasks)
	results := make([]string, 0, 8)
	for i, line := range lines {
		if i >= 3 || line == "" {
			break
		}
		subResult, err := bb.LLM.Generate(fmt.Sprintf("Complete this subtask:\n%s\n\nResult:", line))
		if err != nil {
			continue
		}
		results = append(results, subResult)
	}

	// Reduce phase: combine
	reducePrompt := fmt.Sprintf("Combine these results into a unified answer for the original task:\nTask: %s\n\nResults:\n", task)
	for i, r := range results {
		reducePrompt += fmt.Sprintf("%d. %s\n", i+1, r)
	}
	reducePrompt += "\nUnified answer:"

	final, err := bb.LLM.Generate(reducePrompt)
	if err != nil {
		bb.Outcome = "chain_failed"
		return -1
	}
	bb.Outcome = "chain_success"
	bb.Result = final
	return 1
}

func execRefine(cfg ChainConfig, bb *Blackboard) int {
	// Refine chain: iterative improvement through multiple passes
	task := expandTemplate(cfg.Prompt, bb)
	maxRefinements := 2

	if bb.LLM == nil {
		bb.Outcome = "chain_failed"
		return -1
	}

	// Initial answer
	current, err := bb.LLM.Generate(task)
	if err != nil {
		bb.Outcome = "chain_failed"
		return -1
	}

	for i := 0; i < maxRefinements; i++ {
		refinePrompt := fmt.Sprintf(`Review and improve this answer. Make it more comprehensive, accurate, and well-structured.

ORIGINAL TASK:
%s

CURRENT ANSWER:
%s

Identify weaknesses and provide an improved version.`, task, current)

		improved, err := bb.LLM.Generate(refinePrompt)
		if err != nil {
			break
		}
		current = improved
	}

	bb.Outcome = "chain_success"
	bb.Result = current
	return 1
}

// execAgent runs a ReAct-style agent loop: Thought → Action → Observation → repeat → Final Answer.
// Tools are provided via bb.ChainTools (any objects with Name() and Description() methods)
// or via cfg.Tools (string names). The agent iterates up to MaxIterations times.
func execAgent(cfg ChainConfig, bb *Blackboard) int {
	task := expandTemplate(cfg.Prompt, bb)
	maxIter := cfg.MaxTokens
	if maxIter <= 0 || maxIter > 30 {
		maxIter = 15
	}

	if bb.LLM == nil {
		bb.Outcome = "chain_failed"
		bb.Result = "no LLM available for agent"
		return -1
	}

	// Build tool descriptions
	toolList := buildToolList(cfg, bb)

	// System message sets up the ReAct format
	systemMsg := cfg.SystemMsg
	if systemMsg == "" {
		systemMsg = "You are a helpful AI assistant with access to tools."
	}

	scratchpad := ""
	finalAnswer := ""
	toolUsed := false
	toolsRequired := hasRealTools(bb)

	for i := 0; i < maxIter; i++ {
		prompt := fmt.Sprintf(`%s

TASK: %s

You have access to these tools:
%s

Respond in this format:
Thought: <your reasoning about what to do next>
Action: <tool_name>
Action Input: <parameters for the tool>
...or if you have the final answer...
Final Answer: <your complete answer — INCLUDE ALL tool output data verbatim, do not summarize or omit results>

Previous steps:
%s

What is your next step?`, systemMsg, task, toolList, scratchpad)

		response, err := bb.LLM.Generate(prompt)
		if err != nil {
			bb.Outcome = "chain_failed"
			bb.Result = fmt.Sprintf("agent error at iteration %d: %v", i, err)
			return -1
		}

		// Parse response for action or final answer
		action, actionInput := parseAgentAction(response)
		if action == "" {
			// Check for final answer
			if fa := parseFinalAnswer(response); fa != "" {
				if toolsRequired && !toolUsed {
					scratchpad += fmt.Sprintf("Step %d: rejected unevidenced final answer because no real tool was used. Available tools: %s\n", i+1, availableToolNames(bb))
					continue
				}
				finalAnswer = fa
				break
			}
			// Unparseable — treat as thought
			scratchpad += fmt.Sprintf("Step %d: %s\n", i+1, strings.TrimSpace(response))
			continue
		}

		// Execute the tool
		toolResult := executeAgentTool(action, actionInput, bb)
		scratchpad += fmt.Sprintf("Step %d: Action: %s(%s) → %s\n", i+1, action, actionInput, toolResult)
		if !isToolUnavailableResult(toolResult) {
			toolUsed = true
		}

		// Store tool result for downstream use
		bb.CachedResult = toolResult
	}

	if toolsRequired && !toolUsed {
		bb.Outcome = "tool_evidence_missing"
		bb.Result = fmt.Sprintf("## Blocked: No Tool Evidence\n\nAgent was given real tools but did not successfully use any, so no factual claims were produced.\n\nAvailable real tools: %s\n\nStatus: blocked honestly instead of fabricating output.", availableToolNames(bb))
		bb.Results = append(bb.Results, bb.Result)
		return 1
	}

	if finalAnswer == "" {
		// No final answer produced — generate one from scratchpad
		summaryPrompt := fmt.Sprintf(`Based on the following investigation, provide a final answer. Include ALL data from the investigation log verbatim — do not summarize or omit any results.

TASK: %s

INVESTIGATION LOG:
%s

Final Answer:`, task, scratchpad)
		var err error
		finalAnswer, err = bb.LLM.Generate(summaryPrompt)
		if err != nil {
			bb.Outcome = "chain_failed"
			return -1
		}
	}

	bb.Outcome = "chain_success"
	bb.Result = finalAnswer
	bb.Results = append(bb.Results, finalAnswer)
	return 1
}

// execToolAction directly invokes a tool by name without an agent loop.
// The tool name comes from cfg.Tools[0] or from the node name after "tool_action:".
// Input is the expanded template, and the result is stored in bb.CachedResult.
func execToolAction(cfg ChainConfig, bb *Blackboard) int {
	input := expandTemplate(cfg.Prompt, bb)

	// Determine tool name: from config tools list, or from node name parts
	toolName := ""
	if len(cfg.Tools) > 0 {
		toolName = cfg.Tools[0]
	} else {
		// Parse from prompt: "web_search:query" → tool=web_search, input=query
		if idx := strings.Index(cfg.Prompt, ":"); idx > 0 {
			toolName = strings.TrimSpace(cfg.Prompt[:idx])
		}
	}

	if toolName == "" {
		bb.Outcome = "chain_failed"
		bb.Result = "no tool name specified for tool_action"
		return -1
	}

	// Try to execute the tool
	result := executeAgentTool(toolName, input, bb)
	bb.CachedResult = result
	bb.Result = result
	if isToolUnavailableResult(result) {
		bb.Outcome = "tool_unavailable"
		return -1
	}
	bb.Outcome = "chain_success"
	return 1
}

// buildToolList creates a text description of available tools.
func buildToolList(cfg ChainConfig, bb *Blackboard) string {
	parts := make([]string, 0, 8)

	// Tools from node config
	for _, t := range cfg.Tools {
		parts = append(parts, fmt.Sprintf("- %s: call this tool with the required parameters", t))
	}

	// Tools from blackboard (interface-based)
	for _, t := range bb.ChainTools {
		// Try to get name and description via interface assertions
		type named interface{ Name() string }
		type described interface{ Description() string }
		name := "unknown_tool"
		desc := "no description"
		if n, ok := t.(named); ok {
			name = n.Name()
		}
		if d, ok := t.(described); ok {
			desc = d.Description()
		}
		parts = append(parts, fmt.Sprintf("- %s: %s", name, desc))
	}

	if len(parts) == 0 {
		return "(no tools available — answer directly)"
	}
	return strings.Join(parts, "\n")
}

// parseAgentAction extracts Action and Action Input from a ReAct response.
func parseAgentAction(response string) (action string, input string) {
	lines := strings.Split(response, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Action:") {
			action = strings.TrimSpace(strings.TrimPrefix(trimmed, "Action:"))
		}
		if strings.HasPrefix(trimmed, "Action Input:") {
			input = strings.TrimSpace(strings.TrimPrefix(trimmed, "Action Input:"))
		}
	}
	return
}

// parseFinalAnswer extracts Final Answer from agent response.
// Captures everything after the "Final Answer:" marker, including multi-line content.
func parseFinalAnswer(response string) string {
	trimmed := strings.TrimSpace(response)

	// Fast path: entire response starts with "Final Answer:"
	if strings.HasPrefix(trimmed, "Final Answer:") {
		return strings.TrimSpace(strings.TrimPrefix(trimmed, "Final Answer:"))
	}

	// Scan for "Final Answer:" on a line, then capture everything after it
	lines := strings.Split(response, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "Final Answer:") {
			firstLine := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "Final Answer:"))
			rest := strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
			if rest != "" {
				return firstLine + "\n" + rest
			}
			return firstLine
		}
	}

	return ""
}

// executeAgentTool runs a tool by name against the blackboard.
// Tools on bb.ChainTools must implement a Call(string) string method.
func executeAgentTool(name, input string, bb *Blackboard) string {
	type callable interface {
		Call(input string) string
	}
	type named interface{ Name() string }

	for _, t := range bb.ChainTools {
		if n, ok := t.(named); ok && strings.EqualFold(n.Name(), name) {
			if c, ok := t.(callable); ok {
				return c.Call(input)
			}
			return fmt.Sprintf("tool %s found but has no Call method", name)
		}
	}

	return fmt.Sprintf("TOOL_UNAVAILABLE: real tool '%s' not found. Available real tools: %s. Do not simulate or fabricate this tool output; pick an available tool or report blocked.", name, availableToolNames(bb))
}

func availableToolNames(bb *Blackboard) string {
	type named interface{ Name() string }
	if bb == nil || len(bb.ChainTools) == 0 {
		return "(none)"
	}
	names := make([]string, 0, len(bb.ChainTools))
	for _, t := range bb.ChainTools {
		if n, ok := t.(named); ok {
			names = append(names, n.Name())
		}
	}
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

func hasRealTools(bb *Blackboard) bool {
	return bb != nil && len(bb.ChainTools) > 0 && availableToolNames(bb) != "(none)"
}

func isToolUnavailableResult(result string) bool {
	return strings.Contains(result, "TOOL_UNAVAILABLE") || strings.Contains(result, "STUB_ERROR") || strings.Contains(result, "found but has no Call method")
}

// parseChainConfig extracts ChainConfig from a SerializableNode's metadata.
// The node should have:
//   - Name: chain type identifier (e.g., "llm_call:analyze")
//   - Metadata: optional JSON map with prompt, tools, params
func parseChainConfig(node *evolution.SerializableNode) ChainConfig {
	cfg := ChainConfig{
		MaxTokens: 2048,
		Stream:    false,
	}

	// Parse chain type from node name: "llm_call:analyze" → ChainLLMCall, prompt="analyze"
	nameParts := strings.SplitN(node.Name, ":", 2)
	if len(nameParts) >= 1 {
		cfg.ChainType = nameParts[0]
	}
	if len(nameParts) >= 2 {
		cfg.Prompt = nameParts[1]
	}

	// Parse metadata for additional config
	if node.Metadata != nil {
		if p, ok := node.Metadata["prompt"].(string); ok && cfg.Prompt == "" {
			cfg.Prompt = p
		}
		if s, ok := node.Metadata["system_msg"].(string); ok {
			cfg.SystemMsg = s
		}
		if m, ok := node.Metadata["model_name"].(string); ok {
			cfg.ModelName = m
		}
		if t, ok := node.Metadata["tools"].([]any); ok {
			for _, tt := range t {
				if ts, ok := tt.(string); ok {
					cfg.Tools = append(cfg.Tools, ts)
				}
			}
		}
		if p, ok := node.Metadata["params"].(map[string]any); ok {
			cfg.Params = make(map[string]string)
			for k, v := range p {
				if vs, ok := v.(string); ok {
					cfg.Params[k] = vs
				}
			}
		}
		if mt, ok := node.Metadata["max_tokens"].(float64); ok {
			cfg.MaxTokens = int(mt)
		}
		if st, ok := node.Metadata["stream"].(bool); ok {
			cfg.Stream = st
		}
	}

	return cfg
}

// --- Helpers ---

// expandTemplate replaces {{.Field}} placeholders with blackboard values.
// Supports: .Task, .Plan, .Result, .Outcome, .Complexity, .CachedResult,
// .KgResults, .DurationMs, .QualityScore, .CurrentPath, .FailureCount.
// Also supports .ChainState.<key> for arbitrary chain state lookups.
func expandTemplate(tmpl string, bb *Blackboard) string {
	if tmpl == "" {
		return bb.Task
	}
	result := tmpl
	result = replaceAll(result, "{{.Task}}", bb.Task)
	result = replaceAll(result, "{{.Plan}}", bb.Plan)
	result = replaceAll(result, "{{.Result}}", bb.Result)
	result = replaceAll(result, "{{.Outcome}}", bb.Outcome)
	result = replaceAll(result, "{{.Complexity}}", bb.Complexity)
	result = replaceAll(result, "{{.CachedResult}}", bb.CachedResult)
	result = replaceAll(result, "{{.KgResults}}", bb.KgResults)
	result = replaceAll(result, "{{.DurationMs}}", fmt.Sprintf("%d", bb.DurationMs))
	result = replaceAll(result, "{{.QualityScore}}", fmt.Sprintf("%.2f", bb.QualityScore))
	result = replaceAll(result, "{{.CurrentPath}}", bb.CurrentPath)
	result = replaceAll(result, "{{.FailureCount}}", fmt.Sprintf("%d", bb.FailureCount))
	// Expand {{.ChainState.<key>}} patterns
	result = expandChainStateTemplates(result, bb)
	return result
}

// expandChainStateTemplates replaces {{.ChainState.<key>}} with bb.ChainState[key].
func expandChainStateTemplates(s string, bb *Blackboard) string {
	if bb.ChainState == nil {
		return s
	}
	for {
		idx := strings.Index(s, "{{.ChainState.")
		if idx < 0 {
			break
		}
		end := strings.Index(s[idx:], "}}")
		if end < 0 {
			break
		}
		key := s[idx+len("{{.ChainState.") : idx+end]
		val := ""
		if v, ok := bb.ChainState[key]; ok {
			val = fmt.Sprintf("%v", v)
		}
		s = s[:idx] + val + s[idx+end+2:]
	}
	return s
}

func replaceAll(s, old, newStr string) string {
	result := s
	for {
		next := strings.Replace(result, old, newStr, 1)
		if next == result {
			break
		}
		result = next
	}
	return result
}

func splitLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Strip leading number + dot + space ("1. text" → "text")
		trimmed = strings.TrimLeft(trimmed, "0123456789.")
		trimmed = strings.TrimSpace(trimmed)
		// Strip leading dash + space
		trimmed = strings.TrimPrefix(trimmed, "- ")
		if trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}
