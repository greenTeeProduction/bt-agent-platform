package llm

import (
	"context"
	"fmt"
	"time"
)

// ModelRole defines the role of a model in the ensemble.
type ModelRole int

const (
	RoleExplorer  ModelRole = iota // high throughput, local, generates many candidates
	RoleRefiner                    // high quality, cloud, refines top candidates
	RoleEvaluator                  // evaluates and scores outputs
)

func (r ModelRole) String() string {
	switch r {
	case RoleExplorer:
		return "explorer"
	case RoleRefiner:
		return "refiner"
	case RoleEvaluator:
		return "evaluator"
	default:
		return "unknown"
	}
}

// EnsembleConfig configures a multi-model ensemble.
type EnsembleConfig struct {
	Explorer  LLM // local Ollama — breadth
	Refiner   LLM // cloud DeepSeek — depth
	Evaluator LLM // either — scoring
}

// ModelEnsemble orchestrates multiple LLM backends for the evolution pipeline.
type ModelEnsemble struct {
	explorer  LLM
	refiner   LLM
	evaluator LLM
	stats     EnsembleStats
}

// EnsembleStats tracks per-role usage and performance.
type EnsembleStats struct {
	ExplorerCalls  int                   `json:"explorer_calls"`
	RefinerCalls   int                   `json:"refiner_calls"`
	EvaluatorCalls int                   `json:"evaluator_calls"`
	TotalTokens    int                   `json:"total_tokens"`
	AvgLatencyMs   map[ModelRole]float64 `json:"avg_latency_ms"`
}

// NewModelEnsemble creates an ensemble from configured backends.
func NewModelEnsemble(cfg EnsembleConfig) *ModelEnsemble {
	return &ModelEnsemble{
		explorer:  cfg.Explorer,
		refiner:   cfg.Refiner,
		evaluator: cfg.Evaluator,
		stats: EnsembleStats{
			AvgLatencyMs: make(map[ModelRole]float64),
		},
	}
}

// GenerateBreadth uses the explorer to generate multiple cheap candidates.
// Phase 1 of the AlphaEvolve pipeline: generate many mutations fast.
func (me *ModelEnsemble) GenerateBreadth(prompts []string) ([]string, error) {
	if me.explorer == nil {
		return nil, fmt.Errorf("no explorer model configured")
	}

	results := make([]string, len(prompts))
	for i, prompt := range prompts {
		start := time.Now()
		result, err := me.explorer.Generate(prompt)
		me.stats.ExplorerCalls++
		me.recordLatency(RoleExplorer, time.Since(start))
		if err != nil {
			results[i] = fmt.Sprintf("ERROR: %v", err)
		} else {
			results[i] = result
		}
	}
	return results, nil
}

// RefineDepth uses the refiner to deepen a promising candidate.
// Phase 2: take a top candidate and produce a high-quality variant.
func (me *ModelEnsemble) RefineDepth(prompt string) (string, error) {
	if me.refiner == nil {
		return "", fmt.Errorf("no refiner model configured")
	}
	start := time.Now()
	result, err := me.refiner.Generate(prompt)
	me.stats.RefinerCalls++
	me.recordLatency(RoleRefiner, time.Since(start))
	return result, err
}

// Evaluate scores a candidate using the evaluator model.
func (me *ModelEnsemble) Evaluate(prompt string) (string, error) {
	if me.evaluator == nil {
		// Fall back to explorer if no dedicated evaluator
		if me.explorer != nil {
			start := time.Now()
			result, err := me.explorer.Generate(prompt)
			me.stats.EvaluatorCalls++
			me.recordLatency(RoleEvaluator, time.Since(start))
			return result, err
		}
		return "", fmt.Errorf("no evaluator model configured")
	}
	start := time.Now()
	result, err := me.evaluator.Generate(prompt)
	me.stats.EvaluatorCalls++
	me.recordLatency(RoleEvaluator, time.Since(start))
	return result, err
}

// GenerateCtx delegates to the explorer with context (for the LLM interface).
func (me *ModelEnsemble) GenerateCtx(ctx context.Context, prompt string) (string, error) {
	if me.explorer == nil {
		return "", fmt.Errorf("no explorer model configured")
	}
	start := time.Now()
	result, err := me.explorer.GenerateCtx(ctx, prompt)
	me.stats.ExplorerCalls++
	me.recordLatency(RoleExplorer, time.Since(start))
	return result, err
}

// GenerateWithTimeout delegates to the explorer with timeout.
func (me *ModelEnsemble) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) {
	if me.explorer == nil {
		return "", fmt.Errorf("no explorer model configured")
	}
	start := time.Now()
	result, err := me.explorer.GenerateWithTimeout(prompt, timeout)
	me.stats.ExplorerCalls++
	me.recordLatency(RoleExplorer, time.Since(start))
	return result, err
}

// AnalyzeComplexity, GeneratePlan, Reflect delegate to refiner for quality.
func (me *ModelEnsemble) AnalyzeComplexity(task string) string {
	if me.refiner != nil {
		start := time.Now()
		result := me.refiner.AnalyzeComplexity(task)
		me.recordLatency(RoleRefiner, time.Since(start))
		return result
	}
	if me.explorer != nil {
		return me.explorer.AnalyzeComplexity(task)
	}
	return "medium"
}

