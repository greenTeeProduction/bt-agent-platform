package evolution

import (
	"math"
	"math/rand"
	"sort"
)

// ─── Memetic Local Search ────────────────────────────────────────────────
//
// Based on NotebookLM research (2026-05-26):
// Memetic Algorithms (MAs) combine global genetic algorithm search with
// focused local search heuristics. After GA crossover/mutation produces
// offspring, local search fine-tunes individual nodes for the final few
// mutations needed to reach the absolute optimum.
//
// References: Wikipedia (Genetic Algorithm, Memetic Algorithm)
//
// Three local search strategies are implemented:
//   1. Hill Climbing — deterministic greedy ascent
//   2. Simulated Annealing — probabilistic acceptance of worse moves
//   3. Tabu Search — maintains visited-state list to prevent cycling
//
// Sources: NotebookLM chat queries 2026-05-26, ensemble/expert system
// research (references [1,2,5] from Genetic Algorithm Wikipedia source)

// ─── Local Search Strategy ───────────────────────────────────────────────

// LocalSearchStrategy selects the refinement method.
type LocalSearchStrategy int

const (
	HillClimbSearch      LocalSearchStrategy = iota
	SimulatedAnnealingSearch
	TabuSearch
)

// LocalSearcher performs local refinement of a behavior tree individual.
type LocalSearcher struct {
	Strategy        LocalSearchStrategy
	MaxIterations   int     // max local search steps
	Temperature     float64 // for simulated annealing
	CoolingRate     float64 // temperature decay factor (0 < rate < 1)
	TabuTenure      int     // iterations a move stays in tabu list
	MutationProb    float64 // probability of mutating each step
}

// NewLocalSearcher creates a searcher with sensible defaults.
func NewLocalSearcher(strategy LocalSearchStrategy) *LocalSearcher {
	return &LocalSearcher{
		Strategy:      strategy,
		MaxIterations: 20,
		Temperature:   1.0,
		CoolingRate:   0.95,
		TabuTenure:    7,
		MutationProb:  0.3,
	}
}

// Search runs local search on a tree to improve its fitness.
// Returns the improved tree and the fitness delta achieved.
func (ls *LocalSearcher) Search(
	tree *SerializableNode,
	fitnessFn func(*SerializableNode) float64,
) (*SerializableNode, float64) {
	switch ls.Strategy {
	case HillClimbSearch:
		return ls.hillClimb(tree, fitnessFn)
	case SimulatedAnnealingSearch:
		return ls.simulatedAnnealing(tree, fitnessFn)
	case TabuSearch:
		return ls.tabuSearch(tree, fitnessFn)
	default:
		return tree, 0
	}
}

// ─── Hill Climbing ───────────────────────────────────────────────────────

// hillClimb performs steepest-ascent hill climbing on a tree.
// At each step, it generates a small mutation, evaluates it, and keeps it
// if fitness improves. Stops when no improvement is found or max iterations
// reached.
func (ls *LocalSearcher) hillClimb(
	tree *SerializableNode,
	fitnessFn func(*SerializableNode) float64,
) (*SerializableNode, float64) {
	current := cloneTree(tree)
	currentFitness := fitnessFn(current)
	initialFitness := currentFitness

	// Collect mutable parameters from the tree
	params := extractMutableParams(current)
	if len(params) == 0 {
		return current, 0
	}

	for iter := 0; iter < ls.MaxIterations; iter++ {
		improved := false

		// Try tweaking each mutable parameter
		for _, param := range params {
			original := param.getValue()
			// Small perturbation
			perturbed := original * (1.0 + (rand.Float64()-0.5)*0.1) // ±5%
			param.setValue(perturbed)

			newFitness := fitnessFn(current)
			if newFitness > currentFitness {
				currentFitness = newFitness
				improved = true
				// Keep this parameter change
			} else {
				param.setValue(original) // revert
			}
		}

		if !improved {
			break // local optimum reached
		}
	}

	return current, currentFitness - initialFitness
}

// ─── Simulated Annealing ─────────────────────────────────────────────────

