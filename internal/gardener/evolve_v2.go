package gardener

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/benchmark"
	"github.com/nico/go-bt-evolve/internal/evaluator"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/knowledge"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/util"
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

	// DisableLocalSearch turns off the post-structural-mutation local-search
	// refinement pass (evolution.LocalSearcher over the settled tree's mutable
	// parameters). The pass is ON by default — parameter tuning is the one
	// improvement no MutationOp can express, so a tree whose only remaining
	// slack is a badly-sized MaxRetries/TimeoutMs would otherwise never improve.
	DisableLocalSearch bool

	// LocalSearchStrategy picks which evolution.LocalSearcher strategy the
	// refinement pass runs. The zero value is evolution.HillClimbSearch.
	LocalSearchStrategy evolution.LocalSearchStrategy

	// MCTSStructuralSearch, when true, lets evolution.MCTSMutator compete as a
	// SECOND structural-mutation generator alongside evaluator.OrderMutations.
	// Its search proposals are merged into ONE scored competition
	// (evolution.MergeScoredMutations) before the per-candidate benchmark /
	// pre-score / quality-gate loop, so an MCTS candidate has to earn its place
	// against the heuristic ones on exactly the same evidence — it is never
	// applied on the search's word alone. Whether the search actually runs for
	// a given tree is decided per tree by evolution.SelectStructuralStrategy.
	MCTSStructuralSearch bool

	// MCTSIterations bounds the per-tree search budget. Each iteration costs one
	// evaluator.EvaluateTree call (no benchmark, no LLM). Zero falls back to
	// defaultMCTSCandidateIterations.
	MCTSIterations int

	// Specialists supplies the archetype half of the per-tree strategy choice.
	// Nil means "no archetype knowledge", which biases toward running the
	// search; DefaultEvolveV2Config seeds it from the benchmark-validated
	// specialists so a tree matching a preserved archetype is not gambled on.
	Specialists *evolution.SpecialistRegistry
}

// DefaultEvolveV2Config returns sensible defaults for the v2 pipeline.
func DefaultEvolveV2Config() EvolveV2Config {
	return EvolveV2Config{
		CascadeCfg:               evaluator.DefaultCascadeConfig(),
		BlocksEnabled:            true,
		BlockConfig:              evolution.DefaultBlockConfig(),
		UseRealLLM:               false, // use mock by default for speed
		SelectorOrderingStrategy: evolution.OrderBySuccessRate,
		MCTSStructuralSearch:     true,
		MCTSIterations:           defaultMCTSCandidateIterations,
		Specialists:              evolution.SeedSpecialistRegistry(),
	}
}

