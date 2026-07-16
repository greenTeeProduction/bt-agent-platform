package llm

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

// NamedLLM associates a provider/model label with an LLM implementation.
type NamedLLM struct {
	Name string
	LLM  LLM
}

// modelCircuitBreakerThreshold and modelCircuitBreakerCooldown match the
// values used elsewhere for per-provider breakers (e.g. openai_compat.go,
// a2a/auction.go's winner breaker) so a model that fails on every call stops
// being retried after a handful of consecutive failures.
const (
	modelCircuitBreakerThreshold = 3
	modelCircuitBreakerCooldown  = 60 * time.Second
)

// FallbackLLM tries a primary model first, then fallback models in order.
type FallbackLLM struct {
	models []NamedLLM

	breakersMu sync.Mutex
	breakers   map[string]*reliability.CircuitBreaker
}

// NewFallbackLLM creates an ordered fallback chain. The first model is primary.
func NewFallbackLLM(models []NamedLLM) *FallbackLLM {
	return &FallbackLLM{models: models}
}

func (f *FallbackLLM) Generate(prompt string) (string, error) {
	return f.generate(func(model LLM) (string, error) {
		return model.Generate(prompt)
	})
}

func (f *FallbackLLM) GenerateCtx(ctx context.Context, prompt string) (string, error) {
	return f.generate(func(model LLM) (string, error) {
		return model.GenerateCtx(ctx, prompt)
	})
}

func (f *FallbackLLM) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) {
	return f.generate(func(model LLM) (string, error) {
		return model.GenerateWithTimeout(prompt, timeout)
	})
}

// breakerFor returns the circuit breaker tracking failures for the named
// model, creating it on first use. Keyed by model.Name so each entry in the
// fallback chain trips independently.
func (f *FallbackLLM) breakerFor(name string) *reliability.CircuitBreaker {
	f.breakersMu.Lock()
	defer f.breakersMu.Unlock()
	if f.breakers == nil {
		f.breakers = make(map[string]*reliability.CircuitBreaker)
	}
	cb, ok := f.breakers[name]
	if !ok {
		cb = reliability.NewCircuitBreaker("llm.fallback."+name, modelCircuitBreakerThreshold, modelCircuitBreakerCooldown)
		f.breakers[name] = cb
	}
	return cb
}

func (f *FallbackLLM) generate(call func(LLM) (string, error)) (string, error) {
	if len(f.models) == 0 {
		return "", fmt.Errorf("no LLM models configured")
	}

	// Wrap with %w and join so typed errors (e.g. *reliability.RateLimitError
	// carrying Retry-After) survive the aggregation for errors.As upstream.
	errs := make([]error, 0, len(f.models))
	for _, model := range f.models {
		if model.LLM == nil {
			errs = append(errs, fmt.Errorf("%s: nil model", model.Name))
			continue
		}
		breaker := f.breakerFor(model.Name)
		if !breaker.Allow() {
			errs = append(errs, fmt.Errorf("%s: circuit breaker open", model.Name))
			continue
		}
		var result string
		var err error
		if panicErr := reliability.Recover(model.Name, func() {
			result, err = call(model.LLM)
		}); panicErr != nil {
			breaker.RecordFailure()
			errs = append(errs, fmt.Errorf("%s: %w", model.Name, panicErr))
			continue
		}
		if err == nil {
			breaker.RecordSuccess()
			return result, nil
		}
		breaker.RecordFailure()
		errs = append(errs, fmt.Errorf("%s: %w", model.Name, err))
	}

	return "", fmt.Errorf("all LLM models failed: %w", errors.Join(errs...))
}

func (f *FallbackLLM) AnalyzeComplexity(task string) string {
	if len(f.models) == 0 || f.models[0].LLM == nil {
		return "medium"
	}
	return f.models[0].LLM.AnalyzeComplexity(task)
}

func (f *FallbackLLM) GeneratePlan(task, complexity string) string {
	result, err := f.Generate(fmt.Sprintf("Create a step-by-step execution plan for this %s-complexity task.\nTask: %s\nPlan:", complexity, task))
	if err != nil {
		return fmt.Sprintf("1. Analyze: %s\n2. Execute: %s\n3. Verify result", task, task)
	}
	return result
}

func (f *FallbackLLM) Reflect(task, outcome, plan string) (string, string) {
	result, err := f.Generate(fmt.Sprintf(`Task: %s
Plan: %s
Outcome: %s

Analyze what went well and what could be improved. Respond in exactly this format:
WENT_WELL: <text>
TO_IMPROVE: <text>`, task, plan, outcome))
	if err != nil {
		return "task completed", "better error handling"
	}
	wentWell := extractSection(result, "WENT_WELL:")
	toImprove := extractSection(result, "TO_IMPROVE:")
	if wentWell == "" {
		wentWell = "task completed"
	}
	if toImprove == "" {
		toImprove = "better error handling"
	}
	return wentWell, toImprove
}

var _ LLM = (*FallbackLLM)(nil)
