package evolution

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// FitnessDimension names a single objective axis for multi-objective optimization.
type FitnessDimension string

const (
	DimSuccessRate    FitnessDimension = "success_rate"
	DimPathCoverage   FitnessDimension = "path_coverage"
	DimStability      FitnessDimension = "stability"
	DimNodeEfficiency FitnessDimension = "node_efficiency"
	DimExecutionSpeed FitnessDimension = "execution_speed"
	DimComposite      FitnessDimension = "composite"
	// DimRecoveryRate scores how much of a tree's observed failure volume it
	// recovered from. Sourced from SLO evidence rather than tree structure, so
	// StructuralMultiFitness leaves it unset; the gardener's validation gate
	// uses it as the second axis of its multi-objective acceptance check.
	DimRecoveryRate FitnessDimension = "recovery_rate"
)

// MultiFitness is a vector of fitness scores across multiple objectives.
// Each dimension is scored 0-100. Higher is always better.
type MultiFitness struct {
	Scores map[FitnessDimension]float64 `json:"scores"`
}

// NewMultiFitness creates an empty multi-fitness vector.
func NewMultiFitness() MultiFitness {
	return MultiFitness{Scores: make(map[FitnessDimension]float64)}
}

// Get returns the score for a dimension, or 0 if not set.
func (mf MultiFitness) Get(dim FitnessDimension) float64 {
	return mf.Scores[dim]
}

// Set sets the score for a dimension.
func (mf MultiFitness) Set(dim FitnessDimension, score float64) {
	mf.Scores[dim] = score
}

// CompositeScore computes a weighted composite from all dimensions.
// Weights default to 1.0 if not specified.
func (mf MultiFitness) CompositeScore(weights map[FitnessDimension]float64) float64 {
	if len(mf.Scores) == 0 {
		return 0
	}
	if weights == nil {
		weights = make(map[FitnessDimension]float64)
	}
	total := 0.0
	totalWeight := 0.0
	for dim, score := range mf.Scores {
		w := weights[dim]
		if w == 0 {
			w = 1.0
		}
		total += score * w
		totalWeight += w
	}
	if totalWeight == 0 {
		return 0
	}
	return total / totalWeight
}

// Dominates returns true if mf Pareto-dominates other.
// A dominates B if A is at least as good in ALL dimensions and STRICTLY better in at least one.
func (mf MultiFitness) Dominates(other MultiFitness) bool {
	allDims := make(map[FitnessDimension]bool)
	for dim := range mf.Scores {
		allDims[dim] = true
	}
	for dim := range other.Scores {
		allDims[dim] = true
	}

	better := false
	for dim := range allDims {
		a := mf.Get(dim)
		b := other.Get(dim)
		if a < b {
			return false // worse in at least one dimension
		}
		if a > b {
			better = true
		}
	}
	return better // strictly better in at least one dimension
}

// ParetoAccepts reports whether candidate survives multi-objective acceptance
// against a set of baselines: it is accepted exactly when it lands on front 0
// of a non-dominated sort over baselines ∪ {candidate}, i.e. when no baseline
// Pareto-dominates it.
//
// This is the acceptance rule that replaces scalar-fitness comparison. A
// scalar check collapses the objectives into one number (or, worse, tests each
// against its own threshold in isolation, making every objective a hard
// constraint) and so refuses any candidate that gives up ground on one axis —
// even when it gains far more on another. Non-domination accepts exactly the
// trade-offs and refuses exactly the strict regressions. A candidate that ties
// every baseline on every dimension is accepted: Dominates requires a strict
// win somewhere, so a tie is not a regression.
//
// With no baselines there is nothing to be dominated by, so the candidate is
// trivially on the front.
func ParetoAccepts(candidate MultiFitness, baselines []MultiFitness) bool {
	vecs := make([]MultiFitness, 0, len(baselines)+1)
	vecs = append(vecs, baselines...)
	candidateIdx := len(vecs)
	vecs = append(vecs, candidate)

	fronts := nonDominatedSort(vecs)
	if len(fronts) == 0 {
		return true
	}
	return slices.Contains(fronts[0].Indices, candidateIdx)
}