// defaultMCTSCandidateIterations is the per-tree MCTS search budget when
// MCTSIterations is unset. It is deliberately close to len(AllMutationOps) so
// the search gets one shot at each root-level operation before it starts
// deepening — the root level is where replayable candidates come from.
const defaultMCTSCandidateIterations = 12

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
//
// The tree is SNAPSHOT before it is archived. The caller's tree is almost
// always entry.Tree (evolveTreeV2), and Registry.List copies the entry slice
// but not the *SerializableNode it points at — so every cycle hands over the
// same stable pointer to an object the pipeline then mutates in place.
// Archiving that pointer verbatim (MAPElitesGrid.Insert stores what it is
// given) would leave every cell in the grid aliasing one live object holding
// whatever shape the latest cycle left behind, paired with a stale per-cell
// Fitness. That silently breaks every archive consumer: reseedFromDiversity-
// Archive would re-score current-best against itself and always decline, so an
// elite reseed could never fire in production. Each cell must own an
// independent copy of the shape whose descriptor selected it.
func (g *Gardener) recordDiversityObservation(name string, tree *evolution.SerializableNode, fitness float64) {
	grid := g.treeDiversityGrid(name)
	desc := evolution.Descriptor(tree, "")
	grid.Insert(desc, &evolution.Individual{Tree: cloneTreeForGardener(tree), Fitness: fitness})
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
	crisisReason := ""
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
			maxMutations = max(int(math.Ceil(float64(g.cfg.MaxMutations)/(1-rate))), 1)
			crisisIntervened = true
			crisisReason = reason
			slog.Info("gardener/v2: crisis intervention — boosting mutation budget",
				"tree", entry.Name, "reason", reason,
				"stagnation_epochs", action.StagnationEpochs,
				"budget", maxMutations)
		}
	}

	// ── MAP-Elites active elitism (milestone 2/5) ──
	// A diversity collapse means this tree's archive says every recent shape
	// lands in the same handful of behavioral niches, so mutating current-best
	// once more explores the cell we are already stuck in. Answer it by
	// reseeding this cycle from the archive's fittest elite in a DIFFERENT
	// niche — promoting the grid from the write-only diversity signal it was to
	// an actual elitism source. preReseedTree keeps the pre-cycle lineage as
	// the rollback target, so a later regression or validation-gate rejection
	// discards the reseed along with the mutations built on top of it.
	preReseedTree := cloneTreeForGardener(tree)
	seedFitness := baseFitness
	eliteReseed := false
	if crisisIntervened && crisisReason == evolution.CrisisDiversityCollapse {
		if reseeded, ok := g.reseedFromDiversityArchive(tree, entry.Name, records, baseFitness.Composite); ok {
			seedFitness = reseeded
			eliteReseed = true
		}
	}

	// ── Generate and filter mutations ──
	// Personal trees bias against (and record into) the owning user's bank.
	bank := g.bankFor(entry)
	candidates := biasCandidatesWithExperience(bank, tree, evaluator.OrderMutations(tree, records, seedFitness), lastFailureTask(records))

	// ── MCTS as a second structural-mutation generator (milestone 4/5) ──
	// The heuristic ordering can only propose mutations its hand-written rules
	// know how to look for. Merge in whatever a bounded MCTS search found to
	// actually raise this tree's fitness, so both generators compete on one
	// scored list before the benchmark/gate loop below.
	candidates = g.augmentWithMCTSCandidates(tree, entry.Name, records, seedFitness, candidates, cfg)

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
		// Fail closed on the reseed too: a tree whose gate is disabled is being
		// restored to its last-known-good revision, so this cycle's speculative
		// archive seed must not survive in memory either.
		if eliteReseed {
			*tree = *preReseedTree
		}
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
	// The snapshot pairs a tree with the fitness it scored, so it takes the
	// pre-reseed lineage and baseFitness — reseeding is part of this cycle's
	// speculative work, not a durable last-known-good revision.
	if g.cfg.SnapshotDir != "" {
		if _, err := evolution.SnapshotTreeWithFitness(preReseedTree, entry.Name, g.cfg.SnapshotDir, baseFitness.Composite); err != nil {
			slog.Warn("gardener/v2: pre-mutation snapshot failed", "tree", entry.Name, "error", err)
		}
	}

	applied := 0
	rejected := 0
	rollbacks := 0
	originalTree := preReseedTree
	currentFitness := seedFitness
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
		// originalTree is the pre-reseed lineage, so the restore above undid
		// the archive seed as well — this cycle no longer reseeded anything.
		eliteReseed = false
	}
	// Feed this cycle's settled tree/fitness into the tree's diversity
	// archive so BehavioralDiversity above reflects real accumulated shape
	// exploration on the next cycle, not just the cold-start 0.
	g.recordDiversityObservation(entry.Name, tree, newFitness.Composite)
	improved := newFitness.Composite > baseFitness.Composite
	// ── Local-search parameter refinement — run evolution.LocalSearcher over
	// the settled tree's mutable parameters (MaxRetries, TimeoutMs, metadata
	// knobs), scored by the same cascade fitness function that scored the
	// structural candidates above. Structural mutation can add, remove, and
	// rewire nodes but cannot resize an existing node's parameters, so this is
	// the only pass that can recover a tree whose remaining slack is purely
	// numeric. The tuned tree is committed only when it beats the settled
	// fitness through the acceptance gate.
	//
	// This runs ABOVE the validation gate on purpose: the refinement mutates
	// the live tree and independently forces the save below, so running it
	// after the gate would let a tuned tree the gate just rejected reach disk
	// unvalidated. Sitting here, its delta is covered by the same gate.
	localSearchDelta := g.refineTreeParameters(tree, entry, cfg, records, newFitness.Composite)
	if localSearchDelta > 0 {
		newFitness = evaluator.EvaluateTree(tree, records)
		nodesAfter = evolution.CountNodes(tree)
		improved = newFitness.Composite > baseFitness.Composite
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

	// A reseed changes the persisted tree on its own, so it must clear the
	// validation gate even when no structural mutation applied on top of it —
	// and so does a local-search refinement.
	if applied > 0 || eliteReseed || localSearchDelta > 0 || reordered > 0 {
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
			eliteReseed = false
			// The restore above reverted the tuned parameters too, so this
			// cycle refined nothing — clearing the delta keeps the reported
			// metrics honest and stops it from forcing the save below.
			localSearchDelta = 0
			reordered = 0
		}
	}

	saveFailed := false
	// A refinement — or a diversity-crisis reseed — is itself a persistable
	// change, like the Selector/DT reordering passes: it must force a save even
	// when no mutation applied.
	if applied > 0 || reordered > 0 || localSearchDelta > 0 || eliteReseed {
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
		EliteReseed:        eliteReseed,

		DeepSearchUsed:  deepSearchUsed,
		DeepSearchDepth: deepSearchDepth,
		TTHitRate:       ttHitRate,

		LocalSearchDelta: localSearchDelta,

		SaveFailed: saveFailed,
	}
}

