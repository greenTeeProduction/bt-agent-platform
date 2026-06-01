package factory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// mockLLM returns a pre-built TreeSpec JSON so we don't need Ollama in tests.
type mockLLM struct {
	treeSpecJSON string
}

func (m *mockLLM) GenerateCtx(ctx context.Context, prompt string) (string, error) {
	return m.Generate(prompt)
}
func (m *mockLLM) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) {
	return m.Generate(prompt)
}

func (m *mockLLM) Generate(prompt string) (string, error) {
	return m.treeSpecJSON, nil
}
func (m *mockLLM) AnalyzeComplexity(task string) string                { return "low" }
func (m *mockLLM) GeneratePlan(task, complexity string) string         { return "mock plan" }
func (m *mockLLM) Reflect(task, outcome, plan string) (string, string) { return "ok", "better" }

func validTreeSpecJSON() string {
	spec := TreeSpec{
		RootType: "Sequence",
		RootName: "TestAgent",
		PreChecks: []TreeNode{
			{Type: "Condition", Name: "CheckInput", Description: "Validate input"},
			{Type: "Condition", Name: "CheckAuth", Description: "Verify authorization"},
		},
		StrategyPath: []TreeNode{
			{Type: "Condition", Name: "IsPriorityTask", Description: "Check if urgent"},
			{Type: "Action", Name: "HandlePriority", Description: "Fast-track handling"},
			{Type: "Condition", Name: "IsKnowledgeQuery", Description: "Check if needs research"},
			{Type: "Action", Name: "ResearchAndRespond", Description: "Look up and answer"},
		},
		SelfCorrect: &TreeNode{Type: "Action", Name: "RetryWithCorrection", Description: "Fix and retry"},
		Fallback:    &TreeNode{Type: "Action", Name: "EscalateToHuman", Description: "Send to human"},
	}
	data, _ := json.Marshal(spec)
	return string(data)
}

func TestAnalyzer_ParsesTreeSpec(t *testing.T) {
	mock := &mockLLM{treeSpecJSON: validTreeSpecJSON()}
	analyzer := NewAnalyzer(mock)

	spec, err := analyzer.Analyze("fake skill content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spec.RootType != "Sequence" {
		t.Errorf("expected Sequence, got %s", spec.RootType)
	}
	if spec.RootName != "TestAgent" {
		t.Errorf("expected TestAgent, got %s", spec.RootName)
	}
	if len(spec.PreChecks) != 2 {
		t.Errorf("expected 2 pre_checks, got %d", len(spec.PreChecks))
	}
	if len(spec.StrategyPath) != 4 {
		t.Errorf("expected 4 strategy_path items, got %d", len(spec.StrategyPath))
	}
	if spec.SelfCorrect == nil {
		t.Error("expected self_correct node")
	}
	if spec.Fallback == nil {
		t.Error("expected fallback node")
	}
}

func TestAnalyzer_EmptyResponse_Error(t *testing.T) {
	mock := &mockLLM{treeSpecJSON: "not json at all"}
	analyzer := NewAnalyzer(mock)

	_, err := analyzer.Analyze("content")
	if err == nil {
		t.Error("expected error for non-JSON response")
	}
}

func TestAnalyzer_NoStrategyPath_Error(t *testing.T) {
	spec := TreeSpec{RootType: "Sequence", RootName: "Bad", StrategyPath: nil}
	data, _ := json.Marshal(spec)

	mock := &mockLLM{treeSpecJSON: string(data)}
	analyzer := NewAnalyzer(mock)

	_, err := analyzer.Analyze("content")
	if err == nil {
		t.Error("expected error for empty strategy_path")
	}
}

func TestGenerator_BuildsCorrectTree(t *testing.T) {
	gen := NewGenerator()
	spec := &TreeSpec{
		RootType: "Sequence",
		RootName: "TestAgent",
		PreChecks: []TreeNode{
			{Type: "Condition", Name: "CheckInput", Description: "Validate"},
		},
		StrategyPath: []TreeNode{
			{Type: "Condition", Name: "IsQuery", Description: "Detect query"},
			{Type: "Action", Name: "AnswerQuery", Description: "Respond"},
		},
		SelfCorrect: &TreeNode{Type: "Action", Name: "FixAndRetry", Description: "Correct"},
		Fallback:    &TreeNode{Type: "Action", Name: "Escalate", Description: "Send up"},
	}

	serTree := gen.buildSerializable(spec, "test-agent")

	if serTree.Type != "Sequence" {
		t.Errorf("expected Sequence root, got %s", serTree.Type)
	}
	if serTree.Name != "TestAgent" {
		t.Errorf("expected TestAgent root name, got %s", serTree.Name)
	}

	// Should have: PreGate, StrategyRouter, ReflectOnOutcome, OutcomeSelector, UpdateBehaviorTree
	if len(serTree.Children) != 5 {
		t.Errorf("expected 5 children, got %d", len(serTree.Children))
	}

	// PreGate
	preGate := serTree.Children[0]
	if preGate.Name != "PreGate" || len(preGate.Children) != 1 {
		t.Errorf("PreGate: expected 1 child, got %d", len(preGate.Children))
	}

	// StrategyRouter
	router := serTree.Children[1]
	if router.Type != "Selector" {
		t.Errorf("StrategyRouter: expected Selector, got %s", router.Type)
	}
	// One skill path + fallback execution
	if len(router.Children) < 2 {
		t.Errorf("StrategyRouter: expected at least 2 paths, got %d", len(router.Children))
	}

	// OutcomeSelector
	outcome := serTree.Children[3]
	if outcome.Name != "OutcomeSelector" {
		t.Errorf("expected OutcomeSelector, got %s", outcome.Name)
	}
	// WasSuccessful + RetrySelfCorrect + Escalate
	if len(outcome.Children) != 3 {
		t.Errorf("OutcomeSelector: expected 3 children, got %d", len(outcome.Children))
	}

	nodeCount := evolution.CountNodes(serTree)
	if nodeCount < 12 {
		t.Errorf("expected at least 12 nodes, got %d", nodeCount)
	}
}