// String returns a compact representation.
func (mf MultiFitness) String() string {
	parts := make([]string, 0, 8)
	for dim, score := range mf.Scores {
		parts = append(parts, fmt.Sprintf("%s=%.1f", dim, score))
	}
	slices.Sort(parts)
	return "{" + strings.Join(parts, " ") + "}"
}

// ParetoFront maintains the set of non-dominated individuals.
type ParetoFront struct {
	Individuals []*MultiIndividual `json:"individuals"`
	Dimensions  []FitnessDimension `json:"dimensions"`
	Cap         int                `json:"cap,omitzero"` // max individuals for Save/Load (0 = unbounded)
}

// MultiIndividual extends Individual with multi-objective fitness.
type MultiIndividual struct {
	*Individual
	FitnessVec MultiFitness `json:"fitness_vec"`
}

// NewParetoFront creates an empty Pareto front.
func NewParetoFront(dims []FitnessDimension) *ParetoFront {
	return &ParetoFront{Dimensions: dims}
}

// Add inserts an individual into the Pareto front.
// If the new individual dominates existing members, they are removed.
// If the new individual is dominated by any existing member, it is rejected.
// Returns true if the individual was added.
func (pf *ParetoFront) Add(ind *MultiIndividual) bool {
	// Check if dominated by any existing member
	for _, existing := range pf.Individuals {
		if existing.FitnessVec.Dominates(ind.FitnessVec) {
			return false // rejected — existing is better on all dimensions
		}
	}

	// Remove any existing members that this one dominates
	filtered := make([]*MultiIndividual, 0, len(pf.Individuals))
	for _, existing := range pf.Individuals {
		if !ind.FitnessVec.Dominates(existing.FitnessVec) {
			filtered = append(filtered, existing)
		}
	}
	pf.Individuals = filtered

	// Add the new individual
	pf.Individuals = append(pf.Individuals, ind)
	return true
}

// AddFromPopulation evaluates all individuals against a multi-fitness function
// and adds the non-dominated ones to the Pareto front.
func (pf *ParetoFront) AddFromPopulation(pop *Population, fitnessFn func(*SerializableNode) MultiFitness) int {
	added := 0
	for i := range pop.Individuals {
		fv := fitnessFn(pop.Individuals[i].Tree)
		mi := &MultiIndividual{
			Individual: &pop.Individuals[i],
			FitnessVec: fv,
		}
		if pf.Add(mi) {
			added++
		}
	}
	return added
}

// Size returns the number of individuals on the Pareto front.
func (pf *ParetoFront) Size() int { return len(pf.Individuals) }

// paretoArchive is the durable JSON snapshot of a ParetoFront — just the
// front members, so archive consumers read the shape they already know.
type paretoArchive struct {
	Individuals []*MultiIndividual `json:"individuals"`
}

// cappedIndividuals bounds a slice of individuals to at most limit entries by
// evicting the lowest-fitness individuals first (ties broken by Genome for
// determinism). A limit of zero or less means unbounded. The input slice is
// never mutated; callers get either the original slice or a bounded copy.
func cappedIndividuals(individuals []*MultiIndividual, limit int) []*MultiIndividual {
	if limit <= 0 || len(individuals) <= limit {
		return individuals
	}
	sorted := slices.Clone(individuals)
	slices.SortFunc(sorted, func(a, b *MultiIndividual) int {
		return cmp.Or(
			cmp.Compare(b.Fitness, a.Fitness),
			cmp.Compare(a.Genome, b.Genome),
		)
	})
	return sorted[:limit]
}

// Save persists the front's individuals as JSON at path, creating missing
// parent directories and writing atomically (temp file + rename) under the
// shared advisory flock so concurrent writers cannot interleave partial
// archives (ADR-024). When Cap is set, only the Cap strongest individuals
// (by composite Fitness) are persisted — the weakest are evicted from the
// archive first. The in-memory front is left untouched.
func (pf *ParetoFront) Save(path string) error {
	data, err := json.MarshalIndent(paretoArchive{Individuals: cappedIndividuals(pf.Individuals, pf.Cap)}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pareto archive: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create pareto archive dir: %w", err)
	}
	release, err := acquireExperienceLock(path)
	if err != nil {
		return err
	}
	defer release()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write pareto archive: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("commit pareto archive: %w", err)
	}
	return nil
}

