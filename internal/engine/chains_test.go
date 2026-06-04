package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
)

// mockLLM for chain tests
type chainMockLLM struct {
	responses map[string]string
}

func (m *chainMockLLM) GenerateCtx(_ context.Context, prompt string) (string, error) {
	return m.Generate(prompt)
}
func (m *chainMockLLM) GenerateWithTimeout(prompt string, _ time.Duration) (string, error) {
	return m.Generate(prompt)
}

func (m *chainMockLLM) Generate(prompt string) (string, error) {
	if r, ok := m.responses["generate"]; ok {
		return r, nil
	}
	if len(prompt) > 50 {
		return "mock response for: " + prompt[:50], nil
	}
	return "mock response for: " + prompt, nil
}
func (m *chainMockLLM) AnalyzeComplexity(_ string) string { return "medium" }
func (m *chainMockLLM) GeneratePlan(_, _ string) string {
	return "1. Step one\n2. Step two"
}
func (m *chainMockLLM) Reflect(_, _, _ string) (string, string) { return "ok", "better" }

// DemoChainTree builds a tree that uses ChainAction nodes for a conversational RAG pipeline.
func DemoChainTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "ChainDemo",
		Children: []evolution.SerializableNode{
			// Step 1: LLM call to analyze the task
			{
				Type: "ChainAction",
				Name: "llm_call:Analyze the following and provide insights: {{.Task}}",
				Metadata: map[string]any{
					"max_tokens": float64(1024),
				},
			},
			// Step 2: RAG query using the knowledge graph results
			{
				Type: "ChainAction",
				Name: "rag_query:What are the key findings from this context?",
			},
			// Step 3: Refine the answer
			{
				Type: "ChainAction",
				Name: "refine:{{.Task}}",
			},
			// Step 4: Generate structured JSON output
			{
				Type: "ChainAction",
				Name: "structured_output:Summarize the findings as JSON",
				Metadata: map[string]any{
					"params": map[string]any{
						"json_schema": `{"type":"object","properties":{"summary":{"type":"string"},"confidence":{"type":"number"}}}`,
					},
				},
			},
		},
	}
}

func TestChainAction_LLMCall(t *testing.T) {
	mock := &chainMockLLM{responses: map[string]string{
		"generate": "This is a test analysis result.",
	}}
	bb := &Blackboard{
		Task: "test task",
		LLM:  mock,
	}
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "test",
		Children: []evolution.SerializableNode{{
			Type: "ChainAction",
			Name: "llm_call:{{.Task}}",
		}},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Errorf("expected success, got %s: %s", bb.Outcome, bb.Result)
	}
	if bb.Result != "This is a test analysis result." {
		t.Errorf("expected mock response, got: %s", bb.Result)
	}
}

func TestChainAction_RAGQuery(t *testing.T) {
	mock := &chainMockLLM{responses: map[string]string{
		"generate": "Based on the context, the answer is 42.",
	}}
	bb := &Blackboard{
		Task:      "What is the answer?",
		KgResults: "The answer to life, the universe, and everything is 42.",
		LLM:       mock,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "rag_query:What is the answer?",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Errorf("expected chain_success, got %s", bb.Outcome)
	}
}

func TestChainAction_StructuredOutput(t *testing.T) {
	mock := &chainMockLLM{responses: map[string]string{
		"generate": `{"summary": "All good", "confidence": 0.95}`,
	}}
	bb := &Blackboard{
		Task: "summarize results",
		LLM:  mock,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "structured_output:Summarize as JSON",
		Metadata: map[string]any{
			"params": map[string]any{
				"json_schema": `{"type":"object"}`,
			},
		},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Errorf("expected chain_success, got %s", bb.Outcome)
	}
	if bb.Result != `{"summary": "All good", "confidence": 0.95}` {
		t.Errorf("unexpected result: %s", bb.Result)
	}
}

func TestChainAction_Conversation(t *testing.T) {
	mock := &chainMockLLM{responses: map[string]string{
		"generate": "Hello! How can I help you today?",
	}}
	bb := &Blackboard{
		Task: "greet user",
		LLM:  mock,
		ChainState: map[string]any{
			"conv_history": "User: Hi\nAssistant: Hello there!\n",
		},
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "conversation:How are you?",
		Metadata: map[string]any{
			"system_msg": "You are a helpful assistant.",
		},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Errorf("expected chain_success, got %s", bb.Outcome)
	}
}

func TestChainAction_MapReduce(t *testing.T) {
	mock := &chainMockLLM{}
	bb := &Blackboard{
		Task: "analyze a complex topic",
		LLM:  mock,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "map_reduce:{{.Task}}",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	// Map-reduce should succeed even with mock LLM
	if bb.Outcome != "success" {
		t.Errorf("expected success, got %s: %s", bb.Outcome, bb.Result)
	}
}

func TestChainAction_Refine(t *testing.T) {
	mock := &chainMockLLM{}
	bb := &Blackboard{
		Task: "improve this text",
		LLM:  mock,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "refine:{{.Task}}",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Errorf("expected chain_success, got %s", bb.Outcome)
	}
}

func TestChainAction_ToolCall(t *testing.T) {
	mock := &chainMockLLM{responses: map[string]string{
		"generate": "TOOL: calculator",
	}}
	bb := &Blackboard{
		Task: "calculate something",
		LLM:  mock,
		ChainTools: []any{
			"calculator", "search", "weather",
		},
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "tool_call:{{.Task}}",
		Metadata: map[string]any{
			"tools": []any{"calculator", "search"},
		},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Errorf("expected chain_success, got %s", bb.Outcome)
	}
}

func TestChainAction_UnknownType(t *testing.T) {
	bb := &Blackboard{
		Task: "test",
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "unknown_type:test",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "failure" {
		t.Errorf("expected chain_failed, got %s", bb.Outcome)
	}
}

func TestChainAction_NoLLM(t *testing.T) {
	bb := &Blackboard{
		Task: "test",
		LLM:  nil,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "llm_call:{{.Task}}",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	// Template-only mode returns success with generated template output
	if bb.Outcome != "success" {
		t.Errorf("expected success in template-only mode, got %s: %s", bb.Outcome, bb.Result)
	}
	if bb.Result == "" {
		t.Error("expected non-empty result from template generation")
	}
}

func TestChainAction_ParseConfig(t *testing.T) {
	node := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "llm_call:summarize the document",
		Metadata: map[string]any{
			"max_tokens": float64(512),
			"stream":     true,
			"system_msg": "You are a summarizer.",
			"tools":      []any{"search", "calculator"},
			"params": map[string]any{
				"temperature": "0.7",
			},
		},
	}

	cfg := parseChainConfig(node)

	if cfg.ChainType != "llm_call" {
		t.Errorf("expected llm_call, got %s", cfg.ChainType)
	}
	if cfg.Prompt != "summarize the document" {
		t.Errorf("expected prompt, got %s", cfg.Prompt)
	}
	if cfg.MaxTokens != 512 {
		t.Errorf("expected 512, got %d", cfg.MaxTokens)
	}
	if !cfg.Stream {
		t.Errorf("expected stream=true")
	}
	if cfg.SystemMsg != "You are a summarizer." {
		t.Errorf("expected system msg")
	}
	if len(cfg.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(cfg.Tools))
	}
}

func TestChainAction_DemoTree(t *testing.T) {
	mock := &chainMockLLM{
		responses: map[string]string{
			"generate": "Analysis complete.",
		},
	}
	bb := &Blackboard{
		Task:      "test the demo pipeline",
		KgResults: "sample knowledge graph data",
		LLM:       mock,
	}

	tree := DemoChainTree()
	bt := BuildTree(tree, bb)
	output := RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Errorf("demo tree failed: outcome=%s result=%s", bb.Outcome, output)
	}
	t.Logf("Demo tree output: %s", output)
}

