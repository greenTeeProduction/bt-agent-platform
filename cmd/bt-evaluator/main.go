package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nico/go-bt-evolve/internal/evaluator"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btlog "github.com/nico/go-bt-evolve/internal/log"
	"github.com/nico/go-bt-evolve/internal/mcp"
	"github.com/nico/go-bt-evolve/internal/reflection"
	"github.com/nico/go-bt-evolve/internal/tracing"
)

func main() {
	btlog.Init()
	btlog.Info("bt-evaluator starting", "version", "1.0.0", "binary", "go-bt-evaluator")

	home, _ := os.UserHomeDir()
	refDir := filepath.Join(home, ".go-bt-reflections")

	refStore, _ := reflection.NewStore(refDir)
	treeStore, _ := evolution.NewTreeStore(refDir)
	tt, _ := evaluator.NewTranspositionTable(refDir, 1000)

	server := mcp.NewServer("go-bt-evaluator")

	// Tool 1: Evaluate current tree fitness (Stockfish eval function)
	server.RegisterTool("ev_evaluate", "Multi-dimensional fitness evaluation of the behavior tree (Stockfish-style)",
		map[string]mcp.Property{},
		nil,
		func(args json.RawMessage) *mcp.ToolResult {
			tree, err := treeStore.Load()
			if err != nil || tree == nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{
					Type: "text", Text: `{"error": "no tree loaded"}`,
				}}}
			}
			records, _ := refStore.LoadAll()
			fitness := evaluator.EvaluateTree(tree, records)

			result := map[string]interface{}{
				"success_rate":   fmt.Sprintf("%.1f%%", fitness.SuccessRate*100),
				"avg_duration_ms": fitness.AvgDurationMs,
				"node_count":     fitness.NodeCount,
				"stability":      fmt.Sprintf("%.2f", fitness.Stability),
				"path_coverage":  fmt.Sprintf("%.2f", fitness.PathCoverage),
				"composite":      fmt.Sprintf("%.1f", fitness.Composite),
				"total_tasks":    len(records),
			}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// Tool 2: Order mutations by priority (killer moves first)
	server.RegisterTool("ev_order_mutations", "Rank mutation candidates using Stockfish-style move ordering",
		map[string]mcp.Property{},
		nil,
		func(args json.RawMessage) *mcp.ToolResult {
			tree, _ := treeStore.Load()
			records, _ := refStore.LoadAll()
			fitness := evaluator.EvaluateTree(tree, records)

			candidates := evaluator.OrderMutations(tree, records, fitness)

			type cand struct {
				Operation string  `json:"operation"`
				Target    string  `json:"target"`
				Score     float64 `json:"score"`
				Reason    string  `json:"reason"`
			}
			var items []cand
			for _, c := range candidates {
				items = append(items, cand{
					Operation: c.Op.Operation,
					Target:    c.Op.Target,
					Score:     c.Score,
					Reason:    c.Reason,
				})
			}

			result := map[string]interface{}{
				"candidates": items,
				"total":      len(items),
			}
			data, _ := json.Marshal(result)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// Tool 3: Iterative deepening search for best mutation
	server.RegisterTool("ev_deepen", "Iterative deepening: progressively search deeper mutation combinations",
		map[string]mcp.Property{
			"max_depth": {Type: "integer", Description: "Maximum search depth (default: 2)"},
		},
		nil,
		func(args json.RawMessage) *mcp.ToolResult {
			var params struct {
				MaxDepth int `json:"max_depth"`
			}
			json.Unmarshal(args, &params)
			if params.MaxDepth == 0 {
				params.MaxDepth = 2
			}

			tree, _ := treeStore.Load()
			records, _ := refStore.LoadAll()

			result := evaluator.IterativeDeepening(tree, records, tt, params.MaxDepth)

			// Auto-save TT after every deepen so cache survives restarts
			if err := tt.Save(); err != nil {
				btlog.Info("tt auto-save failed", "error", err)
			}

			out := map[string]interface{}{
				"depth":           result.Depth,
				"base_composite":  fmt.Sprintf("%.1f", result.BaseFitness.Composite),
				"candidates_total": len(result.Candidates),
				"pruned":          result.PrunedCount,
				"tt_probes":       result.TTProbes,
				"tt_hits":         result.TTProbeHits,
			}
			if result.BestMutation != nil {
				out["best_op"] = result.BestMutation.Op.Operation
				out["best_target"] = result.BestMutation.Op.Target
				out["best_reason"] = result.BestMutation.Reason
			}
			if result.BestFitness != nil {
				out["best_composite"] = fmt.Sprintf("%.1f", result.BestFitness.Composite)
			}

			data, _ := json.Marshal(out)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// Tool 4: Transposition table stats
	server.RegisterTool("ev_tt_stats", "Transposition table statistics (cache hits, size)",
		map[string]mcp.Property{},
		nil,
		func(args json.RawMessage) *mcp.ToolResult {
			stats := map[string]interface{}{
				"entries": tt.Stats(),
				"max_size": 1000,
				"path":     filepath.Join(refDir, "transposition.json"),
			}
			data, _ := json.Marshal(stats)
			return &mcp.ToolResult{Content: []mcp.ContentItem{{Type: "text", Text: string(data)}}}
		})

	// Tool 5: Save TT to disk
	server.RegisterTool("ev_tt_save", "Persist transposition table to disk",
		map[string]mcp.Property{},
		nil,
		func(args json.RawMessage) *mcp.ToolResult {
			if err := tt.Save(); err != nil {
				return &mcp.ToolResult{Content: []mcp.ContentItem{{
					Type: "text", Text: fmt.Sprintf(`{"saved": false, "error": %q}`, err.Error()),
				}}}
			}
			return &mcp.ToolResult{Content: []mcp.ContentItem{{
				Type: "text", Text: fmt.Sprintf(`{"saved": true, "entries": %d}`, tt.Stats()),
			}}}
		})

	btlog.Info("bt-evaluator: 5 tools ready, listening on stdin")
	server.SetSecurity(true, os.Getenv("BT_API_KEY"))
	server.SetRateLimit(5, 10) // 5 req/s, burst 10 (evaluator is fast, no Ollama)
	server.SetMaxMessageSize(1 << 20) // 1 MB message size limit

	// ── Tracing: initialize global tracer ──
	tracingLogPath := filepath.Join(home, ".go-bt-evolve", "logs", "traces.log")
	if f, err := os.OpenFile(tracingLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		tracing.SetGlobalTracer(tracing.NewConsoleTracer("bt-evaluator", f))
	}

	if err := server.Run(); err != nil {
		btlog.Error("bt-evaluator: server error", "error", err)
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}