// simulatedAnnealing uses Metropolis criterion: accept better moves always,
// accept worse moves with probability exp(-ΔE/T).
func (ls *LocalSearcher) simulatedAnnealing(
	tree *SerializableNode,
	fitnessFn func(*SerializableNode) float64,
) (*SerializableNode, float64) {
	current := cloneTree(tree)
	currentFitness := fitnessFn(current)
	initialFitness := currentFitness
	bestTree := cloneTree(current)
	bestFitness := currentFitness
	temp := ls.Temperature

	for iter := 0; iter < ls.MaxIterations; iter++ {
		// Generate a neighbor by mutation
		candidate := cloneTree(current)
		ops := randomMutation(candidate)
		ApplyMutations(candidate, ops)
		candidateFitness := fitnessFn(candidate)

		delta := candidateFitness - currentFitness

		// Accept if better, or probabilistically if worse
		if delta > 0 || rand.Float64() < math.Exp(delta/temp) {
			current = candidate
			currentFitness = candidateFitness

			if currentFitness > bestFitness {
				bestTree = cloneTree(current)
				bestFitness = currentFitness
			}
		}

		// Cool down
		temp *= ls.CoolingRate
		if temp < 1e-6 {
			break
		}
	}

	return bestTree, bestFitness - initialFitness
}

// ─── Tabu Search ─────────────────────────────────────────────────────────

// tabuEntry is a genome hash with time-to-live counter for the tabu list.
type tabuEntry struct {
	genome string
	ttl    int
}

// tabuSearch maintains a tabu list of recently visited genomes to prevent
// cycling. At each step, it evaluates multiple neighbors, picks the best
// non-tabu one, and moves there.
func (ls *LocalSearcher) tabuSearch(
	tree *SerializableNode,
	fitnessFn func(*SerializableNode) float64,
) (*SerializableNode, float64) {
	current := cloneTree(tree)
	currentFitness := fitnessFn(current)
	initialFitness := currentFitness
	bestTree := cloneTree(current)
	bestFitness := currentFitness

	// Tabu list is a FIFO of genome hashes
	tabuList := make([]tabuEntry, 0, ls.TabuTenure)

	for iter := 0; iter < ls.MaxIterations; iter++ {
		// Generate multiple candidate neighbors
		type candidate struct {
			tree    *SerializableNode
			fitness float64
			genome  string
		}
		candidates := make([]candidate, 0, 5)

		for k := 0; k < 5; k++ {
			cand := cloneTree(current)
			ops := randomMutation(cand)
			ApplyMutations(cand, ops)
			genome := hashTree(cand)
			candidates = append(candidates, candidate{
				tree:    cand,
				fitness: fitnessFn(cand),
				genome:  genome,
			})
		}

		// Pick best non-tabu candidate
		bestIdx := -1
		bestFit := -1e9
		for i, c := range candidates {
			if ls.isTabu(c.genome, tabuList) {
				continue
			}
			if c.fitness > bestFit {
				bestFit = c.fitness
				bestIdx = i
			}
		}

		if bestIdx < 0 {
			break // all candidates tabu
		}

		// Move to best candidate
		current = candidates[bestIdx].tree
		currentFitness = bestFit

		// Update tabu list
		tabuList = append(tabuList, tabuEntry{
			genome: candidates[bestIdx].genome,
			ttl:    ls.TabuTenure,
		})
		// Decrement TTLs and remove expired entries
		active := make([]tabuEntry, 0, len(tabuList))
		for _, te := range tabuList {
			te.ttl--
			if te.ttl > 0 {
				active = append(active, te)
			}
		}
		tabuList = active

		// Track best
		if currentFitness > bestFitness {
			bestTree = cloneTree(current)
			bestFitness = currentFitness
		}
	}

	return bestTree, bestFitness - initialFitness
}

// isTabu checks if a genome is in the tabu list.
func (ls *LocalSearcher) isTabu(genome string, tabuList []tabuEntry) bool {
	for _, te := range tabuList {
		if te.genome == genome {
			return true
		}
	}
	return false
}

// ─── Memetic Evolution ───────────────────────────────────────────────────

