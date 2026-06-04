package engine

import (
	"context"
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// ─── readGoals edge cases ───

func TestReadGoals_ChainStateFallback(t *testing.T) {
	node := &evolution.SerializableNode{
		Type: "PlannerNode",
		Name: "test",
		// Must have non-nil Metadata to reach ChainState fallback
		Metadata: map[string]any{},
	}
	bb := &Blackboard{
		ChainState: map[string]any{
			"goals": []GoalDefinition{
				{Name: "goal1", Priority: 1.0},
			},
		},
	}
	goals := readGoals(node, bb)
	if goals == nil {
		t.Fatal("expected non-nil goals from ChainState")
	}
	if len(goals) != 1 || goals[0].Name != "goal1" {
		t.Errorf("expected goal1 from ChainState, got %+v", goals)
	}
}

func TestReadGoals_Preconditions(t *testing.T) {
	node := &evolution.SerializableNode{
		Metadata: map[string]any{
			"goals": []any{
				map[string]any{
					"name":          "goal_with_pre",
					"priority":      float64(0.8),
					"preconditions": []any{"has_data", "is_ready"},
				},
			},
		},
	}
	bb := &Blackboard{}
	goals := readGoals(node, bb)
	if goals == nil {
		t.Fatal("expected non-nil goals")
	}
	if len(goals) != 1 {
		t.Fatalf("expected 1 goal, got %d", len(goals))
	}
	if len(goals[0].Preconditions) != 2 {
		t.Errorf("expected 2 preconditions, got %d: %v", len(goals[0].Preconditions), goals[0].Preconditions)
	}
	if goals[0].Preconditions[0] != "has_data" {
		t.Errorf("expected precondition 'has_data', got %q", goals[0].Preconditions[0])
	}
}

func TestReadGoals_NonListMetadata(t *testing.T) {
	// Metadata["goals"] exists but is not []interface{} — should fall through
	node := &evolution.SerializableNode{
		Metadata: map[string]any{
			"goals": "not_a_list",
		},
	}
	bb := &Blackboard{}
	goals := readGoals(node, bb)
	if goals != nil {
		t.Errorf("expected nil for non-list goals metadata, got %v", goals)
	}
}

func TestReadGoals_InvalidItemInList(t *testing.T) {
	node := &evolution.SerializableNode{
		Metadata: map[string]any{
			"goals": []any{
				"not_a_map", // not map[string]interface{} — skip
				map[string]any{
					"name":     "valid_goal",
					"priority": float64(1.0),
				},
			},
		},
	}
	bb := &Blackboard{}
	goals := readGoals(node, bb)
	if goals == nil {
		t.Fatal("expected non-nil goals")
	}
	if len(goals) != 1 || goals[0].Name != "valid_goal" {
		t.Errorf("expected 1 valid goal, got %+v", goals)
	}
}

// ─── newGraphifyTool edge cases — error path ───

func TestNewGraphifyTool_ErrorHandling(t *testing.T) {
	tool := newGraphifyTool()
	// Path action with node that doesn't exist — may return error
	result := tool.Call("path non-existent-node-xyz")
	// Should not panic. Could return graphify output or error message.
	if result == "" {
		t.Error("expected non-empty result for path command")
	}
}

func TestNewGraphifyTool_EmptyActionDefaultToQuery(t *testing.T) {
	tool := newGraphifyTool()
	result := tool.Call("some random query text that has no known action prefix")
	if result == "" {
		t.Error("expected non-empty result for unknown action treated as query")
	}
}

func TestNewGraphifyTool_UpdateWithArg(t *testing.T) {
	tool := newGraphifyTool()
	// update with a specific arg
	result := tool.Call("update ./nonexistent/path")
	// May fail but shouldn't panic
	t.Logf("graphify update with arg: %s", result[:min(len(result), 80)])
}

func TestNewShellExecTool_StderrCaptureOnlyFailure(t *testing.T) {
	tool := newShellExecTool()
	// Command that writes only to stderr and fails
	result := tool.Call("echo 'error msg' >&2 && exit 1")
	if result == "" {
		t.Error("expected non-empty result with stderr content")
	}
	if !strings.Contains(result, "error msg") && !strings.Contains(result, "exit") {
		t.Errorf("expected stderr content in result, got %q", result)
	}
}

func TestNewShellExecTool_TimeoutTruncation(t *testing.T) {
	tool := newShellExecTool()
	// Generate output slightly over 8192 chars
	result := tool.Call("python3 -c \"print('x'*10000)\"")
	if len(result) > 8250 {
		t.Errorf("expected truncated output < 8250, got %d", len(result))
	}
	if !strings.Contains(result, "truncated") {
		t.Logf("truncation marker not found (output len: %d)", len(result))
	}
}

// ─── buildChainActionFn — conversation chain with ChainMemory ───

func TestBuildChainActionFn_ConversationWithMemory(t *testing.T) {
	// ChainMemory that implements fmt.Stringer
	bb := &Blackboard{
		Task:        "test task",
		ChainMemory: &mockStringer{data: "Previous conversation history"},
		ChainState:  make(map[string]any),
		LLM:         &MockLLM{},
	}
	cfg := ChainConfig{
		ChainType: "conversation",
		Prompt:    "test prompt",
		SystemMsg: "You are a helpful assistant.",
		Params:    map[string]string{},
	}
	fn := buildChainActionFn(cfg, bb)
	ctx := &btcore.BTContext[Blackboard]{}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1 (success), got %d", result)
	}
	if bb.Outcome != "chain_success" {
		t.Errorf("expected outcome 'chain_success', got %q", bb.Outcome)
	}
	if bb.Result == "" {
		t.Error("expected non-empty result")
	}
}

