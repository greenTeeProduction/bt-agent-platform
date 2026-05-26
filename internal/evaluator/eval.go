package evaluator

import (
	"fmt"
	"os"

	"github.com/nico/go-bt-evolve/internal/llm"
)

// EvaluatorConfig controls which LLM backend the evaluator uses.
type EvaluatorConfig struct {
	UseDeepSeek bool   // true = deepseek-v4-pro, false = local Ollama
	APIKey      string // DeepSeek API key
}

// DefaultEvalConfig returns evaluator config preferring DeepSeek when available.
func DefaultEvalConfig() EvaluatorConfig {
	return EvaluatorConfig{
		UseDeepSeek: os.Getenv("DEEPSEEK_API_KEY") != "",
		APIKey:      os.Getenv("DEEPSEEK_API_KEY"),
	}
}

// Evaluator wraps the LLM client used for fitness scoring.
type Evaluator struct {
	config   EvaluatorConfig
	ollama   llm.LLM
	deepseek *llm.DeepSeekClient
}

// NewEvaluator creates an evaluator with the configured backend.
func NewEvaluator(cfg EvaluatorConfig) *Evaluator {
	e := &Evaluator{config: cfg}
	if cfg.UseDeepSeek {
		e.deepseek = llm.NewDeepSeekClient(llm.DefaultDeepSeekConfig())
	} else {
		var err error
		e.ollama, err = llm.NewClient(llm.DefaultConfig())
		if err != nil { e.ollama = nil }
	}
	return e
}

// Generate sends a prompt to the configured LLM backend.
func (e *Evaluator) Generate(prompt string) (string, error) {
	if e.config.UseDeepSeek && e.deepseek != nil {
		return e.deepseek.Generate(prompt)
	}
	if e.ollama != nil {
		return e.ollama.Generate(prompt)
	}
	return "", fmt.Errorf("no LLM backend available")
}