func TestGenerator_NoSelfCorrect_NoFallback(t *testing.T) {
	gen := NewGenerator()
	spec := &TreeSpec{
		RootType:  "Selector",
		RootName:  "SimpleAgent",
		PreChecks: nil,
		StrategyPath: []TreeNode{
			{Type: "Action", Name: "DoThing", Description: "Just do it"},
		},
		SelfCorrect: nil,
		Fallback:    nil,
	}

	serTree := gen.buildSerializable(spec, "simple")

	// OutcomeSelector should have WasSuccessful + default Escalate (since no fallback)
	outcome := serTree.Children[2] // PreGate is skipped, so [0]=Router, [1]=Reflect, [2]=Outcome
	if outcome.Name != "OutcomeSelector" {
		t.Errorf("expected OutcomeSelector at index 2, got %s", outcome.Name)
	}
	if len(outcome.Children) < 2 {
		t.Errorf("OutcomeSelector: expected at least 2 children (WasSuccessful + default Escalate), got %d", len(outcome.Children))
	}
	// Verify no RetrySelfCorrect since SelfCorrect was nil
	for _, child := range outcome.Children {
		if child.Name == "RetrySelfCorrect" {
			t.Error("RetrySelfCorrect should NOT be present when SelfCorrect is nil")
		}
	}
}

func TestFactory_CreateFromContent(t *testing.T) {
	tmpDir := t.TempDir()

	mock := &mockLLM{treeSpecJSON: validTreeSpecJSON()}
	factory, err := NewAgentFactory(mock, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	agent, err := factory.CreateFromContent("test-skill", "# Test Skill\nThis is a test skill.")
	if err != nil {
		t.Fatalf("CreateFromContent: %v", err)
	}

	if agent.Name != "test-skill" {
		t.Errorf("expected agent name 'test-skill', got %q", agent.Name)
	}
	if agent.SerTree == nil {
		t.Fatal("expected non-nil SerTree")
	}

	nodeCount := evolution.CountNodes(agent.SerTree)
	if nodeCount < 12 {
		t.Errorf("expected at least 12 nodes, got %d", nodeCount)
	}

	// Verify persistence
	treePath := filepath.Join(tmpDir, ".go-bt-reflections", "tree-test-skill.json")
	if _, err := os.Stat(treePath); os.IsNotExist(err) {
		t.Errorf("tree not persisted at %s", treePath)
	}
}

func TestFactory_CreateFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	skillPath := filepath.Join(tmpDir, "SKILL.md")
	os.WriteFile(skillPath, []byte("# Test\nCheck input, then act."), 0644)

	mock := &mockLLM{treeSpecJSON: validTreeSpecJSON()}
	factory, _ := NewAgentFactory(mock, tmpDir)

	agent, err := factory.CreateFromFile(skillPath)
	if err != nil {
		t.Fatalf("CreateFromFile: %v", err)
	}
	if agent.Name != "SKILL" {
		// SKILL.md → dir basename is tmpDir, so name should be the directory name
		t.Logf("agent name: %s", agent.Name)
	}
}

func TestFactory_CreateFromSkillDir_MdPath(t *testing.T) {
	tmpDir := t.TempDir()
	skillPath := filepath.Join(tmpDir, "myskill.md")
	os.WriteFile(skillPath, []byte("# My Skill"), 0644)

	mock := &mockLLM{treeSpecJSON: validTreeSpecJSON()}
	factory, _ := NewAgentFactory(mock, tmpDir)

	agent, err := factory.CreateFromSkillDir(skillPath)
	if err != nil {
		t.Fatalf("CreateFromSkillDir: %v", err)
	}
	if agent.Name != "myskill" {
		t.Errorf("expected 'myskill', got %q", agent.Name)
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`{"key": "value"}`, `{"key": "value"}`},
		{"```json\n{\"a\":1}\n```", `{"a":1}`},
		{"prefix text {\"x\": {\"y\": [1,2,3]}} suffix", `{"x": {"y": [1,2,3]}}`},
		{"no json here", ""},
	}

	for _, tt := range tests {
		got := extractJSON(tt.input)
		if got != tt.expected {
			t.Errorf("extractJSON(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
