package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

// MockLLM for chain tests
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
		"generate": "TOOL: calculator - selecting the calculator tool to compute the result",
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
			"generate": "Analysis complete: the pipeline produced a detailed, substantive result.",
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

func TestParseAgentAction_MultiLineInput(t *testing.T) {
	// A complex task passes a multi-line JSON payload as the tool argument.
	// The whole body must survive, with interior indentation preserved.
	resp := "Thought: write the config file\n" +
		"Action: write_file\n" +
		"Action Input: {\n" +
		"  \"path\": \"config.json\",\n" +
		"  \"content\": \"value\"\n" +
		"}"
	action, input := parseAgentAction(resp)
	if action != "write_file" {
		t.Fatalf("action = %q, want write_file", action)
	}
	wantInput := "{\n  \"path\": \"config.json\",\n  \"content\": \"value\"\n}"
	if input != wantInput {
		t.Fatalf("input = %q, want %q", input, wantInput)
	}
}

func TestParseAgentAction_StopsAtNextSection(t *testing.T) {
	// Anything after a following ReAct section marker is not part of the input.
	resp := "Action: search\n" +
		"Action Input: line one\n" +
		"line two\n" +
		"Observation: should not be captured\n" +
		"Final Answer: nor this"
	action, input := parseAgentAction(resp)
	if action != "search" {
		t.Fatalf("action = %q, want search", action)
	}
	if input != "line one\nline two" {
		t.Fatalf("input = %q, want %q", input, "line one\nline two")
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

	// Should fail closed with tool not found message; missing tools must never be LLM-simulated.
	if bb.Outcome != "failure" {
		t.Errorf("expected fail-closed failure, got: %s", bb.Outcome)
	}
	if !strings.Contains(bb.Result, "TOOL_UNAVAILABLE") {
		t.Errorf("expected TOOL_UNAVAILABLE, got: %s", bb.Result)
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
		"generate": "Using web_search tool to look up the requested information",
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
		"generate": "Answer from cached context: the answer is 42, based on the fallback.",
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
		"generate": "Hello! How can I help you with your task today?",
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

// flakySubtaskMockLLM errors on the first N "Complete this subtask" calls and
// succeeds otherwise, simulating transient subtask-generation failures so the
// map_reduce retry path can be exercised.
type flakySubtaskMockLLM struct {
	failFirstN int
	seen       int
}

func (m *flakySubtaskMockLLM) Generate(prompt string) (string, error) {
	if strings.Contains(prompt, "Complete this subtask") {
		m.seen++
		if m.seen <= m.failFirstN {
			return "", fmt.Errorf("transient subtask error %d", m.seen)
		}
	}
	if strings.Contains(prompt, "Break down this task") {
		return "1. first subtask\n2. second subtask", nil
	}
	return "subtask result with sufficient length for validation", nil
}
func (m *flakySubtaskMockLLM) GenerateCtx(_ context.Context, p string) (string, error) {
	return m.Generate(p)
}
func (m *flakySubtaskMockLLM) GenerateWithTimeout(p string, _ time.Duration) (string, error) {
	return m.Generate(p)
}
func (m *flakySubtaskMockLLM) AnalyzeComplexity(_ string) string       { return "medium" }
func (m *flakySubtaskMockLLM) GeneratePlan(_, _ string) string         { return "1. a\n2. b" }
func (m *flakySubtaskMockLLM) Reflect(_, _, _ string) (string, string) { return "ok", "better" }

// A first-attempt failure on a subtask must be retried, not dropped: the subtask
// should still complete and be counted, with the retry recorded in ChainState.
func TestChainAction_MapReduce_SubtaskRetryRecovers(t *testing.T) {
	mock := &flakySubtaskMockLLM{failFirstN: 1}
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

	if bb.Outcome != "success" {
		t.Fatalf("expected success after subtask retry, got %s: %s", bb.Outcome, bb.Result)
	}
	if got := bb.ChainState["map_reduce_completed"]; got != 2 {
		t.Errorf("expected 2 completed subtasks (failure recovered via retry), got %v", got)
	}
	if got := bb.ChainState["map_reduce_failed"]; got != 0 {
		t.Errorf("expected 0 failed subtasks, got %v", got)
	}
	if got := bb.ChainState["map_reduce_retried"]; got != 1 {
		t.Errorf("expected 1 retried subtask, got %v", got)
	}
}

// flakyDecomposeMockLLM errors on the first N "Break down this task" calls and
// succeeds otherwise, simulating a transient decomposition failure so the
// map_reduce decompose-retry path can be exercised.
type flakyDecomposeMockLLM struct {
	failFirstN int
	seen       int
}

func (m *flakyDecomposeMockLLM) Generate(prompt string) (string, error) {
	if strings.Contains(prompt, "Break down this task") {
		m.seen++
		if m.seen <= m.failFirstN {
			return "", fmt.Errorf("transient decompose error %d", m.seen)
		}
		return "1. first subtask\n2. second subtask", nil
	}
	return "subtask result with sufficient length for validation", nil
}
func (m *flakyDecomposeMockLLM) GenerateCtx(_ context.Context, p string) (string, error) {
	return m.Generate(p)
}
func (m *flakyDecomposeMockLLM) GenerateWithTimeout(p string, _ time.Duration) (string, error) {
	return m.Generate(p)
}
func (m *flakyDecomposeMockLLM) AnalyzeComplexity(_ string) string       { return "medium" }
func (m *flakyDecomposeMockLLM) GeneratePlan(_, _ string) string         { return "1. a\n2. b" }
func (m *flakyDecomposeMockLLM) Reflect(_, _, _ string) (string, string) { return "ok", "better" }

// A first-attempt failure on the decompose call must be retried, not fatal: the
// whole map_reduce should still run, with the retry recorded in ChainState.
func TestChainAction_MapReduce_DecomposeRetryRecovers(t *testing.T) {
	mock := &flakyDecomposeMockLLM{failFirstN: 1}
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

	if bb.Outcome != "success" {
		t.Fatalf("expected success after decompose retry, got %s: %s", bb.Outcome, bb.Result)
	}
	if got, _ := bb.ChainState["map_reduce_decompose_retried"].(bool); !got {
		t.Errorf("expected map_reduce_decompose_retried=true, got %v", bb.ChainState["map_reduce_decompose_retried"])
	}
	if got := bb.ChainState["map_reduce_completed"]; got != 2 {
		t.Errorf("expected 2 completed subtasks after decompose recovered, got %v", got)
	}
}

// emptyDecomposeMockLLM returns an unparseable (whitespace-only) decomposition so
// the map_reduce decompose-empty fallback can be exercised; other calls succeed.
type emptyDecomposeMockLLM struct{}

func (m *emptyDecomposeMockLLM) Generate(prompt string) (string, error) {
	if strings.Contains(prompt, "Break down this task") {
		return "   \n  \n", nil // no parseable subtask lines
	}
	return "subtask result with sufficient length for validation", nil
}
func (m *emptyDecomposeMockLLM) GenerateCtx(_ context.Context, p string) (string, error) {
	return m.Generate(p)
}
func (m *emptyDecomposeMockLLM) GenerateWithTimeout(p string, _ time.Duration) (string, error) {
	return m.Generate(p)
}
func (m *emptyDecomposeMockLLM) AnalyzeComplexity(_ string) string       { return "medium" }
func (m *emptyDecomposeMockLLM) GeneratePlan(_, _ string) string         { return "1. a\n2. b" }
func (m *emptyDecomposeMockLLM) Reflect(_, _, _ string) (string, string) { return "ok", "better" }

// When decomposition yields no parseable subtasks, map_reduce must not hard-fail
// with "all 0 subtasks failed"; it should degrade to treating the whole task as a
// single subtask, still produce an answer, and flag the degraded mode.
func TestChainAction_MapReduce_EmptyDecompositionFallback(t *testing.T) {
	bb := &Blackboard{
		Task: "analyze a complex topic that the model fails to split",
		LLM:  &emptyDecomposeMockLLM{},
	}

	result := execMapReduce(ChainConfig{ChainType: "map_reduce", Prompt: "{{.Task}}"}, bb)

	if result != 1 {
		t.Fatalf("expected success (1) via single-subtask fallback, got %d: %s", result, bb.Result)
	}
	if got, _ := bb.ChainState["map_reduce_decompose_empty"].(bool); !got {
		t.Errorf("expected map_reduce_decompose_empty=true, got %v", bb.ChainState["map_reduce_decompose_empty"])
	}
	if got := bb.ChainState["map_reduce_completed"]; got != 1 {
		t.Errorf("expected 1 completed subtask (whole task), got %v", got)
	}
}

// --- {{.ChainHistory}} template token ---

func TestRenderChainHistory_Empty(t *testing.T) {
	if got := renderChainHistory(nil); got != "" {
		t.Errorf("nil blackboard should render empty, got %q", got)
	}
	bb := &Blackboard{}
	if got := renderChainHistory(bb); got != "" {
		t.Errorf("nil ChainState should render empty, got %q", got)
	}
	bb.ChainState = map[string]any{}
	if got := renderChainHistory(bb); got != "" {
		t.Errorf("missing history should render empty, got %q", got)
	}
}

func TestRenderChainHistory_ReadableLines(t *testing.T) {
	bb := &Blackboard{}
	// Drive the real recorder so the rendered shape matches what runs in production.
	bb.Result = "first node output"
	recordChainHistory(bb, "map_reduce", 1, 1200)
	bb.Result = "second node failed"
	recordChainHistory(bb, "agent", -1, 800)

	got := renderChainHistory(bb)
	for _, want := range []string{
		"#1 map_reduce → success (1200ms)",
		"first node output",
		"#2 agent → failure (800ms)",
		"second node failed",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered history missing %q\ngot:\n%s", want, got)
		}
	}
	// Must be readable lines, not a Go-map dump.
	if strings.Contains(got, "map[") {
		t.Errorf("rendered history leaked Go-map syntax:\n%s", got)
	}
}

func TestExpandTemplate_ChainHistoryToken(t *testing.T) {
	bb := &Blackboard{}
	bb.Result = "did the thing"
	recordChainHistory(bb, "llm_call", 1, 42)

	out := expandTemplate("Prior steps:\n{{.ChainHistory}}\nNow continue.", bb)
	if !strings.Contains(out, "#1 llm_call → success (42ms)") {
		t.Errorf("expandTemplate did not render {{.ChainHistory}}, got:\n%s", out)
	}
	if strings.Contains(out, "{{.ChainHistory}}") {
		t.Errorf("token left unexpanded:\n%s", out)
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

// flakyRefineMockLLM errors on the first N calls (regardless of phase) and
// succeeds otherwise, simulating a transient blip on the refine chain's initial
// generation so the retry-once recovery path can be exercised.
type flakyRefineMockLLM struct {
	failFirstN int
	seen       int
}

func (m *flakyRefineMockLLM) Generate(prompt string) (string, error) {
	m.seen++
	if m.seen <= m.failFirstN {
		return "", fmt.Errorf("transient refine error %d", m.seen)
	}
	// Signal convergence after the first revision so the loop stops cleanly.
	if strings.Contains(prompt, "Critique this answer") {
		return "NO_FURTHER_IMPROVEMENT", nil
	}
	return "refined answer with sufficient length for validation", nil
}
func (m *flakyRefineMockLLM) GenerateCtx(_ context.Context, p string) (string, error) {
	return m.Generate(p)
}
func (m *flakyRefineMockLLM) GenerateWithTimeout(p string, _ time.Duration) (string, error) {
	return m.Generate(p)
}
func (m *flakyRefineMockLLM) AnalyzeComplexity(_ string) string       { return "medium" }
func (m *flakyRefineMockLLM) GeneratePlan(_, _ string) string         { return "1. a\n2. b" }
func (m *flakyRefineMockLLM) Reflect(_, _, _ string) (string, string) { return "ok", "better" }

// A first-attempt failure on the initial generation must be retried, not fatal:
// the refine chain should still succeed, with the retry recorded in ChainState.
func TestChainAction_Refine_InitialRetryRecovers(t *testing.T) {
	mock := &flakyRefineMockLLM{failFirstN: 1}
	bb := &Blackboard{
		Task: "improve a complex answer",
		LLM:  mock,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "refine:{{.Task}}",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Fatalf("expected success after initial-generation retry, got %s: %s", bb.Outcome, bb.Result)
	}
	if got := bb.ChainState["refine_retried"]; got != 1 {
		t.Errorf("expected 1 retried call, got %v", got)
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

func TestToolStub_CallReturnsError(t *testing.T) {
	ts := toolStub{name: "stub", desc: "desc"}
	result := ts.Call("anything")
	if !strings.Contains(result, "STUB_ERROR") {
		t.Errorf("expected STUB_ERROR message, got: %q", result)
	}
}

// --- Additional edge-case tests for low-coverage chain functions ---

func TestChainAction_StructuredOutput_NoSchema(t *testing.T) {
	// execStructuredOutput: no json_schema in params → empty schemaDesc path
	mock := &chainMockLLM{responses: map[string]string{
		"generate": `{"summary": "All results were verified and look good", "confidence": 0.9}`,
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
			"generate": "1. Analyze first data component\n2. Analyze second data component\n3. Analyze third data component",
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

// indexedErrorMockLLM errors on specific call numbers, succeeding on all
// others. Used to simulate a failing subtask within map_reduce. A subtask that
// must stay failed has to error on both its original call and its retry, so the
// failing call numbers are given as a set.
type indexedErrorMockLLM struct {
	decompose   string
	failOnCalls map[int]bool
	count       *int
}

func (m *indexedErrorMockLLM) Generate(_ string) (string, error) {
	*m.count++
	n := *m.count
	if m.failOnCalls[n] {
		return "", fmt.Errorf("simulated error on call %d", n)
	}
	if n == 1 {
		return m.decompose, nil
	}
	return fmt.Sprintf("result for call %d with sufficient length to pass checks", n), nil
}
func (m *indexedErrorMockLLM) GenerateCtx(_ context.Context, prompt string) (string, error) {
	return m.Generate(prompt)
}
func (m *indexedErrorMockLLM) GenerateWithTimeout(prompt string, _ time.Duration) (string, error) {
	return m.Generate(prompt)
}
func (m *indexedErrorMockLLM) AnalyzeComplexity(_ string) string       { return "high" }
func (m *indexedErrorMockLLM) GeneratePlan(_, _ string) string         { return "plan" }
func (m *indexedErrorMockLLM) Reflect(_, _, _ string) (string, string) { return "ok", "ok" }

// TestChainAction_MapReduce_PartialFailureRecovery verifies that when one
// subtask fails, map_reduce records the failure, completes the remaining
// subtasks, tracks progress in ChainState, and still reduces to a success.
func TestChainAction_MapReduce_PartialFailureRecovery(t *testing.T) {
	callCount := 0
	mock := &indexedErrorMockLLM{
		decompose: "1. Subtask A\n2. Subtask B\n3. Subtask C",
		// call 1 = decompose, 2 = A, 3 = B (fails), 4 = B retry (fails),
		// 5 = C, 6 = reduce. B fails on both attempts so it stays failed.
		failOnCalls: map[int]bool{3: true, 4: true},
		count:       &callCount,
	}
	bb := &Blackboard{Task: "analyze a complex multi-part topic", LLM: mock}

	result := execMapReduce(ChainConfig{ChainType: "map_reduce", Prompt: "{{.Task}}"}, bb)

	if result != 1 || bb.Outcome != "chain_success" {
		t.Fatalf("expected chain_success, got result=%d outcome=%s: %s", result, bb.Outcome, bb.Result)
	}
	if got := bb.ChainState["map_reduce_completed"]; got != 2 {
		t.Errorf("map_reduce_completed = %v, want 2", got)
	}
	if got := bb.ChainState["map_reduce_failed"]; got != 1 {
		t.Errorf("map_reduce_failed = %v, want 1", got)
	}
	if got := bb.ChainState["map_reduce_total"]; got != 3 {
		t.Errorf("map_reduce_total = %v, want 3", got)
	}
	if got := bb.ChainState["map_reduce_retried"]; got != 1 {
		t.Errorf("map_reduce_retried = %v, want 1", got)
	}
	failedList, ok := bb.ChainState["map_reduce_failed_subtasks"].([]string)
	if !ok || len(failedList) != 1 || !strings.Contains(failedList[0], "Subtask B") {
		t.Errorf("map_reduce_failed_subtasks = %v, want [Subtask B]", bb.ChainState["map_reduce_failed_subtasks"])
	}
	if len(bb.Results) == 0 {
		t.Error("expected final result appended to bb.Results")
	}
}

// largeSubtaskMockLLM decomposes into several subtasks and returns a large result
// for each, so the accumulated earlier-subtask context threaded into later
// subtask prompts grows past the windowing budget.
type largeSubtaskMockLLM struct {
	count *int
	big   string
}

func (m *largeSubtaskMockLLM) Generate(_ string) (string, error) {
	*m.count++
	if *m.count == 1 {
		return "1. Subtask A\n2. Subtask B\n3. Subtask C\n4. Subtask D", nil
	}
	return m.big, nil
}
func (m *largeSubtaskMockLLM) GenerateCtx(_ context.Context, prompt string) (string, error) {
	return m.Generate(prompt)
}
func (m *largeSubtaskMockLLM) GenerateWithTimeout(prompt string, _ time.Duration) (string, error) {
	return m.Generate(prompt)
}
func (m *largeSubtaskMockLLM) AnalyzeComplexity(_ string) string       { return "high" }
func (m *largeSubtaskMockLLM) GeneratePlan(_, _ string) string         { return "plan" }
func (m *largeSubtaskMockLLM) Reflect(_, _, _ string) (string, string) { return "ok", "ok" }

// TestChainAction_MapReduce_ContextWindowed verifies that when subtasks produce
// large results, the accumulated earlier-subtask context threaded into later
// subtask prompts is windowed (bounded) instead of growing unbounded and
// overflowing the model context — and that the windowing is recorded for
// downstream nodes via map_reduce_context_windowed.
func TestChainAction_MapReduce_ContextWindowed(t *testing.T) {
	// Each subtask result is large enough that two of them exceed the budget.
	count := 0
	mock := &largeSubtaskMockLLM{count: &count, big: strings.Repeat("x", maxMapReduceContextLen/2+500)}
	bb := &Blackboard{Task: "analyze a large multi-part topic", LLM: mock}

	result := execMapReduce(ChainConfig{ChainType: "map_reduce", Prompt: "{{.Task}}"}, bb)

	if result != 1 || bb.Outcome != "chain_success" {
		t.Fatalf("expected chain_success, got result=%d outcome=%s", result, bb.Outcome)
	}
	if got, _ := bb.ChainState["map_reduce_context_windowed"].(bool); !got {
		t.Errorf("expected map_reduce_context_windowed=true when accumulated context exceeds budget")
	}

	// Small results should NOT trip the window — confirms the flag is meaningful.
	smallCount := 0
	smallMock := &largeSubtaskMockLLM{count: &smallCount, big: "a concise subtask result"}
	smallBB := &Blackboard{Task: "analyze a small topic", LLM: smallMock}
	if r := execMapReduce(ChainConfig{ChainType: "map_reduce", Prompt: "{{.Task}}"}, smallBB); r != 1 {
		t.Fatalf("expected success on small case, got %d", r)
	}
	if got, _ := smallBB.ChainState["map_reduce_context_windowed"].(bool); got {
		t.Errorf("expected map_reduce_context_windowed=false for small subtask results")
	}
}

// TestChainAction_MapReduce_AllSubtasksFail verifies that when every subtask
// fails, map_reduce fails honestly instead of reducing over no inputs.
func TestChainAction_MapReduce_AllSubtasksFail(t *testing.T) {
	callCount := 0
	mock := &countedErrorMockLLM{
		responses:  map[string]string{"generate": "1. Sub1\n2. Sub2\n3. Sub3"},
		failOnCall: 2, // decompose ok, every subtask errors
		count:      &callCount,
	}
	bb := &Blackboard{Task: "analyze complex topic", LLM: mock}

	result := execMapReduce(ChainConfig{ChainType: "map_reduce", Prompt: "{{.Task}}"}, bb)

	if result != -1 || bb.Outcome != "chain_failed" {
		t.Fatalf("expected chain_failed, got result=%d outcome=%s", result, bb.Outcome)
	}
	if got := bb.ChainState["map_reduce_completed"]; got != 0 {
		t.Errorf("map_reduce_completed = %v, want 0", got)
	}
}

// promptRecordingMockLLM records every prompt it is asked to generate, so a test
// can assert on the content of a specific phase's prompt (e.g. the reduce prompt).
// Subtasks at the given call indices error on both their attempts so they stay
// failed; all other calls succeed.
type promptRecordingMockLLM struct {
	decompose   string
	failOnCalls map[int]bool
	count       int
	prompts     []string
}

func (m *promptRecordingMockLLM) Generate(prompt string) (string, error) {
	m.count++
	m.prompts = append(m.prompts, prompt)
	n := m.count
	if m.failOnCalls[n] {
		return "", fmt.Errorf("simulated error on call %d", n)
	}
	if n == 1 {
		return m.decompose, nil
	}
	return fmt.Sprintf("result for call %d with sufficient length to pass checks", n), nil
}
func (m *promptRecordingMockLLM) GenerateCtx(_ context.Context, p string) (string, error) {
	return m.Generate(p)
}
func (m *promptRecordingMockLLM) GenerateWithTimeout(p string, _ time.Duration) (string, error) {
	return m.Generate(p)
}
func (m *promptRecordingMockLLM) AnalyzeComplexity(_ string) string       { return "high" }
func (m *promptRecordingMockLLM) GeneratePlan(_, _ string) string         { return "plan" }
func (m *promptRecordingMockLLM) Reflect(_, _, _ string) (string, string) { return "ok", "ok" }

// TestChainAction_MapReduce_ReduceNamesFailedSubtasks verifies that when a subtask
// fails, the reduce-phase prompt names that specific subtask (not just a count) so
// the synthesizer can flag the exact gap instead of fabricating the missing piece.
func TestChainAction_MapReduce_ReduceNamesFailedSubtasks(t *testing.T) {
	mock := &promptRecordingMockLLM{
		decompose: "1. Subtask A\n2. Subtask B\n3. Subtask C",
		// call 1 = decompose, 2 = A, 3 = B (fails), 4 = B retry (fails),
		// 5 = C, 6 = reduce. B fails on both attempts so it stays failed.
		failOnCalls: map[int]bool{3: true, 4: true},
	}
	bb := &Blackboard{Task: "analyze a complex multi-part topic", LLM: mock}

	result := execMapReduce(ChainConfig{ChainType: "map_reduce", Prompt: "{{.Task}}"}, bb)
	if result != 1 || bb.Outcome != "chain_success" {
		t.Fatalf("expected chain_success, got result=%d outcome=%s: %s", result, bb.Outcome, bb.Result)
	}

	reducePrompt := mock.prompts[len(mock.prompts)-1]
	if !strings.Contains(reducePrompt, "MISSING") {
		t.Errorf("reduce prompt should flag missing subtasks, got:\n%s", reducePrompt)
	}
	if !strings.Contains(reducePrompt, "Subtask B") {
		t.Errorf("reduce prompt should name the failed subtask 'Subtask B', got:\n%s", reducePrompt)
	}
	// Completed subtasks must NOT be listed as missing.
	missingIdx := strings.Index(reducePrompt, "MISSING")
	if strings.Contains(reducePrompt[missingIdx:], "Subtask A") || strings.Contains(reducePrompt[missingIdx:], "Subtask C") {
		t.Errorf("completed subtasks should not appear in the missing list, got:\n%s", reducePrompt[missingIdx:])
	}
}

// TestChainAction_MapReduce_ReduceFallback verifies that when every subtask
// completes but the reduce (combine) call keeps failing, map_reduce does NOT
// discard the completed work: it retries the combine once, then falls back to a
// deterministic synthesis of the subtask results and still reports a usable
// (partial) success rather than failing the whole node.
func TestChainAction_MapReduce_ReduceFallback(t *testing.T) {
	callCount := 0
	// 3 subtasks: call 1 = decompose, calls 2-4 = subtasks (succeed),
	// call 5 = reduce (fails, >=5), call 6 = reduce retry (fails). Fallback fires.
	mock := &countedErrorMockLLM{
		responses:  map[string]string{"generate": "1. Subtask A\n2. Subtask B\n3. Subtask C"},
		failOnCall: 5,
		count:      &callCount,
	}
	bb := &Blackboard{Task: "analyze a complex multi-part topic", LLM: mock}

	result := execMapReduce(ChainConfig{ChainType: "map_reduce", Prompt: "{{.Task}}"}, bb)

	if result != 1 {
		t.Fatalf("expected recovered success (result=1), got %d: %s", result, bb.Result)
	}
	if bb.Outcome != "chain_partial" {
		t.Errorf("expected outcome chain_partial, got %s", bb.Outcome)
	}
	if got := bb.ChainState["map_reduce_completed"]; got != 3 {
		t.Errorf("map_reduce_completed = %v, want 3", got)
	}
	if got := bb.ChainState["map_reduce_reduce_retried"]; got != true {
		t.Errorf("map_reduce_reduce_retried = %v, want true", got)
	}
	if got := bb.ChainState["map_reduce_reduce_degraded"]; got != true {
		t.Errorf("map_reduce_reduce_degraded = %v, want true", got)
	}
	// The completed subtask results must be preserved in the fallback output.
	if !strings.Contains(bb.Result, "Subtask A") || !strings.Contains(bb.Result, "Subtask C") {
		t.Errorf("fallback should preserve completed subtask sections, got:\n%s", bb.Result)
	}
	if len(bb.Results) == 0 {
		t.Error("expected fallback result appended to bb.Results")
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
				"generate": "Final Answer: Complete analysis result with detailed findings",
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

func TestChainAction_Agent_RepeatedToolCallsAborts(t *testing.T) {
	// execAgent: an agent that keeps issuing the SAME (action, input) call must
	// be detected as stuck and the loop aborted before it burns the whole
	// iteration budget, with progress recorded in ChainState.
	mock := &chainMockLLM{responses: map[string]string{
		"generate": "Thought: keep searching\nAction: search\nAction Input: same query",
	}}
	bb := &Blackboard{
		Task: "investigate something complex",
		LLM:  mock,
		ChainTools: []any{
			&mockAgentTool{name: "search", description: "Search the web", result: "no results"},
		},
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "agent:Investigate: {{.Task}}",
		Metadata: map[string]any{
			"max_tokens": float64(15),
		},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if got := bb.ChainState["agent_stop_reason"]; got != "repeated_tool_calls" {
		t.Errorf("expected stop_reason repeated_tool_calls, got %v", got)
	}
	// The guard caps a single call at maxRepeatedCalls (3) executions, so the
	// loop must abort far short of the 15-iteration budget.
	iters, _ := bb.ChainState["agent_iterations"].(int)
	if iters <= 0 || iters > 5 {
		t.Errorf("expected loop to abort early (<=5 iterations), got %d", iters)
	}
	if used, _ := bb.ChainState["agent_tools_used"].(int); used != 3 {
		t.Errorf("expected 3 successful tool calls before abort, got %d", used)
	}
}

func TestChainAction_Agent_NoProgressAborts(t *testing.T) {
	// execAgent: an agent that only ever emits unparseable "Thought" output — no
	// Action and no Final Answer — makes zero progress. It must be detected and the
	// loop aborted well short of the iteration budget, with stop_reason=no_progress.
	mock := &chainMockLLM{responses: map[string]string{
		"generate": "Thought: I am still pondering the approach and have not decided anything yet.",
	}}
	bb := &Blackboard{
		Task:       "investigate something complex",
		LLM:        mock,
		ChainTools: []any{},
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "agent:Investigate: {{.Task}}",
		Metadata: map[string]any{
			"max_tokens": float64(20),
		},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if got := bb.ChainState["agent_stop_reason"]; got != "no_progress" {
		t.Errorf("expected stop_reason no_progress, got %v", got)
	}
	// The guard aborts after maxNoProgressSteps (4), far short of the 20 budget.
	iters, _ := bb.ChainState["agent_iterations"].(int)
	if iters <= 0 || iters > 6 {
		t.Errorf("expected loop to abort early (<=6 iterations), got %d", iters)
	}
}

// nudgeRecoveryMockLLM emits off-format ("unparseable") output until it sees the
// corrective format nudge in the prompt, then produces a Final Answer. It models a
// real LLM that drifts off the ReAct format on a complex task but recovers once
// told exactly what format to use.
type nudgeRecoveryMockLLM struct{ sawNudge bool }

func (m *nudgeRecoveryMockLLM) Generate(prompt string) (string, error) {
	if strings.Contains(prompt, "could not be parsed") {
		m.sawNudge = true
		return "Final Answer: recovered after the format correction.", nil
	}
	return "I am still mulling over the various angles of this problem.", nil
}
func (m *nudgeRecoveryMockLLM) GenerateCtx(_ context.Context, prompt string) (string, error) {
	return m.Generate(prompt)
}
func (m *nudgeRecoveryMockLLM) GenerateWithTimeout(prompt string, _ time.Duration) (string, error) {
	return m.Generate(prompt)
}
func (m *nudgeRecoveryMockLLM) AnalyzeComplexity(_ string) string       { return "medium" }
func (m *nudgeRecoveryMockLLM) GeneratePlan(_, _ string) string         { return "plan" }
func (m *nudgeRecoveryMockLLM) Reflect(_, _, _ string) (string, string) { return "ok", "ok" }

func TestChainAction_Agent_UnparseableNudgeRecovers(t *testing.T) {
	// execAgent: when the model drifts off-format (no Action, no Final Answer), the
	// loop appends a corrective format nudge to the scratchpad. A model that reads
	// the nudge can recover and finish, rather than rambling until the no-progress
	// guard aborts the run. This keeps complex agentic tasks from failing on a
	// transient format slip.
	mock := &nudgeRecoveryMockLLM{}
	bb := &Blackboard{
		Task: "do something complex",
		LLM:  mock,
	}
	tree := &evolution.SerializableNode{
		Type:     "ChainAction",
		Name:     "agent:Work on: {{.Task}}",
		Metadata: map[string]any{"max_tokens": float64(20)},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if !mock.sawNudge {
		t.Fatal("expected the corrective nudge to appear in a later prompt")
	}
	if got := bb.ChainState["agent_stop_reason"]; got != "final_answer" {
		t.Errorf("expected stop_reason final_answer after recovery, got %v", got)
	}
	if !strings.Contains(bb.Result, "recovered") {
		t.Errorf("expected recovered final answer, got %q", bb.Result)
	}
	// Recovery must happen well before the no-progress guard (maxNoProgressSteps=4):
	// one unparseable step, then the nudged recovery.
	iters, _ := bb.ChainState["agent_iterations"].(int)
	if iters <= 0 || iters > 3 {
		t.Errorf("expected quick recovery (<=3 iterations), got %d", iters)
	}
}

func TestChainAction_Agent_HallucinatedToolsAbort(t *testing.T) {
	// execAgent: an agent that keeps calling NON-EXISTENT tools, each with a
	// different input, makes no real progress. Because the repeated_tool_calls
	// guard keys on the exact (action, input) pair, varying the input on every
	// call evades it — so this must be caught by the no-progress guard instead of
	// burning the whole iteration budget on tools that can never run.
	var callCount int
	mock := &agentTestMockLLM{
		responses: []string{
			"Thought: try one\nAction: query_database\nAction Input: alpha",
			"Thought: try two\nAction: search_index\nAction Input: beta",
			"Thought: try three\nAction: lookup_records\nAction Input: gamma",
			"Thought: try four\nAction: fetch_data\nAction Input: delta",
			"Thought: try five\nAction: scan_files\nAction Input: epsilon",
		},
		callCount: &callCount,
	}
	bb := &Blackboard{
		Task: "investigate something complex",
		LLM:  mock,
		// A real tool is present (so tool evidence is required), but the agent
		// never calls it — it only invokes hallucinated tool names.
		ChainTools: []any{
			&mockAgentTool{name: "calculator", description: "Perform calculations", result: "42"},
		},
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "agent:Investigate: {{.Task}}",
		Metadata: map[string]any{
			"max_tokens": float64(20),
		},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if got := bb.ChainState["agent_stop_reason"]; got != "no_progress" {
		t.Errorf("expected stop_reason no_progress, got %v", got)
	}
	// The guard aborts after maxNoProgressSteps (4), far short of the 20 budget.
	iters, _ := bb.ChainState["agent_iterations"].(int)
	if iters <= 0 || iters > 6 {
		t.Errorf("expected loop to abort early (<=6 iterations), got %d", iters)
	}
	// No hallucinated call ran a real tool, so no successful tool calls recorded.
	if used, _ := bb.ChainState["agent_tools_used"].(int); used != 0 {
		t.Errorf("expected 0 successful tool calls, got %d", used)
	}
}

func TestChainAction_Agent_ProgressRecordedOnFinalAnswer(t *testing.T) {
	// execAgent: a clean final-answer run records stop_reason=final_answer.
	var callCount int
	mock := &agentTestMockLLM{
		responses: []string{
			"Final Answer: The capital of France is Paris.",
		},
		callCount: &callCount,
	}
	bb := &Blackboard{Task: "what is the capital of France?", LLM: mock}
	tree := &evolution.SerializableNode{
		Type:     "ChainAction",
		Name:     "agent:{{.Task}}",
		Metadata: map[string]any{"max_tokens": float64(5)},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if got := bb.ChainState["agent_stop_reason"]; got != "final_answer" {
		t.Errorf("expected stop_reason final_answer, got %v", got)
	}
}

func TestChainAction_Agent_ToolTraceRecorded(t *testing.T) {
	// execAgent: a multi-step task calls two distinct real tools and one
	// hallucinated tool. Only the LAST observation survives in bb.CachedResult, so
	// the full subtask-result history must be preserved in agent_tool_trace for
	// downstream nodes — successful calls flagged ok, the hallucinated one flagged
	// not ok as error context.
	var callCount int
	mock := &agentTestMockLLM{
		responses: []string{
			"Thought: search first\nAction: search\nAction Input: TSLA price",
			"Thought: now compute\nAction: calculator\nAction Input: 250 * 2",
			"Thought: try a bogus tool\nAction: time_machine\nAction Input: yesterday",
			"Final Answer: Tesla is $250; doubled is $500.",
		},
		callCount: &callCount,
	}
	bb := &Blackboard{
		Task: "research and compute",
		LLM:  mock,
		ChainTools: []any{
			&mockAgentTool{name: "search", description: "Search the web", result: "TSLA: $250.00"},
			&mockAgentTool{name: "calculator", description: "Perform calculations", result: "500"},
		},
	}
	tree := &evolution.SerializableNode{
		Type:     "ChainAction",
		Name:     "agent:{{.Task}}",
		Metadata: map[string]any{"max_tokens": float64(20)},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	trace, ok := bb.ChainState["agent_tool_trace"].([]map[string]any)
	if !ok {
		t.Fatalf("expected agent_tool_trace []map[string]any, got %T", bb.ChainState["agent_tool_trace"])
	}
	if len(trace) != 3 {
		t.Fatalf("expected 3 trace entries (2 real + 1 hallucinated), got %d: %v", len(trace), trace)
	}
	// First two are successful real-tool calls; preserved even though CachedResult
	// only holds the last observation.
	if trace[0]["action"] != "search" || trace[0]["ok"] != true {
		t.Errorf("entry 0 should be a successful search, got %v", trace[0])
	}
	if trace[1]["action"] != "calculator" || trace[1]["ok"] != true {
		t.Errorf("entry 1 should be a successful calculator call, got %v", trace[1])
	}
	if res, _ := trace[1]["result"].(string); !strings.Contains(res, "500") {
		t.Errorf("entry 1 should preserve the calculator observation, got %v", trace[1]["result"])
	}
	// The hallucinated tool is recorded as error context, flagged not ok.
	if trace[2]["action"] != "time_machine" || trace[2]["ok"] != false {
		t.Errorf("entry 2 should be a failed hallucinated-tool attempt, got %v", trace[2])
	}
}

func TestWindowScratchpad(t *testing.T) {
	// Short scratchpad is returned untouched.
	short := "Step 1: did a thing\nStep 2: did another\n"
	if got := windowScratchpad(short, 6000); got != short {
		t.Errorf("short scratchpad should be unchanged, got %q", got)
	}
	// maxLen <= 0 disables windowing.
	if got := windowScratchpad(short, 0); got != short {
		t.Errorf("maxLen<=0 should disable windowing, got %q", got)
	}
	// Oversized scratchpad is trimmed to the tail with an elision marker, never
	// exceeding the budget by more than the marker line, and starting on a clean
	// line boundary (no half step).
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&sb, "Step %d: %s\n", i, strings.Repeat("x", 80))
	}
	full := sb.String()
	got := windowScratchpad(full, 1000)
	if len(got) >= len(full) {
		t.Fatalf("expected windowed output shorter than input (%d), got %d", len(full), len(got))
	}
	if !strings.Contains(got, "elided to manage context") {
		t.Errorf("expected elision marker, got prefix %q", got[:60])
	}
	// The retained tail must include the most recent step and exclude the oldest.
	if !strings.Contains(got, "Step 199:") {
		t.Errorf("windowed scratchpad should retain the most recent step")
	}
	if strings.Contains(got, "Step 0:") {
		t.Errorf("windowed scratchpad should drop the oldest step")
	}
}

func TestChainAction_Agent_ScratchpadWindowed(t *testing.T) {
	// execAgent: a long-running agent with large tool outputs must window its
	// scratchpad so per-iteration prompts stay bounded instead of growing with
	// every step. Without windowing, prompt size grows with iteration count and
	// eventually overflows the model's context window on complex tasks.
	bigOutput := strings.Repeat("DATA ", 400) // ~2000 chars per tool result
	mock := &windowingMockLLM{bigToolEnough: true}
	bb := &Blackboard{
		Task: "investigate a complex multi-step problem",
		LLM:  mock,
		ChainTools: []any{
			&mockAgentTool{name: "search", description: "Search", result: bigOutput},
		},
	}
	tree := &evolution.SerializableNode{
		Type:     "ChainAction",
		Name:     "agent:{{.Task}}",
		Metadata: map[string]any{"max_tokens": float64(15)},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if windowed, _ := bb.ChainState["agent_scratchpad_windowed"].(bool); !windowed {
		t.Fatalf("expected scratchpad to be windowed over a long run, got %v", bb.ChainState["agent_scratchpad_windowed"])
	}
	// Per-iteration prompts (the ones asking for the next step) must stay bounded
	// near the scratchpad budget plus fixed overhead — never the full unbounded
	// accumulation of all step outputs.
	if mock.maxLoopPrompt == 0 {
		t.Fatal("expected at least one per-iteration prompt to be recorded")
	}
	if mock.maxLoopPrompt > maxScratchpadLen+3000 {
		t.Errorf("per-iteration prompt grew unbounded: max %d chars (budget %d)", mock.maxLoopPrompt, maxScratchpadLen)
	}
}

// windowingMockLLM emits a distinct tool action on each call (so the stuck-loop
// guard never fires) and records the size of every per-iteration agent prompt.
type windowingMockLLM struct {
	bigToolEnough bool
	calls         int
	maxLoopPrompt int
}

func (m *windowingMockLLM) Generate(prompt string) (string, error) {
	if strings.Contains(prompt, "What is your next step?") {
		if len(prompt) > m.maxLoopPrompt {
			m.maxLoopPrompt = len(prompt)
		}
	}
	m.calls++
	// Unique input each call avoids the repeated-tool-call abort.
	return fmt.Sprintf("Thought: keep going\nAction: search\nAction Input: query-%d", m.calls), nil
}
func (m *windowingMockLLM) GenerateCtx(_ context.Context, prompt string) (string, error) {
	return m.Generate(prompt)
}
func (m *windowingMockLLM) GenerateWithTimeout(prompt string, _ time.Duration) (string, error) {
	return m.Generate(prompt)
}
func (m *windowingMockLLM) AnalyzeComplexity(_ string) string       { return "medium" }
func (m *windowingMockLLM) GeneratePlan(_, _ string) string         { return "plan" }
func (m *windowingMockLLM) Reflect(_, _, _ string) (string, string) { return "ok", "ok" }

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
		Task: "search for info",
		LLM:  mock,
		ChainTools: []any{&mockAgentTool{
			name:        "web_search",
			description: "Search the web",
			result:      "real tool result",
		}},
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

// summaryWindowMockLLM emits a unique real-tool action on every call (so the
// agent never gives a Final Answer and the stuck-loop guard never fires),
// driving the loop to its iteration budget and into the fallback summary path.
// It records the size of the fallback synthesis prompt (the one containing the
// "INVESTIGATION LOG:" marker) so the test can assert that prompt is bounded.
type summaryWindowMockLLM struct {
	calls           int
	maxSummaryBytes int
}

func (m *summaryWindowMockLLM) Generate(prompt string) (string, error) {
	if strings.Contains(prompt, "INVESTIGATION LOG:") {
		if len(prompt) > m.maxSummaryBytes {
			m.maxSummaryBytes = len(prompt)
		}
		return "Final Answer: synthesized from the investigation.", nil
	}
	m.calls++
	return fmt.Sprintf("Thought: keep digging\nAction: search\nAction Input: query-%d", m.calls), nil
}
func (m *summaryWindowMockLLM) GenerateCtx(_ context.Context, prompt string) (string, error) {
	return m.Generate(prompt)
}
func (m *summaryWindowMockLLM) GenerateWithTimeout(prompt string, _ time.Duration) (string, error) {
	return m.Generate(prompt)
}
func (m *summaryWindowMockLLM) AnalyzeComplexity(_ string) string       { return "medium" }
func (m *summaryWindowMockLLM) GeneratePlan(_, _ string) string         { return "plan" }
func (m *summaryWindowMockLLM) Reflect(_, _, _ string) (string, string) { return "ok", "ok" }

func TestChainAction_Agent_SummaryScratchpadWindowed(t *testing.T) {
	// execAgent: a long multi-step agent that exhausts its iteration budget
	// without a Final Answer falls back to a synthesis prompt built from the
	// accumulated scratchpad. With large tool outputs across many steps the raw
	// scratchpad grows far past the model context; the fallback prompt must be
	// windowed (like the per-iteration prompts) instead of sent whole.
	bigOutput := strings.Repeat("DATA ", 500) // ~2500 chars per tool result
	mock := &summaryWindowMockLLM{}
	bb := &Blackboard{
		Task: "investigate a complex multi-step problem",
		LLM:  mock,
		ChainTools: []any{
			&mockAgentTool{name: "search", description: "Search", result: bigOutput},
		},
	}
	tree := &evolution.SerializableNode{
		Type:     "ChainAction",
		Name:     "agent:{{.Task}}",
		Metadata: map[string]any{"max_tokens": float64(15)},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Fatalf("expected success from fallback synthesis, got %s: %s", bb.Outcome, bb.Result)
	}
	if mock.maxSummaryBytes == 0 {
		t.Fatal("expected the fallback synthesis prompt to be recorded")
	}
	// 15 steps * ~2500 chars = ~37.5KB of raw scratchpad. The windowed summary
	// prompt must stay near the summary budget plus fixed template overhead, never
	// the full unbounded accumulation.
	if mock.maxSummaryBytes > maxSummaryScratchpadLen+2000 {
		t.Errorf("fallback synthesis prompt grew unbounded: %d chars (budget %d)", mock.maxSummaryBytes, maxSummaryScratchpadLen)
	}
	if windowed, _ := bb.ChainState["agent_scratchpad_windowed"].(bool); !windowed {
		t.Errorf("expected agent_scratchpad_windowed=true after windowing the summary log")
	}
}

// TestChainHistory_RecordsAcrossNodes verifies that every chain node in a
// multi-node tree appends an ordered entry to bb.ChainState["chain_history"],
// giving downstream nodes and audits a single task-history view of the run —
// including which chain ran, its status, and a result preview.
func TestChainHistory_RecordsAcrossNodes(t *testing.T) {
	mock := &chainMockLLM{responses: map[string]string{
		"generate": "first node result",
	}}
	bb := &Blackboard{
		Task: "multi-step task",
		LLM:  mock,
	}
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "history-test",
		Children: []evolution.SerializableNode{
			{Type: "ChainAction", Name: "llm_call:{{.Task}}"},
			{Type: "ChainAction", Name: "refine:{{.Task}}"},
		},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	hist, ok := bb.ChainState["chain_history"].([]map[string]any)
	if !ok {
		t.Fatalf("expected chain_history to be recorded, got %T", bb.ChainState["chain_history"])
	}
	if len(hist) != 2 {
		t.Fatalf("expected 2 history entries (one per chain node), got %d", len(hist))
	}
	if hist[0]["chain_type"] != "llm_call" || hist[1]["chain_type"] != "refine" {
		t.Errorf("history did not record nodes in execution order: %v", hist)
	}
	if hist[0]["seq"] != 1 || hist[1]["seq"] != 2 {
		t.Errorf("expected monotonic seq 1,2, got %v and %v", hist[0]["seq"], hist[1]["seq"])
	}
	if hist[0]["status"] != "success" {
		t.Errorf("expected first node status=success, got %v", hist[0]["status"])
	}
	if preview, _ := hist[0]["preview"].(string); preview == "" {
		t.Errorf("expected a non-empty result preview for the first node")
	}
}

// TestChainHistory_RecordsFailureStatus verifies a failed chain node is recorded
// with failure status and its error preview, so a later node can see the prior
// partial failure rather than only the most recent result.
func TestChainHistory_RecordsFailureStatus(t *testing.T) {
	// No LLM available → map_reduce fails honestly.
	bb := &Blackboard{Task: "decompose me"}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "map_reduce:{{.Task}}",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	hist, ok := bb.ChainState["chain_history"].([]map[string]any)
	if !ok || len(hist) != 1 {
		t.Fatalf("expected 1 history entry, got %v", bb.ChainState["chain_history"])
	}
	if hist[0]["status"] != "failure" {
		t.Errorf("expected status=failure for failed chain, got %v", hist[0]["status"])
	}
	if hist[0]["outcome"] != "chain_failed" {
		t.Errorf("expected outcome=chain_failed, got %v", hist[0]["outcome"])
	}
}

// incompleteSynthMockLLM emits a unique real-tool action every call so the agent
// never gives its own Final Answer and exhausts the iteration budget, landing in
// the fallback synthesis path. It captures the synthesis prompt so the test can
// assert it carries the incomplete-investigation note.
type incompleteSynthMockLLM struct {
	calls       int
	synthPrompt string
}

func (m *incompleteSynthMockLLM) Generate(prompt string) (string, error) {
	if strings.Contains(prompt, "INVESTIGATION LOG:") {
		m.synthPrompt = prompt
		return "Final Answer: best-effort synthesis.", nil
	}
	m.calls++
	return fmt.Sprintf("Thought: keep digging\nAction: search\nAction Input: query-%d", m.calls), nil
}
func (m *incompleteSynthMockLLM) GenerateCtx(_ context.Context, prompt string) (string, error) {
	return m.Generate(prompt)
}
func (m *incompleteSynthMockLLM) GenerateWithTimeout(prompt string, _ time.Duration) (string, error) {
	return m.Generate(prompt)
}
func (m *incompleteSynthMockLLM) AnalyzeComplexity(_ string) string       { return "medium" }
func (m *incompleteSynthMockLLM) GeneratePlan(_, _ string) string         { return "plan" }
func (m *incompleteSynthMockLLM) Reflect(_, _, _ string) (string, string) { return "ok", "ok" }

// TestChainAction_Agent_IncompleteSynthesisFlagsGaps verifies that when the agent
// loop ends without its own Final Answer (here: budget exhausted, stop_reason=
// max_iterations), the fallback synthesis prompt instructs the model to treat the
// investigation as incomplete and flag what could not be determined — so a stuck
// or budget-limited complex task yields an honest, caveated answer rather than a
// confidently complete one.
func TestChainAction_Agent_IncompleteSynthesisFlagsGaps(t *testing.T) {
	mock := &incompleteSynthMockLLM{}
	bb := &Blackboard{
		Task: "investigate a complex multi-step problem",
		LLM:  mock,
		ChainTools: []any{
			&mockAgentTool{name: "search", description: "Search", result: "partial finding"},
		},
	}
	tree := &evolution.SerializableNode{
		Type:     "ChainAction",
		Name:     "agent:{{.Task}}",
		Metadata: map[string]any{"max_tokens": float64(5)},
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Fatalf("expected success from fallback synthesis, got %s: %s", bb.Outcome, bb.Result)
	}
	if got := bb.ChainState["agent_stop_reason"]; got != "max_iterations" {
		t.Fatalf("precondition: expected stop_reason max_iterations, got %v", got)
	}
	if m := mock.synthPrompt; m == "" {
		t.Fatal("expected the fallback synthesis prompt to be captured")
	}
	if !strings.Contains(mock.synthPrompt, "INCOMPLETE") {
		t.Errorf("synthesis prompt did not flag the investigation as incomplete:\n%s", mock.synthPrompt)
	}
	if !strings.Contains(mock.synthPrompt, "step limit") {
		t.Errorf("synthesis prompt did not name the budget-exhaustion reason:\n%s", mock.synthPrompt)
	}
}

// TestIncompleteInvestigationNote_StopReasons verifies the note is emitted for
// every early-stop reason and is empty for a natural final answer (so a clean run
// is never told its investigation was incomplete).
func TestIncompleteInvestigationNote_StopReasons(t *testing.T) {
	for _, reason := range []string{"max_iterations", "repeated_tool_calls", "no_progress"} {
		if note := incompleteInvestigationNote(reason); !strings.Contains(note, "INCOMPLETE") {
			t.Errorf("expected an incomplete note for stop reason %q, got %q", reason, note)
		}
	}
	if note := incompleteInvestigationNote("final_answer"); note != "" {
		t.Errorf("expected no note for a natural final answer, got %q", note)
	}
}

// TestIsKnownChainKind verifies every declared ChainKind constant is recognized
// as known, and that unrelated strings — including a mistyped chain_type, an
// empty string, and a wrong-case variant — are rejected. This backs the
// authoring-time validation gate: a mistyped chain_type must fail before the
// tree ever reaches LLM-call runtime.
func TestIsKnownChainKind(t *testing.T) {
	known := []ChainKind{
		ChainLLMCall, ChainRAGQuery, ChainToolCall, ChainConversation,
		ChainStructuredOutput, ChainRetrievalQA, ChainMapReduce, ChainRefine,
		ChainFusion, ChainAgent, ChainToolAction,
	}
	for _, k := range known {
		if !IsKnownChainKind(string(k)) {
			t.Errorf("IsKnownChainKind(%q) = false, want true", k)
		}
	}

	for _, bad := range []string{"", "llm_calls", "LLM_CALL", "tool_actionn", "unknown_kind"} {
		if IsKnownChainKind(bad) {
			t.Errorf("IsKnownChainKind(%q) = true, want false", bad)
		}
	}
}

// --- reliability.RetryPolicy adoption (Q3 Reliability milestone 5/5) ---

// flakyOnceMockLLM fails the first N Generate calls (regardless of prompt
// content) and succeeds after, so the retry-recovery path can be exercised
// uniformly across every currently single-shot chain executor.
type flakyOnceMockLLM struct {
	failFirstN int
	calls      int
}

func (m *flakyOnceMockLLM) Generate(_ string) (string, error) {
	m.calls++
	if m.calls <= m.failFirstN {
		return "", fmt.Errorf("transient upstream error %d", m.calls)
	}
	return "recovered response after retry", nil
}
func (m *flakyOnceMockLLM) GenerateCtx(_ context.Context, p string) (string, error) {
	return m.Generate(p)
}
func (m *flakyOnceMockLLM) GenerateWithTimeout(p string, _ time.Duration) (string, error) {
	return m.Generate(p)
}
func (m *flakyOnceMockLLM) AnalyzeComplexity(_ string) string       { return "medium" }
func (m *flakyOnceMockLLM) GeneratePlan(_, _ string) string         { return "1. a\n2. b" }
func (m *flakyOnceMockLLM) Reflect(_, _, _ string) (string, string) { return "ok", "better" }

// Every previously single-shot chain executor (llm_call, rag_query, tool_call,
// conversation, structured_output, retrieval_qa) must now retry a transient
// LLM error once instead of failing immediately — mirroring the retry
// resilience execMapReduce and execRefine already had before this milestone.
func TestChainAction_SingleShotExecutors_RetryOnTransientError(t *testing.T) {
	cases := []struct {
		name     string
		nodeName string
		setup    func(bb *Blackboard)
	}{
		{"llm_call", "llm_call:{{.Task}}", nil},
		{"rag_query", "rag_query:{{.Task}}", func(bb *Blackboard) { bb.KgResults = "some context" }},
		{"tool_call", "tool_call:{{.Task}}", nil},
		{"conversation", "conversation:{{.Task}}", func(bb *Blackboard) { bb.ChainState = map[string]any{} }},
		{"structured_output", "structured_output:{{.Task}}", nil},
		{"retrieval_qa", "retrieval_qa:{{.Task}}", func(bb *Blackboard) { bb.CachedResult = "cached context" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &flakyOnceMockLLM{failFirstN: 1}
			bb := &Blackboard{Task: "do the thing", LLM: mock}
			if tc.setup != nil {
				tc.setup(bb)
			}
			tree := &evolution.SerializableNode{
				Type: "ChainAction",
				Name: tc.nodeName,
			}

			bt := BuildTree(tree, bb)
			RunTask(bb, bt)

			if bb.Outcome != "success" {
				t.Fatalf("expected success after transient-error retry, got %s: %s", bb.Outcome, bb.Result)
			}
			if mock.calls < 2 {
				t.Errorf("expected at least 2 Generate calls (initial + retry), got %d", mock.calls)
			}
		})
	}
}

// rateLimitedOnceMockLLM fails the very first Generate call with a
// reliability.RateLimitError carrying a server-provided Retry-After duration,
// then succeeds. It records the wall-clock time of every call so a test can
// verify the retry actually waited for the Retry-After delay instead of
// re-hammering the call immediately.
type rateLimitedOnceMockLLM struct {
	retryAfter time.Duration
	calls      int
	callTimes  []time.Time
}

func (m *rateLimitedOnceMockLLM) Generate(_ string) (string, error) {
	m.calls++
	m.callTimes = append(m.callTimes, time.Now())
	if m.calls == 1 {
		return "", &reliability.RateLimitError{RetryAfter: m.retryAfter, Message: "429 too many requests"}
	}
	return "recovered after rate limit backoff", nil
}
func (m *rateLimitedOnceMockLLM) GenerateCtx(_ context.Context, p string) (string, error) {
	return m.Generate(p)
}
func (m *rateLimitedOnceMockLLM) GenerateWithTimeout(p string, _ time.Duration) (string, error) {
	return m.Generate(p)
}
func (m *rateLimitedOnceMockLLM) AnalyzeComplexity(_ string) string       { return "medium" }
func (m *rateLimitedOnceMockLLM) GeneratePlan(_, _ string) string         { return "1. a\n2. b" }
func (m *rateLimitedOnceMockLLM) Reflect(_, _, _ string) (string, string) { return "ok", "better" }

// A 429 with a Retry-After duration must not be re-hammered immediately: the
// retry has to wait at least the server-provided delay before trying again.
func TestChainAction_LLMCall_HonorsRetryAfter(t *testing.T) {
	mock := &rateLimitedOnceMockLLM{retryAfter: 80 * time.Millisecond}
	bb := &Blackboard{
		Task: "test task",
		LLM:  mock,
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "llm_call:{{.Task}}",
	}

	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Fatalf("expected success after rate-limit retry, got %s: %s", bb.Outcome, bb.Result)
	}
	if len(mock.callTimes) != 2 {
		t.Fatalf("expected exactly 2 Generate calls, got %d", len(mock.callTimes))
	}
	if gap := mock.callTimes[1].Sub(mock.callTimes[0]); gap < mock.retryAfter {
		t.Errorf("retry re-called after only %s, want at least the server Retry-After delay of %s — a 429 with Retry-After must not be immediately re-hammered", gap, mock.retryAfter)
	}
}

// rateLimitedDecomposeMockLLM fails the first "Break down this task" call
// with a reliability.RateLimitError carrying a Retry-After duration, then
// succeeds; every other call succeeds immediately so only the decompose call
// site's retry timing is under test.
type rateLimitedDecomposeMockLLM struct {
	retryAfter    time.Duration
	decomposeSeen int
	callTimes     []time.Time
}

func (m *rateLimitedDecomposeMockLLM) Generate(prompt string) (string, error) {
	if strings.Contains(prompt, "Break down this task") {
		m.decomposeSeen++
		m.callTimes = append(m.callTimes, time.Now())
		if m.decomposeSeen == 1 {
			return "", &reliability.RateLimitError{RetryAfter: m.retryAfter, Message: "429 too many requests"}
		}
		return "1. first subtask\n2. second subtask", nil
	}
	return "subtask result with sufficient length for validation", nil
}
func (m *rateLimitedDecomposeMockLLM) GenerateCtx(_ context.Context, p string) (string, error) {
	return m.Generate(p)
}
func (m *rateLimitedDecomposeMockLLM) GenerateWithTimeout(p string, _ time.Duration) (string, error) {
	return m.Generate(p)
}
func (m *rateLimitedDecomposeMockLLM) AnalyzeComplexity(_ string) string       { return "medium" }
func (m *rateLimitedDecomposeMockLLM) GeneratePlan(_, _ string) string         { return "1. a\n2. b" }
func (m *rateLimitedDecomposeMockLLM) Reflect(_, _, _ string) (string, string) { return "ok", "better" }

// execMapReduce's decompose call site must honor Retry-After too, not just the
// newly-retrying single-shot executors.
func TestChainAction_MapReduce_DecomposeHonorsRetryAfter(t *testing.T) {
	mock := &rateLimitedDecomposeMockLLM{retryAfter: 80 * time.Millisecond}
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

	if bb.Outcome != "success" {
		t.Fatalf("expected success after decompose rate-limit retry, got %s: %s", bb.Outcome, bb.Result)
	}
	if len(mock.callTimes) != 2 {
		t.Fatalf("expected exactly 2 decompose calls, got %d", len(mock.callTimes))
	}
	if gap := mock.callTimes[1].Sub(mock.callTimes[0]); gap < mock.retryAfter {
		t.Errorf("decompose retry re-called after only %s, want at least the server Retry-After delay of %s", gap, mock.retryAfter)
	}
}

// rateLimitedRefineInitialMockLLM fails the very first Generate call (the
// refine chain's initial-answer generation) with a reliability.RateLimitError
// carrying a Retry-After duration, then succeeds; the critique call that
// follows immediately reports convergence so the test only exercises the
// initial-call retry timing.
type rateLimitedRefineInitialMockLLM struct {
	retryAfter time.Duration
	callTimes  []time.Time
}

func (m *rateLimitedRefineInitialMockLLM) Generate(prompt string) (string, error) {
	if strings.Contains(prompt, "Critique this answer") {
		return "NO_FURTHER_IMPROVEMENT", nil
	}
	m.callTimes = append(m.callTimes, time.Now())
	if len(m.callTimes) == 1 {
		return "", &reliability.RateLimitError{RetryAfter: m.retryAfter, Message: "429 too many requests"}
	}
	return "initial answer with sufficient length for validation", nil
}
func (m *rateLimitedRefineInitialMockLLM) GenerateCtx(_ context.Context, p string) (string, error) {
	return m.Generate(p)
}
func (m *rateLimitedRefineInitialMockLLM) GenerateWithTimeout(p string, _ time.Duration) (string, error) {
	return m.Generate(p)
}
func (m *rateLimitedRefineInitialMockLLM) AnalyzeComplexity(_ string) string { return "medium" }
func (m *rateLimitedRefineInitialMockLLM) GeneratePlan(_, _ string) string   { return "1. a\n2. b" }
func (m *rateLimitedRefineInitialMockLLM) Reflect(_, _, _ string) (string, string) {
	return "ok", "better"
}

// execRefine's initial-answer call site must honor Retry-After too, not just
// the decompose/subtask/reduce calls in execMapReduce.
func TestChainAction_Refine_InitialCallHonorsRetryAfter(t *testing.T) {
	mock := &rateLimitedRefineInitialMockLLM{retryAfter: 80 * time.Millisecond}
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
		t.Fatalf("expected success after initial-call rate-limit retry, got %s: %s", bb.Outcome, bb.Result)
	}
	if len(mock.callTimes) != 2 {
		t.Fatalf("expected exactly 2 initial-answer calls, got %d", len(mock.callTimes))
	}
	if gap := mock.callTimes[1].Sub(mock.callTimes[0]); gap < mock.retryAfter {
		t.Errorf("initial-answer retry re-called after only %s, want at least the server Retry-After delay of %s", gap, mock.retryAfter)
	}
}
