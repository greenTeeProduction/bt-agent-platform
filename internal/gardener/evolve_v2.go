package gardener

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nico/go-bt-evolve/internal/benchmark"
	"github.com/nico/go-bt-evolve/internal/evaluator"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
)

// EvolveV2Config controls the v2 evolution pipeline: a structural quick-check
// cascade, block-protected candidate filtering, and per-candidate pre-scored
// mutation application.
//
// Earlier revisions declared MAP-Elites, Pareto, island-model, and ensemble
// stages here; they were constructed and discarded without ever influencing
// selection, so they were removed rather than kept as dead weight.
type EvolveV2Config struct {
	// Tiered evaluation cascade
	CascadeCfg evaluator.CascadeConfig

	// Evolution blocks (protect stable nodes)
	BlocksEnabled bool
	BlockConfig   evolution.BlockConfig

	// Use real LLM or mock
	UseRealLLM bool
}

// DefaultEvolveV2Config returns sensible defaults for the v2 pipeline.
func DefaultEvolveV2Config() EvolveV2Config {
	return EvolveV2Config{
		CascadeCfg:    evaluator.DefaultCascadeConfig(),
		BlocksEnabled: true,
		BlockConfig:   evolution.DefaultBlockConfig(),
		UseRealLLM:    false, // use mock by default for speed
	}
}

// evolveTreeV2 runs the v2 evolution pipeline on a single tree:
// cascade quick-check → ordered candidates → block filter → per-candidate
// benchmark + pre-score + quality gate → apply → validation-gated persist.
func (g *Gardener) evolveTreeV2(entry TreeEntry, cfg EvolveV2Config) CycleMetrics {
	tree := entry.Tree
	if tree == nil {
		return CycleMetrics{TreeName: entry.Name, Improved: false}
	}

	allRecords, _ := g.cfg.RefStore.LoadAll()
	records := evolution.FilterByTreeName(allRecords, entry.Name)
	baseFitness := evaluator.EvaluateTree(tree, records)
	nodesBefore := evolution.CountNodes(tree)

	// ── Evaluation cascade — structural quick check first ──
	quickScore := evaluator.StructuralQuickEval(tree)
	if quickScore < cfg.CascadeCfg.QuickThreshold {
		return CycleMetrics{
			TreeName: entry.Name, Improved: false,
			BaseFitness: baseFitness.Composite, NewFitness: baseFitness.Composite,
			NodesBefore: nodesBefore, NodesAfter: nodesBefore,
		}
	}

	// ── Generate and filter mutations ──
	candidates := evaluator.OrderMutations(tree, records, baseFitness)

	// Evolution blocks — filter mutations targeting frozen blocks
	if cfg.BlocksEnabled && len(candidates) > 0 {
		mutationOps := make([]evolution.MutationOp, len(candidates))
		for i, c := range candidates {
			mutationOps[i] = c.Op
		}
		mutationOps = cfg.BlockConfig.FilterMutations(mutationOps, tree)
		// Rebuild candidates from filtered ops
		filtered := make([]evaluator.MutationCandidate, 0, len(mutationOps))
		for _, c := range candidates {
			for _, op := range mutationOps {
				if c.Op.Operation == op.Operation && c.Op.Target == op.Target {
					filtered = append(filtered, c)
					break
				}
			}
		}
		if len(filtered) < len(candidates) {
			candidates = filtered
		}
	}

	// ── Apply mutations with benchmark validation ──
	suite := benchmark.SuiteForTree(entry.Name)
	var selectedLLM llm.LLM
	if cfg.UseRealLLM {
		selectedLLM = benchmark.DefaultLLM()
	} else {
		selectedLLM = benchmark.DefaultMock()
	}

	applied := 0
	rejected := 0
	rollbacks := 0
	originalTree := cloneTreeForGardener(tree)
	currentFitness := baseFitness
	gateDisabled := g.cfg.Gate != nil && g.cfg.Gate.IsDisabledFor(entry.Name)
	if gateDisabled {
		// Fail closed: a disabled gate means evolution is paused for this tree
		// until process restart — skip every candidate, apply nothing ungated.
		slog.Warn("gardener/v2: quality gate DISABLED — mutations SKIPPED (fail-closed), evolution paused until restart",
			"tree", entry.Name, "consecutive_fails", g.cfg.Gate.FailCountFor(entry.Name))
	}
	for i := 0; !gateDisabled && i < len(candidates) && applied < g.cfg.MaxMutations; i++ {
		if candidates[i].Score < 0.45 {
			break
		}

		score := benchmark.QuickValidate(tree, suite, selectedLLM, []evolution.MutationOp{candidates[i].Op})
		if score < 0 {
			rejected++
			continue
		}

		// Pre-score on a clone before mutating the live tree. This rejects no-op
		// mutations and candidates whose estimated post-mutation fitness regresses.
		candidateTree := cloneTreeForGardener(tree)
		if evolution.ApplyMutations(candidateTree, []evolution.MutationOp{candidates[i].Op}) == 0 {
			rejected++
			continue
		}
		candidateFitness := evaluator.EvaluateTree(candidateTree, records)
		if candidateFitness.Composite < currentFitness.Composite-0.0001 {
			rejected++
			continue
		}
		if g.cfg.Gate != nil { // gateDisabled is always false inside this loop (fail-closed skip above)
			gateResult := g.cfg.Gate.ValidateFor(entry.Name, currentFitness.Composite, candidateFitness.Composite)
			if gateResult != evolution.GateAccepted {
				rejected++
				if gateResult == evolution.GateRollback {
					rollbacks++
				}
				continue
			}
		}

		if evolution.ApplyMutations(tree, []evolution.MutationOp{candidates[i].Op}) > 0 {
			applied++
			currentFitness = candidateFitness
		}
	}

	newFitness := evaluator.EvaluateTree(tree, records)
	nodesAfter := evolution.CountNodes(tree)
	if newFitness.Composite < baseFitness.Composite-0.0001 {
		if originalTree != nil {
			*tree = *originalTree
		}
		newFitness = baseFitness
		nodesAfter = nodesBefore
		applied = 0
		rollbacks++
	}
	improved := newFitness.Composite > baseFitness.Composite
	if applied > 0 {
		// ── Validation gate — prevent persisting evolved trees that fail
		// quality thresholds. A rejection skips this tree only.
		gateErr := ValidationGate(entry.Name, entry.Name, g.cfg.ValidationGate)
		if gateErr != nil {
			slog.Warn("gardener/v2: validation gate rejected, skipping deployment", "error", gateErr)
			// Restore the in-memory tree to its pre-cycle state so that
			// rejected mutations do not accumulate across cycles (baseline-leak fix).
			*tree = *originalTree
			newFitness = baseFitness
			improved = false
			nodesAfter = nodesBefore
			applied = 0
		}
	}
	if applied > 0 {
		_ = g.cfg.Registry.SaveTree(TreeEntry{Name: entry.Name, Tree: tree, FilePath: entry.FilePath})
	}

	return CycleMetrics{
		TreeName: entry.Name, Timestamp: time.Now().Unix(),
		BaseFitness: baseFitness.Composite, NewFitness: newFitness.Composite,
		Delta:     newFitness.Composite - baseFitness.Composite,
		Mutations: applied, NodesBefore: nodesBefore, NodesAfter: nodesAfter,
		Improved:   improved,
		Rejections: rejected,
		Rollbacks:  rollbacks,
	}
}

