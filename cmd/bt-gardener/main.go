package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nico/go-bt-evolve/internal/evaluator"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/gardener"
	"github.com/nico/go-bt-evolve/internal/llm"
	btlog "github.com/nico/go-bt-evolve/internal/log"
	"github.com/nico/go-bt-evolve/internal/reflection"

	"github.com/tmc/langchaingo/agents"
	"github.com/tmc/langchaingo/chains"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/prompts"
	"github.com/tmc/langchaingo/tools"
)

// --- Langchain tools wrapping the gardener ---

type GardenerStatusTool struct {
	registry *gardener.Registry
	metrics  *gardener.MetricsTracker
}

func (t *GardenerStatusTool) Name() string { return "gardener_status" }
func (t *GardenerStatusTool) Description() string {
	return "Get current status: tree count, metrics summary, improvement rate."
}
func (t *GardenerStatusTool) Call(ctx context.Context, input string) (string, error) {
	summary := t.metrics.Summary()
	summary["tree_count"] = t.registry.Count()
	data, _ := json.MarshalIndent(summary, "", "  ")
	return string(data), nil
}

type GardenerRunCycleTool struct {
	gardener *gardener.Gardener
}

func (t *GardenerRunCycleTool) Name() string { return "gardener_run_cycle" }
func (t *GardenerRunCycleTool) Description() string {
	return "Run one evolution cycle over ALL behavior trees. Returns per-tree fitness deltas."
}
func (t *GardenerRunCycleTool) Call(ctx context.Context, input string) (string, error) {
	results, err := t.gardener.RunCycle()
	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error()), nil
	}
	type r struct {
		Tree      string  `json:"tree"`
		Improved  bool    `json:"improved"`
		Delta     float64 `json:"delta"`
		Mutations int     `json:"mutations"`
	}
	var items []r
	for _, m := range results {
		items = append(items, r{Tree: m.TreeName, Improved: m.Improved, Delta: m.Delta, Mutations: m.Mutations})
	}
	data, _ := json.Marshal(map[string]interface{}{"trees": len(results), "results": items})
	return string(data), nil
}

type GardenerRecommendTool struct {
	registry *gardener.Registry
	refStore *reflection.Store
}

func (t *GardenerRecommendTool) Name() string { return "gardener_recommend" }
func (t *GardenerRecommendTool) Description() string {
	return "Analyze all trees and their fitness scores. Recommend which need urgent attention."
}
func (t *GardenerRecommendTool) Call(ctx context.Context, input string) (string, error) {
	entries := t.registry.List()
	records, _ := t.refStore.LoadAll()

	type rec struct {
		Tree    string  `json:"tree"`
		Fitness float64 `json:"fitness"`
		Nodes   int     `json:"nodes"`
		Action  string  `json:"action"`
	}
	var recs []rec
	for _, e := range entries {
		if e.Tree == nil {
			continue
		}
		f := evaluator.EvaluateTree(e.Tree, records)
		action := "monitor"
		switch {
		case f.Composite < 40:
			action = "URGENT: prune dead paths, add retries"
		case f.Composite < 65:
			action = "improve: increase retries, add validation"
		case f.Composite < 80:
			action = "refine: tune mutation depth, add fallbacks"
		default:
			action = "healthy — monitor"
		}
		recs = append(recs, rec{Tree: e.Name, Fitness: f.Composite, Nodes: evolution.CountNodes(e.Tree), Action: action})
	}
	data, _ := json.Marshal(map[string]interface{}{"total": len(recs), "recommendations": recs})
	return string(data), nil
}