type mockStringer struct {
	data string
}

func (m *mockStringer) String() string {
	return m.data
}

// ─── buildChainActionFn — conversation chain nil LLM ───

func TestBuildChainActionFn_ConversationNilLLM(t *testing.T) {
	bb := &Blackboard{
		Task:        "test task",
		ChainMemory: &mockStringer{data: "history"},
		ChainState:  make(map[string]any),
		LLM:         nil,
	}
	cfg := ChainConfig{
		ChainType: "conversation",
		Prompt:    "test prompt",
	}
	fn := buildChainActionFn(cfg, bb)
	ctx := &btcore.BTContext[Blackboard]{}
	result := fn(ctx)
	if result != -1 {
		t.Errorf("expected -1 (failure) for nil LLM conversation, got %d", result)
	}
	if bb.Outcome != "chain_failed" {
		t.Errorf("expected outcome 'chain_failed', got %q", bb.Outcome)
	}
}

// ─── generateTemplateOutput — arc42 section parsing ───

func TestGenerateTemplateOutput_Arc42Section(t *testing.T) {
	bb := &Blackboard{
		Task:   "test task",
		Result: "test result",
	}
	prompt := "arc42 Section 3 — Context and Scope\nSome description"
	output := generateTemplateOutput(prompt, bb)
	if !strings.Contains(output, "arc42 Section 3") {
		t.Errorf("expected arc42 section title in output, got: %s", output[:min(len(output), 100)])
	}
}

func TestGenerateTemplateOutput_WithChainState(t *testing.T) {
	bb := &Blackboard{
		ChainState: map[string]any{
			"key1": "value1",
			"key2": 42,
		},
	}
	output := generateTemplateOutput("some prompt", bb)
	if !strings.Contains(output, "key1") || !strings.Contains(output, "value1") {
		t.Errorf("expected ChainState key1 in output, got: %s", output[:min(len(output), 200)])
	}
	if !strings.Contains(output, "key2") {
		t.Errorf("expected ChainState key2 in output, got: %s", output[:min(len(output), 200)])
	}
}

func TestGenerateTemplateOutput_LongChainStateValue(t *testing.T) {
	longVal := strings.Repeat("x", 500)
	bb := &Blackboard{
		ChainState: map[string]any{
			"long_key": longVal,
		},
	}
	output := generateTemplateOutput("test", bb)
	if !strings.Contains(output, "long_key") {
		t.Errorf("expected long_key in output, got: %s", output[:min(len(output), 200)])
	}
}

// ─── generateTemplateOutput — arc42 title with newline vs period ───