// RunCycleV2 executes one full evolution cycle using the v2 pipeline.
func (g *Gardener) RunCycleV2(cfg EvolveV2Config) ([]CycleMetrics, error) {
	entries := g.cfg.Registry.List()
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	results := make([]CycleMetrics, 0, len(entries))

	for _, entry := range entries {
		if !entry.Active {
			continue
		}

		start := time.Now()
		metrics := g.evolveTreeV2(entry, cfg)
		metrics.DurationMs = time.Since(start).Milliseconds()
		results = append(results, metrics)

		g.cfg.MetricsTracker.Record(metrics)
		// Persist after every tree so a mid-cycle crash or SIGTERM loses at
		// most one tree's result, not the whole cycle.
		_ = g.cfg.MetricsTracker.Save()
	}

	// ── SLO metrics collection ──
	// Collect per-agent SLO data after each cycle for dashboard export.
	sloData := CollectAgentSLOs()
	if len(sloData) > 0 {
		sloPath := filepath.Join(filepath.Dir(g.cfg.MetricsTracker.path), "slo-metrics.json")
		if data, err := json.MarshalIndent(sloData, "", "  "); err == nil {
			tmp := sloPath + ".tmp"
			if err := os.WriteFile(tmp, data, 0644); err == nil {
				_ = os.Rename(tmp, sloPath)
			}
		}
	}

	_ = g.cfg.MetricsTracker.Save()
	return results, nil
}

// ─── Helpers (avoid import cycles, keep in gardener package) ───

func cloneTreeForGardener(t *evolution.SerializableNode) *evolution.SerializableNode {
	if t == nil {
		return nil
	}
	c := &evolution.SerializableNode{
		Type:        t.Type,
		Name:        t.Name,
		Description: t.Description,
		MaxRetries:  t.MaxRetries,
		TimeoutMs:   t.TimeoutMs,
	}
	if t.Metadata != nil {
		c.Metadata = cloneMetadataForGardener(t.Metadata)
	}
	if t.Edges != nil {
		c.Edges = make([]evolution.TypedEdge, len(t.Edges))
		copy(c.Edges, t.Edges)
	}
	for _, ch := range t.Children {
		c.Children = append(c.Children, *cloneTreeForGardener(&ch))
	}
	return c
}

func cloneMetadataForGardener(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		switch vv := v.(type) {
		case []any:
			cp := make([]any, len(vv))
			copy(cp, vv)
			out[k] = cp
		case []string:
			cp := make([]string, len(vv))
			copy(cp, vv)
			out[k] = cp
		case map[string]any:
			out[k] = cloneMetadataForGardener(vv)
		default:
			out[k] = v
		}
	}
	return out
}
