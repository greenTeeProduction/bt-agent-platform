package evolution

import (
	"math"
	"math/rand"
	"sort"
)

// ─── NSGA-II Multi-Objective Optimization ──────────────────────────────────
//
// Non-dominated Sorting Genetic Algorithm II (NSGA-II) for multi-objective BT
// optimization. Builds on the ParetoFront/MultiFitness types in pareto.go.
//
// Key NSGA-II features:
//  1. Fast non-dominated sorting — O(MN²) where M = objectives, N = pop size
//  2. Crowding distance — preserves diversity along the Pareto front
//  3. Crowded-comparison operator (<_n) — tournament selection that prefers
//     lower front rank, then higher crowding distance
//
// Research sources: daily/2026-05-27, daily/2026-05-29
// References: Deb et al. (2002) "A Fast and Elitist Multiobjective Genetic
// Algorithm: NSGA-II", IEEE TEC

// NSGAIISorter handles non-dominated sorting for a population.
type NSGAIISorter struct {
	Dimensions []FitnessDimension
}

// NewNSGAIISorter creates a sorter for the given objective dimensions.
func NewNSGAIISorter(dims []FitnessDimension) *NSGAIISorter {
	return &NSGAIISorter{Dimensions: dims}
}

// NSGAIIFront is a single Pareto front (rank) from non-dominated sorting.
type NSGAIIFront struct {
	Indices []int // indices into the population
}

// NSGAIIPopulation wraps a population with NSGA-II selection.
type NSGAIIPopulation struct {
	*Population
	Dimensions     []FitnessDimension                   `json:"dimensions"`
	FitnessVecs    []MultiFitness                       `json:"fitness_vecs"`  // multi-objective fitness per individual
	Fronts         []NSGAIIFront                        `json:"-"`             // non-dominated fronts
	CrowdingDist   []float64                            `json:"crowding_dist"` // per-individual crowding distance
	FitnessMultiFn func(*SerializableNode) MultiFitness `json:"-"`
}

// NewNSGAIIPopulation creates a population with NSGA-II support.
func NewNSGAIIPopulation(size int, baseTree *SerializableNode, dims []FitnessDimension) *NSGAIIPopulation {
	return &NSGAIIPopulation{
		Population:   NewPopulation(size, baseTree),
		Dimensions:   dims,
		FitnessVecs:  make([]MultiFitness, size),
		CrowdingDist: make([]float64, size),
	}
}

// Evaluate scores all individuals using the multi-fitness function and runs
// non-dominated sorting + crowding distance assignment.
func (nsga2 *NSGAIIPopulation) Evaluate(fitnessFn func(*SerializableNode) MultiFitness) {
	nsga2.FitnessMultiFn = fitnessFn
	for i := range nsga2.Individuals {
		fv := fitnessFn(nsga2.Individuals[i].Tree)
		nsga2.FitnessVecs[i] = fv
		// Use composite for scalar fitness (backward compat)
		nsga2.Individuals[i].Fitness = fv.CompositeScore(nil)
	}

	// Non-dominated sorting
	nsga2.Fronts = nsga2.fastNonDominatedSort(nsga2.FitnessVecs)

	// Assign crowding distance per front
	nsga2.CrowdingDist = make([]float64, len(nsga2.Individuals))
	for _, front := range nsga2.Fronts {
		nsga2.assignCrowdingDistance(front.Indices)
	}

	// Update best tree from front 0
	if len(nsga2.Fronts) > 0 && len(nsga2.Fronts[0].Indices) > 0 {
		bestIdx := nsga2.Fronts[0].Indices[0]
		nsga2.BestFitness = nsga2.Individuals[bestIdx].Fitness
		nsga2.BestTree = nsga2.Individuals[bestIdx].Tree
	}
}

// ─── Fast Non-Dominated Sort ────────────────────────────────────────────────

