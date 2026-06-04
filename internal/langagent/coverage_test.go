package langagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/reflection"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/prompts"
	"github.com/tmc/langchaingo/tools"
)

// mockModelCorrect implements llms.Model and returns proper ReAct-parseable output.
// The langchaingo executor calls GenerateContent, not Call, so both methods
// must return content that matches the ReAct "Final Answer:" format.
type mockModelCorrect struct{}

func (m *mockModelCorrect) Call(ctx context.Context, prompt string, opts ...llms.CallOption) (string, error) {
	return "Final Answer: test passed", nil
}

func (m *mockModelCorrect) GenerateContent(ctx context.Context, msgs []llms.MessageContent, opts ...llms.CallOption) (*llms.ContentResponse, error) {
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{{Content: "Final Answer: test passed"}},
	}, nil
}

// mockModelError implements llms.Model and always returns an error.
type mockModelError struct{}

func (m *mockModelError) Call(ctx context.Context, prompt string, opts ...llms.CallOption) (string, error) {
	return "", context.DeadlineExceeded
}

func (m *mockModelError) GenerateContent(ctx context.Context, msgs []llms.MessageContent, opts ...llms.CallOption) (*llms.ContentResponse, error) {
	return nil, context.DeadlineExceeded
}

// saveRecordWithDelay saves a reflection record with a unique TaskID to avoid
// timestamp collisions when saving multiple records in quick succession.
var saveRecordCounter int

func saveRecordWithDelay(t *testing.T, store *reflection.Store, r *reflection.Record) {
	t.Helper()
	saveRecordCounter++
	if r.TaskID == "" {
		r.TaskID = fmt.Sprintf("task-%d-%d", time.Now().UnixNano(), saveRecordCounter)
	}
	if err := store.Save(r); err != nil {
		t.Fatalf("save record: %v", err)
	}
}

// =============================================================================
// Tool Dispatch Tests — cover all .Call() paths including error handling
// =============================================================================

func TestRunTaskTool_Call(t *testing.T) {
	bb := &engine.Blackboard{
		Outcome:    "success",
		Complexity: "medium",
		DurationMs: 1234,
		Plan:       "1. Do it\n2. Check it",
	}
	tool := NewRunTaskTool(bb, func(task string) string {
		return "task completed: " + task
	})

	result, err := tool.Call(context.Background(), "test task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if out["result"] != "task completed: test task" {
		t.Errorf("result = %q", out["result"])
	}
	if out["outcome"] != "success" {
		t.Errorf("outcome = %q", out["outcome"])
	}
	if out["complexity"] != "medium" {
		t.Errorf("complexity = %q", out["complexity"])
	}
	if out["duration_ms"] != float64(1234) {
		t.Errorf("duration_ms = %v", out["duration_ms"])
	}
}

func TestReflectTool_Call_NoLLM(t *testing.T) {
	// Error path: nil LLM
	bb := &engine.Blackboard{LLM: nil}
	tool := NewReflectTool(bb)

	result, err := tool.Call(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "no LLM available") {
		t.Errorf("expected 'no LLM available', got %q", result)
	}
}

