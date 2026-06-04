// Package engine — Stockfish evolution actions extracted from tree.go actionForName switch.
// Registers 4 actions: transposition table initialization, cached fitness, and storage.
package engine

import (
	"fmt"

	btcore "github.com/rvitorper/go-bt/core"
)

func init() {
	registerStockfishActions()
}

func registerStockfishActions() {
	RegisterAction("InitTranspositionTable", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.ChainState == nil {
			bb.ChainState = make(map[string]any)
		}
		bb.ChainState["tt_hits"] = 0
		bb.ChainState["tt_misses"] = 0
		bb.ChainState["killer_moves"] = []any{}
		bb.ChainState["history_scores"] = make(map[string]any)
		bb.ChainState["best_fitness"] = 0.0
		bb.ChainState["cycles_without_improvement"] = 0
		return 1
	})

	RegisterAction("LoadCachedFitness", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.ChainState != nil {
			if f, ok := bb.ChainState["cached_fitness"].(float64); ok {
				bb.CachedResult = fmt.Sprintf("cached_fitness:%.2f", f)
			}
		}
		return 1
	})

	RegisterAction("StoreInTranspositionTable", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.ChainState != nil {
			bb.ChainState["tt_hits"] = bb.ChainState["tt_hits"].(int) + 1
			// Store current fitness as cached
			if bb.Result != "" {
				bb.ChainState["cached_result"] = bb.Result
			}
		}
		return 1
	})

	// This is used as a Condition despite being in actionForName
	// The getCondition in registry handles this properly
	RegisterAction("hasCachedFitness", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.ChainState != nil {
			if _, ok := bb.ChainState["cached_fitness"]; ok {
				return 1
			}
		}
		return -1
	})
}