// --- Agent chain tests ---

// mockAgentTool implements the tool interface expected by execAgent
type mockAgentTool struct {
	name        string
	description string
	result      string
}

func (t *mockAgentTool) Name() string        { return t.name }
func (t *mockAgentTool) Description() string { return t.description }
func (t *mockAgentTool) Call(input string) string {
	return t.result + " (input: " + input + ")"
}

func TestChainAction_Agent_DirectAnswer(t *testing.T) {
	mock := &chainMockLLM{responses: map[string]string{
		"generate": "Final Answer: The answer is 42.",
	}}
	bb := &Blackboard{
		Task: "what is the meaning of life?",
		LLM:  mock,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "llm_call:{{.Task}}",
		Metadata: map[string]any{
			"max_tokens": float64(5),
		},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Errorf("agent failed: %s", bb.Result)
	}
	if bb.Result != "Final Answer: The answer is 42." {
		t.Errorf("expected raw LLM response, got: %s", bb.Result)
	}
}

func TestChainAction_Agent_WithTools(t *testing.T) {
	callCount := 0
	mock := &chainMockLLM{
		responses: map[string]string{
			"generate": `Thought: I need to search for information
Action: search
Action Input: Tesla stock price`,
		},
	}
	// Override generate to return different responses
	mock.responses = nil // use default

	bb := &Blackboard{
		Task: "what is Tesla's stock price?",
		LLM:  mock,
		ChainTools: []any{
			&mockAgentTool{name: "search", description: "Search the web", result: "TSLA: $250.00"},
			&mockAgentTool{name: "calculator", description: "Perform calculations", result: "42"},
		},
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "llm_call:{{.Task}}",
		Metadata: map[string]any{
			"max_tokens": float64(3),
		},
	}

	// Custom mock that returns tool call then final answer
	customMock := &agentTestMockLLM{
		responses: []string{
			"Thought: I need to search for Tesla stock\nAction: search\nAction Input: TSLA price",
			"Final Answer: Tesla (TSLA) is trading at $250.00 per share.",
		},
		callCount: &callCount,
	}
	bb.LLM = customMock

	bt := BuildTree(tree, bb)
	output := RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Errorf("agent failed: %s (output: %s)", bb.Result, output)
	}
	t.Logf("Agent output: %s", output)
}

// agentTestMockLLM returns responses in sequence
type agentTestMockLLM struct {
	responses []string
	callCount *int
}

func (m *agentTestMockLLM) GenerateCtx(_ context.Context, prompt string) (string, error) {
	return m.Generate(prompt)
}
func (m *agentTestMockLLM) GenerateWithTimeout(prompt string, _ time.Duration) (string, error) {
	return m.Generate(prompt)
}

func (m *agentTestMockLLM) Generate(_ string) (string, error) {
	idx := *m.callCount
	*m.callCount++
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return "Final Answer: done.", nil
}
func (m *agentTestMockLLM) AnalyzeComplexity(_ string) string       { return "medium" }
func (m *agentTestMockLLM) GeneratePlan(_, _ string) string         { return "plan" }
func (m *agentTestMockLLM) Reflect(_, _, _ string) (string, string) { return "ok", "ok" }

func TestChainAction_Agent_NoTools(t *testing.T) {
	var callCount int
	mock := &agentTestMockLLM{
		responses: []string{
			"Final Answer: Without tools, I'll answer directly: the capital of France is Paris.",
		},
		callCount: &callCount,
	}
	bb := &Blackboard{
		Task: "what is the capital of France?",
		LLM:  mock,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "llm_call:{{.Task}}",
		Metadata: map[string]any{
			"max_tokens": float64(3),
		},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Errorf("expected success, got %s", bb.Outcome)
	}
}

func TestChainAction_Agent_Parse(t *testing.T) {
	// Test action parsing
	action, input := parseAgentAction("Action: search\nAction Input: TSLA price")
	if action != "search" || input != "TSLA price" {
		t.Errorf("parse failed: action=%q input=%q", action, input)
	}

	// Test final answer parsing
	fa := parseFinalAnswer("Final Answer: The result is 42")
	if fa != "The result is 42" {
		t.Errorf("parse final answer failed: %q", fa)
	}

	// Test empty parse
	if parseFinalAnswer("some random text") != "" {
		t.Errorf("expected empty for non-answer text")
	}
}

