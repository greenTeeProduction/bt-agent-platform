package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/fusion"

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
	ChainFusion           ChainKind = "fusion"
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
		// Registered first → runs last (LIFO), so it observes the final result,
		// including any value set by the panic-recovery defer below. Records this
		// chain node's execution into the cross-node task history on the blackboard.
		defer func() {
			bb.DurationMs = time.Since(start).Milliseconds()
			recordChainHistory(bb, cfg.ChainType, result, bb.DurationMs)
		}()

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
		case ChainFusion:
			return execFusion(cfg, bb)
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

// chainHistoryLimit bounds how many chain-node execution records are retained on
// the blackboard. Complex trees run many chain nodes; keeping the most recent N
// gives downstream routers, quality gates, and audits a task-history view without
// letting ChainState grow unbounded over a long run.
const chainHistoryLimit = 50

// recordChainHistory appends one entry per chain-node execution to
// bb.ChainState["chain_history"], building a single chronological record of the
// whole run: which chain ran, its outcome, success/failure, how long it took, and
// a short preview of the result (or error message). Individual chain executors
// each write their own progress keys (map_reduce_*, agent_*, refine_*), but none
// can see the sequence of nodes that ran before them — this fills that gap so a
// later node can reason about task history and prior partial failures.
//
// seq is a monotonic counter derived from the last entry so it stays meaningful
// even after the oldest entries are trimmed off.
func recordChainHistory(bb *Blackboard, chainType string, result int, durationMs int64) {
	if bb.ChainState == nil {
		bb.ChainState = map[string]any{}
	}
	history, _ := bb.ChainState["chain_history"].([]map[string]any)

	seq := 1
	if n := len(history); n > 0 {
		if prev, ok := history[n-1]["seq"].(int); ok {
			seq = prev + 1
		}
	}

	status := "running"
	switch {
	case result > 0:
		status = "success"
	case result < 0:
		status = "failure"
	}

	history = append(history, map[string]any{
		"seq":         seq,
		"chain_type":  chainType,
		"outcome":     bb.Outcome,
		"status":      status,
		"duration_ms": durationMs,
		"result_len":  len(bb.Result),
		"preview":     truncateStr(strings.TrimSpace(bb.Result), 160),
	})
	// Retain only the most recent entries so a long run can't grow ChainState without bound.
	if len(history) > chainHistoryLimit {
		history = history[len(history)-chainHistoryLimit:]
	}
	bb.ChainState["chain_history"] = history
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
	// Map-Reduce: split a complex task into subtasks (map), process each while
	// carrying forward earlier results (so later subtasks can build on them —
	// multi-step reasoning, not just independent splits), then combine (reduce).
	// Progress is tracked in bb.ChainState and partial failures are recorded
	// rather than silently dropped, so downstream nodes can react to gaps.
	task := expandTemplate(cfg.Prompt, bb)

	if bb.LLM == nil {
		bb.Outcome = "chain_failed"
		bb.Result = "no LLM available for map_reduce"
		return -1
	}
	if bb.ChainState == nil {
		bb.ChainState = map[string]any{}
	}

	// Map phase: decompose
	mapPrompt := fmt.Sprintf("Break down this task into 3-5 independent subtasks:\n%s\n\nSubtasks (one per line, numbered):", task)
	subtasks, err := bb.LLM.Generate(mapPrompt)
	if err != nil {
		bb.Outcome = "chain_failed"
		bb.Result = fmt.Sprintf("map_reduce decompose error: %v", err)
		return -1
	}

	// Process each subtask, threading accumulated context into later subtasks.
	const maxSubtasks = 5
	lines := splitLines(subtasks)
	results := make([]string, 0, maxSubtasks)
	var failedSubtasks []string
	completed, failed := 0, 0
	accumulated := ""
	for i, line := range lines {
		if i >= maxSubtasks || line == "" {
			break
		}
		subPrompt := fmt.Sprintf("Complete this subtask:\n%s\n", line)
		if accumulated != "" {
			subPrompt += fmt.Sprintf("\nResults from earlier subtasks (build on these, do not repeat them):\n%s\n", accumulated)
		}
		subPrompt += "\nResult:"
		subResult, err := bb.LLM.Generate(subPrompt)
		if err != nil {
			// Partial failure recovery: record the failed subtask instead of
			// silently dropping it, then continue with the remaining subtasks.
			failed++
			failedSubtasks = append(failedSubtasks, line)
			continue
		}
		results = append(results, subResult)
		accumulated += fmt.Sprintf("- %s\n", subResult)
		completed++
	}

	// Record progress so downstream nodes can inspect subtask outcomes.
	bb.ChainState["map_reduce_total"] = completed + failed
	bb.ChainState["map_reduce_completed"] = completed
	bb.ChainState["map_reduce_failed"] = failed
	if len(failedSubtasks) > 0 {
		bb.ChainState["map_reduce_failed_subtasks"] = failedSubtasks
	}

	// If every subtask failed there is nothing to combine — fail honestly rather
	// than fabricate a unified answer from no inputs.
	if completed == 0 {
		bb.Outcome = "chain_failed"
		bb.Result = fmt.Sprintf("map_reduce: all %d subtask(s) failed, no results to combine", failed)
		return -1
	}

	// Reduce phase: combine
	reducePrompt := fmt.Sprintf("Combine these results into a unified answer for the original task:\nTask: %s\n\nResults:\n", task)
	for i, r := range results {
		reducePrompt += fmt.Sprintf("%d. %s\n", i+1, r)
	}
	if failed > 0 {
		reducePrompt += fmt.Sprintf("\nNote: %d subtask(s) could not be completed; produce the best answer from the available results and flag the missing parts.\n", failed)
	}
	reducePrompt += "\nUnified answer:"

	final, err := bb.LLM.Generate(reducePrompt)
	if err != nil {
		bb.Outcome = "chain_failed"
		bb.Result = fmt.Sprintf("map_reduce reduce error: %v", err)
		return -1
	}
	bb.Outcome = "chain_success"
	bb.Result = final
	bb.Results = append(bb.Results, final)
	return 1
}