// reseedFromDiversityArchive answers a diversity-collapse crisis for name by
// replacing tree's contents, in place, with the fittest elite from a DIFFERENT
// niche of the tree's MAP-Elites archive — the active-elitism half of
// milestone 2/5. Before this, treeDiversityGrid was written every cycle and
// read only for its DiversityScore; the archived elites themselves were never
// used for anything.
//
// The archive's own recorded fitness only decides which elite is worth
// considering (MAPElitesGrid.EliteSeed, floored at the live tree's fitness).
// Adoption is decided by re-scoring the candidate against THIS tree's current
// reflection records — an elite archived cycles ago under different evidence
// can easily be stale — so a reseed only lands when the elite genuinely beats
// the live tree right now. The elite is cloned before adoption: the grid keeps
// pointers to its cell winners, and the caller mutates tree.
//
// Returns the adopted seed's fitness and true on reseed; the tree is left
// untouched and ok is false otherwise.
func (g *Gardener) reseedFromDiversityArchive(
	tree *evolution.SerializableNode,
	name string,
	records []evolution.Record,
	floor float64,
) (evaluator.FitnessScore, bool) {
	if tree == nil {
		return evaluator.FitnessScore{}, false
	}
	elite := g.treeDiversityGrid(name).EliteSeed(evolution.Descriptor(tree, ""), floor)
	if elite == nil {
		return evaluator.FitnessScore{}, false
	}

	seed := cloneTreeForGardener(elite.Tree)
	seedFitness := evaluator.EvaluateTree(seed, records)
	if seedFitness.Composite <= floor+0.0001 {
		return evaluator.FitnessScore{}, false
	}

	*tree = *seed
	slog.Info("gardener/v2: diversity collapse — reseeding from MAP-Elites elite",
		"tree", name, "base_fitness", floor, "seed_fitness", seedFitness.Composite,
		"archived_fitness", elite.Fitness, "seed_nodes", evolution.CountNodes(tree))
	return seedFitness, true
}

