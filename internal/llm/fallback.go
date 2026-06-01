package llm

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// NamedLLM associates a provider/model label with an LLM implementation.
type NamedLLM struct {
	Name string
	LLM  LLM
}

// FallbackLLM tries a primary model first, then fallback models in order.
type FallbackLLM struct {
	models []NamedLLM
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

func (f *FallbackLLM) generate(call func(LLM) (string, error)) (string, error) {
	if len(f.models) == 0 {
		return "", fmt.Errorf("no LLM models configured")
	}

	failures := make([]string, 0, len(f.models))
	for _, model := range f.models {
		if model.LLM == nil {
			failures = append(failures, fmt.Sprintf("%s: nil model", model.Name))
			continue
		}
		result, err := call(model.LLM)
		if err == nil {
			return result, nil
		}
		failures = append(failures, fmt.Sprintf("%s: %v", model.Name, err))
	}

	return "", fmt.Errorf("all LLM models failed: %s", strings.Join(failures, "; "))
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
