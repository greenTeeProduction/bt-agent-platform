package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
	btcore "github.com/rvitorper/go-bt/core"
)

// budgetTokensMockLLM returns a fixed response on every call so tests can
// compute the exact token estimate generateWithRetry is expected to add to
// bb.TokensUsed on each invocation.
type budgetTokensMockLLM struct {
	resp string
}

func (m *budgetTokensMockLLM) Generate(_ string) (string, error) { return m.resp, nil }
func (m *budgetTokensMockLLM) GenerateCtx(_ context.Context, prompt string) (string, error) {
	return m.Generate(prompt)
}
func (m *budgetTokensMockLLM) GenerateWithTimeout(prompt string, _ time.Duration) (string, error) {
	return m.Generate(prompt)
}
func (m *budgetTokensMockLLM) AnalyzeComplexity(_ string) string       { return "medium" }
func (m *budgetTokensMockLLM) GeneratePlan(_, _ string) string         { return "1. Step one" }
func (m *budgetTokensMockLLM) Reflect(_, _, _ string) (string, string) { return "ok", "better" }

var _ llm.LLM = (*budgetTokensMockLLM)(nil)

// estimateTokensForTest mirrors the ~4-chars-per-token heuristic
// generateWithRetry (internal/engine/chains.go) is expected to use when
// turning an LLM response into a token count for bb.TokensUsed
// (internal/engine/tree.go:146).
func estimateTokensForTest(s string) int {
	return (len(s) + 3) / 4
}

func TestGenerateWithRetry_IncrementsBlackboardTokensUsed(t *testing.T) {
	resp := strings.Repeat("A", 160) // 160 chars -> 40 estimated tokens
	mock := &budgetTokensMockLLM{resp: resp}
	bb := &Blackboard{Task: "test task", LLM: mock}

	if bb.TokensUsed != 0 {
		t.Fatalf("expected TokensUsed to start at 0, got %d", bb.TokensUsed)
	}

	if _, err := generateWithRetry(bb, "prompt", 0); err != nil {
		t.Fatalf("generateWithRetry returned error: %v", err)
	}

	want := estimateTokensForTest(resp)
	if bb.TokensUsed != want {
		t.Fatalf("expected TokensUsed=%d after one call (response=%d chars), got %d", want, len(resp), bb.TokensUsed)
	}

	// A second call should accumulate, not overwrite.
	if _, err := generateWithRetry(bb, "prompt", 0); err != nil {
		t.Fatalf("generateWithRetry returned error: %v", err)
	}
	if bb.TokensUsed != want*2 {
		t.Fatalf("expected TokensUsed=%d after two calls, got %d", want*2, bb.TokensUsed)
	}
}

func TestBuildBudget_MaxTokens_TripsOnCumulativeUsage(t *testing.T) {
	resp := strings.Repeat("B", 160) // 160 chars -> 40 estimated tokens/call
	mock := &budgetTokensMockLLM{resp: resp}
	perCall := estimateTokensForTest(resp)
	maxTokens := perCall + perCall/2 // crosses on the 2nd call, not the 1st

	tree := &evolution.SerializableNode{
		Type:     "Budget",
		Name:     "B",
		Metadata: map[string]any{"max_tokens": float64(maxTokens)},
		Children: []evolution.SerializableNode{
			{Type: "ChainAction", Name: "llm_call:{{.Task}}"},
		},
	}
	bb := &Blackboard{Task: "test task", LLM: mock, ChainState: make(map[string]any)}
	cmd := BuildBudget(tree, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)

	if c := cmd.Run(ctx); c != 1 {
		t.Fatalf("run 1: expected success (tokens=%d, budget=%d), got code=%d outcome=%s", bb.TokensUsed, maxTokens, c, bb.Outcome)
	}
	if bb.TokensUsed != perCall {
		t.Fatalf("run 1: expected TokensUsed=%d, got %d", perCall, bb.TokensUsed)
	}

	if c := cmd.Run(ctx); c != -1 {
		t.Fatalf("run 2: expected budget_exhausted_tokens once cumulative usage (%d) crosses maxTokens=%d, got code=%d outcome=%s", bb.TokensUsed, maxTokens, c, bb.Outcome)
	}
	if bb.Outcome != "budget_exhausted_tokens" {
		t.Fatalf("run 2: expected Outcome=budget_exhausted_tokens, got %q", bb.Outcome)
	}

	// Once exhausted, further runs must not invoke the child again — TokensUsed
	// stays put and the budget stays exhausted.
	tokensAfterTrip := bb.TokensUsed
	if c := cmd.Run(ctx); c != -1 {
		t.Fatalf("run 3: expected budget to remain exhausted, got code=%d", c)
	}
	if bb.TokensUsed != tokensAfterTrip {
		t.Fatalf("run 3: expected TokensUsed to stay at %d once budget is exhausted, got %d", tokensAfterTrip, bb.TokensUsed)
	}
}