// refineTreeParameters runs evolution.LocalSearcher over tree's mutable
// parameters using the same evaluator.EvaluateTree cascade fitness that scored
// this cycle's structural candidates, and commits the tuned tree in place only
// when RefineGated accepts it — i.e. when it strictly beats settledFitness and
// clears the quality gate. Returns the composite gain, or 0 when the pass is
// disabled, finds nothing tunable, or its result is rejected.
//
// The gate is consulted non-recordingly (RefineGated → QualityGate.Probe): a
// speculative tuning attempt that fails is not a regression of the live tree,
// so it must never burn the tree's consecutive-failure streak toward
// fail-closed.
func (g *Gardener) refineTreeParameters(
	tree *evolution.SerializableNode,
	entry TreeEntry,
	cfg EvolveV2Config,
	records []evolution.Record,
	settledFitness float64,
) float64 {
	if cfg.DisableLocalSearch || tree == nil {
		return 0
	}

	searcher := evolution.NewLocalSearcher(cfg.LocalSearchStrategy)
	res := searcher.RefineGated(tree, settledFitness, func(candidate *evolution.SerializableNode) float64 {
		return evaluator.EvaluateTree(candidate, records).Composite
	}, g.cfg.Gate, entry.Name)
	if !res.Accepted || res.Tree == nil {
		return 0
	}

	*tree = *res.Tree
	slog.Info("gardener/v2: local search refined tree parameters",
		"tree", entry.Name, "delta", res.Delta, "fitness", res.Fitness)
	return res.Delta
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
	so := g.seedSelectorOptimizer(treeID, cfg)
	if so == nil {
		return 0
	}
	return so.ApplyLearnedOrdering(tree)
}

// seedSelectorOptimizer builds a SelectorOptimizer seeded from the durable
// per-tree Selector telemetry for treeID (selectorStatsPathFor's per-tree-first
// resolution). Returns nil when no stats path resolves or the file cannot be
// read, so callers degrade to "no learned signal" rather than to an optimizer
// silently pretending it has one. Shared by the ordering pass above and by the
// MCTS strategy choice, which needs the same telemetry read-only.
func (g *Gardener) seedSelectorOptimizer(treeID string, cfg EvolveV2Config) *evolution.SelectorOptimizer {
	path := g.selectorStatsPathFor(treeID)
	if path == "" {
		return nil
	}
	strategy := cfg.SelectorOrderingStrategy
	if strategy == "" {
		strategy = evolution.OrderBySuccessRate
	}
	so := evolution.NewSelectorOptimizer(strategy)
	if err := so.LoadSelectorStats(path); err != nil {
		slog.Warn("gardener/v2: loading selector stats failed",
			"path", path, "error", err)
		return nil
	}
	return so
}

// augmentWithMCTSCandidates merges evolution.MCTSMutator's search proposals
// into the heuristic candidate list, producing ONE descending-score competition
// for evolveTreeV2's per-candidate benchmark / pre-score / quality-gate loop.
//
// Whether the search runs at all is a per-tree decision made by
// evolution.SelectStructuralStrategy from two independent "what do we already
// know about this tree" signals: the specialist registry's archetype knowledge
// and the Selector optimizer's learned-ordering telemetry. A tree that is a
// preserved archetype AND whose Selectors are fully telemetered keeps today's
// heuristic-only ordering — those are exactly the trees where speculative
// search is most likely to break something already proven.
//
// The search is scored with evaluator.EvaluateTree (records-derived fitness, no
// benchmark and no LLM), so its cost is MCTSIterations cheap evaluations. Its
// output is only ever an ORDERING input: every merged candidate still has to
// clear the same benchmark, pre-score, quality gate and meta-validation as a
// heuristic one before it is applied.
func (g *Gardener) augmentWithMCTSCandidates(
	tree *evolution.SerializableNode,
	treeID string,
	records []evolution.Record,
	seedFitness evaluator.FitnessScore,
	heuristic []evaluator.MutationCandidate,
	cfg EvolveV2Config,
) []evaluator.MutationCandidate {
	if !cfg.MCTSStructuralSearch || tree == nil {
		return heuristic
	}
	strategy := evolution.SelectStructuralStrategy(cfg.Specialists, g.seedSelectorOptimizer(treeID, cfg), tree)
	if strategy != evolution.StrategyMCTSAugmented {
		return heuristic
	}

	iterations := cfg.MCTSIterations
	if iterations <= 0 {
		iterations = defaultMCTSCandidateIterations
	}
	mutator := evolution.NewMCTSMutator()
	mutator.Iterations = iterations
	mutator.SetFitnessEvaluator(func(candidate *evolution.SerializableNode) float64 {
		return evaluator.EvaluateTree(candidate, records).Composite
	})

	found := mutator.Candidates(tree, seedFitness.Composite)
	if len(found) == 0 {
		return heuristic
	}

	merged := candidatesFromScored(evolution.MergeScoredMutations(scoredFromCandidates(heuristic), found))
	slog.Info("gardener/v2: MCTS search joined the structural-mutation competition",
		"tree", treeID, "heuristic_candidates", len(heuristic),
		"mcts_candidates", len(found), "merged_candidates", len(merged))
	return merged
}