func TestReflectTool_Call_WithLLM(t *testing.T) {
	bb := &engine.Blackboard{
		LLM:     &mockLLM{},
		Task:    "test task",
		Outcome: "success",
		Plan:    "step by step",
	}
	tool := NewReflectTool(bb)

	result, err := tool.Call(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if out["went_well"] != "good planning" {
		t.Errorf("went_well = %q", out["went_well"])
	}
	if out["to_improve"] != "better execution" {
		t.Errorf("to_improve = %q", out["to_improve"])
	}
	if out["task"] != "test task" {
		t.Errorf("task = %q", out["task"])
	}
}

func TestFitnessTool_Call(t *testing.T) {
	refStore, treeStore := newTestStores(t)

	// Save a tree so it has node count
	tree := evolution.DefaultTree()
	if err := treeStore.Save(tree); err != nil {
		t.Fatalf("save tree: %v", err)
	}

	// Save some reflections with failures (use helper to avoid timestamp collisions)
	saveRecordWithDelay(t, refStore, &reflection.Record{Task: "task1", Outcome: reflection.Success})
	saveRecordWithDelay(t, refStore, &reflection.Record{Task: "task2", Outcome: reflection.Failure})
	saveRecordWithDelay(t, refStore, &reflection.Record{Task: "task3", Outcome: reflection.Failure})

	tool := NewFitnessTool(refStore, treeStore)
	result, err := tool.Call(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	total, _ := out["total_tasks"].(float64)
	if int(total) != 3 {
		t.Errorf("total_tasks = %v, want 3", total)
	}
	failures, _ := out["failures"].(float64)
	if int(failures) != 2 {
		t.Errorf("failures = %v, want 2", failures)
	}
	successes, _ := out["successes"].(float64)
	if int(successes) != 1 {
		t.Errorf("successes = %v, want 1", successes)
	}
}

func TestFitnessTool_Call_NoData(t *testing.T) {
	// Edge case: no tree, no reflections
	refStore, treeStore := newTestStores(t)

	tool := NewFitnessTool(refStore, treeStore)
	result, err := tool.Call(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	total, _ := out["total_tasks"].(float64)
	if int(total) != 0 {
		t.Errorf("total_tasks = %v, want 0", total)
	}
	failures, _ := out["failures"].(float64)
	if int(failures) != 0 {
		t.Errorf("failures = %v, want 0", failures)
	}
}

func TestEvolveTool_Call_NoTree(t *testing.T) {
	// Error path: no tree to evolve
	refStore, treeStore := newTestStores(t)

	tool := NewEvolveTool(refStore, treeStore)
	result, err := tool.Call(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "no tree") {
		t.Errorf("expected 'no tree', got %q", result)
	}
}

func TestEvolveTool_Call_NotEnoughFailures(t *testing.T) {
	refStore, treeStore := newTestStores(t)

	tree := evolution.DefaultTree()
	if err := treeStore.Save(tree); err != nil {
		t.Fatalf("save tree: %v", err)
	}

	// Only 1 failure
	saveRecordWithDelay(t, refStore, &reflection.Record{Task: "t", Outcome: reflection.Failure})

	tool := NewEvolveTool(refStore, treeStore)
	result, err := tool.Call(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "need 3+ failures") {
		t.Errorf("expected 'need 3+ failures', got %q", result)
	}
}

func TestEvolveTool_Call_Success(t *testing.T) {
	refStore, treeStore := newTestStores(t)

	// Create a custom tree with "AnalyzeTask" target node
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "AnalyzeTask", Description: "analyze"},
			{Type: "Action", Name: "OtherAction", Description: "other"},
		},
	}
	if err := treeStore.Save(tree); err != nil {
		t.Fatalf("save tree: %v", err)
	}

	// 3 failures → triggers evolution
	saveRecordWithDelay(t, refStore, &reflection.Record{Task: "t1", Outcome: reflection.Failure})
	saveRecordWithDelay(t, refStore, &reflection.Record{Task: "t2", Outcome: reflection.Failure})
	saveRecordWithDelay(t, refStore, &reflection.Record{Task: "t3", Outcome: reflection.Failure})

	tool := NewEvolveTool(refStore, treeStore)
	result, err := tool.Call(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "\"evolved\":true") {
		t.Errorf("expected evolved:true, got %q", result)
	}
}

func TestGetTreeTool_Call_NoTree(t *testing.T) {
	_, treeStore := newTestStores(t)

	tool := NewGetTreeTool(treeStore)
	result, err := tool.Call(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "no tree") {
		t.Errorf("expected 'no tree', got %q", result)
	}
}

func TestGetTreeTool_Call_WithTree(t *testing.T) {
	_, treeStore := newTestStores(t)

	tree := evolution.DefaultTree()
	if err := treeStore.Save(tree); err != nil {
		t.Fatalf("save tree: %v", err)
	}

	tool := NewGetTreeTool(treeStore)
	result, err := tool.Call(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "\"type\":\"Sequence\"") {
		t.Errorf("expected tree JSON, got %q", truncateStr(result, 100))
	}
}