func execRefine(cfg ChainConfig, bb *Blackboard) int {
	// Refine chain: iterative critique-then-revise. Each pass first critiques the
	// current answer against the task, then revises it to address that critique.
	// The loop stops early when the critique reports the answer has converged
	// (nothing material left to add) — this avoids wasted LLM calls and the
	// quality regression a blind "improve again" pass can cause on an already-good
	// answer. Refinement progress (rounds run, why it stopped) is recorded in
	// bb.ChainState so downstream nodes can inspect how much work was done.
	task := expandTemplate(cfg.Prompt, bb)

	if bb.LLM == nil {
		bb.Outcome = "chain_failed"
		bb.Result = "no LLM available for refine"
		return -1
	}
	if bb.ChainState == nil {
		bb.ChainState = map[string]any{}
	}

	maxRefinements := 3
	if v, ok := cfg.Params["max_refinements"]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 && n <= 10 {
			maxRefinements = n
		}
	}

	// Initial answer
	current, err := bb.LLM.Generate(task)
	if err != nil {
		bb.Outcome = "chain_failed"
		bb.Result = fmt.Sprintf("refine initial error: %v", err)
		return -1
	}

	rounds := 0
	stopReason := "max_refinements_reached"
	for i := 0; i < maxRefinements; i++ {
		critiquePrompt := fmt.Sprintf(`Critique this answer against the task. List concrete, specific weaknesses — missing detail, inaccuracies, poor structure. If the answer is already complete and accurate with nothing material to add, reply with exactly: NO_FURTHER_IMPROVEMENT

TASK:
%s

CURRENT ANSWER:
%s

Critique:`, task, current)

		critique, err := bb.LLM.Generate(critiquePrompt)
		if err != nil {
			stopReason = "critique_error"
			break
		}
		if refineConverged(critique) {
			stopReason = "converged"
			break
		}

		revisePrompt := fmt.Sprintf(`Revise the answer so it addresses every point in the critique. Output only the full improved answer — no critique, no commentary.

TASK:
%s

CURRENT ANSWER:
%s

CRITIQUE:
%s

Improved answer:`, task, current, critique)

		improved, err := bb.LLM.Generate(revisePrompt)
		if err != nil {
			stopReason = "revise_error"
			break
		}
		current = improved
		rounds++
	}

	bb.ChainState["refine_rounds"] = rounds
	bb.ChainState["refine_stop_reason"] = stopReason

	bb.Outcome = "chain_success"
	bb.Result = current
	bb.Results = append(bb.Results, current)
	return 1
}