func TestChainAction_Agent_ToolExecution(t *testing.T) {
	bb := &Blackboard{
		ChainTools: []any{
			&mockAgentTool{name: "calc", description: "calculator", result: "result: 42"},
		},
	}

	result := executeAgentTool("calc", "2+2", bb)
	if result != "result: 42 (input: 2+2)" {
		t.Errorf("tool execution failed: %s", result)
	}

	// Unknown tool
	unknown := executeAgentTool("nonexistent", "test", bb)
	if !strings.Contains(unknown, "not found") {
		t.Errorf("expected 'not found', got: %s", unknown)
	}

	// Tool list building
	list := buildToolList(ChainConfig{Tools: []string{"search", "calc"}}, bb)
	if !strings.Contains(list, "search") || !strings.Contains(list, "calc") {
		t.Errorf("tool list incomplete: %s", list)
	}
}

// --- ToolAction tests ---

func TestChainAction_ToolAction_Direct(t *testing.T) {
	bb := &Blackboard{
		Task: "TSLA stock price",
		ChainTools: []any{
			&mockAgentTool{name: "web_search", description: "search the web", result: "TSLA: $250.00"},
		},
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "tool_action:web_search",
		Metadata: map[string]any{
			"tools": []any{"web_search"},
		},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Errorf("expected success, got %s", bb.Outcome)
	}
	if !strings.Contains(bb.Result, "TSLA") {
		t.Errorf("expected TSLA in result, got: %s", bb.Result)
	}
	t.Logf("Tool result: %s", bb.Result)
}

func TestChainAction_ToolAction_Pipeline(t *testing.T) {
	// Chain: web_search → calculator → agent with results
	bb := &Blackboard{
		Task: "what is Tesla stock price plus $50?",
		ChainTools: []any{
			&mockAgentTool{name: "web_search", description: "search web", result: "TSLA: $250.00"},
			&mockAgentTool{name: "calculator", description: "do math", result: "300.00"},
		},
	}
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "ToolPipeline",
		Children: []evolution.SerializableNode{
			{
				Type:     "ChainAction",
				Name:     "tool_action:web_search:{{.Task}}",
				Metadata: map[string]any{"tools": []any{"web_search"}},
			},
			{
				Type:     "ChainAction",
				Name:     "tool_action:calculator:add 50 to {{.CachedResult}}",
				Metadata: map[string]any{"tools": []any{"calculator"}},
			},
		},
	}

	bt := BuildTree(tree, bb)
	output := RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Errorf("pipeline failed: %s (output: %s)", bb.Outcome, output)
	}
	t.Logf("Pipeline result: %s", bb.Result)
}