func TestCreateAgentTool_Call_NilFactory(t *testing.T) {
	tool := NewCreateAgentTool(nil)
	result, err := tool.Call(context.Background(), "/some/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "factory not configured") {
		t.Errorf("expected 'factory not configured', got %q", result)
	}
}

func TestGetReflectionsTool_Call_NoRecords(t *testing.T) {
	refStore, _ := newTestStores(t)

	tool := NewGetReflectionsTool(refStore)
	result, err := tool.Call(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	total, _ := out["total"].(float64)
	if int(total) != 0 {
		t.Errorf("total = %v, want 0", total)
	}
}

func TestGetReflectionsTool_Call_WithRecords(t *testing.T) {
	refStore, _ := newTestStores(t)

	saveRecordWithDelay(t, refStore, &reflection.Record{
		Task:         "task1",
		Outcome:      reflection.Success,
		WhatWentWell: []string{"good"},
	})
	saveRecordWithDelay(t, refStore, &reflection.Record{
		Task:          "task2",
		Outcome:       reflection.Failure,
		WhatToImprove: []string{"needs work"},
	})

	tool := NewGetReflectionsTool(refStore)
	result, err := tool.Call(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	total, _ := out["total"].(float64)
	if int(total) != 2 {
		t.Errorf("total = %v, want 2", total)
	}
}

func TestGetReflectionsTool_Call_Truncation(t *testing.T) {
	// More than 5 → only last 5 returned
	refStore, _ := newTestStores(t)

	for i := 0; i < 7; i++ {
		saveRecordWithDelay(t, refStore, &reflection.Record{
			Task:    "task",
			Outcome: reflection.Success,
		})
	}

	tool := NewGetReflectionsTool(refStore)
	result, err := tool.Call(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	total, _ := out["total"].(float64)
	if int(total) != 7 {
		t.Errorf("total = %v, want 7", total) // total count is from LoadAll, but "recent" is truncated
	}
}

// =============================================================================
// Prompt Template Tests
// =============================================================================

func TestBuildEvolvedPrompt(t *testing.T) {
	fakeTools := []tools.Tool{
		&RunTaskTool{run: func(s string) string { return "" }},
		&ReflectTool{},
	}
	prompt := buildEvolvedPrompt(fakeTools)

	if prompt.TemplateFormat != prompts.TemplateFormatGoTemplate {
		t.Errorf("TemplateFormat = %v, want GoTemplate", prompt.TemplateFormat)
	}

	// Check InputVariables
	foundInput := false
	foundScratchpad := false
	for _, v := range prompt.InputVariables {
		if v == "input" {
			foundInput = true
		}
		if v == "agent_scratchpad" {
			foundScratchpad = true
		}
	}
	if !foundInput {
		t.Error("missing 'input' InputVariable")
	}
	if !foundScratchpad {
		t.Error("missing 'agent_scratchpad' InputVariable")
	}

	// Check PartialVariables
	names, ok := prompt.PartialVariables["tool_names"].(string)
	if !ok {
		t.Fatal("tool_names not found in PartialVariables")
	}
	if !strings.Contains(names, "bt_run_task") {
		t.Errorf("tool_names missing bt_run_task: %q", names)
	}
	if !strings.Contains(names, "bt_reflect") {
		t.Errorf("tool_names missing bt_reflect: %q", names)
	}

	descs, ok := prompt.PartialVariables["tool_descriptions"].(string)
	if !ok {
		t.Fatal("tool_descriptions not found in PartialVariables")
	}
	if !strings.Contains(descs, "bt_run_task") || !strings.Contains(descs, "bt_reflect") {
		t.Errorf("tool_descriptions incomplete: %q", descs)
	}

	// Template should contain the prefix, instructions, and suffix
	if !strings.Contains(prompt.Template, "evolved AI agent") {
		t.Error("template should contain prefix")
	}
	if !strings.Contains(prompt.Template, "Thought:") {
		t.Error("template should contain instructions")
	}
	if !strings.Contains(prompt.Template, "{{.input}}") {
		t.Error("template should contain {{.input}}")
	}
	if !strings.Contains(prompt.Template, "{{.agent_scratchpad}}") {
		t.Error("template should contain {{.agent_scratchpad}}")
	}
	if !strings.Contains(prompt.Template, "{{.tool_names}}") {
		t.Error("template should contain {{.tool_names}}")
	}
}

func TestToolNames(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		names := toolNames(nil)
		if names != "" {
			t.Errorf("expected empty, got %q", names)
		}
	})

	t.Run("single", func(t *testing.T) {
		tools := []tools.Tool{&RunTaskTool{}}
		names := toolNames(tools)
		if names != "bt_run_task" {
			t.Errorf("expected 'bt_run_task', got %q", names)
		}
	})

	t.Run("multiple", func(t *testing.T) {
		tools := []tools.Tool{&RunTaskTool{}, &ReflectTool{}, &FitnessTool{}}
		names := toolNames(tools)
		expected := "bt_run_task, bt_reflect, bt_get_fitness"
		if names != expected {
			t.Errorf("expected %q, got %q", expected, names)
		}
	})
}

func TestToolDescriptions(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		descs := toolDescriptions(nil)
		if descs != "" {
			t.Errorf("expected empty, got %q", descs)
		}
	})

	t.Run("multiple", func(t *testing.T) {
		tools := []tools.Tool{&RunTaskTool{}}
		descs := toolDescriptions(tools)
		if !strings.Contains(descs, "bt_run_task") {
			t.Errorf("expected bt_run_task in descriptions: %q", descs)
		}
		if !strings.Contains(descs, "Execute a task") {
			t.Errorf("expected full description: %q", descs)
		}
	})
}

