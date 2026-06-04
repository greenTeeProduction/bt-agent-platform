package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/config"
)

type stubLLM struct {
	name  string
	err   error
	calls int
}

func (s *stubLLM) Generate(prompt string) (string, error) {
	s.calls++
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

func (s *stubLLM) AnalyzeComplexity(_ string) string             { return "low" }
func (s *stubLLM) GeneratePlan(_, complexity string) string      { return "plan" }
func (s *stubLLM) Reflect(_, outcome, _ string) (string, string) { return "ok", "none" }

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