// Load warm-starts the front from the archive at path by merging disk
// individuals into memory via the existing dominance-based Add: a disk
// individual is dropped if anything already in memory dominates it, and it
// evicts any memory individual it dominates in turn. After the merge the
// front is bounded back to Cap by evicting the lowest-fitness individuals
// first. A missing archive is a silent cold start; a corrupt archive is an
// error that leaves the in-memory state untouched.
func (pf *ParetoFront) Load(path string) error {
	// Cold start before touching the flock sidecar: the archive directory may
	// not exist yet, and acquiring the lock would fail trying to create it.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	release, err := acquireExperienceLock(path)
	if err != nil {
		return err
	}
	defer release()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read pareto archive: %w", err)
	}
	var snap paretoArchive
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("parse pareto archive %s: %w", path, err)
	}
	for _, ind := range snap.Individuals {
		if ind == nil {
			continue
		}
		pf.Add(ind)
	}
	pf.Individuals = cappedIndividuals(pf.Individuals, pf.Cap)
	return nil
}

// Best returns all Pareto-optimal individuals sorted by composite score.
func (pf *ParetoFront) Best(n int) []*MultiIndividual {
	slices.SortFunc(pf.Individuals, func(a, b *MultiIndividual) int {
		return cmp.Compare(b.FitnessVec.CompositeScore(nil), a.FitnessVec.CompositeScore(nil))
	})
	if n > 0 && n < len(pf.Individuals) {
		return pf.Individuals[:n]
	}
	return pf.Individuals
}

// DiversityScore measures how spread out the Pareto front is.
// 0 = all individuals have identical fitness vectors, 1 = maximally diverse.
func (pf *ParetoFront) DiversityScore() float64 {
	if len(pf.Individuals) < 2 {
		return 0
	}

	// Compute variance per dimension
	totalVar := 0.0
	for _, dim := range pf.Dimensions {
		mean := 0.0
		for _, ind := range pf.Individuals {
			mean += ind.FitnessVec.Get(dim)
		}
		mean /= float64(len(pf.Individuals))

		variance := 0.0
		for _, ind := range pf.Individuals {
			diff := ind.FitnessVec.Get(dim) - mean
			variance += diff * diff
		}
		variance /= float64(len(pf.Individuals))
		totalVar += variance
	}

	// Normalize: max possible variance is 2500 (scores are 0-100, max std dev 50 → var 2500)
	maxVar := 2500.0 * float64(len(pf.Dimensions))
	if maxVar == 0 {
		return 0
	}
	div := totalVar / maxVar
	if div > 1 {
		div = 1
	}
	return div
}

// ParetoPopulation wraps a Population with a Pareto front for multi-objective evolution.
type ParetoPopulation struct {
	*Population
	Front     *ParetoFront
	FitnessFn func(*SerializableNode) MultiFitness
	// ExpertKnowledge is an optional, caller-owned learning archive that
	// EvolvePareto's mutation-application step observes every
	// genuinely-improving mutation into via Observe, mirroring the ek
	// plumbing Population.EvolveQLearning/qLearnMutate already have
	// (learning.go:845-921). A nil ExpertKnowledge is a no-op.
	ExpertKnowledge *ExpertKnowledge
}

// NewParetoPopulation creates a population with Pareto multi-objective optimization.
func NewParetoPopulation(size int, baseTree *SerializableNode, dims []FitnessDimension) *ParetoPopulation {
	return &ParetoPopulation{
		Population: NewPopulation(size, baseTree),
		Front:      NewParetoFront(dims),
	}
}

// Evaluate scores all individuals against the multi-objective fitness function.
func (pp *ParetoPopulation) Evaluate(fitnessFn func(*SerializableNode) MultiFitness) {
	pp.FitnessFn = fitnessFn
	pp.Front = NewParetoFront(pp.Front.Dimensions)

	for i := range pp.Individuals {
		fv := fitnessFn(pp.Individuals[i].Tree)
		pp.Individuals[i].Fitness = fv.CompositeScore(nil) // scalar for tournament selection
		pp.Front.Add(&MultiIndividual{
			Individual: &pp.Individuals[i],
			FitnessVec: fv,
		})
	}

	// Update best tree from Pareto front
	if pp.Front.Size() > 0 {
		best := pp.Front.Best(1)[0]
		pp.BestFitness = best.Fitness
		pp.BestTree = best.Tree
	}
}