// fastNonDominatedSort performs NSGA-II's O(MN²) non-dominated sorting.
// Returns fronts where front[0] is the Pareto-optimal set, front[1] is the
// second-best set, etc.
func (nsga2 *NSGAIIPopulation) fastNonDominatedSort(fitnessVecs []MultiFitness) []NSGAIIFront {
	n := len(fitnessVecs)

	// dominationCount[i] = number of individuals dominating i
	// dominated[i] = list of individuals that i dominates
	dominationCount := make([]int, n)
	dominated := make([][]int, n)

	for i := 0; i < n; i++ {
		dominated[i] = make([]int, 0, n)
		for j := 0; j < n; j++ {
			if i == j {
				continue
			}
			if fitnessVecs[i].Dominates(fitnessVecs[j]) {
				dominated[i] = append(dominated[i], j)
			} else if fitnessVecs[j].Dominates(fitnessVecs[i]) {
				dominationCount[i]++
			}
		}
	}

	// Front 0: individuals with dominationCount == 0
	var fronts []NSGAIIFront
	currentFront := NSGAIIFront{Indices: make([]int, 0)}
	for i := 0; i < n; i++ {
		if dominationCount[i] == 0 {
			currentFront.Indices = append(currentFront.Indices, i)
		}
	}
	fronts = append(fronts, currentFront)

	// Iteratively generate subsequent fronts
	frontIdx := 0
	for len(fronts[frontIdx].Indices) > 0 {
		nextFront := NSGAIIFront{Indices: make([]int, 0)}
		for _, i := range fronts[frontIdx].Indices {
			for _, j := range dominated[i] {
				dominationCount[j]--
				if dominationCount[j] == 0 {
					nextFront.Indices = append(nextFront.Indices, j)
				}
			}
		}
		frontIdx++
		fronts = append(fronts, nextFront)
	}

	// Remove the last empty front
	return fronts[:len(fronts)-1]
}

// ─── Crowding Distance ────────────────────────────────────────────────────

// assignCrowdingDistance computes crowding distance for a set of front indices.
// Higher crowding distance = more isolated in objective space (preferred for diversity).
func (nsga2 *NSGAIIPopulation) assignCrowdingDistance(indices []int) {
	m := len(nsga2.Dimensions)
	if m == 0 || len(indices) <= 2 {
		// Boundary individuals get infinite distance
		for _, i := range indices {
			nsga2.CrowdingDist[i] = math.Inf(1)
		}
		return
	}

	n := len(indices)
	for _, i := range indices {
		nsga2.CrowdingDist[i] = 0
	}

	for _, dim := range nsga2.Dimensions {
		// Sort indices by this dimension
		sorted := make([]int, n)
		copy(sorted, indices)
		sort.Slice(sorted, func(a, b int) bool {
			return nsga2.FitnessVecs[sorted[a]].Get(dim) < nsga2.FitnessVecs[sorted[b]].Get(dim)
		})

		// Boundary points get infinite distance
		nsga2.CrowdingDist[sorted[0]] = math.Inf(1)
		nsga2.CrowdingDist[sorted[n-1]] = math.Inf(1)

		// Normalization factor
		fMin := nsga2.FitnessVecs[sorted[0]].Get(dim)
		fMax := nsga2.FitnessVecs[sorted[n-1]].Get(dim)
		range_ := fMax - fMin
		if range_ == 0 {
			range_ = 1
		}

		// Interior points
		for k := 1; k < n-1; k++ {
			prev := nsga2.FitnessVecs[sorted[k-1]].Get(dim)
			next := nsga2.FitnessVecs[sorted[k+1]].Get(dim)
			nsga2.CrowdingDist[sorted[k]] += (next - prev) / range_
		}
	}
}

// ─── Crowded Tournament Selection ──────────────────────────────────────────

