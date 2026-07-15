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

	// SelectorOrdering, when true, applies learned Selector child ordering from
	// the durable telemetry at Config.SelectorStatsPath before an evolved tree
	// is persisted (Selector-ordering optimizer milestone 4). Fallback and
	// AlwaysSucceed children stay last, preserving short-circuit semantics.
	// Off by default so the pass is a no-op until explicitly enabled.
	SelectorOrdering bool
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
	records := recordsForEntry(allRecords, entry)
	baseFitness := evaluator.EvaluateTree(tree, records)
	nodesBefore := evolution.CountNodes(tree)

	// Evidence gate (ported from the retired v1 pipeline, extended for
	// personal trees in ADR-010 Phase 5): a tree with no reflection records
	// has no run-derived fitness gradient, so mutation is a blind coin flip
	// that only burns benchmark compute. Personal trees use strict filtering
	// (recordsForEntry), so a freshly compiled tree relies on its seed
	// reflection to pass this gate.
	if len(records) == 0 && !g.cfg.EvolveWithoutReflections {
		return CycleMetrics{
			TreeName: entry.Name, Improved: false,
			BaseFitness: baseFitness.Composite, NewFitness: baseFitness.Composite,
			NodesBefore: nodesBefore, NodesAfter: nodesBefore,
			SkippedNoEvidence: true,
		}
	}

	// Bloat cap (ported from v1): only stop evolution at extreme growth
	// (20x the tree's baseline) — trees should be allowed to grow.
	if nodesBefore > baseNodeCount(entry.Name)*20 {
		return CycleMetrics{
			TreeName: entry.Name, Improved: false,
			BaseFitness: baseFitness.Composite, NewFitness: baseFitness.Composite,
			NodesBefore: nodesBefore, NodesAfter: nodesBefore,
		}
	}

	// ── Evaluation cascade — structural quick check first ──
	quickScore := evaluator.StructuralQuickEval(tree)
	if quickScore < cfg.CascadeCfg.QuickThreshold {
		return CycleMetrics{
			TreeName: entry.Name, Improved: false,
			BaseFitness: baseFitness.Composite, NewFitness: baseFitness.Composite,
			NodesBefore: nodesBefore, NodesAfter: nodesBefore,
		}
	}

	// Crisis detection (Stage 3.5, ported from v1 so the production wiring in
	// cmd/bt-gardener is live again): on diversity collapse or stagnation,
	// boost the mutation budget for this cycle. Complements the reactive
	// QualityGate below.
	maxMutations := g.cfg.MaxMutations
	crisisIntervened := false
	if g.cfg.CrisisDetector != nil {
		state := evolution.CrisisState{
			TreeName:            entry.Name,
			CurrentFitness:      baseFitness.Composite,
			BehavioralDiversity: g.cfg.CrisisDetector.LastDiversity(),
		}
		if crisis, reason := g.cfg.CrisisDetector.Detect(state); crisis {
			action := g.cfg.CrisisDetector.Intervene(entry.Name, reason)
			maxMutations = g.cfg.MaxMutations * 2
			if maxMutations < 1 {
				maxMutations = 1
			}
			crisisIntervened = true
			slog.Info("gardener/v2: crisis intervention — boosting mutation budget",
				"tree", entry.Name, "reason", reason,
				"stagnation_epochs", action.StagnationEpochs,
				"budget", maxMutations)
		}
	}

	// ── Generate and filter mutations ──
	// Personal trees bias against (and record into) the owning user's bank.
	bank := g.bankFor(entry)
	candidates := biasCandidatesWithExperience(bank, tree, evaluator.OrderMutations(tree, records, baseFitness))

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

	// Durable pre-mutation snapshot (Q2 Evolvability milestone 1): originalTree
	// below only lives in-memory for this cycle, so a process crash mid-cycle
	// loses the pre-mutation state entirely. Persist it to g.cfg.SnapshotDir
	// (when configured) so RestoreTree can recover it after a crash.
	if g.cfg.SnapshotDir != "" {
		if _, err := evolution.SnapshotTree(tree, entry.Name, g.cfg.SnapshotDir); err != nil {
			slog.Warn("gardener/v2: pre-mutation snapshot failed", "tree", entry.Name, "error", err)
		}
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
	for i := 0; !gateDisabled && i < len(candidates) && applied < maxMutations; i++ {
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

		// Meta-validation — the fitness/quality/SLO gates above only look at
		// composite scores, so a structurally broken candidate (empty
		// selector, unbounded retry, expert antipattern) can still clear them.
		// Consult the structural safety layer last, right before commit.
		if g.cfg.MetaValidator != nil {
			metaReport := g.cfg.MetaValidator.ValidateMutation(tree, candidateTree, currentFitness.Composite, candidateFitness.Composite)
			if metaReport.Decision == evolution.MetaReject {
				rejected++
				continue
			}
		}

		if evolution.ApplyMutations(tree, []evolution.MutationOp{candidates[i].Op}) > 0 {
			applied++
			if bank != nil {
				// Record before advancing currentFitness so the delta is the
				// per-candidate improvement, not cumulative across the cycle.
				if err := bank.AddFromMutation(tree, candidates[i].Op, currentFitness.Composite, candidateFitness.Composite, nil); err != nil {
					slog.Warn("gardener/v2: recording mutation experience failed", "tree", entry.Name, "error", err)
				}
			}
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
	// ── Learned Selector ordering (milestone 4) — apply real-telemetry child
	// ordering to the evolved tree just before it is persisted. Flag-gated and
	// seeded from the durable stats; fallback/AlwaysSucceed children stay last
	// so Selector short-circuit semantics are preserved. A reorder is itself a
	// persistable change, so it forces a save even when no mutation applied.
	reordered := g.applyLearnedSelectorOrdering(tree, cfg)
	if applied > 0 || reordered > 0 {
		_ = g.cfg.Registry.SaveTree(TreeEntry{Name: entry.Name, Tree: tree, FilePath: entry.FilePath})
	}

	// A successful crisis intervention resets the stagnation counter so the
	// next cycle starts from a clean slate.
	if crisisIntervened && applied > 0 && g.cfg.CrisisDetector != nil {
		g.cfg.CrisisDetector.ResetStagnation(entry.Name)
	}

	return CycleMetrics{
		TreeName: entry.Name, Timestamp: time.Now().Unix(),
		BaseFitness: baseFitness.Composite, NewFitness: newFitness.Composite,
		Delta:     newFitness.Composite - baseFitness.Composite,
		Mutations: applied, NodesBefore: nodesBefore, NodesAfter: nodesAfter,
		Improved:   improved,
		Rejections: rejected,
		Rollbacks:  rollbacks,

		CrisisIntervention: crisisIntervened,
		CrisisIntervened:   crisisIntervened,
		MutationBudget:     maxMutations,
	}
}

// applyLearnedSelectorOrdering seeds a SelectorOptimizer from the durable
// telemetry at Config.SelectorStatsPath and reorders every Selector's children
// in tree by their learned success rate, keeping fallback/AlwaysSucceed children
// last. It is a no-op (returns 0) unless the pass is enabled and a stats path is
// configured. Returns the number of Selector nodes whose ordering changed.
func (g *Gardener) applyLearnedSelectorOrdering(tree *evolution.SerializableNode, cfg EvolveV2Config) int {
	if !cfg.SelectorOrdering || g.cfg.SelectorStatsPath == "" || tree == nil {
		return 0
	}
	so := evolution.NewSelectorOptimizer(evolution.OrderBySuccessRate)
	if err := so.LoadSelectorStats(g.cfg.SelectorStatsPath); err != nil {
		slog.Warn("gardener/v2: loading selector stats failed, skipping ordering",
			"path", g.cfg.SelectorStatsPath, "error", err)
		return 0
	}
	return so.ApplyLearnedOrdering(tree)
}

const (
	// experienceBiasTopK bounds how many prior experiences candidate biasing
	// retrieves per cycle.
	experienceBiasTopK = 5

	// experienceBiasMinQuality is the floor below which a past entry is not
	// considered proven enough to bias heuristic ordering.
	experienceBiasMinQuality = 0.5

	// experienceBiasBoost scales the score bump a matching candidate receives,
	// weighted by the matched entry's quality score.
	experienceBiasBoost = 0.15
)

// biasCandidatesWithExperience reorders OrderMutations candidates using
// ExperienceBank retrieval: a candidate whose op/target matches a high-quality
// past entry for the same tree type gets its score boosted (proportional to the
// entry's quality) and the matched entry is marked reused. Non-matching
// candidates keep their relative heuristic order (stable sort). A nil or empty
// bank — or one with no usable matches — leaves the ordering untouched.
func biasCandidatesWithExperience(bank *evolution.ExperienceBank, tree *evolution.SerializableNode, candidates []evaluator.MutationCandidate) []evaluator.MutationCandidate {
	hints := evolution.RetrieveExperienceHints(bank, tree, experienceBiasTopK)
	if len(hints) == 0 || len(candidates) == 0 {
		return candidates
	}

	// Index hints by op/target, keeping the highest-quality entry per key.
	type opTarget struct{ op, target string }
	best := make(map[opTarget]evolution.ExperienceEntry, len(hints))
	for _, h := range hints {
		if h.QualityScore < experienceBiasMinQuality {
			continue
		}
		key := opTarget{h.MutationOp, h.TargetNode}
		if cur, ok := best[key]; !ok || h.QualityScore > cur.QualityScore {
			best[key] = h
		}
	}
	if len(best) == 0 {
		return candidates
	}

	biased := make([]evaluator.MutationCandidate, len(candidates))
	copy(biased, candidates)
	reused := make(map[string]bool)
	for i := range biased {
		h, ok := best[opTarget{biased[i].Op.Operation, biased[i].Op.Target}]
		if !ok {
			continue
		}
		biased[i].Score += experienceBiasBoost * h.QualityScore
		reused[h.ID] = true
	}
	if len(reused) == 0 {
		return candidates
	}

	sort.SliceStable(biased, func(i, j int) bool {
		return biased[i].Score > biased[j].Score
	})

	reusedIDs := make([]string, 0, len(reused))
	for id := range reused {
		reusedIDs = append(reusedIDs, id)
	}
	if err := bank.MarkReused(reusedIDs); err != nil {
		slog.Warn("gardener/v2: marking experience entries reused failed", "error", err)
	}
	return biased
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