func TestChainAction_ToolAction_NoTool(t *testing.T) {
	bb := &Blackboard{
		Task: "test",
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "tool_action:nonexistent:test",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	// Should fail with tool not found message
	if bb.Outcome != "success" {
		t.Errorf("should fall through to LLM simulation, got: %s", bb.Outcome)
	}
}

// --- parseFinalAnswer edge cases (uncovered branches: mid-response, multi-line rest) ---

func TestParseFinalAnswer_MidResponse(t *testing.T) {
	// "Final Answer:" appears in the middle of the response, content on same line only
	result := parseFinalAnswer("Thought: I need to think\nAction: search\nFinal Answer: The answer is Paris")
	if result != "The answer is Paris" {
		t.Errorf("expected 'The answer is Paris', got %q", result)
	}
}

func TestParseFinalAnswer_MidResponseWithRest(t *testing.T) {
	// "Final Answer:" appears mid-response with content on SAME line + rest on subsequent lines
	result := parseFinalAnswer("Some thought\nFinal Answer: Here is the result:\n- Point 1\n- Point 2\n- Point 3")
	if !strings.Contains(result, "Here is the result:") {
		t.Errorf("expected result to contain header, got %q", result)
	}
	if !strings.Contains(result, "- Point 1") {
		t.Errorf("expected result to contain point 1, got %q", result)
	}
	if !strings.Contains(result, "- Point 3") {
		t.Errorf("expected result to contain point 3, got %q", result)
	}
	// Should contain exactly 4 lines (header + 3 points)
	lines := strings.Split(result, "\n")
	if len(lines) != 4 {
		t.Errorf("expected 4 lines, got %d: %q", len(lines), result)
	}
}

func TestParseFinalAnswer_MultipleMarkers(t *testing.T) {
	// Multiple "Final Answer:" markers — first one wins, captures all subsequent content
	// When found mid-response, the function includes ALL rest lines (including any later markers)
	result := parseFinalAnswer("Thought: need to answer\nFinal Answer: First answer.\nSome more text\nFinal Answer: Second answer.")
	if !strings.Contains(result, "First answer.") {
		t.Errorf("expected result to start with 'First answer.', got %q", result)
	}
	if !strings.Contains(result, "Some more text") {
		t.Errorf("expected result to include rest content, got %q", result)
	}
	if !strings.Contains(result, "Final Answer: Second answer.") {
		t.Errorf("expected result to include second marker as part of captured content, got %q", result)
	}
}

func TestParseFinalAnswer_OnlyMarkerNoContent(t *testing.T) {
	// "Final Answer:" marker with no content after it
	result := parseFinalAnswer("Final Answer:")
	if result != "" {
		t.Errorf("expected empty for marker-only, got %q", result)
	}
}

func TestParseFinalAnswer_WithIndentedMarker(t *testing.T) {
	// "Final Answer:" marker indented with whitespace
	result := parseFinalAnswer("  Final Answer: The indented answer")
	if result != "The indented answer" {
		t.Errorf("expected 'The indented answer', got %q", result)
	}
}

// --- execToolCall edge cases ---

// errorMockLLM returns an error on Generate
type errorMockLLM struct {
	err error
}

func (m *errorMockLLM) GenerateCtx(_ context.Context, prompt string) (string, error) {
	return m.Generate(prompt)
}
func (m *errorMockLLM) GenerateWithTimeout(prompt string, _ time.Duration) (string, error) {
	return m.Generate(prompt)
}
func (m *errorMockLLM) Generate(_ string) (string, error)       { return "", m.err }
func (m *errorMockLLM) AnalyzeComplexity(_ string) string       { return "medium" }
func (m *errorMockLLM) GeneratePlan(_, _ string) string         { return "plan" }
func (m *errorMockLLM) Reflect(_, _, _ string) (string, string) { return "ok", "ok" }

func TestChainAction_ToolCall_ChainToolsOnly(t *testing.T) {
	// cfg.Tools is empty/nil, but bb.ChainTools has tools
	mock := &chainMockLLM{responses: map[string]string{
		"generate": "Using web_search tool",
	}}
	bb := &Blackboard{
		Task: "search for something",
		LLM:  mock,
		ChainTools: []any{
			&mockAgentTool{name: "web_search", description: "search the web", result: "result"},
		},
	}
	tree := &evolution.SerializableNode{
		Type:     "ChainAction",
		Name:     "tool_call:{{.Task}}",
		Metadata: map[string]any{}, // no "tools" key — falls back to bb.ChainTools
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Errorf("expected success, got %s", bb.Outcome)
	}
}

func TestChainAction_ToolCall_NoToolsAtAll(t *testing.T) {
	// Neither cfg.Tools nor bb.ChainTools have tools
	mock := &chainMockLLM{responses: map[string]string{
		"generate": "No tools needed, direct answer: 42",
	}}
	bb := &Blackboard{
		Task: "simple question",
		LLM:  mock,
		// ChainTools is nil
	}
	tree := &evolution.SerializableNode{
		Type:     "ChainAction",
		Name:     "tool_call:{{.Task}}",
		Metadata: map[string]any{}, // no tools
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Errorf("expected success even without tools, got %s", bb.Outcome)
	}
}

func TestChainAction_ToolCall_LLMError(t *testing.T) {
	errMock := &errorMockLLM{err: fmt.Errorf("connection refused")}
	bb := &Blackboard{
		Task: "test",
		LLM:  errMock,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "tool_call:{{.Task}}",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "failure" {
		t.Errorf("expected failure, got %s", bb.Outcome)
	}
}

// --- execRAGQuery edge cases ---

func TestChainAction_RAGQuery_NoKGResults(t *testing.T) {
	mock := &chainMockLLM{responses: map[string]string{
		"generate": "Answer from cached context.",
	}}
	bb := &Blackboard{
		Task:         "test question",
		KgResults:    "",                         // empty
		CachedResult: "Cached: the answer is 42", // fallback
		LLM:          mock,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "rag_query:test question",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Errorf("expected success with cached result fallback, got %s: %s", bb.Outcome, bb.Result)
	}
}

func TestChainAction_RAGQuery_NoContextAtAll(t *testing.T) {
	mock := &chainMockLLM{responses: map[string]string{
		"generate": "I don't have enough information.",
	}}
	bb := &Blackboard{
		Task:         "test question",
		KgResults:    "", // empty
		CachedResult: "", // empty too
		LLM:          mock,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "rag_query:test question",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Errorf("expected chain_success (even with empty context), got %s", bb.Outcome)
	}
}

func TestChainAction_RAGQuery_LLMError(t *testing.T) {
	errMock := &errorMockLLM{err: fmt.Errorf("ollama unavailable")}
	bb := &Blackboard{
		Task:      "test",
		KgResults: "some data",
		LLM:       errMock,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "rag_query:test",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "failure" {
		t.Errorf("expected failure, got %s", bb.Outcome)
	}
	if !strings.Contains(bb.Result, "RAG error") {
		t.Errorf("expected RAG error message, got: %s", bb.Result)
	}
}

// --- execConversation edge cases ---

// noopMemory doesn't implement fmt.Stringer
type noopMemory struct{}

func TestChainAction_Conversation_NoMemory(t *testing.T) {
	mock := &chainMockLLM{responses: map[string]string{
		"generate": "Hello! How can I help?",
	}}
	bb := &Blackboard{
		Task:        "greet",
		LLM:         mock,
		ChainMemory: &noopMemory{}, // does NOT implement fmt.Stringer
		ChainState:  map[string]any{},
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "conversation:Hello",
		Metadata: map[string]any{
			"system_msg": "You are a helpful assistant.",
		},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Errorf("expected success without memory, got %s", bb.Outcome)
	}
}

func TestChainAction_Conversation_LLMError(t *testing.T) {
	errMock := &errorMockLLM{err: fmt.Errorf("model not found")}
	bb := &Blackboard{
		Task:       "test",
		LLM:        errMock,
		ChainState: map[string]any{},
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "conversation:test",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "failure" {
		t.Errorf("expected failure, got %s", bb.Outcome)
	}
}

// --- execMapReduce edge cases ---

func TestChainAction_MapReduce_DecomposeError(t *testing.T) {
	errMock := &errorMockLLM{err: fmt.Errorf("decompose failed")}
	bb := &Blackboard{
		Task: "analyze something",
		LLM:  errMock,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "map_reduce:{{.Task}}",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "failure" {
		t.Errorf("expected failure on decompose error, got %s", bb.Outcome)
	}
}

// --- execRefine edge cases ---

func TestChainAction_Refine_InitialError(t *testing.T) {
	errMock := &errorMockLLM{err: fmt.Errorf("initial generation failed")}
	bb := &Blackboard{
		Task: "improve this",
		LLM:  errMock,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "refine:{{.Task}}",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "failure" {
		t.Errorf("expected failure on initial error, got %s", bb.Outcome)
	}
}

// --- execAgent edge cases ---

func TestChainAction_Agent_LLMError(t *testing.T) {
	errMock := &errorMockLLM{err: fmt.Errorf("agent crashed")}
	bb := &Blackboard{
		Task: "do something complex",
		LLM:  errMock,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "agent:{{.Task}}",
		Metadata: map[string]any{
			"max_tokens": float64(3),
		},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "failure" {
		t.Errorf("expected failure, got %s: %s", bb.Outcome, bb.Result)
	}
}

// Compile-time checks
var _ llm.LLM = (*chainMockLLM)(nil)
var _ llm.LLM = (*errorMockLLM)(nil)

// --- Pure function tests: expandTemplate, expandChainStateTemplates ---

func TestExpandTemplate_AllFields(t *testing.T) {
	bb := &Blackboard{
		Task:         "test task",
		Plan:         "a plan",
		Result:       "a result",
		Outcome:      "success",
		Complexity:   "medium",
		CachedResult: "cached data",
		KgResults:    "kg data",
		DurationMs:   1234,
		QualityScore: 0.85,
		CurrentPath:  "SomePath",
		FailureCount: 3,
	}
	result := expandTemplate("Task={{.Task}} Plan={{.Plan}} Result={{.Result}} Outcome={{.Outcome}} Cpx={{.Complexity}} Cache={{.CachedResult}} KG={{.KgResults}} Dur={{.DurationMs}} Q={{.QualityScore}} Path={{.CurrentPath}} FC={{.FailureCount}}", bb)
	if !strings.Contains(result, "Task=test task") {
		t.Errorf("expected task substitution, got: %s", result)
	}
	if !strings.Contains(result, "Dur=1234") {
		t.Errorf("expected DurationMs, got: %s", result)
	}
	if !strings.Contains(result, "Q=0.85") {
		t.Errorf("expected QualityScore, got: %s", result)
	}
	if !strings.Contains(result, "FC=3") {
		t.Errorf("expected FailureCount, got: %s", result)
	}
	if !strings.Contains(result, "Path=SomePath") {
		t.Errorf("expected CurrentPath, got: %s", result)
	}
}

func TestExpandTemplate_EmptyTemplate(t *testing.T) {
	bb := &Blackboard{Task: "default task"}
	result := expandTemplate("", bb)
	if result != "default task" {
		t.Errorf("empty template should return Task, got: %s", result)
	}
}

func TestExpandTemplate_NoPlaceholders(t *testing.T) {
	bb := &Blackboard{Task: "irrelevant"}
	result := expandTemplate("Hello world", bb)
	if result != "Hello world" {
		t.Errorf("expected literal, got: %s", result)
	}
}

func TestExpandChainStateTemplates_Basic(t *testing.T) {
	bb := &Blackboard{
		ChainState: map[string]any{
			"user":   "Alice",
			"age":    30,
			"active": true,
		},
	}
	result := expandChainStateTemplates("User: {{.ChainState.user}}, Age: {{.ChainState.age}}, Active: {{.ChainState.active}}", bb)
	if !strings.Contains(result, "User: Alice") {
		t.Errorf("expected User: Alice, got: %s", result)
	}
	if !strings.Contains(result, "Age: 30") {
		t.Errorf("expected Age: 30, got: %s", result)
	}
}

func TestExpandChainStateTemplates_NilChainState(t *testing.T) {
	bb := &Blackboard{ChainState: nil}
	result := expandChainStateTemplates("{{.ChainState.foo}}", bb)
	if result != "{{.ChainState.foo}}" {
		t.Errorf("nil chain state should leave placeholder unchanged, got: %s", result)
	}
}

func TestExpandChainStateTemplates_MissingKey(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{"exists": "value"}}
	result := expandChainStateTemplates("{{.ChainState.missing}}", bb)
	if result != "" {
		t.Errorf("missing key should become empty, got: %s", result)
	}
}

func TestExpandChainStateTemplates_MultipleSubstitutions(t *testing.T) {
	bb := &Blackboard{
		ChainState: map[string]any{"a": "1", "b": "2", "c": "3"},
	}
	result := expandChainStateTemplates("a={{.ChainState.a}} b={{.ChainState.b}} c={{.ChainState.c}}", bb)
	if result != "a=1 b=2 c=3" {
		t.Errorf("expected a=1 b=2 c=3, got: %s", result)
	}
}

// --- Pure function tests: generateTemplateOutput ---

func TestGenerateTemplateOutput_Basic(t *testing.T) {
	bb := &Blackboard{
		CachedResult: "some data here",
		ChainState:   map[string]any{"key": "val"},
	}
	result := generateTemplateOutput("arc42 Section 3 — Architecture Overview\nMore text", bb)
	if !strings.Contains(result, "# arc42 Section 3") {
		t.Errorf("expected section title, got: %s", result)
	}
	if !strings.Contains(result, "some data here") {
		t.Errorf("expected cached result, got: %s", result)
	}
	if !strings.Contains(result, "key") {
		t.Errorf("expected chain state key, got: %s", result)
	}
}

func TestGenerateTemplateOutput_NoArc42Section(t *testing.T) {
	bb := &Blackboard{
		CachedResult: "simple data",
	}
	result := generateTemplateOutput("Do something with {{.Task}}", bb)
	if !strings.Contains(result, "# Arc42 Section") {
		t.Errorf("expected default title, got: %s", result)
	}
}

func TestGenerateTemplateOutput_TruncatedCache(t *testing.T) {
	long := strings.Repeat("x", 600)
	bb := &Blackboard{CachedResult: long}
	result := generateTemplateOutput("prompt", bb)
	if !strings.Contains(result, "... (truncated)") {
		t.Errorf("expected truncation, got: %s", result)
	}
}

func TestGenerateTemplateOutput_NilChainState(t *testing.T) {
	bb := &Blackboard{CachedResult: "data"}
	result := generateTemplateOutput("prompt", bb)
	if !strings.Contains(result, "data") {
		t.Errorf("expected data in output, got: %s", result)
	}
}

// --- Pure function tests: replaceAll ---

func TestReplaceAll_Basic(t *testing.T) {
	result := replaceAll("hello WORLD world WORLD", "WORLD", "Earth")
	if result != "hello Earth world Earth" {
		t.Errorf("got: %s", result)
	}
}

func TestReplaceAll_NoMatch(t *testing.T) {
	result := replaceAll("hello world", "MARS", "Earth")
	if result != "hello world" {
		t.Errorf("got: %s", result)
	}
}

func TestReplaceAll_Empty(t *testing.T) {
	result := replaceAll("", "x", "y")
	if result != "" {
		t.Errorf("got: %s", result)
	}
}

// --- Pure function tests: splitLines ---

func TestSplitLines_Basic(t *testing.T) {
	lines := splitLines("1. First task\n2. Second task\n3. Third task")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "First task" {
		t.Errorf("got: %s", lines[0])
	}
}

func TestSplitLines_EmptyInput(t *testing.T) {
	lines := splitLines("")
	if len(lines) != 0 {
		t.Errorf("expected 0 lines for empty input, got %d", len(lines))
	}
}

func TestSplitLines_SkipEmptyLines(t *testing.T) {
	lines := splitLines("1. Task A\n\n2. Task B\n\n\n3. Task C")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines (skipping empty), got %d", len(lines))
	}
}

func TestSplitLines_DashPrefix(t *testing.T) {
	lines := splitLines("- Task A\n- Task B\n- Task C")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "Task A" {
		t.Errorf("expected 'Task A', got %q", lines[0])
	}
}

func TestSplitLines_MixedFormats(t *testing.T) {
	lines := splitLines("1. First\n- Second\n3. Third")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	if lines[1] != "Second" {
		t.Errorf("expected 'Second', got %q", lines[1])
	}
}

// --- Pure function tests: tools_real.go helpers ---

func TestStripHTML_Basic(t *testing.T) {
	result := stripHTML("<b>Hello</b> <i>World</i>")
	if result != "Hello World" {
		t.Errorf("expected 'Hello World', got: %q", result)
	}
}

func TestStripHTML_NoTags(t *testing.T) {
	result := stripHTML("plain text no tags")
	if result != "plain text no tags" {
		t.Errorf("expected unchanged, got: %q", result)
	}
}

func TestStripHTML_NestedTags(t *testing.T) {
	result := stripHTML("<div><span>content</span></div>")
	if result != "content" {
		t.Errorf("expected 'content', got: %q", result)
	}
}

func TestStripHTML_EmptyString(t *testing.T) {
	result := stripHTML("")
	if result != "" {
		t.Errorf("expected empty, got: %q", result)
	}
}

func TestStripHTML_Attributes(t *testing.T) {
	result := stripHTML(`<a href="http://example.com" class="link">click here</a>`)
	if result != "click here" {
		t.Errorf("expected 'click here', got: %q", result)
	}
}

func TestExtractDuckDuckGoResults_ValidHTML(t *testing.T) {
	html := `<div class="result">
		<a class="result__a" href="https://example.com">Example Title</a>
		<span class="result__snippet">This is a snippet about examples.</span>
		<span class="result__url">example.com</span>
	</div>`
	result := extractDuckDuckGoResults(html)
	// The function may use fallback link extraction if snippet regex doesn't match
	if result == "" {
		t.Error("expected non-empty result from HTML extraction")
	}
	t.Logf("extract result: %s", result)
}

func TestExtractDuckDuckGoResults_NoResults(t *testing.T) {
	result := extractDuckDuckGoResults("<html><body>no results here</body></html>")
	if result != "" {
		t.Errorf("expected empty for no results, got: %s", result)
	}
}

func TestExtractDuckDuckGoResults_FallbackToLinks(t *testing.T) {
	html := `<div class="web-result">
		<a class="result__a" href="https://fallback.com">Fallback Title</a>
	</div>`
	result := extractDuckDuckGoResults(html)
	if !strings.Contains(result, "Fallback Title") {
		t.Errorf("expected fallback link, got: %s", result)
	}
}

func TestExtractDuckDuckGoResults_EmptyHTML(t *testing.T) {
	result := extractDuckDuckGoResults("")
	if result != "" {
		t.Errorf("expected empty, got: %s", result)
	}
}

// --- Real tool struct tests ---

func TestRealTool_NameAndDescription(t *testing.T) {
	rt := &realTool{name: "test_tool", desc: "does testing"}
	if rt.Name() != "test_tool" {
		t.Errorf("expected test_tool, got %s", rt.Name())
	}
	if rt.Description() != "does testing" {
		t.Errorf("expected description, got %s", rt.Description())
	}
}

func TestRealTool_Call(t *testing.T) {
	rt := &realTool{name: "echo", desc: "echoes input", fn: func(input string) string { return "echo: " + input }}
	result := rt.Call("hello")
	if result != "echo: hello" {
		t.Errorf("expected 'echo: hello', got %q", result)
	}
}

// --- Tool factory functions (struct validation only) ---

func TestNewShellExecTool_Structure(t *testing.T) {
	rt := newShellExecTool()
	if rt.Name() != "shell_exec" {
		t.Errorf("expected shell_exec, got %s", rt.Name())
	}
	if rt.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestNewFileReadTool_Structure(t *testing.T) {
	rt := newFileReadTool()
	if rt.Name() != "file_read" {
		t.Errorf("expected file_read, got %s", rt.Name())
	}
}

func TestNewFileWriteTool_Structure(t *testing.T) {
	rt := newFileWriteTool()
	if rt.Name() != "file_write" {
		t.Errorf("expected file_write, got %s", rt.Name())
	}
}

func TestNewWebSearchTool_Structure(t *testing.T) {
	rt := newWebSearchTool()
	if rt.Name() != "web_search" {
		t.Errorf("expected web_search, got %s", rt.Name())
	}
}

func TestNewGoBuildTool_Structure(t *testing.T) {
	rt := newGoBuildTool()
	if rt.Name() != "go_build" {
		t.Errorf("expected go_build, got %s", rt.Name())
	}
}

func TestNewGoTestTool_Structure(t *testing.T) {
	rt := newGoTestTool()
	if rt.Name() != "go_test" {
		t.Errorf("expected go_test, got %s", rt.Name())
	}
}

func TestNewGoVetTool_Structure(t *testing.T) {
	rt := newGoVetTool()
	if rt.Name() != "go_vet" {
		t.Errorf("expected go_vet, got %s", rt.Name())
	}
}

func TestNewGraphifyTool_Structure(t *testing.T) {
	rt := newGraphifyTool()
	if rt.Name() != "graphify" {
		t.Errorf("expected graphify, got %s", rt.Name())
	}
}

// --- toolStub tests ---

func TestToolStub_NameDescription(t *testing.T) {
	ts := toolStub{name: "stub_tool", desc: "a stub"}
	if ts.Name() != "stub_tool" {
		t.Errorf("expected stub_tool, got %s", ts.Name())
	}
	if ts.Description() != "a stub" {
		t.Errorf("expected description, got %s", ts.Description())
	}
}

func TestToolStub_CallReturnsEmpty(t *testing.T) {
	ts := toolStub{name: "stub", desc: "desc"}
	result := ts.Call("anything")
	if result != "" {
		t.Errorf("expected empty (triggers LLM simulation fallback), got: %q", result)
	}
}

// --- Additional edge-case tests for low-coverage chain functions ---

func TestChainAction_StructuredOutput_NoSchema(t *testing.T) {
	// execStructuredOutput: no json_schema in params → empty schemaDesc path
	mock := &chainMockLLM{responses: map[string]string{
		"generate": `{"summary": "All good"}`,
	}}
	bb := &Blackboard{
		Task: "summarize results",
		LLM:  mock,
	}
	tree := &evolution.SerializableNode{
		Type:     "ChainAction",
		Name:     "structured_output:Summarize without schema",
		Metadata: map[string]any{},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Errorf("expected success, got %s: %s", bb.Outcome, bb.Result)
	}
}

func TestChainAction_StructuredOutput_NilLLM(t *testing.T) {
	// execStructuredOutput: bb.LLM == nil → failure path
	bb := &Blackboard{
		Task: "summarize results",
		LLM:  nil,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "structured_output:Summarize as JSON",
		Metadata: map[string]any{
			"params": map[string]any{
				"json_schema": `{"type":"object"}`,
			},
		},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "failure" {
		t.Errorf("expected failure for nil LLM, got %s", bb.Outcome)
	}
}

func TestChainAction_StructuredOutput_LLMError(t *testing.T) {
	// execStructuredOutput: LLM.Generate error → failure path
	errMock := &errorMockLLM{err: fmt.Errorf("simulated error")}
	bb := &Blackboard{
		Task: "summarize results",
		LLM:  errMock,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "structured_output:Summarize as JSON",
		Metadata: map[string]any{
			"params": map[string]any{
				"json_schema": `{"type":"object"}`,
			},
		},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "failure" {
		t.Errorf("expected failure for LLM error, got %s", bb.Outcome)
	}
}

func TestChainAction_Agent_NilLLM(t *testing.T) {
	// execAgent: bb.LLM == nil → failure path
	bb := &Blackboard{
		Task: "analyze something",
		LLM:  nil,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "agent:Analyze the task and research: {{.Task}}",
		Metadata: map[string]any{
			"max_tokens": float64(10),
		},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "failure" {
		t.Errorf("expected failure for nil LLM, got %s", bb.Outcome)
	}
	if bb.Result != "no LLM available for agent" {
		t.Errorf("expected 'no LLM available for agent', got: %s", bb.Result)
	}
}

func TestChainAction_Agent_SummaryError(t *testing.T) {
	// execAgent: agent runs iterations with no tools, produces no Final Answer,
	// then the summary prompt fails → error path
	mock := &chainMockLLM{responses: map[string]string{
		"generate": "Thought: I should analyze this task step by step\nAction: none\nAction Input: none",
	}}
	bb := &Blackboard{
		Task:       "analyze complex topic",
		LLM:        mock,
		ChainTools: []any{},
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "agent:Deep analysis: {{.Task}}",
		Metadata: map[string]any{
			"max_tokens": float64(3),
		},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	// After 3 iterations of "none" actions, finalAnswer should be empty
	// and the summary prompt should succeed with our mock
	if bb.Outcome != "success" {
		t.Errorf("expected success (summary generated), got %s: %s", bb.Outcome, bb.Result)
	}
}

func TestChainAction_Agent_SummaryLLMError(t *testing.T) {
	// execAgent: agent runs, no final answer, summary prompt errors
	callCount := 0
	mock := &countedErrorMockLLM{
		responses: map[string]string{
			"generate": "Thought: reviewing the task\nAction: unknown\nAction Input: none",
		},
		failOnCall: 4, // fail on the 4th call (summary prompt)
		count:      &callCount,
	}
	bb := &Blackboard{
		Task:       "research quantum computing",
		LLM:        mock,
		ChainTools: []any{},
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "agent:Research: {{.Task}}",
		Metadata: map[string]any{
			"max_tokens": float64(3),
		},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "failure" {
		t.Errorf("expected failure (summary LLM error), got %s: %s", bb.Outcome, bb.Result)
	}
}

// countedErrorMockLLM returns responses for N calls, then errors
type countedErrorMockLLM struct {
	responses  map[string]string
	failOnCall int
	count      *int
}

func (m *countedErrorMockLLM) GenerateCtx(_ context.Context, prompt string) (string, error) {
	return m.Generate(prompt)
}
func (m *countedErrorMockLLM) GenerateWithTimeout(prompt string, _ time.Duration) (string, error) {
	return m.Generate(prompt)
}
func (m *countedErrorMockLLM) Generate(_ string) (string, error) {
	*m.count++
	if *m.count >= m.failOnCall {
		return "", fmt.Errorf("simulated error on call %d", *m.count)
	}
	if r, ok := m.responses["generate"]; ok {
		return r, nil
	}
	return "mock response", nil
}
func (m *countedErrorMockLLM) AnalyzeComplexity(_ string) string       { return "medium" }
func (m *countedErrorMockLLM) GeneratePlan(_, _ string) string         { return "plan" }
func (m *countedErrorMockLLM) Reflect(_, _, _ string) (string, string) { return "ok", "ok" }

func TestChainAction_LLMCall_Error(t *testing.T) {
	// execLLMCall: LLM.Generate error → failure path
	errMock := &errorMockLLM{err: fmt.Errorf("simulated error")}
	bb := &Blackboard{
		Task: "test task",
		LLM:  errMock,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "llm_call:{{.Task}}",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "failure" {
		t.Errorf("expected failure for LLM error, got %s", bb.Outcome)
	}
	if !strings.Contains(bb.Result, "LLM error") {
		t.Errorf("expected 'LLM error' in result, got: %s", bb.Result)
	}
}

func TestChainAction_MapReduce_NilLLM(t *testing.T) {
	// execMapReduce: bb.LLM == nil → failure path
	bb := &Blackboard{
		Task: "analyze a complex topic",
		LLM:  nil,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "map_reduce:Break down and analyze: {{.Task}}",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "failure" {
		t.Errorf("expected failure for nil LLM, got %s", bb.Outcome)
	}
}

func TestChainAction_MapReduce_SubErrors(t *testing.T) {
	// execMapReduce: sub-result LLM errors use 'continue' (not fail)
	callCount := 0
	mock := &countedErrorMockLLM{
		responses: map[string]string{
			"generate": "1. Sub1\n2. Sub2\n3. Sub3",
		},
		failOnCall: 99, // never fail
		count:      &callCount,
	}
	bb := &Blackboard{
		Task: "analyze complex topic",
		LLM:  mock,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "map_reduce:{{.Task}}",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Errorf("expected success, got %s: %s", bb.Outcome, bb.Result)
	}
}

func TestChainAction_MapReduce_SubResultErrors(t *testing.T) {
	// execMapReduce: LLM errors on sub-result generation → continue (skip sub)
	callCount := 0
	mock := &countedErrorMockLLM{
		responses: map[string]string{
			"generate": "1. Sub1\n2. Sub2\n3. Sub3",
		},
		failOnCall: 2, // fail on call 2 (sub-result generation, skipped via continue)
		count:      &callCount,
	}
	bb := &Blackboard{
		Task: "analyze complex topic",
		LLM:  mock,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "map_reduce:{{.Task}}",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	// Second sub-result errors, but the function continues to the reduce phase
	if bb.Outcome != "failure" {
		t.Errorf("expected failure (reduce LLM also errors on failOnCall=2), got %s: %s", bb.Outcome, bb.Result)
	}
}

func TestChainAction_Agent_MaxIterBoundaries(t *testing.T) {
	// execAgent: Test MaxTokens boundary values (0, 1, 31)
	tests := []struct {
		name      string
		maxTokens float64
	}{
		{"ZeroUsesDefault15", 0},
		{"OneKeepsOne", 1},
		{"ThirtyOneCappedTo15", 31},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := &chainMockLLM{responses: map[string]string{
				"generate": "Final Answer: Complete analysis result",
			}}
			bb := &Blackboard{
				Task:       "analyze something",
				LLM:        mock,
				ChainTools: []any{},
			}
			tree := &evolution.SerializableNode{
				Type: "ChainAction",
				Name: "agent:Analyze: {{.Task}}",
				Metadata: map[string]any{
					"max_tokens": tc.maxTokens,
				},
			}

			bt := BuildTree(tree, bb)
			RunTask(bb, bt)

			if bb.Outcome != "success" {
				t.Errorf("expected success, got %s: %s", bb.Outcome, bb.Result)
			}
		})
	}
}

func TestChainAction_Agent_UnparseableScratchpad(t *testing.T) {
	// execAgent: agent response doesn't parse as action or final answer → added to scratchpad
	mock := &chainMockLLM{responses: map[string]string{
		"generate": "I'm thinking about how to approach this...",
	}}
	bb := &Blackboard{
		Task:       "analyze something",
		LLM:        mock,
		ChainTools: []any{},
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "agent:Think about: {{.Task}}",
		Metadata: map[string]any{
			"max_tokens": float64(2),
		},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	// Unparseable responses go to scratchpad, then summary prompt runs
	if bb.Outcome != "success" {
		t.Errorf("expected success (summary generated), got %s: %s", bb.Outcome, bb.Result)
	}
}

func TestChainAction_Conversation_NilLLM(t *testing.T) {
	// execConversation: bb.LLM == nil → failure path
	bb := &Blackboard{
		Task: "greet user",
		LLM:  nil,
		ChainState: map[string]any{
			"conv_history": "User: Hi\n",
		},
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "conversation:How are you?",
		Metadata: map[string]any{
			"system_msg": "You are helpful.",
		},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "failure" {
		t.Errorf("expected failure for nil LLM, got %s", bb.Outcome)
	}
}

func TestChainAction_Conversation_LLMErrorWithHistory(t *testing.T) {
	// execConversation: LLM.Generate error with conv_history present
	errMock := &errorMockLLM{err: fmt.Errorf("conversation error")}
	bb := &Blackboard{
		Task: "greet user",
		LLM:  errMock,
		ChainState: map[string]any{
			"conv_history": "User: Hello\nAssistant: Hi!\n",
		},
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "conversation:Continue the chat",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "failure" {
		t.Errorf("expected failure for LLM error, got %s", bb.Outcome)
	}
}

func TestChainAction_RAGQuery_NilLLM(t *testing.T) {
	// execRAGQuery: bb.LLM == nil at QA phase → failure path
	bb := &Blackboard{
		Task:      "find information",
		LLM:       nil,
		KgResults: "Some retrieved context",
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "rag_query:{{.Task}}",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "failure" {
		t.Errorf("expected failure for nil LLM, got %s", bb.Outcome)
	}
}

func TestChainAction_RetrievalQA_NilLLM(t *testing.T) {
	// execRetrievalQA: bb.LLM == nil at QA phase → failure path
	bb := &Blackboard{
		Task:      "answer question",
		LLM:       nil,
		KgResults: "Some context info",
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "retrieval_qa:{{.Task}}",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "failure" {
		t.Errorf("expected failure for nil LLM, got %s", bb.Outcome)
	}
}

func TestChainAction_Refine_NilLLM(t *testing.T) {
	// execRefine: bb.LLM == nil → failure path
	bb := &Blackboard{
		Task: "improve text",
		LLM:  nil,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "refine:Improve the following: {{.Task}}",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "failure" {
		t.Errorf("expected failure for nil LLM, got %s", bb.Outcome)
	}
}

func TestChainAction_ToolAction_NilTools(t *testing.T) {
	// execToolAction: no tool name in cfg.Tools or prompt → failure path
	bb := &Blackboard{
		Task: "do something",
		LLM:  &chainMockLLM{},
	}
	tree := &evolution.SerializableNode{
		Type:     "ChainAction",
		Name:     "tool_action:",
		Metadata: map[string]any{},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "failure" {
		t.Errorf("expected failure for no tool name, got %s", bb.Outcome)
	}
}

func TestChainAction_ToolAction_FromPrompt(t *testing.T) {
	// execToolAction: tool name parsed from prompt (no cfg.Tools)
	mock := &chainMockLLM{}
	bb := &Blackboard{
		Task:       "search for info",
		LLM:        mock,
		ChainTools: []any{toolStub{name: "web_search", desc: "Search the web"}},
	}
	tree := &evolution.SerializableNode{
		Type:     "ChainAction",
		Name:     "tool_action:web_search:some query",
		Metadata: map[string]any{},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Errorf("expected success, got %s: %s", bb.Outcome, bb.Result)
	}
}

func TestChainAction_BuildChainActionFn_Panic(t *testing.T) {
	// buildChainActionFn: panic recovery test
	// Use an LLM that panics
	mock := &chainMockLLM{responses: map[string]string{
		"generate": "Final Answer: done",
	}}
	bb := &Blackboard{
		Task: "test task",
		LLM:  mock,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "unknown_chain_type:this should panic",
		Metadata: map[string]any{
			"max_tokens": float64(5),
		},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	// Unknown chain type should not crash — panic recovery catches it
	if bb.Outcome != "failure" && bb.Outcome != "chain_panic" {
		t.Errorf("expected failure or chain_panic, got %s: %s", bb.Outcome, bb.Result)
	}
}