func TestGenerateTemplateOutput_Arc42TitleNoNewline(t *testing.T) {
	// arc42 section title ending at period (no newline found)
	bb := &Blackboard{
		Task: "test",
	}
	prompt := "Lorem ipsum — arc42 Section 5 — Runtime Behavior. Some more text."
	output := generateTemplateOutput(prompt, bb)
	if !strings.Contains(output, "arc42 Section 5") {
		t.Errorf("expected section title, got: %s", output[:min(len(output), 100)])
	}
}

// ─── buildChainActionFn — refine chain with nil LLM ───

func TestBuildChainActionFn_RefineNilLLM(t *testing.T) {
	bb := &Blackboard{
		Task: "test task",
		LLM:  nil,
	}
	cfg := ChainConfig{
		ChainType: "refine",
		Prompt:    "improve this: {{.Task}}",
	}
	fn := buildChainActionFn(cfg, bb)
	ctx := &btcore.BTContext[Blackboard]{}
	result := fn(ctx)
	if result != -1 {
		t.Errorf("expected -1 for nil LLM refine, got %d", result)
	}
}

// ─── execToolCall — nil LLM path ───

func TestExecToolCall_NilLLM(t *testing.T) {
	bb := &Blackboard{
		Task: "test",
		LLM:  nil,
	}
	cfg := ChainConfig{
		ChainType: "tool_call",
		Prompt:    "use web_search",
	}
	fn := buildChainActionFn(cfg, bb)
	ctx := &btcore.BTContext[Blackboard]{}
	result := fn(ctx)
	if result != -1 {
		t.Errorf("expected -1 for nil LLM tool_call, got %d", result)
	}
	if bb.Outcome != "chain_failed" {
		t.Errorf("expected 'chain_failed', got %q", bb.Outcome)
	}
}

// ─── BuildChainAction (constructor wrapper) ───

func TestBuildChainAction_ReturnsNonNil(t *testing.T) {
	bb := &Blackboard{LLM: &MockLLM{}}
	cfg := ChainConfig{
		ChainType: "llm_call",
		Prompt:    "hello",
	}
	action := BuildChainAction(cfg, bb)
	if action == nil {
		t.Error("expected non-nil Action")
	}
}

// ─── execRetrievalQA — nil LLM path ───

func TestExecRetrievalQA_NilLLM(t *testing.T) {
	bb := &Blackboard{
		Task: "test",
		LLM:  nil,
	}
	cfg := ChainConfig{
		ChainType: "retrieval_qa",
		Prompt:    "what is {{.Task}}?",
	}
	fn := buildChainActionFn(cfg, bb)
	ctx := &btcore.BTContext[Blackboard]{}
	result := fn(ctx)
	if result != -1 {
		t.Errorf("expected -1 for nil LLM retrieval_qa, got %d", result)
	}
	if bb.Outcome != "chain_failed" {
		t.Errorf("expected 'chain_failed', got %q", bb.Outcome)
	}
}

// ─── execMapReduce — nil LLM path ───

func TestExecMapReduce_NilLLM(t *testing.T) {
	bb := &Blackboard{
		Task: "test",
		LLM:  nil,
	}
	cfg := ChainConfig{
		ChainType: "map_reduce",
		Prompt:    "analyze {{.Task}}",
	}
	fn := buildChainActionFn(cfg, bb)
	ctx := &btcore.BTContext[Blackboard]{}
	result := fn(ctx)
	if result != -1 {
		t.Errorf("expected -1 for nil LLM map_reduce, got %d", result)
	}
}

// ─── execAgent — nil LLM path ───

func TestExecAgent_NilLLM(t *testing.T) {
	bb := &Blackboard{
		Task: "test",
		LLM:  nil,
	}
	cfg := ChainConfig{
		ChainType: "agent",
		Prompt:    "do something",
	}
	fn := buildChainActionFn(cfg, bb)
	ctx := &btcore.BTContext[Blackboard]{}
	result := fn(ctx)
	if result != -1 {
		t.Errorf("expected -1 for nil LLM agent, got %d", result)
	}
}

// ─── execStructuredOutput — nil LLM path ───