func main() {
	btlog.Init()
	btlog.Info("bt-gardener starting", "version", "1.0.0", "binary", "go-bt-gardener")

	home, _ := os.UserHomeDir()
	refDir := filepath.Join(home, ".go-bt-reflections")
	metricsDir := filepath.Join(home, ".go-bt-gardener")

	os.MkdirAll(metricsDir, 0755)

	refStore, _ := reflection.NewStore(refDir)
	registry := gardener.NewRegistry(refDir)
	metricsTracker, _ := gardener.NewMetricsTracker(metricsDir)
	tt, _ := evaluator.NewTranspositionTable(refDir, 2000)

	g := gardener.NewGardener(gardener.Config{
		Registry:       registry,
		MetricsTracker: metricsTracker,
		RefStore:       refStore,
		TT:             tt,
		Interval:       5 * time.Minute,
		MaxMutations:   2,
		UseRealLLM:     false, // mock for speed — idempotency guards prevent bloat
	})

	// Ollama LLM for langchain agent — uses platform config
	llmCfg := llm.DefaultConfig()
	ollamaLLM, err := ollama.New(
		ollama.WithModel(llmCfg.Model),
		ollama.WithServerURL(llmCfg.ServerURL),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: ollama: %v\n", err)
		os.Exit(1)
	}

	// Langchain tools
	agentTools := []tools.Tool{
		&GardenerStatusTool{registry: registry, metrics: metricsTracker},
		&GardenerRunCycleTool{gardener: g},
		&GardenerRecommendTool{registry: registry, refStore: refStore},
	}

	prompt := prompts.NewPromptTemplate(
		`You are a Tree Gardener — a meta-agent that monitors and evolves behavior trees 24/7.

Available tools:
- gardener_status: see current state of all trees and metrics
- gardener_run_cycle: run one evolution cycle across ALL trees
- gardener_recommend: analyze trees and recommend which need attention

WORKFLOW:
1. Start with gardener_status to see current state
2. Use gardener_recommend to identify trees needing improvement
3. Run gardener_run_cycle to evolve trees
4. Check gardener_status again to verify improvements

{{.agent_scratchpad}}
Question: {{.input}}`,
		[]string{"input", "agent_scratchpad"},
	)

	agent := agents.NewOneShotAgent(ollamaLLM, agentTools, agents.WithPrompt(prompt))
	executor := agents.NewExecutor(agent, agents.WithMaxIterations(5))

	btlog.Info("bt-gardener: initialized", "trees", registry.Count(), "max_mutations", 2)
	fmt.Fprintf(os.Stderr, "bt-gardener: %d trees, 3 tools, 5min cycle, langchain analysis every 5th cycle\n", registry.Count())
	fmt.Fprintf(os.Stderr, "Metrics dir: %s\n", metricsDir)

	// Run initial cycle immediately
	fmt.Fprintf(os.Stderr, "\n=== Initial Cycle @ %s ===\n", time.Now().Format("15:04:05"))
	results, _ := g.RunCycle()
	for _, r := range results {
		mark := "  "
		if r.Improved {
			mark = "✓ "
		}
		fmt.Fprintf(os.Stderr, "%s%-25s %.1f → %.1f (Δ%+.1f) mut=%d\n",
			mark, r.TreeName, r.BaseFitness, r.NewFitness, r.Delta, r.Mutations)
	}
	metricsTracker.Save()

	// 24/7 loop
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	cycleCount := 1
	for range ticker.C {
		cycleCount++
		fmt.Fprintf(os.Stderr, "\n=== Cycle %d @ %s ===\n", cycleCount, time.Now().Format("15:04:05"))

		results, err := g.RunCycle()
		if err != nil {
			btlog.Error("bt-gardener: cycle failed", "error", err, "cycle", cycleCount)
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			continue
		}

		improved := 0
		for _, r := range results {
			if r.Improved {
				improved++
				fmt.Fprintf(os.Stderr, "✓ %-25s %.1f → %.1f (+%.1f) mut=%d\n",
					r.TreeName, r.BaseFitness, r.NewFitness, r.Delta, r.Mutations)
			}
		}
		btlog.Info("bt-gardener: cycle complete", "cycle", cycleCount, "improved", improved, "total", len(results))
		fmt.Fprintf(os.Stderr, "Improved: %d/%d\n", improved, len(results))

		// Every 5 cycles, run langchain analysis
		if cycleCount%5 == 0 && len(results) > 0 {
			fmt.Fprintf(os.Stderr, "\n--- Langchain Analysis ---\n")
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			result, err := chains.Call(ctx, executor, map[string]any{"input": "analyze the current state and suggest which trees to focus on next"})
			cancel()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Agent error: %v\n", err)
			} else if output, ok := result["output"].(string); ok {
				fmt.Fprintf(os.Stderr, "Agent: %s\n", truncateStr(output, 500))
			}
		}

		metricsTracker.Save()
		sum := metricsTracker.Summary()
		fmt.Fprintf(os.Stderr, "Total: %v cycles | Rate: %v\n", sum["total_cycles"], sum["improvement_rate"])
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Ensure unused imports don't error
var _ = llm.DefaultConfig
