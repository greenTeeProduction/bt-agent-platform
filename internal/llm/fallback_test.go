package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/config"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

// TestFallbackLLM_ValidationErrorsDoNotTripBreaker mirrors the openai_compat
// gating one layer up: a model that keeps returning typed caller-side errors
// (400 validation) must not have its per-model breaker walked open — otherwise
// three malformed prompts cool a healthy model down for 60s and force the
// chain onto worse fallbacks for well-formed prompts.
func TestFallbackLLM_ValidationErrorsDoNotTripBreaker(t *testing.T) {
	stub := &stubLLM{name: "m1", err: reliability.NewCategorizedError(reliability.ErrCatValidation, errors.New("api status 400"))}
	f := NewFallbackLLM([]NamedLLM{{Name: "m1", LLM: stub}})

	for i := 0; i < 5; i++ {
		if _, err := f.Generate("bad prompt"); err == nil {
			t.Fatalf("call %d: expected the validation error to surface", i)
		}
	}
	// A healthy request must still reach the model, not hit an open breaker.
	stub.err = nil
	got, err := f.Generate("good prompt")
	if err != nil {
		t.Fatalf("well-formed prompt after 5 client errors was blocked (breaker wrongly tripped): %v", err)
	}
	if !strings.Contains(got, "good prompt") {
		t.Fatalf("got %q, want the model's answer", got)
	}
	if stub.calls != 6 {
		t.Fatalf("model saw %d calls, want 6 (no call may be short-circuited by a breaker opened on validation errors)", stub.calls)
	}
}

type stubLLM struct {
	name      string
	err       error
	panicWith any
	calls     int
}

func (s *stubLLM) Generate(prompt string) (string, error) {
	s.calls++
	if s.panicWith != nil {
		panic(s.panicWith)
	}
	if s.err != nil {
		return "", s.err
	}
	return s.name + ":" + prompt, nil
}

func (s *stubLLM) GenerateCtx(_ context.Context, prompt string) (string, error) {
	return s.Generate(prompt)
}

func (s *stubLLM) GenerateWithTimeout(prompt string, _ time.Duration) (string, error) {
	return s.Generate(prompt)
}

func (s *stubLLM) AnalyzeComplexity(_ string) string       { return "low" }
func (s *stubLLM) GeneratePlan(_, _ string) string         { return "plan" }
func (s *stubLLM) Reflect(_, _, _ string) (string, string) { return "ok", "none" }

func TestFallbackLLM_GenerateUsesNextModelAfterPrimaryFailure(t *testing.T) {
	primary := &stubLLM{name: "primary", err: errors.New("primary down")}
	fallback := &stubLLM{name: "fallback"}
	chain := NewFallbackLLM([]NamedLLM{
		{Name: "ollama:qwen", LLM: primary},
		{Name: "deepseek:deepseek-v4-flash", LLM: fallback},
	})

	got, err := chain.Generate("hello")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if got != "fallback:hello" {
		t.Fatalf("expected fallback response, got %q", got)
	}
	if primary.calls != 1 || fallback.calls != 1 {
		t.Fatalf("expected both models to be tried once, primary=%d fallback=%d", primary.calls, fallback.calls)
	}
}

func TestFallbackLLM_GenerateRecoversPanicAndTriesNextModel(t *testing.T) {
	primary := &stubLLM{name: "primary", panicWith: "primary exploded"}
	fallback := &stubLLM{name: "fallback"}
	chain := NewFallbackLLM([]NamedLLM{
		{Name: "ollama:qwen", LLM: primary},
		{Name: "deepseek:deepseek-v4-flash", LLM: fallback},
	})

	got, err := chain.Generate("hello")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if got != "fallback:hello" {
		t.Fatalf("expected fallback response, got %q", got)
	}
	if primary.calls != 1 || fallback.calls != 1 {
		t.Fatalf("expected both models to be tried once, primary=%d fallback=%d", primary.calls, fallback.calls)
	}
}

