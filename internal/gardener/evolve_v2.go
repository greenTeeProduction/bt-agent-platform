package gardener

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/benchmark"
	"github.com/nico/go-bt-evolve/internal/evaluator"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/knowledge"
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
	// the durable per-tree telemetry (agent.SelectorStatsFile, falling back to
	// Config.SelectorStatsPath) before an evolved tree is persisted
	// (Selector-ordering optimizer milestone 4). Fallback and AlwaysSucceed
	// children stay last, preserving short-circuit semantics. Off by default so
	// the pass is a no-op until explicitly enabled.
	SelectorOrdering bool

	// SelectorOrderingStrategy picks the ranking algorithm applyLearnedSelectorOrdering
	// uses when SelectorOrdering is enabled — evolution.OrderByIG/OrderByGini/
	// OrderByHybrid were otherwise unreachable in production despite being
	// fully implemented (Selector-reordering consolidation milestone 4). A
	// zero value falls back to evolution.OrderBySuccessRate, preserving the
	// pass's original behavior.
	SelectorOrderingStrategy evolution.SelectorOrderingStrategy

	// DTOrdering, when true, applies entropy/Gini-based BTOptimizer reordering
	// from the durable per-tree DTAnalyzer telemetry (agent.DecisionTreeStatsFile,
	// falling back to Config.DTStatsPath) before an evolved tree is persisted —
	// the sibling of SelectorOrdering above, using information gain instead of
	// raw success rate. Fallback and AlwaysSucceed children stay last,
	// preserving short-circuit semantics. Off by default so the pass is a
	// no-op until explicitly enabled.
	DTOrdering bool
}

// DefaultEvolveV2Config returns sensible defaults for the v2 pipeline.
func DefaultEvolveV2Config() EvolveV2Config {
	return EvolveV2Config{
		CascadeCfg:               evaluator.DefaultCascadeConfig(),
		BlocksEnabled:            true,
		BlockConfig:              evolution.DefaultBlockConfig(),
		UseRealLLM:               false, // use mock by default for speed
		SelectorOrderingStrategy: evolution.OrderBySuccessRate,
	}
}

// transpositionTableMaxSize bounds the number of cached (tree,task)
// evaluations the gardener's transposition table keeps in memory/on disk.
const transpositionTableMaxSize = 5000

// deepSearchMaxDepth bounds how many mutations deep
// evaluator.IterativeDeepening searches per cycle (Q2 Evolvability milestone
// 2) once a transposition table is configured.
const deepSearchMaxDepth = 2

// transpositionTable lazily constructs (and caches) the Stockfish-style
// transposition table from Config.TranspositionTablePath, so cached
// (tree,task) evaluations survive gardener restarts instead of only the
// standalone bt-evaluator binary persisting them (Q2 Evolvability milestone
// 1). Returns nil when TranspositionTablePath is unset or the table failed
// to open.
func (g *Gardener) transpositionTable() *evaluator.TranspositionTable {
	if g.cfg.TranspositionTablePath == "" {
		return nil
	}
	g.ttMu.Lock()
	defer g.ttMu.Unlock()
	if g.tt != nil {
		return g.tt
	}
	tt, err := evaluator.NewTranspositionTable(g.cfg.TranspositionTablePath, transpositionTableMaxSize)
	if err != nil {
		slog.Warn("gardener/v2: opening transposition table failed", "path", g.cfg.TranspositionTablePath, "error", err)
		return nil
	}
	g.tt = tt
	return g.tt
}

// diversityGridEliteSize bounds the elite individuals evolution.Elites()
// returns per tree diversity archive; the grid itself grows one cell per
// occupied behavioral niche regardless of this cap.
const diversityGridEliteSize = 20