// crowdedComparison implements NSGA-II's crowded-comparison operator (<_n).
// Returns true if individual i is better than individual j.
// Better = lower front rank OR same front rank + higher crowding distance.
func (nsga2 *NSGAIIPopulation) crowdedComparison(i, j int) bool {
	// Find front ranks
	rankI := nsga2.frontRank(i)
	rankJ := nsga2.frontRank(j)
	if rankI != rankJ {
		return rankI < rankJ // lower rank is better
	}
	// Same front: prefer higher crowding distance
	return nsga2.CrowdingDist[i] > nsga2.CrowdingDist[j]
}

// frontRank returns the front index (rank) for individual i.
func (nsga2 *NSGAIIPopulation) frontRank(i int) int {
	for rank, front := range nsga2.Fronts {
		for _, idx := range front.Indices {
			if idx == i {
				return rank
			}
		}
	}
	return len(nsga2.Fronts) // dominated by all
}

// TournamentSelect performs crowded tournament selection.
// Selects n parents from the population using k-tournament.
func (nsga2 *NSGAIIPopulation) TournamentSelect(k int) []*SerializableNode {
	parents := make([]*SerializableNode, 2)
	for j := 0; j < 2; j++ {
		best := -1
		for t := 0; t < k; t++ {
			idx := rand.Intn(len(nsga2.Individuals))
			if best == -1 || nsga2.crowdedComparison(idx, best) {
				best = idx
			}
		}
		parents[j] = nsga2.Individuals[best].Tree
	}
	return parents
}

// ─── NSGA-II Evolution ─────────────────────────────────────────────────────

// Evolve runs NSGA-II evolution for the given number of generations.
// Uses crowded tournament selection, simulated binary crossover (SBX),
// and polynomial mutation.
func (nsga2 *NSGAIIPopulation) Evolve(
	generations int,
	fitnessFn func(*SerializableNode) MultiFitness,
) *SerializableNode {
	nsga2.Evaluate(fitnessFn)
	popSize := len(nsga2.Individuals)

	for gen := 0; gen < generations; gen++ {
		nsga2.Generation++

		// Create offspring population via crowded tournament selection
		offspring := make([]Individual, popSize)
		for i := 0; i < popSize; i++ {
			parents := nsga2.TournamentSelect(3)
			child := Crossover(parents[0], parents[1])
			// Mutation
			if rand.Float64() < 0.3 {
				ops := randomMutation(child)
				ApplyMutations(child, ops)
			}
			fv := fitnessFn(child)
			offspring[i] = Individual{
				Tree:    child,
				Genome:  hashTree(child),
				Fitness: fv.CompositeScore(nil),
			}
		}

		// Combine parent + offspring populations (R_t = P_t ∪ Q_t)
		combined := make([]Individual, 2*popSize)
		copy(combined[:popSize], nsga2.Individuals)
		copy(combined[popSize:], offspring)

		combinedVecs := make([]MultiFitness, 2*popSize)
		for i := 0; i < popSize; i++ {
			combinedVecs[i] = nsga2.FitnessVecs[i]
		}
		for i := 0; i < popSize; i++ {
			combinedVecs[popSize+i] = fitnessFn(offspring[i].Tree)
		}

		// Non-dominated sort on combined population
		sorter := NewNSGAIISorter(nsga2.Dimensions)
		fronts := sorter.fastNonDominatedSort(combinedVecs)

		// Build next generation: fill from best fronts
		nextPop := make([]Individual, 0, popSize)
		nextVecs := make([]MultiFitness, 0, popSize)
		nextCrowding := make([]float64, 0, popSize)

		for _, front := range fronts {
			if len(nextPop)+len(front.Indices) <= popSize {
				// Take entire front
				for _, idx := range front.Indices {
					nextPop = append(nextPop, combined[idx])
					nextVecs = append(nextVecs, combinedVecs[idx])
				}
				// Assign crowding distance for this front (needed for next gen)
				nsga2.assignCrowdingDistance(front.Indices)
			} else {
				// Front is too large: sort by crowding distance, take the best
				remaining := popSize - len(nextPop)
				indices := front.Indices
				// Assign crowding distance to this front
				cd := sorter.assignCrowdingDistance(indices, combinedVecs)
				// Sort by crowding distance descending
				sort.Slice(indices, func(a, b int) bool {
					return cd[indices[a]] > cd[indices[b]]
				})
				for k := 0; k < remaining && k < len(indices); k++ {
					idx := indices[k]
					nextPop = append(nextPop, combined[idx])
					nextVecs = append(nextVecs, combinedVecs[idx])
				}
				break
			}
		}

		// Update population
		nsga2.Individuals = nextPop
		nsga2.FitnessVecs = nextVecs
		nsga2.CrowdingDist = nextCrowding // will be recalculated next Evaluate

		// Re-evaluate (re-assigns fronts and crowding distances)
		nsga2.Evaluate(fitnessFn)
	}

	return nsga2.BestTree
}