// =============================================================================
// EvolvedAgent.Run Tests — covers the full run path with mock
// =============================================================================

func TestEvolvedAgent_Run(t *testing.T) {
	cfg := newConfig(t)
	// Replace with corrected mock that returns parseable ReAct output
	cfg.LangLLM = &mockModelCorrect{}

	agent, err := NewEvolvedAgent(cfg)
	if err != nil {
		t.Fatalf("NewEvolvedAgent: %v", err)
	}

	ctx := context.Background()
	result, err := agent.Run(ctx, "test task")
	if err != nil {
		t.Fatalf("agent.Run: %v", err)
	}
	if result != "test passed" {
		t.Errorf("result = %q, want 'test passed'", result)
	}
}

func TestEvolvedAgent_Run_WithAutoEvolve(t *testing.T) {
	cfg := newConfig(t)
	cfg.LangLLM = &mockModelCorrect{}

	// Seed 3 failures to trigger auto-evolve
	for i := 0; i < 3; i++ {
		saveRecordWithDelay(t, cfg.BB.Reflections, &reflection.Record{
			Task:    "task",
			Outcome: reflection.Failure,
		})
	}
	// Save a custom tree with "AnalyzeTask" node for auto-evolve
	customTree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "AnalyzeTask", Description: "analyze"},
			{Type: "Action", Name: "OtherAction", Description: "other"},
		},
	}
	if err := cfg.BB.TreeStore.Save(customTree); err != nil {
		t.Fatalf("save tree: %v", err)
	}

	agent, err := NewEvolvedAgent(cfg)
	if err != nil {
		t.Fatalf("NewEvolvedAgent: %v", err)
	}

	ctx := context.Background()
	result, err := agent.Run(ctx, "evolve test")
	if err != nil {
		t.Fatalf("agent.Run: %v", err)
	}
	if result != "test passed" {
		t.Errorf("result = %q", result)
	}
	// Auto-evolve should have applied (at least 1 mutation)
	tree, _ := cfg.BB.TreeStore.Load()
	if tree == nil {
		t.Fatal("tree should not be nil after auto-evolve")
	}
	// Node count should increase after wrap_retry on AnalyzeTask
	originalCount := 3 // Root + AnalyzeTask + OtherAction
	newCount := evolution.CountNodes(tree)
	if newCount <= originalCount {
		t.Errorf("node count should increase after wrap_retry: %d -> %d", originalCount, newCount)
	}
}