func TestFallbackLLM_GenerateAllPanicReturnsAggregatedError(t *testing.T) {
	chain := NewFallbackLLM([]NamedLLM{
		{Name: "primary", LLM: &stubLLM{name: "primary", panicWith: "primary exploded"}},
		{Name: "fallback", LLM: &stubLLM{name: "fallback", panicWith: "fallback exploded"}},
	})

	_, err := chain.Generate("hello")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "primary") || !strings.Contains(msg, "primary exploded") {
		t.Fatalf("expected primary panic in aggregated error, got %q", msg)
	}
	if !strings.Contains(msg, "fallback") || !strings.Contains(msg, "fallback exploded") {
		t.Fatalf("expected fallback panic in aggregated error, got %q", msg)
	}
}

func TestFallbackLLM_GenerateReturnsAllFailures(t *testing.T) {
	chain := NewFallbackLLM([]NamedLLM{
		{Name: "primary", LLM: &stubLLM{name: "primary", err: errors.New("primary down")}},
		{Name: "fallback", LLM: &stubLLM{name: "fallback", err: errors.New("fallback down")}},
	})

	_, err := chain.Generate("hello")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "primary: primary down") || !strings.Contains(msg, "fallback: fallback down") {
		t.Fatalf("expected aggregated model failures, got %q", msg)
	}
}

// TestFallbackLLM_CircuitBreakerSkipsPersistentlyFailingModel exercises
// milestone 4/5 of the Q3 Reliability program: FallbackLLM.generate must keep
// a per-model reliability.CircuitBreaker (keyed by model.Name) so a model that
// fails on every call trips its breaker and gets skipped on subsequent
// requests instead of being re-invoked (and re-failing) every time. Today
// generate has no failure memory across calls, so it relaunches the
// perpetually-failing primary model on every single Generate call — this test
// fails because the failing model's invocation count keeps pace with the
// call count.
func TestFallbackLLM_CircuitBreakerSkipsPersistentlyFailingModel(t *testing.T) {
	failing := &stubLLM{name: "failing", err: errors.New("model A down")}
	healthy := &stubLLM{name: "healthy"}
	chain := NewFallbackLLM([]NamedLLM{
		{Name: "model-a", LLM: failing},
		{Name: "model-b", LLM: healthy},
	})

	const attempts = 10
	for i := 0; i < attempts; i++ {
		got, err := chain.Generate("hello")
		if err != nil {
			t.Fatalf("attempt %d: Generate returned error: %v", i, err)
		}
		if got != "healthy:hello" {
			t.Fatalf("attempt %d: expected fallback response, got %q", i, got)
		}
	}

	if failing.calls >= attempts {
		t.Fatalf("circuit breaker did not skip persistently-failing model: model A was invoked %d times "+
			"across %d Generate calls; expected consecutive failures to trip the breaker and route "+
			"straight to model B without re-invoking model A", failing.calls, attempts)
	}
	if healthy.calls != attempts {
		t.Fatalf("expected model B to be invoked on every call, got %d out of %d", healthy.calls, attempts)
	}
}

func TestNewProvider_BuildsFallbackChainFromConfiguredModels(t *testing.T) {
	cfg := &config.Config{
		LLMProvider:    "deepseek",
		DeepSeekHost:   "http://127.0.0.1:1",
		DeepSeekModel:  "primary-model",
		DeepSeekKey:    "test-key",
		LLMTimeout:     1,
		FallbackModels: "deepseek:fallback-a,deepseek/fallback-b",
	}

	client, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("NewProvider returned error: %v", err)
	}
	chain, ok := client.(*FallbackLLM)
	if !ok {
		t.Fatalf("expected *FallbackLLM, got %T", client)
	}
	if len(chain.models) != 3 {
		t.Fatalf("expected primary plus two fallbacks, got %d", len(chain.models))
	}
	gotNames := []string{chain.models[0].Name, chain.models[1].Name, chain.models[2].Name}
	wantNames := []string{"deepseek:primary-model", "deepseek:fallback-a", "deepseek:fallback-b"}
	for i := range wantNames {
		if gotNames[i] != wantNames[i] {
			t.Fatalf("model[%d]: expected %q, got %q", i, wantNames[i], gotNames[i])
		}
	}
}