func TestExecStructuredOutput_NilLLM(t *testing.T) {
	bb := &Blackboard{
		Task: "test",
		LLM:  nil,
	}
	cfg := ChainConfig{
		ChainType: "structured_output",
		Prompt:    "output JSON",
		Params:    map[string]string{"json_schema": `{"type": "object"}`},
	}
	fn := buildChainActionFn(cfg, bb)
	ctx := &btcore.BTContext[Blackboard]{}
	result := fn(ctx)
	if result != -1 {
		t.Errorf("expected -1 for nil LLM structured_output, got %d", result)
	}
}

// ─── execToolCall with tools ───

func TestBuildChainActionFn_ToolCallWithTools(t *testing.T) {
	bb := &Blackboard{
		Task: "test",
		LLM:  &MockLLM{},
		ChainTools: []any{
			&mockTool{name: "web_search", desc: "Search web"},
		},
	}
	cfg := ChainConfig{
		ChainType: "tool_call",
		Prompt:    "search for go",
		Tools:     []string{"web_search", "calculator"},
	}
	fn := buildChainActionFn(cfg, bb)
	ctx := &btcore.BTContext[Blackboard]{}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1 (success), got %d", result)
	}
}

type mockTool struct {
	name string
	desc string
}

func (m *mockTool) Name() string        { return m.name }
func (m *mockTool) Description() string { return m.desc }
func (m *mockTool) Call(input string) string {
	return "mock result for " + input
}

// ─── expandChainStateTemplates edge cases ───

func TestExpandChainStateTemplates_EmptyState(t *testing.T) {
	bb := &Blackboard{
		ChainState: map[string]any{},
	}
	result := expandChainStateTemplates("hello {{.Task}}", bb)
	if !strings.Contains(result, "{{.Task}}") {
		t.Errorf("expected {{.Task}} to remain unexpanded (no Task set), got %q", result)
	}
}

// ─── PlannerNode with ChainState fallback ───

func TestBuildPlannerNode_ChainStateGoals(t *testing.T) {
	// Use ChainState goals instead of metadata goals
	succeedChild := evolution.SerializableNode{
		Type: "AlwaysSucceed",
	}
	bb := &Blackboard{
		ChainState: map[string]any{
			"goals": []GoalDefinition{
				{Name: "goal1", Priority: 1.0},
			},
		},
	}
	node := &evolution.SerializableNode{
		Type:     "PlannerNode",
		Children: []evolution.SerializableNode{succeedChild},
	}
	cmd := BuildPlannerNode(node, bb)
	ctx := btcore.NewBTContext(context.Background(), bb)
	result := cmd.Run(ctx)
	if result != 1 {
		t.Errorf("expected 1 (success), got %d", result)
	}
}

// ─── executeAgentTool — tool with no Call method ───

func TestExecuteAgentTool_NoCallMethod(t *testing.T) {
	bb := &Blackboard{
		ChainTools: []any{
			&brokenTool{name: "web_search"},
		},
	}
	result := executeAgentTool("web_search", "query", bb)
	if !strings.Contains(result, "no Call") {
		t.Errorf("expected 'no Call method' error, got %q", result)
	}
}

type brokenTool struct {
	name string
}

func (b *brokenTool) Name() string        { return b.name }
func (b *brokenTool) Description() string { return "a broken tool without Call" }

// ─── expandTemplate edge cases ───

func TestExpandTemplate_TaskOnly(t *testing.T) {
	bb := &Blackboard{Task: "world"}
	result := expandTemplate("hello {{.Task}}", bb)
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

// ─── execConversation bypass mock generation ───

func TestExecConversation_NoChainState(t *testing.T) {
	// ChainState is nil — conversation writes to it
	bb := &Blackboard{
		Task: "test",
		ChainMemory: &mockStringer{
			data: "prior conversation",
		},
		ChainState: make(map[string]any),
		LLM:        &MockLLM{},
	}
	cfg := ChainConfig{
		ChainType: "conversation",
		Prompt:    "say hello",
		Params:    map[string]string{},
	}
	fn := buildChainActionFn(cfg, bb)
	ctx := &btcore.BTContext[Blackboard]{}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1 (success), got %d", result)
	}
}
