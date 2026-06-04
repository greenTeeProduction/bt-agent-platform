package blocks_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/blocks"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/hitl"
	btcore "github.com/rvitorper/go-bt/core"
)

type pipelineMockLLM struct{}

func (pipelineMockLLM) AnalyzeComplexity(task string) string { return "low" }
func (pipelineMockLLM) GeneratePlan(task, complexity string) string {
	return "1. Analyze\n2. Execute\n3. Verify"
}
func (pipelineMockLLM) Reflect(task, outcome, plan string) (string, string) {
	return "ok", "none"
}
func (pipelineMockLLM) Generate(prompt string) (string, error) {
	return "Mock agent output with enough length for quality validation checks in tests.", nil
}
func (pipelineMockLLM) GenerateCtx(ctx context.Context, prompt string) (string, error) {
	return pipelineMockLLM{}.Generate(prompt)
}
func (pipelineMockLLM) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) {
	return pipelineMockLLM{}.Generate(prompt)
}

func TestComposedTaskTree_BuildExpand_EmptyTaskFails(t *testing.T) {
	dir := t.TempDir()
	blocks.InitRegistry(dir)
	hitl.SetPolicy(hitl.Policy{Enabled: true, AutoApprove: true, Timeout: hitl.DefaultPolicy().Timeout})
	t.Cleanup(func() { hitl.SetPolicy(hitl.DefaultPolicy()) })

	tree, err := blocks.ComposeTaskTree(blocks.DefaultRegistry, "ComposedTask", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !blocks.HasSubTreeRefs(tree) {
		t.Fatal("expected SubTreeRef before build-time expand")
	}

	bb := &engine.Blackboard{Task: "", ChainState: make(map[string]any)}
	cmd, err := engine.BuildAndValidate(tree, bb)
	if err != nil {
		t.Fatalf("BuildAndValidate: %v", err)
	}

	ctx := btcore.NewBTContext(t.Context(), bb)
	if code := cmd.Run(ctx); code == 1 {
		t.Fatal("empty task should not succeed through expanded PreGate")
	}
}

func TestComposedTaskTree_BuildExpand_ValidTaskRuns(t *testing.T) {
	dir := t.TempDir()
	blocks.InitRegistry(dir)
	hitl.SetPolicy(hitl.Policy{Enabled: true, AutoApprove: true, Timeout: hitl.DefaultPolicy().Timeout})
	t.Cleanup(func() { hitl.SetPolicy(hitl.DefaultPolicy()) })

	tree, err := blocks.ComposeTaskTree(blocks.DefaultRegistry, "ComposedTask", nil)
	if err != nil {
		t.Fatal(err)
	}

	bb := &engine.Blackboard{
		Task:       "Summarize behavior trees for agentic AI in two sentences.",
		ChainState: make(map[string]any),
		LLM:        pipelineMockLLM{},
	}
	cmd, err := engine.BuildAndValidate(tree, bb)
	if err != nil {
		t.Fatalf("BuildAndValidate: %v", err)
	}

	engine.RunTask(bb, cmd)
	if bb.Outcome != "success" {
		t.Fatalf("expected success, outcome=%q result=%q", bb.Outcome, bb.Result)
	}
	if bb.Result == "" {
		t.Fatal("expected non-empty result from expanded tool_execution block")
	}
}

func TestComposedTaskTreeAgentic_SetsPlan(t *testing.T) {
	dir := t.TempDir()
	blocks.InitRegistry(dir)
	hitl.SetPolicy(hitl.Policy{Enabled: true, AutoApprove: true, Timeout: hitl.DefaultPolicy().Timeout})
	t.Cleanup(func() { hitl.SetPolicy(hitl.DefaultPolicy()) })

	tree, err := blocks.ComposeTaskTreeAgentic(blocks.DefaultRegistry, "Agentic", nil)
	if err != nil {
		t.Fatal(err)
	}

	bb := &engine.Blackboard{
		Task:       "Implement a feature to export metrics in Prometheus format for the BT platform.",
		ChainState: make(map[string]any),
		LLM:        pipelineMockLLM{},
	}
	cmd, err := engine.BuildAndValidate(tree, bb)
	if err != nil {
		t.Fatalf("BuildAndValidate: %v", err)
	}

	engine.RunTask(bb, cmd)
	if bb.Plan == "" {
		t.Fatal("expected bb.Plan from core:plan block")
	}
	if !strings.Contains(bb.Plan, "Analyze") {
		t.Fatalf("unexpected plan: %q", bb.Plan)
	}
}

func TestExpand_BlockMetadata(t *testing.T) {
	reg := blocks.NewRegistry("")
	tree, err := blocks.Compose(reg, blocks.ComposeSpec{
		Name:   "MetaTest",
		Blocks: []string{"core:pre_gate"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	expanded, err := blocks.Expand(reg, tree)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	var walk func(*evolution.SerializableNode)
	walk = func(n *evolution.SerializableNode) {
		if n == nil {
			return
		}
		if n.Metadata != nil {
			if _, ok := n.Metadata["block_id"].(string); ok {
				found = true
			}
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(expanded)
	if !found {
		t.Fatal("expected block_id metadata on expanded nodes")
	}
}