// scoredFromCandidates converts the evaluator package's heuristic candidates
// into the shared evolution.ScoredMutation currency MergeScoredMutations
// speaks. The two shapes are identical by design — evaluator imports evolution,
// so the shared type has to live on the evolution side.
func scoredFromCandidates(candidates []evaluator.MutationCandidate) []evolution.ScoredMutation {
	if len(candidates) == 0 {
		return nil
	}
	out := make([]evolution.ScoredMutation, len(candidates))
	for i, c := range candidates {
		out[i] = evolution.ScoredMutation{Op: c.Op, Score: c.Score, Reason: c.Reason}
	}
	return out
}

// candidatesFromScored is the inverse of scoredFromCandidates, handing the
// merged competition back to evolveTreeV2's existing candidate loop unchanged.
func candidatesFromScored(scored []evolution.ScoredMutation) []evaluator.MutationCandidate {
	if len(scored) == 0 {
		return nil
	}
	out := make([]evaluator.MutationCandidate, len(scored))
	for i, s := range scored {
		out[i] = evaluator.MutationCandidate{Op: s.Op, Score: s.Score, Reason: s.Reason}
	}
	return out
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

	biased := slices.Clone(candidates)
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

	slices.SortStableFunc(biased, func(a, b evaluator.MutationCandidate) int {
		return cmp.Compare(b.Score, a.Score)
	})

	reusedIDs := slices.Collect(maps.Keys(reused))
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

const (
	// defaultIslandInterval is how many cycles apart the island-exploration
	// pass runs when Config.IslandInterval is unset — at the production
	// 5-minute cycle interval, roughly hourly.
	defaultIslandInterval = 12

	// islandPopulationSize is how many individuals a freshly warm-started
	// domain island holds (the live tree plus randomly mutated variants of it).
	// Small on purpose: every individual is re-scored against the domain's
	// reflection records on each pass.
	islandPopulationSize = 8
)

// islandInterval resolves the configured island-pass interval, falling back to
// defaultIslandInterval for non-positive values.
func (g *Gardener) islandInterval() int {
	if g.cfg.IslandInterval > 0 {
		return g.cfg.IslandInterval
	}
	return defaultIslandInterval
}

// islandPassDue reports whether the 1-based cycle number cycle should run the
// island-exploration pass: cycle 1 always, then every islandInterval()-th cycle
// after it (with interval 3: cycles 1, 4, 7, …).
func (g *Gardener) islandPassDue(cycle int) bool {
	if g.cfg.IslandModel == nil || cycle < 1 {
		return false
	}
	return (cycle-1)%g.islandInterval() == 0
}

// runIslandExploration runs one evolution.IslandModel.EvolveAll generation
// across one island per active domain and migrates each island's winner back
// into the domain's live TreeEntry state, returning the set of tree names that
// adopted one.
//
// This is the population-exploration half of the daemon's evolution: the
// per-tree pipeline (evolveTreeV2) only ever mutates current-best, so a tree
// whose improvement needs several coordinated changes at once can never be
// reached from there. An island holds a whole subpopulation per domain, breeds
// it with crossover + mutation, and keeps it across cycles, so those multi-step
// shapes get explored off to the side of the live tree.
//
// EvolveAll scores every island with one shared fitness function, so
// exploration is scored against the whole reflection corpus. Adoption is
// decided separately, per domain, against that domain's own records (the same
// re-score-before-adopting discipline reseedFromDiversityArchive uses), so a
// coarse exploration score can never push a tree backwards.
func (g *Gardener) runIslandExploration(entries []TreeEntry) map[string]bool {
	im := g.cfg.IslandModel
	if im == nil {
		return nil
	}

	active := make([]TreeEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Active && entry.Tree != nil {
			active = append(active, entry)
		}
	}
	if len(active) == 0 {
		return nil
	}

	allRecords, err := g.cfg.RefStore.LoadAll()
	if err != nil {
		slog.Warn("gardener/v2: loading reflections for island pass failed, skipping exploration", "error", err)
		return nil
	}

	// One island per active domain, warm-started from that domain's current
	// tree. Domains that already carry an island keep the subpopulation they
	// have been evolving across earlier passes.
	for _, entry := range active {
		if im.SeedIsland(entry.Name, entry.Tree, islandPopulationSize) {
			slog.Info("gardener/v2: island seeded for domain", "tree", entry.Name, "size", islandPopulationSize)
		}
	}

	im.EvolveAll(func(tree *evolution.SerializableNode) float64 {
		return evaluator.EvaluateTree(tree, allRecords).Composite
	})

	adopted := make(map[string]bool, len(active))
	for _, entry := range active {
		if g.adoptIslandWinner(entry, recordsForEntry(allRecords, entry)) {
			adopted[entry.Name] = true
		}
	}
	return adopted
}

// adoptIslandWinner migrates entry's island champion into the live tree, in
// place, and persists it — but only when that champion genuinely beats the live
// tree under entry's own reflection records and stays within the same bloat cap
// evolveTreeV2 enforces (an island breeds with random mutation, so it can grow
// individuals the per-tree pipeline would refuse to evolve further). Returns
// whether the migration happened.
//
// This is a persist path, so it clears the same safety gates evolveTreeV2
// clears before ITS save (quality gate, evidence gate, validation gate) rather
// than only the fitness/bloat checks below. It runs BEFORE the per-tree loop
// (RunCycleV2), so a gate this pass skipped could not be caught downstream:
// a randomly bred individual would already be on disk by the time
// evolveTreeV2 reached the same tree.
func (g *Gardener) adoptIslandWinner(entry TreeEntry, records []evolution.Record) bool {
	if entry.Tree == nil {
		return false
	}

	// Fail closed on a disabled quality gate, exactly as evolveTreeV2 does: the
	// tree has regressed ConsecutiveFails times running and is about to be
	// rolled back to its last known-good revision, so overwriting it here would
	// clobber the very state the rollback restores.
	if g.cfg.Gate != nil && g.cfg.Gate.IsDisabledFor(entry.Name) {
		slog.Warn("gardener/v2: quality gate DISABLED — skipping island adoption (fail-closed)",
			"tree", entry.Name, "consecutive_fails", g.cfg.Gate.FailCountFor(entry.Name))
		return false
	}

	// Evidence gate, mirroring evolveTreeV2: with no reflection records the
	// scores below are computed over an empty corpus, so "the winner beats the
	// live tree" is noise rather than a measured improvement.
	if len(records) == 0 && !g.cfg.EvolveWithoutReflections {
		return false
	}

	score := func(tree *evolution.SerializableNode) float64 {
		return evaluator.EvaluateTree(tree, records).Composite
	}
	live := score(entry.Tree)

	winner, winnerFitness := g.cfg.IslandModel.BestIndividualFor(entry.Name, score)
	if winner == nil || winnerFitness <= live+0.0001 {
		return false
	}
	if nodes := evolution.CountNodes(winner); nodes > baseNodeCount(entry.Name)*20 {
		slog.Warn("gardener/v2: island winner exceeds bloat cap, not migrating",
			"tree", entry.Name, "winner_nodes", nodes)
		return false
	}

	// Validation gate — checked before the in-place assignment, not after, so a
	// rejection leaves the live tree untouched and needs no restore.
	if err := ValidationGate(entry.Name, entry.Name, g.cfg.ValidationGate); err != nil {
		slog.Warn("gardener/v2: validation gate rejected island winner, not migrating",
			"tree", entry.Name, "error", err)
		return false
	}

	*entry.Tree = *winner
	slog.Info("gardener/v2: island exploration — migrating winner into live tree",
		"tree", entry.Name, "base_fitness", live, "winner_fitness", winnerFitness,
		"winner_nodes", evolution.CountNodes(entry.Tree))

	if err := g.cfg.Registry.SaveTree(TreeEntry{Name: entry.Name, Tree: entry.Tree, FilePath: entry.FilePath}); err != nil {
		slog.Error("gardener/v2: saving migrated island winner failed, migration is not durably persisted",
			"tree", entry.Name, "error", err)
	}
	return true
}

// RunCycleV2 executes one full evolution cycle using the v2 pipeline.
func (g *Gardener) RunCycleV2(cfg EvolveV2Config) ([]CycleMetrics, error) {
	g.cycleInFlight.Store(true)
	defer g.cycleInFlight.Store(false)

	entries := g.cfg.Registry.List()
	ranks := g.treePriorityRanks()
	slices.SortStableFunc(entries, func(a, b TreeEntry) int {
		return cmp.Or(
			cmp.Compare(treePriorityRank(ranks, a.Name), treePriorityRank(ranks, b.Name)),
			cmp.Compare(a.Name, b.Name),
		)
	})

	// ── Island-model population exploration ──
	// Runs before the per-tree pipeline so a migrated winner becomes this
	// cycle's baseline: evolveTreeV2 then mutates the champion the island
	// surfaced instead of the tree it superseded.
	var islandAdopted map[string]bool
	if cycle := int(g.cycleCount.Add(1)); g.islandPassDue(cycle) {
		islandAdopted = g.runIslandExploration(entries)
	}

	results := make([]CycleMetrics, 0, len(entries))
	var errs []error

	for _, entry := range entries {
		if !entry.Active {
			continue
		}

		start := time.Now()
		metrics := g.evolveTreeV2(entry, cfg)
		metrics.DurationMs = time.Since(start).Milliseconds()
		metrics.IslandAdopted = islandAdopted[entry.Name]
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
	sloData := CollectAgentSLOs(g.cfg.ValidationGate.EvidencePath)
	if len(sloData) > 0 {
		sloPath := filepath.Join(filepath.Dir(g.cfg.MetricsTracker.path), "slo-metrics.json")
		if err := exportSLOMetrics(sloPath, sloData); err != nil {
			slog.Error("gardener/v2: exporting SLO metrics failed, dashboard snapshot is stale", "path", sloPath, "error", err)
			errs = append(errs, fmt.Errorf("exporting SLO metrics: %w", err))
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

// exportSLOMetrics writes sloData to path via a temp-file-then-rename so a
// crash mid-write never leaves a truncated slo-metrics.json for the dashboard
// to read.
func exportSLOMetrics(path string, sloData map[string]float64) error {
	return util.SaveJSONAtomic(path, sloData)
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
		c.Metadata = evolution.CloneMetadata(t.Metadata)
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
