package evolution

import (
	"fmt"
	"math/rand"
	"sort"
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

// String returns a compact representation.
func (mf MultiFitness) String() string {
	var parts []string
	for dim, score := range mf.Scores {
		parts = append(parts, fmt.Sprintf("%s=%.1f", dim, score))
	}
	sort.Strings(parts)
	return "{" + strings.Join(parts, " ") + "}"
}

// ParetoFront maintains the set of non-dominated individuals.
type ParetoFront struct {
	Individuals []*MultiIndividual `json:"individuals"`
	Dimensions  []FitnessDimension  `json:"dimensions"`
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

// Best returns all Pareto-optimal individuals sorted by composite score.
func (pf *ParetoFront) Best(n int) []*MultiIndividual {
	sort.Slice(pf.Individuals, func(i, j int) bool {
		ci := pf.Individuals[i].FitnessVec.CompositeScore(nil)
		cj := pf.Individuals[j].FitnessVec.CompositeScore(nil)
		return ci > cj
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
	Front  *ParetoFront
	FitnessFn func(*SerializableNode) MultiFitness
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
		return pp.Population.Select()
	}

	// Pick two parents from opposite ends of the Pareto front (maximally diverse)
	parents := make([]*SerializableNode, 2)
	parents[0] = front[0].Tree
	parents[1] = front[len(front)-1].Tree
	return parents
}

// EvolvePareto runs the genetic algorithm with Pareto multi-objective selection.
func (pp *ParetoPopulation) EvolvePareto(generations int, fitnessFn func(*SerializableNode) MultiFitness) *SerializableNode {
	pp.Evaluate(fitnessFn)
	eliteCount := max(2, len(pp.Individuals)/10)

	for gen := 0; gen < generations; gen++ {
		pp.Generation++

		// Sort by composite score
		sort.Slice(pp.Individuals, func(i, j int) bool {
			return pp.Individuals[i].Fitness > pp.Individuals[j].Fitness
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
			if rand.Float64() < 0.3 {
				ops := randomMutation(child)
				ApplyMutations(child, ops)
			}
			newPop[i] = Individual{Tree: child, Genome: hashTree(child)}
		}

		pp.Individuals = newPop
		pp.Evaluate(fitnessFn)
	}

	return pp.BestTree
}

// ParetoStats reports multi-objective metrics.
type ParetoStats struct {
	FrontSize      int                `json:"front_size"`
	DiversityScore float64            `json:"diversity_score"`
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
	maxDepth := maxTreeDepthEvo(tree, 0)

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