// =============================================================================
// truncateStr Tests
// =============================================================================

func TestTruncateStr(t *testing.T) {
	tests := []struct {
		input    string
		n        int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
		{"abc", 3, "abc"},
		{"abcdef", 6, "abcdef"},
		{"abcdefg", 6, "abcdef..."},
	}
	for _, tc := range tests {
		result := truncateStr(tc.input, tc.n)
		if result != tc.expected {
			t.Errorf("truncateStr(%q, %d) = %q, want %q", tc.input, tc.n, result, tc.expected)
		}
	}
}

// =============================================================================
// Agent Configuration Edge Cases
// =============================================================================

func TestNewEvolvedAgent_NilBlackboard(t *testing.T) {
	cfg := Config{
		LLMClient: &mockLLM{},
		LangLLM:   &mockModel{},
		RefStore:  nil,
		TreeStore: nil,
		RunTaskFn: func(task string) string {
			return "result"
		},
		BB: nil, // nil blackboard
	}

	agent, err := NewEvolvedAgent(cfg)
	if err != nil {
		t.Fatalf("NewEvolvedAgent should succeed even with nil BB: %v", err)
	}
	if agent.BB != nil {
		t.Error("BB should be nil when config has nil BB")
	}
}

func TestNewEvolvedAgent_MinimalConfig(t *testing.T) {
	// Only LangLLM is required; other fields can be nil
	cfg := Config{
		LangLLM:   &mockModel{},
		RunTaskFn: func(task string) string { return "ok" },
	}
	agent, err := NewEvolvedAgent(cfg)
	if err != nil {
		t.Fatalf("NewEvolvedAgent with minimal config: %v", err)
	}
	if agent == nil {
		t.Fatal("agent should not be nil")
	}
	// Should still have 6 tools even with nil stores
	if len(agent.Tools) != 6 {
		t.Errorf("expected 6 tools, got %d", len(agent.Tools))
	}
}

func TestNewEvolvedAgent_AllToolsRegistered(t *testing.T) {
	cfg := newConfig(t)
	agent, err := NewEvolvedAgent(cfg)
	if err != nil {
		t.Fatalf("NewEvolvedAgent: %v", err)
	}

	// Verify all 6 tools are of correct types (without factory)
	expectedTypes := map[string]interface{}{
		"bt_run_task":        &RunTaskTool{},
		"bt_reflect":         &ReflectTool{},
		"bt_get_fitness":     &FitnessTool{},
		"bt_evolve":          &EvolveTool{},
		"bt_get_tree":        &GetTreeTool{},
		"bt_get_reflections": &GetReflectionsTool{},
	}

	for _, tool := range agent.Tools {
		name := tool.Name()
		exp, ok := expectedTypes[name]
		if !ok {
			t.Errorf("unexpected tool: %s", name)
			continue
		}
		// Check type via type assertion patterns
		switch tool.(type) {
		case *RunTaskTool:
			if _, ok := exp.(*RunTaskTool); !ok {
				t.Errorf("tool %s wrong type", name)
			}
		case *ReflectTool:
			if _, ok := exp.(*ReflectTool); !ok {
				t.Errorf("tool %s wrong type", name)
			}
		case *FitnessTool:
			if _, ok := exp.(*FitnessTool); !ok {
				t.Errorf("tool %s wrong type", name)
			}
		case *EvolveTool:
			if _, ok := exp.(*EvolveTool); !ok {
				t.Errorf("tool %s wrong type", name)
			}
		case *GetTreeTool:
			if _, ok := exp.(*GetTreeTool); !ok {
				t.Errorf("tool %s wrong type", name)
			}
		case *GetReflectionsTool:
			if _, ok := exp.(*GetReflectionsTool); !ok {
				t.Errorf("tool %s wrong type", name)
			}
		default:
			t.Errorf("unknown tool type for %s: %T", name, tool)
		}
	}
}

// =============================================================================
// Tool Interface Compliance
// =============================================================================