// MemeticEvolve runs the full memetic algorithm: GA + local search.
// After each generation of the genetic algorithm, the best individual(s)
// undergo local search refinement before being fed back into the population.
func (p *Population) MemeticEvolve(
	generations int,
	fitnessFn func(*SerializableNode) float64,
	searcher *LocalSearcher,
	refineTopN int, // how many top individuals to refine per generation
) *SerializableNode {
	p.Evaluate(fitnessFn)
	eliteCount := max(2, len(p.Individuals)/10)
	if refineTopN <= 0 {
		refineTopN = 1
	}

	for gen := 0; gen < generations; gen++ {
		p.Generation++

		// Sort by fitness descending
		sort.Slice(p.Individuals, func(i, j int) bool {
			return p.Individuals[i].Fitness > p.Individuals[j].Fitness
		})

		// --- MEMETIC: Local search on top N individuals ---
		refineCount := refineTopN
		if refineCount > eliteCount {
			refineCount = eliteCount
		}
		for i := 0; i < refineCount; i++ {
			refined, delta := searcher.Search(p.Individuals[i].Tree, fitnessFn)
			if delta > 0 {
				p.Individuals[i].Tree = refined
				p.Individuals[i].Fitness += delta
				p.Individuals[i].Genome = hashTree(refined)
			}
		}

		// Keep elites
		newPop := make([]Individual, len(p.Individuals))
		copy(newPop[:eliteCount], p.Individuals[:eliteCount])

		// Fill rest with crossover + mutation
		for i := eliteCount; i < len(p.Individuals); i++ {
			parents := p.Select()
			child := Crossover(parents[0], parents[1])
			if rand.Float64() < 0.3 {
				ops := randomMutation(child)
				ApplyMutations(child, ops)
			}
			newPop[i] = Individual{Tree: child, Genome: hashTree(child)}
		}

		p.Individuals = newPop
		p.Evaluate(fitnessFn)

		// Update best tree
		for i := range p.Individuals {
			if p.Individuals[i].Fitness > p.BestFitness {
				p.BestFitness = p.Individuals[i].Fitness
				p.BestTree = p.Individuals[i].Tree
			}
		}
	}

	return p.BestTree
}

// ─── Mutable Parameter Extraction ────────────────────────────────────────

// mutableParam represents a tunable numeric parameter in a tree node.
type mutableParam struct {
	node   *SerializableNode
	getter func() float64
	setter func(float64)
}

func (mp *mutableParam) getValue() float64 {
	if mp.getter != nil {
		return mp.getter()
	}
	return 0
}

func (mp *mutableParam) setValue(v float64) {
	if mp.setter != nil {
		mp.setter(v)
	}
}

// extractMutableParams collects tunable numeric parameters from a tree.
// These are stored in node metadata and include: timeout, max_retries,
// threshold values, etc.
func extractMutableParams(node *SerializableNode) []mutableParam {
	var params []mutableParam

	// Extract from metadata
	if node.Metadata != nil {
		// TimeoutMs
		if tm, ok := node.Metadata["timeout_ms"]; ok {
			if v, ok := toFloat64(tm); ok {
				n := node // capture
				params = append(params, mutableParam{
					node:   n,
					getter: func() float64 { return getFloatMeta(n, "timeout_ms") },
					setter: func(v float64) { setFloatMeta(n, "timeout_ms", v) },
				})
				_ = v // silence unused
			}
		}
		// Threshold
		if _, ok := node.Metadata["threshold"]; ok {
			n := node
			params = append(params, mutableParam{
				node:   n,
				getter: func() float64 { return getFloatMeta(n, "threshold") },
				setter: func(v float64) { setFloatMeta(n, "threshold", v) },
			})
		}
	}

	// Recurse into children
	for i := range node.Children {
		params = append(params, extractMutableParams(&node.Children[i])...)
	}

	return params
}

func getFloatMeta(node *SerializableNode, key string) float64 {
	if v, ok := node.Metadata[key]; ok {
		if f, ok := toFloat64(v); ok {
			return f
		}
	}
	return 0
}

func setFloatMeta(node *SerializableNode, key string, val float64) {
	if node.Metadata == nil {
		node.Metadata = make(map[string]any)
	}
	node.Metadata[key] = val
}

func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	default:
		return 0, false
	}
}