// treeDiversityGrid lazily creates (and caches) the per-tree MAP-Elites
// archive that tracks behavioral diversity of tree structures produced for
// name across evolution cycles, mirroring the transpositionTable lazy-
// singleton pattern above. Same name always returns the same *MAPElitesGrid
// instance; distinct names get distinct grids.
func (g *Gardener) treeDiversityGrid(name string) *evolution.MAPElitesGrid {
	g.diversityGridsMu.Lock()
	defer g.diversityGridsMu.Unlock()
	if g.diversityGrids == nil {
		g.diversityGrids = make(map[string]*evolution.MAPElitesGrid)
	}
	grid, ok := g.diversityGrids[name]
	if !ok {
		grid = evolution.NewMAPElitesGrid(diversityGridEliteSize)
		g.diversityGrids[name] = grid
	}
	return grid
}

// recordDiversityObservation feeds tree's behavioral descriptor and fitness
// into name's diversity grid, so BehavioralDiversity (evolveTreeV2) can
// eventually read a live DiversityScore instead of the placeholder 0.
func (g *Gardener) recordDiversityObservation(name string, tree *evolution.SerializableNode, fitness float64) {
	grid := g.treeDiversityGrid(name)
	desc := evolution.Descriptor(tree, "")
	grid.Insert(desc, &evolution.Individual{Tree: tree, Fitness: fitness})
}

