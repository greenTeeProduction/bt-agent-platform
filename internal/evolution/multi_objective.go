package evolution

import (
	"cmp"
	"math"
	"slices"
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
	Cap            int                                  `json:"cap,omitzero"` // max individuals for Save/Load (0 = unbounded)
	Archive        *ParetoFront                         `json:"-"`            // durable cross-run archive of front 0, populated by Load
	// ExpertKnowledge is an optional, caller-owned learning archive that
	// Evolve's offspring mutation step observes every genuinely-improving
	// mutation into via Observe, mirroring the ek plumbing
	// Population.EvolveQLearning/qLearnMutate already have
	// (learning.go:845-921) and EvolvePareto (pareto.go). A nil
	// ExpertKnowledge is a no-op.
	ExpertKnowledge *ExpertKnowledge `json:"-"`
}

// NewNSGAIIPopulation creates a population with NSGA-II support. Specialists
// is seeded up front — mirroring the nil-guard IslandModel.EvolveAll applies
// in island.go — so the self-healing envelope has a registry to consult from
// the very first generation instead of only after some other caller happens
// to set one.
func NewNSGAIIPopulation(size int, baseTree *SerializableNode, dims []FitnessDimension) *NSGAIIPopulation {
	pop := NewPopulation(size, baseTree)
	pop.Specialists = NewSpecialistRegistry()
	return &NSGAIIPopulation{
		Population:   pop,
		Dimensions:   dims,
		FitnessVecs:  make([]MultiFitness, size),
		CrowdingDist: make([]float64, size),
	}
}

// frontZeroIndividuals wraps the current Pareto-optimal front (Fronts[0]) as
// MultiIndividuals so it can be handed to a ParetoFront for persistence —
// front 0 members are mutually non-dominated by construction, same as
// ParetoFront.Individuals, so the existing dominance-based Save/Load logic
// applies unchanged. Returns nil if no non-dominated sort has run yet.
func (nsga2 *NSGAIIPopulation) frontZeroIndividuals() []*MultiIndividual {
	if len(nsga2.Fronts) == 0 {
		return nil
	}
	front0 := nsga2.Fronts[0].Indices
	individuals := make([]*MultiIndividual, 0, len(front0))
	for _, idx := range front0 {
		individuals = append(individuals, &MultiIndividual{
			Individual: &nsga2.Individuals[idx],
			FitnessVec: nsga2.FitnessVecs[idx],
		})
	}
	return individuals
}

// Save persists the current Pareto-optimal front (Fronts[0]) as a durable
// cross-run archive at path, evicting the weakest individuals first when Cap
// is set — mirrors ParetoFront.Save (pareto.go), which this delegates to.
func (nsga2 *NSGAIIPopulation) Save(path string) error {
	archive := &ParetoFront{
		Individuals: nsga2.frontZeroIndividuals(),
		Dimensions:  nsga2.Dimensions,
		Cap:         nsga2.Cap,
	}
	return archive.Save(path)
}

