// Package engine provides shared test doubles for the LLM interface.
package engine

import (
	"context"
	"time"
)

// MockLLM is a shared test double that implements llm.LLM for use across
// engine, evolution, agent, and other internal packages.
//
// Use NewMockLLM() to create a configured instance, or set fields directly.
type MockLLM struct {
	ComplexityResp string
	PlanResp       string
	WentWellResp   string
	ToImproveResp  string
	GenerateResp   string
	GenerateErr    error
}

// NewMockLLM returns a MockLLM with sensible defaults for all responses.
// All Generate* methods return at least 40 chars to pass validateOutputQuality.
func NewMockLLM() *MockLLM {
	return &MockLLM{
		ComplexityResp: "low",
		PlanResp:       "1. Execute the task\n2. Verify results\n3. Report outcome",
		WentWellResp:   "Task completed successfully",
		ToImproveResp:  "Add more error handling",
		GenerateResp:   defaultGenerateResp,
	}
}

// Generate implements llm.LLM. Returns GenerateResp if set, otherwise a default.
func (m *MockLLM) Generate(prompt string) (string, error) {
	if m.GenerateErr != nil {
		return "", m.GenerateErr
	}
	if m.GenerateResp != "" {
		return m.GenerateResp, nil
	}
	return defaultGenerateResp, nil
}

// defaultGenerateResp is returned by Generate when GenerateResp is empty.
// Must be >= 40 chars for validateOutputQuality.
const defaultGenerateResp = "Mock response with sufficient length for quality validation checks"

// GenerateCtx implements llm.LLM.
func (m *MockLLM) GenerateCtx(ctx context.Context, prompt string) (string, error) {
	return m.Generate(prompt)
}

// GenerateWithTimeout implements llm.LLM.
func (m *MockLLM) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) {
	return m.Generate(prompt)
}

// AnalyzeComplexity implements llm.LLM.
func (m *MockLLM) AnalyzeComplexity(task string) string {
	return m.ComplexityResp
}

// GeneratePlan implements llm.LLM.
func (m *MockLLM) GeneratePlan(task, complexity string) string {
	return m.PlanResp
}

// Reflect implements llm.LLM.
func (m *MockLLM) Reflect(task, outcome, plan string) (string, string) {
	return m.WentWellResp, m.ToImproveResp
}

// Compile-time check: MockLLM implements llm.LLM.
var _ LLMInterface = (*MockLLM)(nil)

// LLMInterface is the subset of llm.LLM used by engine tests.
// This avoids a direct dependency on internal/llm from the engine package
// for MockLLM-only usage. For full llm.LLM satisfaction, import internal/llm
// and use `var _ llm.LLM = (*MockLLM)(nil)` in your test file.
type LLMInterface interface {
	Generate(prompt string) (string, error)
	GenerateCtx(ctx context.Context, prompt string) (string, error)
	GenerateWithTimeout(prompt string, timeout time.Duration) (string, error)
	AnalyzeComplexity(task string) string
	GeneratePlan(task, complexity string) string
	Reflect(task, outcome, plan string) (wentWell string, toImprove string)
}
