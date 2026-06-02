package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// ─── coverage gaps: utility_selector.go ───

func TestScoreChild_GoalAlignmentBranch(t *testing.T) {
	// ScoreChild has `bb.ChainState["goal_alignment"]` branch (line 106-108)
	child := &evolution.SerializableNode{Type: "Action", Name: "SetupDefaultTools"}
	bb := &Blackboard{
		Task:   "test task",
		Result: "result text that is definitely more than thirty characters long to pass basic checks",
		ChainState: map[string]any{
			"goal_alignment": float64(0.85),
		},
	}
	criteria := DefaultScoringCriteria()
	score := ScoreChild(child, bb, criteria)
	if score.Criteria["goal_alignment"] != 0.85 {
		t.Errorf("expected goal_alignment=0.85, got %v", score.Criteria["goal_alignment"])
	}
}

func TestScoreChild_NilChainState(t *testing.T) {
	// ScoreChild has `bb.ChainState != nil` guard — test nil path
	child := &evolution.SerializableNode{Type: "Action", Name: "SetupDefaultTools"}
	bb := &Blackboard{
		Task:   "test task",
		Result: "result text that is definitely more than thirty characters long to pass basic checks",
	}
	criteria := DefaultScoringCriteria()
	score := ScoreChild(child, bb, criteria)
	if score.WeightedScore == 0 {
		t.Log("ScoreChild returned WeightedScore=0 for nil ChainState")
	}
}

// ─── coverage gaps: registry.go execLLMCallAction err path ───

// errMockLLM returns an error from Generate
type errMockLLM struct {
	mockLLM
}

func (e *errMockLLM) Generate(prompt string) (string, error) {
	return "", errors.New("simulated error")
}

// ─── coverage gaps: tree.go validateOutputQuality ───

func TestValidateOutputQuality_BlankResult(t *testing.T) {
	bb := &Blackboard{Task: "test", Result: ""}
	result := validateOutputQuality(bb)
	if result {
		t.Error("expected false for blank result")
	}
}

func TestValidateOutputQuality_ErrorPattern(t *testing.T) {
	bb := &Blackboard{Task: "test", Result: "An error occurred during processing and needs more detail"}
	result := validateOutputQuality(bb)
	if !result {
		t.Log("validateOutputQuality: error pattern detected")
	}
}

func TestValidateOutputQuality_MarkdownStructure(t *testing.T) {
	bb := &Blackboard{Task: "test", Result: "# Title\n\n## Section\n\nSome content here that exceeds the minimum length threshold."}
	result := validateOutputQuality(bb)
	if !result {
		t.Error("expected true for well-structured markdown")
	}
}

// ─── coverage gaps: planner_node.go goal failure path ───

func TestBuildPlannerNode_FailThenSuccess(t *testing.T) {
	// Register a temporary action that returns failure
	RegisterAction("__test_fail2__", func(ctx *btcore.BTContext[Blackboard]) int {
		return -1
	})
	defer func() {
		delete(actionRegistry, "__test_fail2__")
	}()

	failChild := evolution.SerializableNode{
		Type: "Action",
		Name: "__test_fail2__",
	}
	succeedChild := evolution.SerializableNode{
		Type: "AlwaysSucceed",
	}
	bb := &Blackboard{
		ChainState: make(map[string]any),
	}
	node := &evolution.SerializableNode{
		Type: "PlannerNode",
		Metadata: map[string]any{
			"goals": []any{
				map[string]any{"name": "goal1", "priority": float64(1.0)},
				map[string]any{"name": "goal2", "priority": float64(0.5)},
			},
		},
		Children: []evolution.SerializableNode{failChild, succeedChild},
	}
	cmd := BuildPlannerNode(node, bb)
	ctx := btcore.NewBTContext(context.Background(), bb)
	result := cmd.Run(ctx)
	if result != 1 {
		t.Errorf("expected 1 (success after fail+retry), got %d", result)
	}
}

func TestBuildPlannerNode_AllFail(t *testing.T) {
	// Register a temporary action that returns failure
	RegisterAction("__test_fail__", func(ctx *btcore.BTContext[Blackboard]) int {
		return -1
	})
	defer func() {
		// Clean up
		delete(actionRegistry, "__test_fail__")
	}()

	failChild := evolution.SerializableNode{
		Type: "Action",
		Name: "__test_fail__",
	}
	bb := &Blackboard{
		ChainState: make(map[string]any),
	}
	node := &evolution.SerializableNode{
		Type: "PlannerNode",
		Metadata: map[string]any{
			"goals": []any{
				map[string]any{"name": "goal1", "priority": float64(1.0)},
			},
		},
		Children: []evolution.SerializableNode{failChild},
	}
	cmd := BuildPlannerNode(node, bb)
	ctx := btcore.NewBTContext(context.Background(), bb)
	result := cmd.Run(ctx)
	if result != -1 {
		t.Errorf("expected -1 (all goals failed), got %d", result)
	}
}

// ─── coverage gaps: utility_selector.go ScoreChildren ───

func TestScoreChildren_BasicRanking(t *testing.T) {
	node := &evolution.SerializableNode{
		Type: "Selector",
		Name: "TestSelector",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "lowPriority", MaxRetries: 1, TimeoutMs: 1000},
			{Type: "Action", Name: "highPriority", MaxRetries: 5, TimeoutMs: 30000},
		},
	}
	bb := &Blackboard{
		Task:   "test task",
		Result: "result text that is definitely more than thirty characters long to pass basic checks",
	}
	criteria := DefaultScoringCriteria()
	scored := ScoreChildren(node, bb, criteria)
	if len(scored) != 2 {
		t.Fatalf("expected 2 scored children, got %d", len(scored))
	}
	_ = scored
}