// Load warm-starts Archive from the archive at path, merging disk
// individuals into the current front 0 via dominance — mirrors
// ParetoFront.Load (pareto.go), which this delegates to. A missing archive is
// a silent cold start that leaves Archive populated from front 0 alone.
func (nsga2 *NSGAIIPopulation) Load(path string) error {
	archive := &ParetoFront{
		Individuals: nsga2.frontZeroIndividuals(),
		Dimensions:  nsga2.Dimensions,
		Cap:         nsga2.Cap,
	}
	if err := archive.Load(path); err != nil {
		return err
	}
	nsga2.Archive = archive
	return nil
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

// ─── Fast Non-Dominated Sort (canonical) ────────────────────────────────────

// nonDominatedSort is THE implementation of NSGA-II's O(MN²) fast
// non-dominated sort. Returns fronts where front[0] is the Pareto-optimal set,
// front[1] is the second-best set, etc.
//
// Both NSGAIIPopulation.fastNonDominatedSort (read by Evaluate) and
// NSGAIISorter.fastNonDominatedSort (read by Evolve's offspring replacement
// step) delegate here. They used to carry byte-for-byte copies of this loop,
// which meant a correctness fix could land in one caller's path and silently
// miss the other's — Evaluate's fronts and Evolve's replacement fronts could
// disagree about the same population.
func nonDominatedSort(fitnessVecs []MultiFitness) []NSGAIIFront {
	n := len(fitnessVecs)

	// dominationCount[i] = number of individuals dominating i
	// dominated[i] = list of individuals that i dominates
	dominationCount := make([]int, n)
	dominated := make([][]int, n)

	for i := range n {
		dominated[i] = make([]int, 0, n)
		for j := range n {
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
	for i := range n {
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

// fastNonDominatedSort sorts fitnessVecs into Pareto fronts. Thin wrapper over
// the canonical nonDominatedSort.
func (nsga2 *NSGAIIPopulation) fastNonDominatedSort(fitnessVecs []MultiFitness) []NSGAIIFront {
	return nonDominatedSort(fitnessVecs)
}

// ─── Crowding Distance (canonical) ─────────────────────────────────────────

// crowdingDistances is THE implementation of NSGA-II's crowding-distance
// assignment for one front. Higher distance = more isolated in objective space
// (preferred for diversity); the two boundary members along any dimension are
// infinitely far apart and always survive truncation.
//
// Both NSGAIIPopulation.assignCrowdingDistance (slice-valued, indexed by
// individual) and NSGAIISorter.assignCrowdingDistance (map-valued) delegate
// here so the arithmetic exists once. indices is never mutated.
func crowdingDistances(indices []int, fitnessVecs []MultiFitness, dims []FitnessDimension) map[int]float64 {
	dist := make(map[int]float64, len(indices))
	if len(dims) == 0 || len(indices) <= 2 {
		// Degenerate front: every member is a boundary member.
		for _, i := range indices {
			dist[i] = math.Inf(1)
		}
		return dist
	}

	n := len(indices)
	for _, i := range indices {
		dist[i] = 0
	}

	sorted := make([]int, n)
	for _, dim := range dims {
		// Sort a copy of the front by this dimension, ascending.
		copy(sorted, indices)
		slices.SortFunc(sorted, func(x, y int) int {
			return cmp.Compare(fitnessVecs[x].Get(dim), fitnessVecs[y].Get(dim))
		})

		// Boundary points get infinite distance.
		dist[sorted[0]] = math.Inf(1)
		dist[sorted[n-1]] = math.Inf(1)

		// Normalization factor.
		fMin := fitnessVecs[sorted[0]].Get(dim)
		fMax := fitnessVecs[sorted[n-1]].Get(dim)
		range_ := fMax - fMin
		if range_ == 0 {
			range_ = 1
		}

		// Interior points accumulate their normalized neighbour gap.
		for k := 1; k < n-1; k++ {
			prev := fitnessVecs[sorted[k-1]].Get(dim)
			next := fitnessVecs[sorted[k+1]].Get(dim)
			dist[sorted[k]] += (next - prev) / range_
		}
	}
	return dist
}

// assignCrowdingDistance computes crowding distance for a set of front indices
// and writes it into the population's parallel CrowdingDist slice. Thin
// wrapper over the canonical crowdingDistances.
func (nsga2 *NSGAIIPopulation) assignCrowdingDistance(indices []int) {
	for idx, d := range crowdingDistances(indices, nsga2.FitnessVecs, nsga2.Dimensions) {
		nsga2.CrowdingDist[idx] = d
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
		if slices.Contains(front.Indices, i) {
			return rank
		}
	}
	return len(nsga2.Fronts) // dominated by all
}

// TournamentSelect performs crowded tournament selection.
// Selects n parents from the population using k-tournament.
func (nsga2 *NSGAIIPopulation) TournamentSelect(k int) []*SerializableNode {
	parents := make([]*SerializableNode, 2)
	for j := range 2 {
		best := -1
		for range k {
			idx := evoIntn(len(nsga2.Individuals))
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
// and polynomial mutation. Each generation's offspring/replacement step runs
// inside the same selfHealGeneration envelope Evolve, EvolveWithExperience
// (learning.go), and EvolvePareto (pareto.go) already use, so the seeded
// Specialists registry is observed and extinct specialists get resurrected on
// a population-level crisis, matching every other production Evolve variant.
func (nsga2 *NSGAIIPopulation) Evolve(
	generations int,
	fitnessFn func(*SerializableNode) MultiFitness,
) *SerializableNode {
	nsga2.Evaluate(fitnessFn)
	popSize := len(nsga2.Individuals)
	// Clamp so degenerate populations (size < 2) don't overflow the elite copy.
	eliteCount := min(max(2, popSize/10), popSize)
	supervisor := NewLLMSupervisor()

	for range generations {
		nsga2.Generation++

		nsga2.selfHealGeneration(eliteCount, supervisor, func(mutationRate float64) {
			// selfHealGeneration just re-sorted Individuals by scalar Fitness,
			// which desyncs the parallel FitnessVecs/Fronts/CrowdingDist
			// slices from the new order — re-evaluate so tournament selection
			// below reads consistent state.
			nsga2.Evaluate(fitnessFn)

			// Create offspring population via crowded tournament selection
			offspring := make([]Individual, popSize)
			for i := range popSize {
				parents := nsga2.TournamentSelect(3)
				child := Crossover(parents[0], parents[1])
				// Mutation
				if evoFloat64() < mutationRate {
					ops := randomMutation(child)
					if len(ops) > 0 {
						ops[0] = materializeMutationOp(ops[0])
					}
					before := fitnessFn(child).CompositeScore(nil)
					if applied := ApplyMutations(child, ops); applied > 0 && len(ops) > 0 {
						after := fitnessFn(child).CompositeScore(nil)
						nsga2.ExpertKnowledge.Observe(ops[0].Operation, "nsga2", after-before)
					}
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
			copy(combinedVecs[:popSize], nsga2.FitnessVecs)
			for i := range popSize {
				combinedVecs[popSize+i] = fitnessFn(offspring[i].Tree)
			}

			// Non-dominated sort on combined population
			sorter := NewNSGAIISorter(nsga2.Dimensions)
			fronts := sorter.fastNonDominatedSort(combinedVecs)

			// Build next generation: fill from best fronts
			nextPop := make([]Individual, 0, popSize)
			nextVecs := make([]MultiFitness, 0, popSize)

			for _, front := range fronts {
				if len(nextPop)+len(front.Indices) <= popSize {
					// Take entire front
					for _, idx := range front.Indices {
						nextPop = append(nextPop, combined[idx])
						nextVecs = append(nextVecs, combinedVecs[idx])
					}
					// Crowding distance is recalculated below by Evaluate against the
					// rebuilt population; assigning it here would index the pre-update
					// slices with combined-population indices (out of range).
				} else {
					// Front is too large: sort by crowding distance, take the best
					remaining := popSize - len(nextPop)
					indices := front.Indices
					// Assign crowding distance to this front
					cd := sorter.assignCrowdingDistance(indices, combinedVecs)
					// Sort by crowding distance descending
					slices.SortFunc(indices, func(x, y int) int {
						return cmp.Compare(cd[y], cd[x])
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

			// Re-evaluate (re-assigns fronts and crowding distances)
			nsga2.Evaluate(fitnessFn)
		})
	}

	return nsga2.BestTree
}

// ─── Standalone NSGA-II Sorter (for external use) ──────────────────────────

// fastNonDominatedSort sorts fitnessVecs into Pareto fronts (standalone
// variant, for callers holding no population). Thin wrapper over the canonical
// nonDominatedSort — see that function for why this is not a second copy.
func (s *NSGAIISorter) fastNonDominatedSort(fitnessVecs []MultiFitness) []NSGAIIFront {
	return nonDominatedSort(fitnessVecs)
}

// assignCrowdingDistance computes crowding distances for a front (standalone).
// Thin wrapper over the canonical crowdingDistances.
func (s *NSGAIISorter) assignCrowdingDistance(indices []int, fitnessVecs []MultiFitness) map[int]float64 {
	return crowdingDistances(indices, fitnessVecs, s.Dimensions)
}