// lastFailureTask returns the Task text of the most recent (by Timestamp)
// record in records whose Outcome is evolution.Failure, so ExperienceBank
// entries recorded during this cycle can carry ADR-109 failing-task context
// for later retrieval-by-failure-semantics. Returns "" when no record failed.
func lastFailureTask(records []evolution.Record) string {
	var latest evolution.Record
	found := false
	for _, r := range records {
		if r.Outcome != evolution.Failure {
			continue
		}
		if !found || r.Timestamp >= latest.Timestamp {
			latest = r
			found = true
		}
	}
	if !found {
		return ""
	}
	return latest.Task
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

	// Transposition-table caching (Q2 Evolvability milestone 1): cache this
	// cycle's (tree,task) evaluation before any gate can short-circuit the
	// rest of the pipeline, so every processed tree contributes an entry.
	if tt := g.transpositionTable(); tt != nil {
		tt.Store(tree, entry.Name, evaluator.TranspositionEntry{
			SuccessRate: baseFitness.SuccessRate,
			DurationMs:  baseFitness.AvgDurationMs,
		})
	}

	// Evidence gate (ported from the retired v1 pipeline, extended for
	// personal trees in ADR-133 Phase 5): a tree with no reflection records
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
		// BehavioralDiversity is read from this tree's own MAP-Elites archive
		// (treeDiversityGrid), fed each cycle by recordDiversityObservation
		// below — replacing the old wiring that fed the detector its OWN
		// LastDiversity() back, which nothing else ever set, permanently
		// zero, and made the diversity-collapse branch dead code pretending
		// to be live (2026-07-23 review gap 6). A grid with no observations
		// yet still scores 0, which Detect correctly treats as "no data".
		state := evolution.CrisisState{
			TreeName:            entry.Name,
			CurrentFitness:      baseFitness.Composite,
			BehavioralDiversity: g.treeDiversityGrid(entry.Name).DiversityScore(),
		}
		if crisis, reason := g.cfg.CrisisDetector.Detect(state); crisis {
			action := g.cfg.CrisisDetector.Intervene(entry.Name, reason)
			rate := action.EmergencyRate
			if rate < 0 {
				rate = 0
			} else if rate >= 1 {
				rate = 0.99
			}
			maxMutations = int(math.Ceil(float64(g.cfg.MaxMutations) / (1 - rate)))
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
	candidates := biasCandidatesWithExperience(bank, tree, evaluator.OrderMutations(tree, records, baseFitness), lastFailureTask(records))

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

	// Fail closed AND self-heal (Q2 Evolvability milestone 2): a disabled gate
	// means this tree regressed ConsecutiveFails times in a row. Rather than
	// merely pausing mutations and leaving the tree frozen in that regressed
	// state until process restart, restore its last-known-good pre-mutation
	// snapshot right now via Registry.RollbackTree. This check must happen
	// before the pre-mutation snapshot below — snapshotting the current
	// (regressed) state first would make it the new "most recent" revision
	// and defeat the rollback.
	if g.cfg.Gate != nil && g.cfg.Gate.IsDisabledFor(entry.Name) {
		slog.Warn("gardener/v2: quality gate DISABLED — automatic rollback triggered (fail-closed), evolution paused until restart",
			"tree", entry.Name, "consecutive_fails", g.cfg.Gate.FailCountFor(entry.Name))
		rolledBack := 0
		if g.cfg.SnapshotDir != "" {
			if err := g.cfg.Registry.RollbackTree(entry.Name, g.cfg.SnapshotDir); err != nil {
				slog.Warn("gardener/v2: automatic rollback failed, tree remains frozen in regressed state", "tree", entry.Name, "error", err)
			} else {
				rolledBack = 1
			}
		}
		return CycleMetrics{
			TreeName: entry.Name, Timestamp: time.Now().Unix(),
			BaseFitness: baseFitness.Composite, NewFitness: baseFitness.Composite,
			NodesBefore: nodesBefore, NodesAfter: nodesBefore,
			Rollbacks: rolledBack,
		}
	}

	// Durable pre-mutation snapshot (Q2 Evolvability milestone 1): originalTree
	// below only lives in-memory for this cycle, so a process crash mid-cycle
	// loses the pre-mutation state entirely. Persist it to g.cfg.SnapshotDir
	// (when configured) so RestoreTree can recover it after a crash. Recording
	// baseFitness alongside each revision (SnapshotTreeWithFitness) is what
	// lets RestoreTreeBeforeRegressionStreak later walk back past a
	// multi-cycle regression streak instead of just the latest cycle.
	if g.cfg.SnapshotDir != "" {
		if _, err := evolution.SnapshotTreeWithFitness(tree, entry.Name, g.cfg.SnapshotDir, baseFitness.Composite); err != nil {
			slog.Warn("gardener/v2: pre-mutation snapshot failed", "tree", entry.Name, "error", err)
		}
	}

	applied := 0
	rejected := 0
	rollbacks := 0
	originalTree := cloneTreeForGardener(tree)
	currentFitness := baseFitness
	for i := 0; i < len(candidates) && applied < maxMutations; i++ {
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
				if err := bank.AddFromMutation(tree, candidates[i].Op, currentFitness.Composite, candidateFitness.Composite, nil, lastFailureTask(records)); err != nil {
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
	// Feed this cycle's settled tree/fitness into the tree's diversity
	// archive so BehavioralDiversity above reflects real accumulated shape
	// exploration on the next cycle, not just the cold-start 0.
	g.recordDiversityObservation(entry.Name, tree, newFitness.Composite)
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
	reordered := g.applyLearnedSelectorOrdering(tree, entry.Name, cfg)
	// ── DT-optimizer ordering (Q2 Evolvability milestone 3) — the
	// entropy/Gini-based sibling of the Selector-ordering pass above, applied
	// to the same tree just before persistence.
	reordered += g.applyDTOptimizerOrdering(tree, entry.Name, cfg)
	saveFailed := false
	if applied > 0 || reordered > 0 {
		if err := g.cfg.Registry.SaveTree(TreeEntry{Name: entry.Name, Tree: tree, FilePath: entry.FilePath}); err != nil {
			slog.Error("gardener/v2: saving evolved tree failed, evolution result is not durably persisted", "tree", entry.Name, "error", err)
			saveFailed = true
		}
	}

	// Crisis bookkeeping needs no post-cycle reset here: Intervene() itself
	// consumes the stagnation evidence at fire time (transition semantics),
	// so the old `crisisIntervened && applied > 0` reset — whose applied>0
	// condition was unsatisfiable for exactly the plateaued trees that kept
	// firing — is gone with the latch it tried to paper over.

	// Deep search (Q2 Evolvability milestone 2/3): probe further ahead from
	// the post-cycle tree with the Stockfish-style transposition-table
	// search, gated on a configured transposition table (IterativeDeepening
	// requires a non-nil *evaluator.TranspositionTable to run at all). When
	// the search surfaces a mutation that beats the tree's current fitness,
	// apply it directly to the live tree instead of discarding it as
	// metrics-only — this is the only path that can still improve the tree
	// once the greedy per-candidate loop's budget is exhausted or zero.
	var deepSearchUsed bool
	var deepSearchDepth int
	var ttHitRate float64
	if tt := g.transpositionTable(); tt != nil {
		deep := evaluator.IterativeDeepening(tree, records, tt, deepSearchMaxDepth)
		deepSearchUsed = true
		deepSearchDepth = deep.Depth
		if deep.TTProbes > 0 {
			ttHitRate = float64(deep.TTProbeHits) / float64(deep.TTProbes)
		}
		if deep.BestMutation != nil && deep.BestFitness != nil && deep.BestFitness.Composite > newFitness.Composite+0.0001 {
			preDeepSearchTree := cloneTreeForGardener(tree)
			preDeepSearchFitness := newFitness

			// ── Meta-validation — mirror the greedy loop's own check above:
			// consult the structural safety layer on a cloned candidate before
			// committing the deep-search mutation to the live tree at all.
			metaRejected := false
			if g.cfg.MetaValidator != nil {
				candidateTree := cloneTreeForGardener(tree)
				if evolution.ApplyMutations(candidateTree, []evolution.MutationOp{deep.BestMutation.Op}) > 0 {
					metaReport := g.cfg.MetaValidator.ValidateMutation(preDeepSearchTree, candidateTree, preDeepSearchFitness.Composite, deep.BestFitness.Composite)
					if metaReport.Decision == evolution.MetaReject {
						slog.Warn("gardener/v2: meta validator rejected deep-search mutation, skipping", "tree", entry.Name)
						metaRejected = true
					}
				}
			}

			preDeepSearchApplied := applied
			if !metaRejected && evolution.ApplyMutations(tree, []evolution.MutationOp{deep.BestMutation.Op}) > 0 {
				applied++
				if bank != nil {
					if err := bank.AddFromMutation(tree, deep.BestMutation.Op, newFitness.Composite, deep.BestFitness.Composite, nil, lastFailureTask(records)); err != nil {
						slog.Warn("gardener/v2: recording deep-search mutation experience failed", "tree", entry.Name, "error", err)
					}
				}
				newFitness = *deep.BestFitness
				nodesAfter = evolution.CountNodes(tree)
				improved = newFitness.Composite > baseFitness.Composite

				// ── Validation gate — mirror the greedy loop's own gate above:
				// re-validate the deep-search mutation before persisting it, and
				// revert to the pre-deep-search state on rejection so a mutation
				// that fails validation is never saved.
				if gateErr := ValidationGate(entry.Name, entry.Name, g.cfg.ValidationGate); gateErr != nil {
					slog.Warn("gardener/v2: validation gate rejected deep-search mutation, reverting", "tree", entry.Name, "error", gateErr)
					*tree = *preDeepSearchTree
					newFitness = preDeepSearchFitness
					applied = preDeepSearchApplied
					nodesAfter = evolution.CountNodes(tree)
					improved = newFitness.Composite > baseFitness.Composite
				} else if err := g.cfg.Registry.SaveTree(TreeEntry{Name: entry.Name, Tree: tree, FilePath: entry.FilePath}); err != nil {
					slog.Error("gardener/v2: saving deep-search evolved tree failed, evolution result is not durably persisted", "tree", entry.Name, "error", err)
					saveFailed = true
				}
			}
		}
	}

	// Knowledge-graph write-back (Q2 Evolvability milestone 2): a cycle that
	// accepted at least one mutation is evidence the KG's fitness-aware
	// discovery should see, mirroring recordEvolvedFitness in
	// cmd/bt-agent/tools.go.
	if applied > 0 {
		g.recordEvolvedRun(entry.Name, newFitness.Composite)
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

		DeepSearchUsed:  deepSearchUsed,
		DeepSearchDepth: deepSearchDepth,
		TTHitRate:       ttHitRate,

		SaveFailed: saveFailed,
	}
}

// recordEvolvedRun writes an "evolved" RunRecord for treeID back into the
// configured KnowledgeGraph, mirroring recordEvolvedFitness in
// cmd/bt-agent/tools.go so fitness-aware discovery can surface trees the
// gardener daemon (not just QD/island runs) has improved. A nil
// KnowledgeGraph is a safe no-op; an unregistered tree ID relies on
// KnowledgeGraph.RecordRun's own no-op for unknown tree IDs.
func (g *Gardener) recordEvolvedRun(treeID string, fitness float64) {
	if g.cfg.KnowledgeGraph == nil {
		return
	}
	g.cfg.KnowledgeGraph.RecordRun(knowledge.RunRecord{
		TreeID:  treeID,
		Task:    "gardener v2 evolution cycle",
		Outcome: "evolved",
		Quality: fitness,
	})
}

// applyLearnedSelectorOrdering seeds a SelectorOptimizer from the durable
// per-tree telemetry the real production writer
// (agent.RunDeps.flushSelectorTelemetry) produces — agent.SelectorStatsFile(treeID),
// under agent.HomeDir()/selector-stats/ — and reorders every Selector's
// children in tree by their learned success rate, keeping fallback/
// AlwaysSucceed children last. Falls back to the single shared
// Config.SelectorStatsPath when no per-tree file exists yet, so callers that
// seed that path directly (tests, or trees with no per-tree telemetry) keep
// working. It is a no-op (returns 0) unless the pass is enabled and a stats
// path resolves. Returns the number of Selector nodes whose ordering changed.
func (g *Gardener) applyLearnedSelectorOrdering(tree *evolution.SerializableNode, treeID string, cfg EvolveV2Config) int {
	if !cfg.SelectorOrdering || tree == nil {
		return 0
	}
	path := g.selectorStatsPathFor(treeID)
	if path == "" {
		return 0
	}
	strategy := cfg.SelectorOrderingStrategy
	if strategy == "" {
		strategy = evolution.OrderBySuccessRate
	}
	so := evolution.NewSelectorOptimizer(strategy)
	if err := so.LoadSelectorStats(path); err != nil {
		slog.Warn("gardener/v2: loading selector stats failed, skipping ordering",
			"path", path, "error", err)
		return 0
	}
	return so.ApplyLearnedOrdering(tree)
}

// selectorStatsPathFor resolves the durable Selector telemetry path for
// treeID, preferring the real per-tree file over Config.SelectorStatsPath — a
// single global path no writer in the repo ever produces (the real writer,
// agent.RunDeps.flushSelectorTelemetry, only ever writes per-tree files keyed
// by tree ID). Falls back to Config.SelectorStatsPath when the per-tree file
// does not exist yet, or when treeID is empty.
func (g *Gardener) selectorStatsPathFor(treeID string) string {
	if treeID != "" {
		if perTree := agent.SelectorStatsFile(treeID); fileExists(perTree) {
			return perTree
		}
	}
	return g.cfg.SelectorStatsPath
}

// fileExists reports whether path names a regular, readable file.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// applyDTOptimizerOrdering seeds a BTOptimizer from the durable DTAnalyzer
// telemetry for treeID and reorders every Selector's children in tree by
// information gain (entropy/Gini), keeping fallback/AlwaysSucceed children
// last. It is a no-op (returns 0) unless the pass is enabled and a stats path
// resolves. Returns the number of Selector nodes whose ordering changed — the
// DTAnalyzer/BTOptimizer sibling of applyLearnedSelectorOrdering above.
func (g *Gardener) applyDTOptimizerOrdering(tree *evolution.SerializableNode, treeID string, cfg EvolveV2Config) int {
	if !cfg.DTOrdering || tree == nil {
		return 0
	}
	path := g.dtStatsPathFor(treeID)
	if path == "" {
		return 0
	}
	bo := evolution.NewBTOptimizer()
	if err := bo.Analyzer.Load(path); err != nil {
		slog.Warn("gardener/v2: loading decision-tree stats failed, skipping ordering",
			"path", path, "error", err)
		return 0
	}
	return bo.OptimizeSelectors(tree)
}

// dtStatsPathFor resolves the durable DTAnalyzer telemetry path for treeID,
// preferring the real per-tree file the daemon writes —
// agent.DecisionTreeStatsFile(treeID), the sidecar
// agent.RunDeps.flushSelectorTelemetry produces alongside SelectorStatsFile —
// over the single shared Config.DTStatsPath, mirroring
// selectorStatsPathFor's per-tree-first resolution for Selector stats above.
// ADR-191's activation was inert without this: Config.DTStatsPath (e.g.
// ~/.go-bt-gardener/dt-stats.json) has no production writer, so
// applyDTOptimizerOrdering silently no-opped forever until this per-tree
// preference was added. Falls back to Config.DTStatsPath when the per-tree
// file does not exist yet, or when treeID is empty.
func (g *Gardener) dtStatsPathFor(treeID string) string {
	if treeID != "" {
		if perTree := agent.DecisionTreeStatsFile(treeID); fileExists(perTree) {
			return perTree
		}
	}
	return g.cfg.DTStatsPath
}

// AnalyzeTreeDiagnostics runs evolution.BTOptimizer.AnalyzeTree — milestone
// 4/4 of the DTAnalyzer/BTOptimizer wiring program — against a clone of
// entry.Tree so its destructive OptimizeSelectors/PruneDeadPaths/
// MergeOverlappingPaths passes never touch the live production tree. The
// resulting DTImprovementReport surfaces those counts for HITL review only;
// callers must not treat it as an in-place mutation like
// applyDTOptimizerOrdering above. Loads the BTOptimizer's Analyzer from
// dtStatsPathFor(entry.Name) — the same per-tree-first resolution
// applyDTOptimizerOrdering uses — rather than Config.DTStatsPath directly, so
// this sees the real per-tree telemetry the daemon writes at
// agent.DecisionTreeStatsFile(treeID) instead of only the producer-less
// shared path; a missing or unreadable stats file degrades to an unseeded
// BTOptimizer rather than failing the diagnostic. Returns nil for a nil
// entry.Tree.
func (g *Gardener) AnalyzeTreeDiagnostics(entry TreeEntry) *evolution.DTImprovementReport {
	if entry.Tree == nil {
		return nil
	}
	clone := cloneTreeForGardener(entry.Tree)
	bo := evolution.NewBTOptimizer()
	if path := g.dtStatsPathFor(entry.Name); path != "" {
		if err := bo.Analyzer.Load(path); err != nil {
			slog.Warn("gardener/v2: loading decision-tree stats failed, analyzing unseeded",
				"path", path, "error", err)
		}
	}
	return bo.AnalyzeTree(clone, entry.Name)
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
// past entry gets its score boosted (proportional to the entry's quality) and
// the matched entry is marked reused. Non-matching candidates keep their
// relative heuristic order (stable sort). A nil or empty bank — or one with no
// usable matches — leaves the ordering untouched.
//
// query is the milestone-2 lastFailureTask signal for this cycle. When
// non-empty, retrieval routes through bank.Retrieve(query, topK) — a
// similarity/quality-ranked search across the whole bank, not filtered by
// tree type — so an entry recorded against a different tree's failing task
// can still warm-start this tree's candidates. When empty, retrieval falls
// back to today's tree-type-only evolution.RetrieveExperienceHints path.
func biasCandidatesWithExperience(bank *evolution.ExperienceBank, tree *evolution.SerializableNode, candidates []evaluator.MutationCandidate, query string) []evaluator.MutationCandidate {
	var hints []evolution.ExperienceEntry
	if query != "" {
		if bank != nil {
			hints = bank.Retrieve(query, experienceBiasTopK)
		}
	} else {
		hints = evolution.RetrieveExperienceHints(bank, tree, experienceBiasTopK)
	}
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

// treePriorityDefaultRank is the rank assigned to trees that ComputeAnalytics
// neither flags as a Bottleneck nor as SelectionPressure — they keep the
// historical flat alphabetical ordering relative to each other.
const treePriorityDefaultRank = 2

// treePriorityRanks maps tree names to a priority rank derived from
// Config.KnowledgeGraph's ComputeAnalytics() output: Bottleneck trees (low
// success rate, enough runs to trust) get rank 0, SelectionPressure trees
// (proven but underbred) get rank 1. A nil KnowledgeGraph or a name absent
// from the returned map falls back to treePriorityDefaultRank in
// treePriorityRank, preserving today's alphabetical-only behavior.
func (g *Gardener) treePriorityRanks() map[string]int {
	if g.cfg.KnowledgeGraph == nil {
		return nil
	}
	analytics := g.cfg.KnowledgeGraph.ComputeAnalytics()
	ranks := make(map[string]int, len(analytics.Bottlenecks)+len(analytics.SelectionPressure))
	for _, b := range analytics.Bottlenecks {
		ranks[b.TreeID] = 0
	}
	for _, sp := range analytics.SelectionPressure {
		if _, exists := ranks[sp.TreeID]; !exists {
			ranks[sp.TreeID] = 1
		}
	}
	return ranks
}

// treePriorityRank looks up name's priority rank, defaulting to
// treePriorityDefaultRank when ranks is nil or does not mention name.
func treePriorityRank(ranks map[string]int, name string) int {
	if rank, ok := ranks[name]; ok {
		return rank
	}
	return treePriorityDefaultRank
}

// RunCycleV2 executes one full evolution cycle using the v2 pipeline.
func (g *Gardener) RunCycleV2(cfg EvolveV2Config) ([]CycleMetrics, error) {
	entries := g.cfg.Registry.List()
	ranks := g.treePriorityRanks()
	sort.SliceStable(entries, func(i, j int) bool {
		ri, rj := treePriorityRank(ranks, entries[i].Name), treePriorityRank(ranks, entries[j].Name)
		if ri != rj {
			return ri < rj
		}
		return entries[i].Name < entries[j].Name
	})

	results := make([]CycleMetrics, 0, len(entries))
	var errs []error

	for _, entry := range entries {
		if !entry.Active {
			continue
		}

		start := time.Now()
		metrics := g.evolveTreeV2(entry, cfg)
		metrics.DurationMs = time.Since(start).Milliseconds()
		results = append(results, metrics)
		if metrics.SaveFailed {
			errs = append(errs, fmt.Errorf("tree %q: saving evolved tree failed", entry.Name))
		}

		g.cfg.MetricsTracker.Record(metrics)
		// Persist after every tree so a mid-cycle crash or SIGTERM loses at
		// most one tree's result, not the whole cycle.
		if err := g.cfg.MetricsTracker.Save(); err != nil {
			slog.Error("gardener/v2: saving metrics failed, snapshot is not durably persisted", "tree", entry.Name, "error", err)
			errs = append(errs, fmt.Errorf("saving metrics after tree %q: %w", entry.Name, err))
		}
		if tt := g.transpositionTable(); tt != nil {
			if err := tt.Save(); err != nil {
				slog.Warn("gardener/v2: transposition table save failed", "error", err)
			}
		}
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

	if err := g.cfg.MetricsTracker.Save(); err != nil {
		slog.Error("gardener/v2: final metrics save failed, snapshot is not durably persisted", "error", err)
		errs = append(errs, fmt.Errorf("final metrics save: %w", err))
	}
	if len(errs) > 0 {
		return results, errors.Join(errs...)
	}
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
