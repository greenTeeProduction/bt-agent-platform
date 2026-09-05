// Package llm provides language model abstractions and shared test helpers.
package llm

import (
	"context"
	"time"
)

// MockLLM is a reusable test double that implements the full LLM interface.
// It returns configurable responses for all methods, suitable for BT engine,
// factory, and chain tests.
//
// Usage:
//
//	mock := &llm.MockLLM{
//	    GenerateResponse: "sufficiently long mock response for quality checks",
//	    Complexity:       "medium",
//	    Plan:             "1. Do X\n2. Verify",
//	    WentWell:         "completed successfully",
//	    ToImprove:        "add error handling",
//	}
//	bb := &engine.Blackboard{LLM: mock, ...}
type MockLLM struct {
	// GenerateResponse is the canned response for Generate / GenerateCtx / GenerateWithTimeout.
	// Default: "MockLLM response with sufficient length for quality validation"
	GenerateResponse string

	// Complexity is returned by AnalyzeComplexity. Default: "medium".
	Complexity string

	// Plan is returned by GeneratePlan. Default: "mock plan".
	Plan string

	// WentWell is the first return of Reflect. Default: "ok".
	WentWell string

	// ToImprove is the second return of Reflect. Default: "none".
	ToImprove string
}

// DefaultMockResponse is used when GenerateResponse is empty.
const DefaultMockResponse = "MockLLM response with sufficient length for quality validation checks"

func (m *MockLLM) defGen() string {
	if m.GenerateResponse == "" {
		return DefaultMockResponse
	}
	return m.GenerateResponse
}
func (m *MockLLM) defComplexity() string {
	if m.Complexity == "" {
		return "medium"
	}
	return m.Complexity
}
func (m *MockLLM) defPlan() string {
	if m.Plan == "" {
		return "mock plan"
	}
	return m.Plan
}

// Generate returns the canned GenerateResponse.
func (m *MockLLM) Generate(_ string) (string, error) {
	return m.defGen(), nil
}

// GenerateCtx delegates to Generate, ignoring context (mock has no cancellation).
func (m *MockLLM) GenerateCtx(_ context.Context, prompt string) (string, error) {
	return m.Generate(prompt)
}

// GenerateWithTimeout delegates to Generate, ignoring timeout (mock is instant).
func (m *MockLLM) GenerateWithTimeout(prompt string, _ time.Duration) (string, error) {
	return m.Generate(prompt)
}

// AnalyzeComplexity returns the canned Complexity.
func (m *MockLLM) AnalyzeComplexity(_ string) string {
	return m.defComplexity()
}

// GeneratePlan returns the canned Plan.
func (m *MockLLM) GeneratePlan(_, _ string) string {
	return m.defPlan()
}

// Reflect returns the canned WentWell and ToImprove.
func (m *MockLLM) Reflect(_, _, _ string) (string, string) {
	wentWell := m.WentWell
	if wentWell == "" {
		wentWell = "ok"
	}
	toImprove := m.ToImprove
	if toImprove == "" {
		toImprove = "none"
	}
	return wentWell, toImprove
}
