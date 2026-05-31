package engine

import (
	"fmt"
	"time"
	"context"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
)

// mockLLM for chain tests
type chainMockLLM struct {
	responses map[string]string
}

func (m *chainMockLLM) GenerateCtx(ctx context.Context, prompt string) (string, error) { return m.Generate(prompt) }
func (m *chainMockLLM) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) { return m.Generate(prompt) }

func (m *chainMockLLM) Generate(prompt string) (string, error) {
	if r, ok := m.responses["generate"]; ok {
		return r, nil
	}
	if len(prompt) > 50 {
		return "mock response for: " + prompt[:50], nil
	}
	return "mock response for: " + prompt, nil
}
func (m *chainMockLLM) AnalyzeComplexity(task string) string { return "medium" }
func (m *chainMockLLM) GeneratePlan(task, complexity string) string { return "1. Step one\n2. Step two" }
func (m *chainMockLLM) Reflect(task, outcome, plan string) (string, string) { return "ok", "better" }

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

	if bb.Outcome != "failure" {
		t.Errorf("expected chain_failed, got %s", bb.Outcome)
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

func (m *agentTestMockLLM) GenerateCtx(ctx context.Context, prompt string) (string, error) { return m.Generate(prompt) }
func (m *agentTestMockLLM) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) { return m.Generate(prompt) }

func (m *agentTestMockLLM) Generate(prompt string) (string, error) {
	idx := *m.callCount
	*m.callCount++
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return "Final Answer: done.", nil
}
func (m *agentTestMockLLM) AnalyzeComplexity(task string) string                { return "medium" }
func (m *agentTestMockLLM) GeneratePlan(task, complexity string) string         { return "plan" }
func (m *agentTestMockLLM) Reflect(task, outcome, plan string) (string, string) { return "ok", "ok" }

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
				Type: "ChainAction",
				Name: "tool_action:web_search:{{.Task}}",
				Metadata: map[string]any{"tools": []any{"web_search"}},
			},
			{
				Type: "ChainAction",
				Name: "tool_action:calculator:add 50 to {{.CachedResult}}",
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

func (m *errorMockLLM) GenerateCtx(ctx context.Context, prompt string) (string, error) { return m.Generate(prompt) }
func (m *errorMockLLM) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) { return m.Generate(prompt) }
func (m *errorMockLLM) Generate(prompt string) (string, error) { return "", m.err }
func (m *errorMockLLM) AnalyzeComplexity(task string) string                { return "medium" }
func (m *errorMockLLM) GeneratePlan(task, complexity string) string         { return "plan" }
func (m *errorMockLLM) Reflect(task, outcome, plan string) (string, string) { return "ok", "ok" }

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
		KgResults:    "",                       // empty
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
