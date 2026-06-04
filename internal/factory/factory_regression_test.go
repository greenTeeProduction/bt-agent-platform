package factory

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
)

type regressionMockLLM struct{ resp string }

func (m regressionMockLLM) Generate(_ string) (string, error) { return m.resp, nil }
func (m regressionMockLLM) GenerateCtx(_ context.Context, _ string) (string, error) {
	return m.resp, nil
}
func (m regressionMockLLM) GenerateWithTimeout(_ string, _ time.Duration) (string, error) {
	return m.resp, nil
}
func (m regressionMockLLM) AnalyzeComplexity(_ string) string { return "low" }
func (m regressionMockLLM) GeneratePlan(_, _ string) string {
	return "plan"
}
func (m regressionMockLLM) Reflect(_, _, _ string) (string, string) {
	return "ok", "none"
}

func TestGenerator_CompilesSkillActionsToChainActions(t *testing.T) {
	gen := NewGenerator()
	spec := &TreeSpec{
		RootType: "Sequence",
		RootName: "GeneratedSkillAgent",
		PreChecks: []TreeNode{
			{Type: "Condition", Name: "CheckRepositoryClean", Description: "Repository must be ready"},
		},
		StrategyPath: []TreeNode{
			{Type: "Condition", Name: "IsCodeReview", Description: "Task asks for review"},
			{Type: "Action", Name: "ReviewCode", Description: "Review code for issues"},
		},
		SelfCorrect: &TreeNode{Type: "Action", Name: "FixGeneratedOutput", Description: "Correct the previous output"},
		Fallback:    &TreeNode{Type: "Action", Name: "EscalateGeneratedFailure", Description: "Fallback when primary fails"},
	}

	serTree := gen.buildSerializable(spec, "generated-skill")
	info := engine.ValidateTreeFull(serTree)
	if !info.Valid() {
		t.Fatalf("generated tree should validate without invented handlers, errors: %v", info.Errors)
	}

	router := serTree.Children[1]
	if router.Type != "DecisionTree" {
		t.Fatalf("expected StrategyRouter to use DecisionTree, got %s", router.Type)
	}
	primary := router.Children[0]
	if primary.Children[0].Type != "ChainAction" {
		t.Fatalf("expected generated action path to compile to ChainAction, got %#v", primary.Children[0])
	}
	if !strings.Contains(primary.Children[0].Name, "Review code for issues") {
		t.Fatalf("ChainAction prompt should include action description, got %q", primary.Children[0].Name)
	}

	outcome := serTree.Children[3]
	if outcome.Children[1].Type != "Retry" || outcome.Children[1].Children[0].Type != "ChainAction" {
		t.Fatalf("self-correct should be Retry around ChainAction, got %#v", outcome.Children[1])
	}
	if outcome.Children[2].Type != "ChainAction" {
		t.Fatalf("explicit fallback should compile to ChainAction, got %#v", outcome.Children[2])
	}
}

func TestFactory_RejectsInvalidGeneratedTreeBeforePersisting(t *testing.T) {
	badSpec := TreeSpec{
		RootType: "BogusNodeType",
		RootName: "InvalidAgent",
		StrategyPath: []TreeNode{
			{Type: "Action", Name: "DoThing", Description: "Do the thing"},
		},
	}
	data, _ := json.Marshal(badSpec)

	mock := regressionMockLLM{resp: string(data)}
	factory, err := NewAgentFactory(mock, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	_, err = factory.CreateFromContent("invalid-skill", "# Invalid")
	if err == nil {
		t.Fatal("expected invalid generated tree to be rejected before persistence")
	}
	if !strings.Contains(err.Error(), "generated invalid tree") {
		t.Fatalf("expected validation error, got %v", err)
	}
}
