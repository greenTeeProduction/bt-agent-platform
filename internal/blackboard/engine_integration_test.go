package blackboard_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

type seqMockLLM struct {
	responses []string
	idx       int
}

func (m *seqMockLLM) GenerateCtx(_ context.Context, _ string) (string, error) {
	return m.Generate("")
}
func (m *seqMockLLM) GenerateWithTimeout(_ string, _ time.Duration) (string, error) {
	return m.Generate("")
}
func (m *seqMockLLM) Generate(_ string) (string, error) {
	if m.idx < len(m.responses) {
		r := m.responses[m.idx]
		m.idx++
		return r, nil
	}
	return "Final Answer: done.", nil
}
func (m *seqMockLLM) AnalyzeComplexity(_ string) string       { return "medium" }
func (m *seqMockLLM) GeneratePlan(_, _ string) string         { return "plan" }
func (m *seqMockLLM) Reflect(_, _, _ string) (string, string) { return "ok", "ok" }

func TestEngineBlackboardToolsRoundTrip(t *testing.T) {
	const payload = "SECRET_PAYLOAD_FOR_BB_TEST"
	mock := &seqMockLLM{
		responses: []string{
			`Thought: store context off-prompt
Action: bb_write
Action Input: {"key":"work/payload","value":"` + payload + `"}`,
			`Thought: read back
Action: bb_read
Action Input: work/payload`,
			"Final Answer: Retrieved from blackboard: " + payload,
		},
	}

	mgr := blackboard.DefaultManager()
	h := blackboard.NewHandle(mgr, "run_agent_bb", "", "demo")
	bb := &engine.Blackboard{
		Task: "offload context to blackboard and read it back",
		LLM:  mock,
		BB:   h,
	}
	engine.PrepareBlackboard(bb)

	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "agent:{{.Task}}",
		Metadata: map[string]any{
			"max_tokens": float64(5),
		},
	}

	bt := engine.BuildTree(tree, bb)
	engine.RunTask(bb, bt)

	if bb.Outcome != "success" {
		t.Fatalf("expected success, got %s: %s", bb.Outcome, bb.Result)
	}
	if !strings.Contains(bb.Result, payload) {
		t.Fatalf("final answer missing payload: %s", bb.Result)
	}
	e, err := h.Get("work/payload")
	if err != nil {
		t.Fatal(err)
	}
	if e.Value != payload {
		t.Fatalf("stored value mismatch: %q", e.Value)
	}
}

func TestEnginePrepareBlackboard_Idempotent(t *testing.T) {
	h := blackboard.NewHandle(blackboard.DefaultManager(), "run_idem", "", "a")
	bb := &engine.Blackboard{BB: h}
	engine.PrepareBlackboard(bb)
	n := len(bb.ChainTools)
	engine.PrepareBlackboard(bb)
	if len(bb.ChainTools) != n {
		t.Fatalf("tools attached twice: before=%d after=%d", n, len(bb.ChainTools))
	}
}

type promptCaptureLLM struct {
	lastPrompt string
	seqMockLLM
}

func (m *promptCaptureLLM) Generate(prompt string) (string, error) {
	m.lastPrompt = prompt
	return m.seqMockLLM.Generate(prompt)
}

func TestEngineExpandBBTemplate_InLLMCall(t *testing.T) {
	mgr := blackboard.DefaultManager()
	h := blackboard.NewHandle(mgr, "run_tpl", "sess_tpl", "demo-agent")
	_ = h.Set("work/note", "stored note value", "short summary", "text")

	mock := &promptCaptureLLM{
		seqMockLLM: seqMockLLM{responses: []string{"Final Answer: done"}},
	}
	bb := &engine.Blackboard{
		Task: "base task",
		LLM:  mock,
		BB:   h,
		RunID: "run_tpl",
	}
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "llm_call:Agent={{.BB.agent}} Note={{.BB.work/note}}",
		Metadata: map[string]any{
			"max_tokens": float64(2),
		},
	}
	bt := engine.BuildTree(tree, bb)
	engine.RunTask(bb, bt)
	if !strings.Contains(mock.lastPrompt, "demo-agent") {
		t.Fatalf("prompt missing agent: %q", mock.lastPrompt)
	}
	if !strings.Contains(mock.lastPrompt, "stored note value") {
		t.Fatalf("prompt missing BB key value: %q", mock.lastPrompt)
	}
}