// SelectPareto picks two diverse parents from different regions of the Pareto front.
func (pp *ParetoPopulation) SelectPareto() []*SerializableNode {
	front := pp.Front.Individuals
	if len(front) < 2 {
		return pp.Select()
	}

	// Pick two parents from opposite ends of the Pareto front (maximally diverse)
	parents := make([]*SerializableNode, 2)
	parents[0] = front[0].Tree
	parents[1] = front[len(front)-1].Tree
	return parents
}

// EvolvePareto runs the genetic algorithm with Pareto multi-objective selection.
// Each generation runs inside the same selfHealGeneration envelope Evolve and
// EvolveWithExperience use, so the seeded Specialists registry is observed and
// extinct specialists get resurrected on a population-level crisis — Evaluate
// already scalarizes MultiFitness into Individual.Fitness via CompositeScore,
// which is what the crisis detector reads.
func (pp *ParetoPopulation) EvolvePareto(generations int, fitnessFn func(*SerializableNode) MultiFitness) *SerializableNode {
	pp.Evaluate(fitnessFn)
	// Clamp so degenerate populations (size < 2) don't overflow the elite copy.
	eliteCount := min(max(2, len(pp.Individuals)/10), len(pp.Individuals))
	supervisor := NewLLMSupervisor()

	for range generations {
		pp.Generation++

		pp.selfHealGeneration(eliteCount, supervisor, func(mutationRate float64) {
			// Sort by composite score
			slices.SortFunc(pp.Individuals, func(a, b Individual) int {
				return cmp.Compare(b.Fitness, a.Fitness)
			})

			newPop := make([]Individual, len(pp.Individuals))

			// Keep Pareto front elites + top fitness
			copied := 0
			paretoElites := pp.Front.Best(eliteCount)
			for i := 0; i < len(paretoElites) && copied < eliteCount; i++ {
				newPop[copied] = *paretoElites[i].Individual
				copied++
			}
			// Fill remaining elite slots with top fitness
			for i := 0; copied < eliteCount && i < len(pp.Individuals); i++ {
				newPop[copied] = pp.Individuals[i]
				copied++
			}

			// Fill rest with crossover + mutation from Pareto-diverse parents
			for i := eliteCount; i < len(pp.Individuals); i++ {
				parents := pp.SelectPareto()
				child := Crossover(parents[0], parents[1])
				if evoFloat64() < mutationRate {
					ops := randomMutation(child)
					if len(ops) > 0 {
						ops[0] = materializeMutationOp(ops[0])
					}
					before := fitnessFn(child).CompositeScore(nil)
					if applied := ApplyMutations(child, ops); applied > 0 && len(ops) > 0 {
						after := fitnessFn(child).CompositeScore(nil)
						pp.ExpertKnowledge.Observe(ops[0].Operation, "pareto", after-before)
					}
				}
				newPop[i] = Individual{Tree: child, Genome: hashTree(child)}
			}

			pp.Individuals = newPop
			pp.Evaluate(fitnessFn)
		})
	}

	return pp.BestTree
}

// materializeMutationOp fills in the concrete Node payload for a
// generically-named mutation op (add_before/add_after/add_fallback) that
// randomMutation's fallback vocabulary leaves incomplete — ApplyMutations
// silently no-ops those without one (mutate.go:217-252). Without
// materializing a payload here, EvolvePareto's and NSGA-II Evolve's
// mutation-application steps could never register a genuine ExpertKnowledge
// gain: every op that survives application would be node-count-neutral or
// -decreasing, making an "improving" mutation structurally impossible to
// observe under a fitness function that rewards node count. Mirrors the
// payload MCTSMutator.concreteMutationOp synthesizes for the same op names
// (mcts_mutate.go:303-317), keeping the Target randomMutation already chose.
func materializeMutationOp(op MutationOp) MutationOp {
	if op.Node != nil {
		return op
	}
	switch op.Operation {
	case "add_before", "add_after":
		op.Node = &SerializableNode{Type: "Condition", Name: fmt.Sprintf("Evolved_%s_%d", op.Operation, evoIntn(1_000_000))}
	case "add_fallback":
		op.Node = &SerializableNode{Type: "Action", Name: fmt.Sprintf("Evolved_%s_%d", op.Operation, evoIntn(1_000_000))}
	}
	return op
}