func (me *ModelEnsemble) GeneratePlan(task, complexity string) string {
	if me.refiner != nil {
		return me.refiner.GeneratePlan(task, complexity)
	}
	if me.explorer != nil {
		return me.explorer.GeneratePlan(task, complexity)
	}
	return fmt.Sprintf("1. Analyze: %s\n2. Execute: %s\n3. Verify result", task, task)
}

func (me *ModelEnsemble) Reflect(task, outcome, plan string) (string, string) {
	if me.refiner != nil {
		return me.refiner.Reflect(task, outcome, plan)
	}
	if me.explorer != nil {
		return me.explorer.Reflect(task, outcome, plan)
	}
	return "task completed", "better error handling"
}

// Stats returns aggregate usage statistics.
func (me *ModelEnsemble) Stats() EnsembleStats {
	return me.stats
}

// recordLatency updates the rolling average latency for a role.
func (me *ModelEnsemble) recordLatency(role ModelRole, d time.Duration) {
	prev := me.stats.AvgLatencyMs[role]
	n := float64(me.callCount(role))
	ms := float64(d.Milliseconds())
	me.stats.AvgLatencyMs[role] = (prev*n + ms) / (n + 1)
}

func (me *ModelEnsemble) callCount(role ModelRole) float64 {
	switch role {
	case RoleExplorer:
		return float64(me.stats.ExplorerCalls)
	case RoleRefiner:
		return float64(me.stats.RefinerCalls)
	case RoleEvaluator:
		return float64(me.stats.EvaluatorCalls)
	default:
		return 0
	}
}

// ─── Rich Context Builder ───

// EvolutionContext holds rich context for LLM mutation prompts.
type EvolutionContext struct {
	CurrentTree        string             `json:"current_tree"`     // serialized tree
	CurrentFitness     float64            `json:"current_fitness"`  // scalar composite
	PriorSolutions     []PriorSolution    `json:"prior_solutions"`  // top-N from history
	EvaluatorBreakdown map[string]float64 `json:"eval_breakdown"`   // per-metric scores
	ResearchHints      []string           `json:"research_hints"`   // paper citations
	Domain             string             `json:"domain"`           // godev, research, etc.
	MutationHistory    []string           `json:"mutation_history"` // recent mutation descriptions
}

// PriorSolution represents a previously successful tree.
type PriorSolution struct {
	Fitness     float64 `json:"fitness"`
	Description string  `json:"description"`
	Serialized  string  `json:"serialized"`
	Generation  int     `json:"generation"`
}

// BuildMutationPrompt constructs a rich prompt for the LLM to generate mutations.
// Incorporates AlphaEvolve-style context: prior solutions, evaluator breakdown, research hints.
func BuildMutationPrompt(ctx EvolutionContext) string {
	prompt := fmt.Sprintf(`## Behavior Tree Evolution — Mutation Request

### Current Tree (fitness: %.1f, domain: %s)
%s

`, ctx.CurrentFitness, ctx.Domain, ctx.CurrentTree)

	// Evaluator breakdown
	if len(ctx.EvaluatorBreakdown) > 0 {
		prompt += "### Evaluator Breakdown\n"
		for metric, score := range ctx.EvaluatorBreakdown {
			prompt += fmt.Sprintf("- **%s**: %.1f/100\n", metric, score)
		}
		prompt += "\n"
	}

	// Prior solutions
	if len(ctx.PriorSolutions) > 0 {
		prompt += "### Top Prior Solutions\n"
		for i, sol := range ctx.PriorSolutions {
			prompt += fmt.Sprintf("%d. (fitness: %.1f, gen %d) — %s\n",
				i+1, sol.Fitness, sol.Generation, sol.Description)
		}
		prompt += "\n"
	}

	// Research context
	if len(ctx.ResearchHints) > 0 {
		prompt += "### Research Context\n"
		for _, hint := range ctx.ResearchHints {
			prompt += fmt.Sprintf("- %s\n", hint)
		}
		prompt += "\n"
	}

	// Mutation history
	if len(ctx.MutationHistory) > 0 {
		prompt += "### Recent Mutations\n"
		for _, m := range ctx.MutationHistory {
			prompt += fmt.Sprintf("- %s\n", m)
		}
		prompt += "\n"
	}

	prompt += `### Task
Propose one targeted mutation to improve the lowest-scoring dimension in the evaluator breakdown.
Choose from: add_before (prepend a Condition or Action), add_after (append), add_fallback (add fallback path),
replace_node (swap a node), remove_node (delete dead node), adjust_retries (change MaxRetries).

Respond in SEARCH/REPLACE format:
SEARCH: <node name or description to find>
REPLACE: <new node definition>
`
	return prompt
}

// DefaultResearchHints returns research paper citations for the evolution prompt.
func DefaultResearchHints() []string {
	return []string{
		"MCTS-AHD (ICML 2025): UCT-based exploration of underperforming candidates that population methods discard",
		"Hybrid LLM-GP (Tan et al., MDPI Robotics 2026): LLM supervises GP population, adapts mutation rates (0.10-0.50), 71.7% faster emergence",
		"LEAR (Northwestern 2025): LLMs as mutation operators in genetic programming",
		"EvoFlow (arXiv 2025): Niching evolutionary algorithm maintains population diversity, 1.23-29.86% improvement from heterogeneity",
		"AlphaEvolve (Google DeepMind 2025): MAP-Elites + evaluation cascade + model ensemble for algorithmic discovery",
	}
}