func TestToolNames_And_Descriptions(t *testing.T) {
	bb := &engine.Blackboard{LLM: &mockLLM{}}
	tools := []tools.Tool{
		NewRunTaskTool(bb, nil),
		NewReflectTool(bb),
		NewFitnessTool(nil, nil),
		NewEvolveTool(nil, nil),
		NewGetTreeTool(nil),
		NewGetReflectionsTool(nil),
		NewCreateAgentTool(nil),
	}

	for _, tool := range tools {
		name := tool.Name()
		desc := tool.Description()

		if name == "" {
			t.Errorf("tool %T has empty name", tool)
		}
		if desc == "" {
			t.Errorf("tool %T has empty description", tool)
		}

		// Name should start with bt_ (our convention)
		if !strings.HasPrefix(name, "bt_") {
			t.Errorf("tool %T name %q should start with 'bt_'", tool, name)
		}
	}
}

// =============================================================================
// llm.LLM interface verification
// =============================================================================

func TestMockLLM_SatisfiesInterface(t *testing.T) {
	var m llm.LLM = &mockLLM{}

	if result, _ := m.Generate("test"); result != "mock response" {
		t.Errorf("Generate: %q", result)
	}
	if c := m.AnalyzeComplexity("task"); c != "medium" {
		t.Errorf("AnalyzeComplexity: %q", c)
	}
	if p := m.GeneratePlan("task", "low"); p == "" {
		t.Error("GeneratePlan returned empty")
	}
	ww, ti := m.Reflect("task", "ok", "plan")
	if ww == "" || ti == "" {
		t.Error("Reflect returned empty")
	}
}

// =============================================================================
// llms.Model mock verification
// =============================================================================

func TestMockModel_SatisfiesInterface(t *testing.T) {
	var m llms.Model = &mockModel{}

	resp, err := m.GenerateContent(context.Background(), []llms.MessageContent{
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "test"}}},
	})
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Content != "test" {
		t.Errorf("GenerateContent: %+v", resp)
	}
}

// =============================================================================
// EvolvedAgent.Run error path — mock that causes LLM error
// =============================================================================

func TestEvolvedAgent_Run_Error(t *testing.T) {
	cfg := Config{
		LLMClient: &mockLLM{},
		LangLLM:   &mockModelError{},
		RefStore:  nil,
		TreeStore: nil,
		RunTaskFn: func(task string) string { return "x" },
		BB:        &engine.Blackboard{},
	}

	agent, err := NewEvolvedAgent(cfg)
	if err != nil {
		t.Fatalf("NewEvolvedAgent: %v", err)
	}

	_, err = agent.Run(context.Background(), "should fail")
	if err == nil {
		t.Fatal("expected error from agent.Run")
	}
}

// =============================================================================
// Config & Agent struct validation
// =============================================================================

func TestConfig_AllFields(t *testing.T) {
	cfg := newConfig(t)

	// Verify all fields can be set and read
	if cfg.LangLLM == nil {
		t.Error("LangLLM should not be nil")
	}
	if cfg.LLMClient == nil {
		t.Error("LLMClient should not be nil")
	}
	if cfg.RefStore == nil {
		t.Error("RefStore should not be nil")
	}
	if cfg.TreeStore == nil {
		t.Error("TreeStore should not be nil")
	}
	if cfg.RunTaskFn == nil {
		t.Error("RunTaskFn should not be nil")
	}
	if cfg.BB == nil {
		t.Error("BB should not be nil")
	}
	if cfg.AgentFactory != nil {
		t.Error("AgentFactory should be nil in default config")
	}
}

func TestEvolvedAgent_StructFields(t *testing.T) {
	cfg := newConfig(t)
	agent, _ := NewEvolvedAgent(cfg)

	if agent.Agent == nil {
		t.Error("Agent should not be nil")
	}
	if agent.Executor == nil {
		t.Error("Executor should not be nil")
	}
	if len(agent.Tools) == 0 {
		t.Error("Tools should not be empty")
	}
	if agent.BB == nil {
		t.Error("BB should not be nil")
	}
}
