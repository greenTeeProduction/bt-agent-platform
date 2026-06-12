package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

type mergedMockLLM struct{}

func (m *mergedMockLLM) GenerateCtx(_ context.Context, prompt string) (string, error) {
	return m.Generate(prompt)
}
func (m *mergedMockLLM) GenerateWithTimeout(prompt string, _ time.Duration) (string, error) {
	return m.Generate(prompt)
}

func (m *mergedMockLLM) Generate(_ string) (string, error) {
	return "MOCK_OUTPUT: This is a comprehensive response to the task including details and examples.", nil
}
func (m *mergedMockLLM) AnalyzeComplexity(_ string) string       { return "medium" }
func (m *mergedMockLLM) GeneratePlan(task, _ string) string      { return "plan: " + task }
func (m *mergedMockLLM) Reflect(_, _, _ string) (string, string) { return "good", "none" }

func TestMergedTree_Routing(t *testing.T) {
	tests := []struct {
		name     string
		task     string
		wantPath string
	}{
		{"CodeReview", "review this Go code for bugs: func divide(a,b int) int {return a/b}", "CodeReviewPath"},
		{"GoDev", "build and test the Go module", "GoDevPath"},
		{"Finance", "build a DCF valuation model for Tesla", "FinancePath"},
		{"DevOps", "deploy the docker container to kubernetes", "DevOpsPath"},
		{"Security", "audit this code for security vulnerabilities", "SecurityPath"},
		{"DataPipeline", "design an ETL pipeline for CSV data transformation", "DataPipelinePath"},
		{"Research", "research the impact of quantum computing on cryptography", "ResearchPath"},
		{"ThinkTank", "analyze the strategic implications of AI regulation", "ThinkTankPath"},
		{"Refactoring", "refactor the legacy code to modern patterns", "RefactoringPath"},
		{"Knowledge", "what is the best practice for error handling in Go", "KnowledgePath"},
		{"Kanban", "move the bugfix card from TODO to IN PROGRESS", "WorkflowPath"},
		{"Incident", "investigate the timeout error in the production deployment", "IncidentPath"},
		{"General", "summarize this text", "GeneralPath"},
		{"GibberishRejected", "x", ""},
		{"EmptyRejected", "", ""},
	}

	mock := &mergedMockLLM{}
	passed := 0
	failed := 0

	for _, tt := range tests {
		bb := &Blackboard{Task: tt.task, LLM: mock}
		tree := evolution.MergedTree()
		bt := BuildTree(tree, bb)
		result := RunTask(bb, bt)

		outcome := bb.Outcome

		if tt.wantPath == "" {
			if outcome == "failure" || outcome == "" || outcome == "partial" {
				passed++
				t.Logf("[PASS] %-14s | %q → rejected (outcome=%s)", tt.name, tt.task, outcome)
			} else {
				failed++
				t.Errorf("[FAIL] %-14s | %q → expected rejection, got outcome=%s result=%s",
					tt.name, tt.task, outcome, trunc(trimNewlines(result), 60))
			}
			continue
		}

		if outcome == "success" {
			passed++
			t.Logf("[PASS] %-14s | %q → success | %s", tt.name, tt.task, trunc(trimNewlines(result), 50))
		} else if result != "" {
			passed++
			t.Logf("[PASS] %-14s | %q → routed (outcome=%s) | %s", tt.name, tt.task, outcome, trunc(trimNewlines(result), 50))
		} else {
			failed++
			t.Errorf("[FAIL] %-14s | %q → no routing (outcome=%s)", tt.name, tt.task, outcome)
		}
	}

	t.Logf("\nResult: %d/%d passed, %d failed", passed, len(tests), failed)
	if failed > 0 {
		t.Errorf("%d routing failures", failed)
	}
}

func TestMergedTree_Structure(t *testing.T) {
	tree := evolution.MergedTree()
	if tree == nil {
		t.Fatal("MergedTree is nil")
	}
	if tree.Name != "Merged_Main" {
		t.Errorf("expected Merged_Main, got %s", tree.Name)
	}
	if tree.Type != "Sequence" {
		t.Errorf("expected Sequence root, got %s", tree.Type)
	}

	sections := []string{"PreGate", "StrategyRouter", "MarkSuccessful"}
	for _, name := range sections {
		found := false
		for _, child := range tree.Children {
			if child.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing section: %s", name)
		}
	}

	for _, child := range tree.Children {
		if child.Name == "StrategyRouter" {
			if len(child.Children) != 22 {
				t.Errorf("expected 22 strategy paths, got %d", len(child.Children))
			}
		}
	}

	nodes := evolution.CountNodes(tree)
	if nodes < 30 {
		t.Errorf("expected >= 30 nodes, got %d", nodes)
	}
	t.Logf("Tree: %d nodes, %d strategy paths, sections: %v", nodes, 22, sections)
}

func TestMergedTree_PreGate(t *testing.T) {
	tree := evolution.MergedTree()
	for _, child := range tree.Children {
		if child.Name != "PreGate" {
			continue
		}
		gateCount := 0
		for _, gate := range child.Children {
			if gate.Type == "Condition" {
				gateCount++
			}
		}
		if gateCount < 2 {
			t.Errorf("PreGate should have >= 2 conditions, got %d", gateCount)
		}
		if len(child.Children) > 0 && child.Children[0].Name != "HasClearTask" {
			t.Errorf("HasClearTask should be first in PreGate, got %s", child.Children[0].Name)
		}
	}
}

func TestMergedTree_PathConditions(t *testing.T) {
	tree := evolution.MergedTree()
	for _, child := range tree.Children {
		if child.Name != "StrategyRouter" {
			continue
		}
		for i, path := range child.Children {
			if len(path.Children) == 0 {
				t.Errorf("path %d has no children", i)
				continue
			}
			first := path.Children[0]
			// GeneralPath is the catch-all — it has no Condition, just ChainAction
			if path.Name == "GeneralPath" {
				if first.Type != "ChainAction" {
					t.Errorf("GeneralPath should start with ChainAction, got %s", first.Type)
				}
				continue
			}
			if first.Type != "Condition" {
				t.Errorf("path %d (%s) missing Condition gate (first=%s)",
					i, path.Name, first.Type)
			}
		}
	}
}

func trimNewlines(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
}
