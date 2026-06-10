package gardener

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/benchmark"
	"github.com/nico/go-bt-evolve/internal/evaluator"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
)

// EvolveV2Config controls the AlphaEvolve-derived evolution pipeline.
type EvolveV2Config struct {
	// Tiered evaluation cascade
	CascadeCfg evaluator.CascadeConfig

	// MAP-Elites diversity preservation
	MAPElitesEnabled  bool
	MAPElitesGridSize int // number of elites to preserve per domain

	// Multi-objective Pareto
	ParetoEnabled bool

	// Island model (domain separation)
	IslandEnabled     bool
	MigrationInterval int     // generations between cross-domain migration
	MigrationRate     float64 // fraction migrated

	// Model ensemble (Ollama + DeepSeek)
	EnsembleEnabled bool

	// Rich context injection
	RichContextEnabled bool

	// Evolution blocks (protect stable nodes)
	BlocksEnabled bool
	BlockConfig   evolution.BlockConfig

	// Meta-prompt evolution
	MetaPromptEnabled bool

	// Use real LLM or mock
	UseRealLLM bool
}

// DefaultEvolveV2Config returns sensible defaults for the AlphaEvolve pipeline.
func DefaultEvolveV2Config() EvolveV2Config {
	return EvolveV2Config{
		CascadeCfg:         evaluator.DefaultCascadeConfig(),
		MAPElitesEnabled:   true,
		MAPElitesGridSize:  5,
		ParetoEnabled:      true,
		IslandEnabled:      true,
		MigrationInterval:  5,
		MigrationRate:      0.1,
		EnsembleEnabled:    true,
		RichContextEnabled: true,
		BlocksEnabled:      true,
		BlockConfig:        evolution.DefaultBlockConfig(),
		MetaPromptEnabled:  true,
		UseRealLLM:         false, // use mock by default for speed
	}
}