// ParetoStats reports multi-objective metrics.
type ParetoStats struct {
	FrontSize      int                          `json:"front_size"`
	DiversityScore float64                      `json:"diversity_score"`
	BestPerDim     map[FitnessDimension]float64 `json:"best_per_dim"`
}

// Stats returns aggregate Pareto front statistics.
func (pf *ParetoFront) Stats() ParetoStats {
	stats := ParetoStats{
		FrontSize:      pf.Size(),
		DiversityScore: pf.DiversityScore(),
		BestPerDim:     make(map[FitnessDimension]float64),
	}

	for _, dim := range pf.Dimensions {
		for _, ind := range pf.Individuals {
			score := ind.FitnessVec.Get(dim)
			if score > stats.BestPerDim[dim] {
				stats.BestPerDim[dim] = score
			}
		}
	}
	return stats
}

// StructuralMultiFitness computes a multi-objective fitness vector from structural properties only.
// This is the Quick tier equivalent — no LLM calls.
func StructuralMultiFitness(tree *SerializableNode) MultiFitness {
	mf := NewMultiFitness()
	if tree == nil {
		return mf
	}

	nodeCount := CountNodes(tree)
	maxDepth := MaxDepth(tree, 0)

	// Success rate proxy: based on structure completeness
	hasConditions := countConditions(tree)
	hasActions := countActions(tree)
	srScore := 0.0
	if hasConditions >= 3 && hasActions >= 5 {
		srScore = 60
	} else if hasConditions >= 1 && hasActions >= 2 {
		srScore = 40
	} else {
		srScore = 20
	}
	// Bonus for balanced condition:action ratio
	if hasActions > 0 && hasConditions > 0 {
		ratio := float64(hasConditions) / float64(hasActions)
		if ratio >= 0.3 && ratio <= 1.5 {
			srScore += 20
		}
	}
	mf.Set(DimSuccessRate, clampScore(srScore))

	// Path coverage: more children = more paths
	pcScore := float64(len(tree.Children)) * 10
	if pcScore > 100 {
		pcScore = 100
	}
	mf.Set(DimPathCoverage, clampScore(pcScore))

	// Stability: moderate depth, moderate node count
	stabScore := 100.0
	if nodeCount < 5 {
		stabScore -= 20
	}
	if nodeCount > 50 {
		stabScore -= 30
	}
	if maxDepth > 8 {
		stabScore -= 20
	}
	if maxDepth < 2 {
		stabScore -= 10
	}
	mf.Set(DimStability, clampScore(stabScore))

	// Node efficiency: score is higher for moderate node counts
	neScore := 0.0
	if nodeCount >= 15 && nodeCount <= 35 {
		neScore = 80
	} else if nodeCount >= 5 && nodeCount <= 50 {
		neScore = 50
	} else {
		neScore = 20
	}
	mf.Set(DimNodeEfficiency, clampScore(neScore))

	// Execution speed: shallower trees are faster
	esScore := 100.0 - float64(maxDepth)*8
	if esScore < 10 {
		esScore = 10
	}
	mf.Set(DimExecutionSpeed, clampScore(esScore))

	return mf
}

func countConditions(node *SerializableNode) int {
	if node == nil {
		return 0
	}
	c := 0
	if node.Type == "Condition" {
		c++
	}
	for i := range node.Children {
		c += countConditions(&node.Children[i])
	}
	return c
}

func countActions(node *SerializableNode) int {
	if node == nil {
		return 0
	}
	a := 0
	if node.Type == "Action" {
		a++
	}
	for i := range node.Children {
		a += countActions(&node.Children[i])
	}
	return a
}

func clampScore(s float64) float64 {
	if s < 0 {
		return 0
	}
	if s > 100 {
		return 100
	}
	return s
}