// refineConverged reports whether a refine critique signals the answer is good
// enough to stop iterating. It matches the explicit sentinel plus the common
// natural-language ways an LLM says "nothing more to improve".
func refineConverged(critique string) bool {
	lower := strings.ToLower(critique)
	for _, m := range []string{
		"no_further_improvement", "no further improvement", "no significant",
		"no material", "no changes needed", "already complete", "already comprehensive",
		"nothing to add", "no weaknesses", "no notable",
	} {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

func execFusion(cfg ChainConfig, bb *Blackboard) int {
	if bb.LLM == nil {
		bb.Outcome = "chain_failed"
		bb.Result = "no LLM available for fusion"
		return -1
	}
	prompt := expandTemplate(cfg.Prompt, bb)
	fcfg := fusionConfigFromParams(cfg.Params)
	caller := fusionCaller{llm: bb.LLM}
	result, err := fusion.Run(context.Background(), caller, fcfg, prompt, fusionToolsFromBB(bb))
	if bb.ChainState == nil {
		bb.ChainState = map[string]any{}
	}
	bb.ChainState["fusion_status"] = result.Status
	bb.ChainState["fusion_responses"] = result.Responses
	bb.ChainState["fusion_analysis"] = result.Analysis
	bb.ChainState["fusion_models"] = fcfg.Normalize().AnalysisModels
	bb.ChainState["fusion_panel_count"] = len(result.Responses)
	bb.ChainState["fusion_success_count"] = len(fusionSuccessfulResponses(result.Responses))
	persistFusionArtifacts(bb, prompt, result)
	if err != nil {
		bb.Outcome = "chain_failed"
		bb.Result = fmt.Sprintf("fusion error: %v", err)
		return -1
	}
	bb.Outcome = "chain_success"
	bb.Result = result.Final
	bb.Results = append(bb.Results, result.Final)
	return 1
}

type fusionCaller struct{ llm interface{} }

func (c fusionCaller) GenerateWithModel(ctx context.Context, model, system, prompt string) (string, error) {
	if m, ok := c.llm.(interface {
		GenerateWithModel(context.Context, string, string, string) (string, error)
	}); ok {
		return m.GenerateWithModel(ctx, model, system, prompt)
	}
	if m, ok := c.llm.(interface {
		GenerateCtx(context.Context, string) (string, error)
	}); ok {
		return m.GenerateCtx(ctx, system+"\n\n"+prompt)
	}
	if m, ok := c.llm.(interface{ Generate(string) (string, error) }); ok {
		return m.Generate(system + "\n\n" + prompt)
	}
	return "", fmt.Errorf("LLM does not support Generate")
}

func fusionConfigFromParams(params map[string]string) fusion.Config {
	cfg := fusion.DefaultConfig()
	if params == nil {
		return cfg
	}
	if raw := strings.TrimSpace(params["analysis_models"]); raw != "" {
		cfg.AnalysisModels = splitCSV(raw)
	}
	if raw := strings.TrimSpace(params["model"]); raw != "" {
		cfg.JudgeModel = raw
	}
	if raw := strings.TrimSpace(params["max_tool_calls"]); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			cfg.MaxToolCalls = n
		}
	}
	if raw := strings.TrimSpace(params["force"]); raw != "" {
		cfg.Force = raw == "1" || strings.EqualFold(raw, "true") || strings.EqualFold(raw, "yes")
	}
	if raw := strings.TrimSpace(params["enabled"]); raw != "" {
		cfg.Enabled = !(raw == "0" || strings.EqualFold(raw, "false") || strings.EqualFold(raw, "no"))
	}
	return cfg
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func fusionToolsFromBB(bb *Blackboard) []fusion.Tool {
	if bb == nil {
		return nil
	}
	tools := make([]fusion.Tool, 0, len(bb.ChainTools))
	for _, raw := range bb.ChainTools {
		if t, ok := raw.(interface {
			Name() string
			Description() string
			Call(string) string
		}); ok {
			tools = append(tools, t)
		}
	}
	return tools
}

func fusionSuccessfulResponses(responses []fusion.Response) []fusion.Response {
	out := make([]fusion.Response, 0, len(responses))
	for _, r := range responses {
		if r.Error == "" && strings.TrimSpace(r.Content) != "" {
			out = append(out, r)
		}
	}
	return out
}

func persistFusionArtifacts(bb *Blackboard, prompt string, result fusion.Result) {
	if bb == nil || bb.BB == nil {
		return
	}
	_ = bb.BB.Set("fusion/input", prompt, "Fusion input", "text")
	if b, err := json.MarshalIndent(result.Responses, "", "  "); err == nil {
		_ = bb.BB.Set("fusion/responses.json", string(b), "Fusion panel responses", "json")
	}
	if b, err := json.MarshalIndent(result.Analysis, "", "  "); err == nil {
		_ = bb.BB.Set("fusion/analysis.json", string(b), "Fusion judge analysis", "json")
	}
	if result.Final != "" {
		_ = bb.BB.Set("fusion/final.md", result.Final, "Fusion final answer", "markdown")
	}
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
	if bb.ChainState == nil {
		bb.ChainState = map[string]any{}
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

	// Stuck-loop detection: complex tasks frequently send a ReAct agent into a
	// loop where it repeats the same (action, input) call and burns its whole
	// iteration budget without progress. We count identical calls, nudge the
	// agent once a repeat appears, and abort the loop once a call has been
	// attempted maxRepeatedCalls times so the budget isn't wasted.
	const maxRepeatedCalls = 3
	toolCallCounts := map[string]int{}
	iterations := 0
	successfulToolCalls := 0
	stopReason := "max_iterations"
	scratchpadWindowed := false

	// No-progress detection: the repeated_tool_calls guard catches an agent stuck
	// repeating one tool call, but the complementary failure mode on complex tasks
	// is an agent that never emits a usable step at all — it rambles in pure
	// "Thought:" output (unparseable, no Action) or keeps proposing a Final Answer
	// it isn't allowed to give (unevidenced, tools required). Left unchecked it
	// burns the entire iteration budget making zero progress. We count consecutive
	// no-progress iterations and abort once they exceed maxNoProgressSteps, resetting
	// the streak whenever a tool actually runs.
	const maxNoProgressSteps = 4
	noProgressStreak := 0

	// Tool-call trace: every real tool invocation overwrites bb.CachedResult, so
	// once the agent chain node finishes only the LAST observation survives for
	// downstream nodes. Complex multi-step tasks call several tools and the earlier
	// observations are otherwise buried in the local scratchpad and discarded. We
	// record each invocation (including hallucinated/unavailable tool attempts, as
	// error context) into bb.ChainState so routers, quality gates, and audits can
	// inspect the full subtask-result history across nodes.
	var toolTrace []map[string]any

	for i := 0; i < maxIter; i++ {
		iterations = i + 1
		// Context management: a long-running agent accumulates a large scratchpad,
		// and the whole scratchpad is re-sent every iteration. Left unbounded this
		// makes prompt size grow with iteration count (quadratic total token cost)
		// and eventually overflows the model's context window — the failure mode for
		// complex, many-step tasks. Window it to the most recent steps, which carry
		// the agent's live reasoning state.
		recentSteps := windowScratchpad(scratchpad, maxScratchpadLen)
		if len(recentSteps) < len(scratchpad) {
			scratchpadWindowed = true
		}
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

What is your next step?`, systemMsg, task, toolList, recentSteps)

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
					noProgressStreak++
					if noProgressStreak >= maxNoProgressSteps {
						stopReason = "no_progress"
						break
					}
					continue
				}
				finalAnswer = fa
				stopReason = "final_answer"
				break
			}
			// Unparseable — the model emitted neither a usable Action nor a Final
			// Answer (commonly pure "Thought:" rambling on complex tasks). Record it,
			// then append a corrective format nudge so the model has a concrete chance
			// to recover *within* the no-progress budget instead of drifting off-format
			// until the guard aborts the run. The nudge restates the exact format the
			// next step must use, steering the agent back toward progress.
			scratchpad += fmt.Sprintf("Step %d: %s\n", i+1, strings.TrimSpace(response))
			scratchpad += unparseableNudge(bb)
			noProgressStreak++
			if noProgressStreak >= maxNoProgressSteps {
				stopReason = "no_progress"
				break
			}
			continue
		}

		// Stuck-loop guard: if the agent has already attempted this exact call the
		// maximum number of times, stop rather than spend the rest of the budget
		// repeating it. Downstream nodes can read agent_stop_reason to react.
		sig := action + "\x00" + actionInput
		toolCallCounts[sig]++
		if toolCallCounts[sig] > maxRepeatedCalls {
			stopReason = "repeated_tool_calls"
			break
		}

		// Execute the tool
		toolResult := executeAgentTool(action, actionInput, bb)
		scratchpad += fmt.Sprintf("Step %d: Action: %s(%s) → %s\n", i+1, action, actionInput, toolResult)
		// Store tool result for downstream use
		bb.CachedResult = toolResult

		if isToolUnavailableResult(toolResult) {
			// The agent named a tool that does not exist — a common complex-task
			// failure where the model hallucinates tool names. Varying the input on
			// each call evades the repeated_tool_calls guard (which keys on the exact
			// (action, input) pair), so an agent cycling through made-up tool names
			// would otherwise burn its whole iteration budget on calls that can never
			// run. A call that executed no real tool is not progress: nudge the agent
			// toward a valid tool and count it against the no-progress streak so the
			// loop aborts instead of spinning.
			scratchpad += fmt.Sprintf("Note: '%s' is not an available tool. Choose one of: %s\n", action, availableToolNames(bb))
			toolTrace = append(toolTrace, map[string]any{
				"step":   i + 1,
				"action": action,
				"input":  truncateStr(actionInput, 200),
				"result": truncateStr(toolResult, 400),
				"ok":     false,
			})
			noProgressStreak++
			if noProgressStreak >= maxNoProgressSteps {
				stopReason = "no_progress"
				break
			}
			continue
		}

		// A parsed call that ran a real tool is genuine progress — reset the streak.
		noProgressStreak = 0
		toolUsed = true
		successfulToolCalls++
		toolTrace = append(toolTrace, map[string]any{
			"step":   i + 1,
			"action": action,
			"input":  truncateStr(actionInput, 200),
			"result": truncateStr(toolResult, 400),
			"ok":     true,
		})
		// On a detected repeat, nudge the agent to change approach before it
		// exhausts its remaining iterations on the same dead end.
		if toolCallCounts[sig] > 1 {
			scratchpad += fmt.Sprintf("Note: you have already run %s(%s) %d times with the same result. Do not repeat it — try a different tool, different input, or give your Final Answer.\n", action, actionInput, toolCallCounts[sig])
		}
	}

	// Record agent progress so downstream nodes can inspect how far the loop got
	// and why it stopped (mirrors map_reduce/refine progress tracking).
	bb.ChainState["agent_iterations"] = iterations
	bb.ChainState["agent_tools_used"] = successfulToolCalls
	bb.ChainState["agent_stop_reason"] = stopReason
	bb.ChainState["agent_scratchpad_windowed"] = scratchpadWindowed
	if len(toolTrace) > 0 {
		bb.ChainState["agent_tool_trace"] = toolTrace
	}

	if toolsRequired && !toolUsed {
		bb.Outcome = "tool_evidence_missing"
		bb.Result = fmt.Sprintf("## Blocked: No Tool Evidence\n\nAgent was given real tools but did not successfully use any, so no factual claims were produced.\n\nAvailable real tools: %s\n\nStatus: blocked honestly instead of fabricating output.", availableToolNames(bb))
		bb.Results = append(bb.Results, bb.Result)
		return 1
	}

	if finalAnswer == "" {
		// No final answer produced — generate one from the scratchpad. The loop
		// reached here without a Final Answer, which on complex tasks means it ran
		// many steps (max_iterations / repeated_tool_calls / no_progress), so the
		// accumulated scratchpad can be large. Sending it whole would overflow the
		// model context — the same failure the per-iteration windowing prevents — so
		// bound this synthesis prompt too. The budget is larger than the per-step
		// window because this is the final pass where we want as much of the
		// investigation as fits; the most recent steps (kept by windowScratchpad)
		// carry the freshest tool observations, and bb.ChainState["agent_tool_trace"]
		// still retains the per-step results for any downstream node.
		summaryLog := windowScratchpad(scratchpad, maxSummaryScratchpadLen)
		if len(summaryLog) < len(scratchpad) {
			bb.ChainState["agent_scratchpad_windowed"] = true
		}
		// Reaching the fallback means the loop ended without the agent emitting its
		// own Final Answer, which on complex tasks is precisely when it ran out of
		// budget (max_iterations) or got stuck (repeated_tool_calls / no_progress).
		// The investigation is therefore incomplete, so steer the synthesis toward an
		// honest answer that flags the gaps instead of one that reads as confidently
		// complete — mirroring the map_reduce note that flags failed subtasks.
		incompleteNote := incompleteInvestigationNote(stopReason)
		summaryPrompt := fmt.Sprintf(`Based on the following investigation, provide a final answer. Include ALL data from the investigation log verbatim — do not summarize or omit any results.%s

TASK: %s

INVESTIGATION LOG:
%s

Final Answer:`, incompleteNote, task, summaryLog)
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

// incompleteInvestigationNote returns a sentence appended to the fallback
// synthesis prompt when the agent loop stopped without producing its own Final
// Answer. It names the reason the investigation was cut short and instructs the
// model to answer only from the evidence gathered and to explicitly flag what
// remains unresolved, so a budget-exhausted or stuck run yields an honest,
// caveated answer rather than a confidently complete-sounding one. A natural stop
// (or any unrecognized reason) adds no note.
func incompleteInvestigationNote(stopReason string) string {
	var why string
	switch stopReason {
	case "max_iterations":
		why = "the agent reached its step limit before finishing"
	case "repeated_tool_calls":
		why = "the agent got stuck repeating the same tool call"
	case "no_progress":
		why = "the agent stopped making progress"
	default:
		return ""
	}
	return fmt.Sprintf("\n\nNote: this investigation is INCOMPLETE — %s. Base your answer only on the evidence actually gathered above, and explicitly flag what could not be determined or verified. Do not invent results that are not present in the log.", why)
}

// unparseableNudge returns a corrective note appended to the scratchpad when the
// agent's last response could not be parsed into a ReAct step (no Action/Action
// Input and no Final Answer). It restates the exact format the next step must use,
// giving an off-format model a concrete chance to recover instead of repeating
// freeform reasoning until the no-progress guard fires. The instruction adapts to
// whether real tools are available: with tools the agent may act or answer, without
// tools the only forward move is a Final Answer.
func unparseableNudge(bb *Blackboard) string {
	if hasRealTools(bb) {
		return fmt.Sprintf("Note: that response could not be parsed — it had no 'Action:'/'Action Input:' line and no 'Final Answer:'. Reply with EITHER an Action plus Action Input using one of these tools: %s, OR a Final Answer. Do not reply with reasoning alone.\n", availableToolNames(bb))
	}
	return "Note: that response could not be parsed — it had no 'Action:'/'Action Input:' line and no 'Final Answer:'. Reply with your Final Answer now. Do not reply with reasoning alone.\n"
}

// maxScratchpadLen bounds how much of the ReAct scratchpad is fed back into the
// agent prompt each iteration. It is a character budget, not a hard token limit,
// chosen to leave ample headroom for the task, tool list, and format instructions
// within typical model context windows while preserving several recent steps.
const maxScratchpadLen = 6000

// maxSummaryScratchpadLen bounds the investigation log fed into the fallback
// final-answer synthesis prompt (when the loop ends without a Final Answer). It
// is larger than the per-iteration window because this is the final pass — we
// want as much of the investigation as fits in one prompt — while still capping
// it so a long, many-step complex task cannot overflow the model's context.
const maxSummaryScratchpadLen = 12000

// windowScratchpad returns the scratchpad unchanged when it fits within maxLen,
// otherwise it keeps the most recent content (cut at a line boundary so a step is
// never fed back half-formed) prefixed with an elision marker. Keeping the tail
// rather than the head preserves the agent's live reasoning state: the latest
// observations and the stuck-loop nudges that steer it away from dead ends.
func windowScratchpad(scratchpad string, maxLen int) string {
	if maxLen <= 0 || len(scratchpad) <= maxLen {
		return scratchpad
	}
	tail := scratchpad[len(scratchpad)-maxLen:]
	// Advance to the start of the next whole line so we don't begin mid-step.
	if nl := strings.IndexByte(tail, '\n'); nl >= 0 && nl < len(tail)-1 {
		tail = tail[nl+1:]
	}
	return "... (earlier steps elided to manage context; most recent steps shown) ...\n" + tail
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
//
// Action Input may span multiple lines. Complex tasks routinely pass structured
// arguments to tools — a JSON payload, a code block, or file contents — and the
// model emits them across several lines after the "Action Input:" marker. Capturing
// only the first line (the previous behavior) silently truncated those arguments,
// so a write_file or run_query tool received `{` instead of the whole body. We now
// capture every line from the marker until the next ReAct section marker
// (Thought:/Observation:/Final Answer:/a subsequent Action:) or end of response,
// preserving interior indentation so JSON and code survive intact. Single-line
// inputs are unaffected.
func parseAgentAction(response string) (action string, input string) {
	lines := strings.Split(response, "\n")
	inInput := false
	var inputLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "Action Input:"):
			inInput = true
			inputLines = append(inputLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "Action Input:")))
		case strings.HasPrefix(trimmed, "Action:"):
			action = strings.TrimSpace(strings.TrimPrefix(trimmed, "Action:"))
			inInput = false
		case isReActSectionMarker(trimmed):
			// A new Thought/Observation/Final Answer section ends the input block.
			inInput = false
		case inInput:
			// Preserve the raw line (with its original indentation) so multi-line
			// JSON and code blocks are passed to the tool verbatim.
			inputLines = append(inputLines, line)
		}
	}
	input = strings.TrimSpace(strings.Join(inputLines, "\n"))
	return
}

// isReActSectionMarker reports whether a trimmed line starts a ReAct section other
// than Action/Action Input, marking the end of a multi-line Action Input block.
func isReActSectionMarker(trimmed string) bool {
	for _, m := range []string{"Thought:", "Observation:", "Final Answer:"} {
		if strings.HasPrefix(trimmed, m) {
			return true
		}
	}
	return false
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
	result = replaceAll(result, "{{.RunID}}", bbRunID(bb))
	result = expandBBTemplates(result, bb)
	// Expand {{.ChainState.<key>}} patterns
	result = expandChainStateTemplates(result, bb)
	return result
}

const bbTemplateMaxLen = 500

func bbRunID(bb *Blackboard) string {
	if bb == nil {
		return ""
	}
	if bb.RunID != "" {
		return bb.RunID
	}
	if bb.BB != nil {
		return bb.BB.RunID
	}
	return ""
}

// expandBBTemplates replaces {{.BB.run_id}}, {{.BB.session_id}}, {{.BB.agent}},
// and {{.BB.<key>}} (run scope value/summary) when a blackboard handle is attached.
func expandBBTemplates(s string, bb *Blackboard) string {
	if bb == nil || bb.BB == nil {
		return s
	}
	h := bb.BB
	s = replaceAll(s, "{{.BB.run_id}}", h.RunID)
	s = replaceAll(s, "{{.BB.session_id}}", h.SessionID)
	s = replaceAll(s, "{{.BB.agent}}", h.AgentName)
	for {
		idx := strings.Index(s, "{{.BB.")
		if idx < 0 {
			break
		}
		end := strings.Index(s[idx:], "}}")
		if end < 0 {
			break
		}
		key := s[idx+len("{{.BB.") : idx+end]
		if key == "run_id" || key == "session_id" || key == "agent" {
			s = s[:idx] + s[idx+end+2:]
			continue
		}
		val := bbTemplateValue(h, key)
		s = s[:idx] + val + s[idx+end+2:]
	}
	return s
}

func bbTemplateValue(h *blackboard.Handle, key string) string {
	if h == nil || strings.TrimSpace(key) == "" {
		return ""
	}
	if e, err := h.Get(key); err == nil {
		return bbTemplateDisplay(e)
	}
	if h.SessionID != "" {
		if e, err := h.GetSession(key); err == nil {
			return bbTemplateDisplay(e)
		}
	}
	return ""
}

func bbTemplateDisplay(e blackboard.Entry) string {
	if e.Summary != "" && len(e.Value) > bbTemplateMaxLen {
		return e.Summary
	}
	if len(e.Value) > bbTemplateMaxLen {
		return e.Value[:bbTemplateMaxLen] + "..."
	}
	return e.Value
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