// ─── Standalone NSGA-II Sorter (for external use) ──────────────────────────

// fastNonDominatedSort on NSGAIISorter (static method variant).
func (s *NSGAIISorter) fastNonDominatedSort(fitnessVecs []MultiFitness) []NSGAIIFront {
	n := len(fitnessVecs)
	dominationCount := make([]int, n)
	dominated := make([][]int, n)

	for i := 0; i < n; i++ {
		dominated[i] = make([]int, 0, n)
		for j := 0; j < n; j++ {
			if i == j {
				continue
			}
			if fitnessVecs[i].Dominates(fitnessVecs[j]) {
				dominated[i] = append(dominated[i], j)
			} else if fitnessVecs[j].Dominates(fitnessVecs[i]) {
				dominationCount[i]++
			}
		}
	}

	var fronts []NSGAIIFront
	currentFront := NSGAIIFront{Indices: make([]int, 0)}
	for i := 0; i < n; i++ {
		if dominationCount[i] == 0 {
			currentFront.Indices = append(currentFront.Indices, i)
		}
	}
	fronts = append(fronts, currentFront)

	frontIdx := 0
	for len(fronts[frontIdx].Indices) > 0 {
		nextFront := NSGAIIFront{Indices: make([]int, 0)}
		for _, i := range fronts[frontIdx].Indices {
			for _, j := range dominated[i] {
				dominationCount[j]--
				if dominationCount[j] == 0 {
					nextFront.Indices = append(nextFront.Indices, j)
				}
			}
		}
		frontIdx++
		fronts = append(fronts, nextFront)
	}
	return fronts[:len(fronts)-1]
}

// assignCrowdingDistance computes crowding distances for a front (standalone).
func (s *NSGAIISorter) assignCrowdingDistance(indices []int, fitnessVecs []MultiFitness) map[int]float64 {
	dist := make(map[int]float64)
	m := len(s.Dimensions)
	if m == 0 || len(indices) <= 2 {
		for _, i := range indices {
			dist[i] = math.Inf(1)
		}
		return dist
	}

	n := len(indices)
	for _, i := range indices {
		dist[i] = 0
	}

	for _, dim := range s.Dimensions {
		sorted := make([]int, n)
		copy(sorted, indices)
		sort.Slice(sorted, func(a, b int) bool {
			return fitnessVecs[sorted[a]].Get(dim) < fitnessVecs[sorted[b]].Get(dim)
		})
		dist[sorted[0]] = math.Inf(1)
		dist[sorted[n-1]] = math.Inf(1)
		fMin := fitnessVecs[sorted[0]].Get(dim)
		fMax := fitnessVecs[sorted[n-1]].Get(dim)
		range_ := fMax - fMin
		if range_ == 0 {
			range_ = 1
		}
		for k := 1; k < n-1; k++ {
			prev := fitnessVecs[sorted[k-1]].Get(dim)
			next := fitnessVecs[sorted[k+1]].Get(dim)
			dist[sorted[k]] += (next - prev) / range_
		}
	}
	return dist
}