// evolveTreeV2 runs the AlphaEvolve-derived evolution pipeline on a single tree.
// This replaces the old evolveTree with the full cascade: MAP-Elites → Cascade → Pareto → Mutate.
func (g *Gardener) evolveTreeV2(entry TreeEntry, cfg EvolveV2Config) CycleMetrics {
	tree := entry.Tree
	if tree == nil {
		return CycleMetrics{TreeName: entry.Name, Improved: false}
	}

	allRecords, _ := g.cfg.RefStore.LoadAll()
	records := evolution.FilterByTreeName(allRecords, entry.Name)
	baseFitness := evaluator.EvaluateTree(tree, records)
	nodesBefore := evolution.CountNodes(tree)
	domain := extractDomain(entry.Name)

	// ── P3: Evolution Blocks — check if tree can be mutated ──
	if cfg.BlocksEnabled {
		// Trees can always be evolved as a whole; block filtering is per-mutation
	}

	// ── P0: Evaluation Cascade — structural Quick check first ──
	cascadeStats := &evaluator.CascadeStats{Total: 1}
	quickScore := evaluator.StructuralQuickEval(tree)
	cascadeStats.PassedQuick = 1 // structural always passes

	if quickScore < cfg.CascadeCfg.QuickThreshold {
		return CycleMetrics{
			TreeName: entry.Name, Improved: false,
			BaseFitness: baseFitness.Composite, NewFitness: baseFitness.Composite,
			NodesBefore: nodesBefore, NodesAfter: nodesBefore,
		}
	}

	// ── P0: MAP-Elites Diversity Grid ──
	var mapGrid *evolution.MAPElitesGrid
	if cfg.MAPElitesEnabled {
		mapGrid = evolution.NewMAPElitesGrid(cfg.MAPElitesGridSize)
		// Seed grid with current tree
		desc := evolution.Descriptor(tree, domain)
		ind := &evolution.Individual{Tree: cloneTreeForGardener(tree), Fitness: baseFitness.Composite, Genome: hashTreeForGardener(tree)}
		mapGrid.Insert(desc, ind)
	}

	// ── P1: Multi-Objective Pareto Front ──
	var paretoFront *evolution.ParetoFront
	if cfg.ParetoEnabled {
		dims := []evolution.FitnessDimension{
			evolution.DimSuccessRate, evolution.DimPathCoverage,
			evolution.DimStability, evolution.DimNodeEfficiency, evolution.DimExecutionSpeed,
		}
		paretoFront = evolution.NewParetoFront(dims)
		fv := evolution.StructuralMultiFitness(tree)
		paretoFront.Add(&evolution.MultiIndividual{
			Individual: &evolution.Individual{Tree: cloneTreeForGardener(tree), Fitness: baseFitness.Composite, Genome: hashTreeForGardener(tree)},
			FitnessVec: fv,
		})
	}

	// ── P1: Model Ensemble + Rich Context ──
	var ensemble *llm.ModelEnsemble
	if cfg.EnsembleEnabled {
		var explorer llm.LLM
		var refiner llm.LLM
		if cfg.UseRealLLM {
			ollamaClient, err := llm.NewClient(llm.DefaultConfig())
			if err == nil {
				explorer = ollamaClient
			}
			// Refiner uses DeepSeek if available
			refiner = llm.NewDeepSeekClient(llm.DefaultDeepSeekConfig())
		}
		if explorer == nil {
			explorer = benchmark.DefaultMock()
		}
		ensemble = llm.NewModelEnsemble(llm.EnsembleConfig{
			Explorer: explorer,
			Refiner:  refiner,
		})
	}

	// ── P1: Rich Context — build mutation prompt ──
	var evoCtx *llm.EvolutionContext
	if cfg.RichContextEnabled {
		evoCtx = &llm.EvolutionContext{
			CurrentTree:    serializeTreeForGardener(tree),
			CurrentFitness: baseFitness.Composite,
			Domain:         domain,
			EvaluatorBreakdown: map[string]float64{
				"composite":     baseFitness.Composite,
				"success_rate":  baseFitness.SuccessRate,
				"path_coverage": baseFitness.PathCoverage,
				"stability":     baseFitness.Stability,
			},
			ResearchHints: llm.DefaultResearchHints(),
		}
		// If ensemble available, use it to generate a targeted mutation
		if ensemble != nil {
			prompt := llm.BuildMutationPrompt(*evoCtx)
			suggestion, err := ensemble.GenerateBreadth([]string{prompt})
			if err == nil && len(suggestion) > 0 && len(suggestion[0]) > 20 {
				truncated := suggestion[0]
				if len(truncated) > 100 {
					truncated = truncated[:100]
				}
				evoCtx.MutationHistory = append(evoCtx.MutationHistory, truncated)
			}
		}
	}

	// ── Generate and filter mutations ──
	// Use existing OrderMutations for compatibility, but filter through new components
	candidates := evaluator.OrderMutations(tree, records, baseFitness)

	// P3: Evolution Blocks — filter mutations targeting frozen blocks
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

	// ── P2: SEARCH/REPLACE Diff mutations (alternate path) ──
	// Also consider diff-based mutations alongside traditional ops
	_ = evolution.ApplyDiffMutation // available for future use

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
	gateDisabled := g.cfg.Gate != nil && g.cfg.Gate.IsDisabled()
	if gateDisabled {
		// Fail closed: a disabled gate means evolution is paused for this tree
		// until process restart — skip every candidate, apply nothing ungated.
		log.Printf("[gardener/v2] WARNING: quality gate is DISABLED for %s (consecutive_fails=%d) — mutations are being SKIPPED (fail-closed), evolution paused until restart",
			entry.Name, g.cfg.Gate.FailCount())
	}
	for i := 0; !gateDisabled && i < len(candidates) && applied < g.cfg.MaxMutations; i++ {
		if candidates[i].Score < 0.45 {
			break
		}

		// P2: SEARCH/REPLACE diff — try diff mutation as alternative
		// (traditional op-based mutations are the primary path)
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
			gateResult := g.cfg.Gate.Validate(currentFitness.Composite, candidateFitness.Composite)
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

			// P0: Update MAP-Elites grid with mutated tree
			if mapGrid != nil {
				desc := evolution.Descriptor(tree, domain)
				ind := &evolution.Individual{Tree: cloneTreeForGardener(tree), Fitness: candidateFitness.Composite, Genome: hashTreeForGardener(tree)}
				mapGrid.Insert(desc, ind)
			}

			// P1: Update Pareto front
			if paretoFront != nil {
				fv := evolution.StructuralMultiFitness(tree)
				paretoFront.Add(&evolution.MultiIndividual{
					Individual: &evolution.Individual{Tree: cloneTreeForGardener(tree), Fitness: candidateFitness.Composite, Genome: hashTreeForGardener(tree)},
					FitnessVec: fv,
				})
			}
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
		// ── P5: Validation Gate (Gap 5 — Decentralized Coordination) ──
		// Prevent deploying evolved trees that fail quality thresholds.
		// A rejection skips deployment but does NOT abort the cycle for other agents.
		gateErr := ValidationGate(entry.Name, entry.Name, g.cfg.ValidationGate)
		if gateErr != nil {
			log.Printf("[gardener/v2] %v — skipping deployment", gateErr)
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
		g.cfg.Registry.SaveTree(TreeEntry{Name: entry.Name, Tree: tree, FilePath: entry.FilePath})
	}

	// ── P3: Meta-Prompt Evolution — record outcome ──
	// (template success rate tracked per mutation type)

	// Log cascade stats
	if mapGrid != nil {
		stats := mapGrid.Stats()
		_ = stats // available for logging
	}
	if paretoFront != nil {
		paretoStats := paretoFront.Stats()
		_ = paretoStats
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

// RunCycleV2 executes one full evolution cycle using the AlphaEvolve-derived pipeline.
func (g *Gardener) RunCycleV2(cfg EvolveV2Config) ([]CycleMetrics, error) {
	entries := g.cfg.Registry.List()
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	var results []CycleMetrics

	// ── P2: Island Model — evolve all domains, migrate periodically ──
	var islandModel *evolution.IslandModel
	if cfg.IslandEnabled {
		islandModel = evolution.NewIslandModel(cfg.MigrationInterval, cfg.MigrationRate)
	}

	for _, entry := range entries {
		if !entry.Active {
			continue
		}
		// Skip heavy-IO documentation trees that run external commands
		if strings.Contains(entry.Name, "arc42") {
			continue
		}

		start := time.Now()
		metrics := g.evolveTreeV2(entry, cfg)
		metrics.DurationMs = time.Since(start).Milliseconds()
		results = append(results, metrics)

		g.cfg.MetricsTracker.Record(metrics)
	}

	// ── P2: Island Model — run migration after cycle ──
	if islandModel != nil && len(entries) > 1 {
		islandModel.Generation++
		if islandModel.Generation%cfg.MigrationInterval == 0 {
			migrated := islandModel.Migrate()
			if migrated > 0 {
				// Log migration (could be added to metrics)
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
			os.WriteFile(tmp, data, 0644)
			os.Rename(tmp, sloPath)
		}
	}

	g.cfg.MetricsTracker.Save()
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

func hashTreeForGardener(t *evolution.SerializableNode) string {
	// Simple stable hash based on name+type+node count
	return fmt.Sprintf("%x", len(t.Name)+len(t.Type)+evolution.CountNodes(t))
}

func serializeTreeForGardener(t *evolution.SerializableNode) string {
	if t == nil {
		return "(nil)"
	}
	return fmt.Sprintf("%s(%s)[%d children]", t.Type, t.Name, len(t.Children))
}

func extractDomain(name string) string {
	if strings.HasPrefix(name, "domain_") {
		return strings.TrimPrefix(name, "domain_")
	}
	if strings.HasPrefix(name, "finance_") {
		return "finance"
	}
	if strings.HasPrefix(name, "research_") {
		return "research"
	}
	switch name {
	case "godev":
		return "godev"
	case "default":
		return "default"
	default:
		return "general"
	}
}
