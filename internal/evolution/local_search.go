package evolution

import (
	"cmp"
	"math"
	"math/rand"
	"slices"
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
	HillClimbSearch LocalSearchStrategy = iota
	SimulatedAnnealingSearch
	TabuSearch
)

// LocalSearcher performs local refinement of a behavior tree individual.
type LocalSearcher struct {
	Strategy      LocalSearchStrategy
	MaxIterations int     // max local search steps
	Temperature   float64 // for simulated annealing
	CoolingRate   float64 // temperature decay factor (0 < rate < 1)
	TabuTenure    int     // iterations a move stays in tabu list
	MutationProb  float64 // probability of mutating each step
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

// ─── Gated Refinement ────────────────────────────────────────────────────

// RefineResult is the outcome of a gated local-search refinement pass.
//
// Tree is the tuned tree when Accepted, and the caller's original tree
// (untouched) otherwise — a rejected refinement is never a reason to hand the
// caller a different tree than it came in with.
type RefineResult struct {
	Tree     *SerializableNode
	Fitness  float64    // fitness of Tree; baseFitness when rejected
	Delta    float64    // Fitness - baseFitness; 0 when rejected
	Accepted bool       // true only when the tuned tree beat baseFitness and cleared the gate
	Gate     GateResult // the gate's verdict on the pre→post pair
}

// RefineGated runs the configured strategy over tree's mutable parameters,
// scored by fitnessFn, and keeps the tuned tree only when it strictly beats
// baseFitness AND the quality gate accepts the baseFitness→tuned pair. It is
// the entry point evolveTreeV2 calls after its structural-mutation loop
// settles, so parameter tuning that no MutationOp can express still lands.
//
// The gate is consulted through Probe, not Validate/ValidateFor: a speculative
// tuning attempt that does not pan out is not a regression of the live tree, so
// it must not burn treeKey's consecutive-failure streak. Burning it here would
// eventually fail-close evolution for a perfectly healthy tree that simply has
// no tunable slack left. A nil gate skips the gate check entirely; a gate
// already disabled for treeKey refuses the refinement outright.
//
// The AND in that contract is load-bearing only because Probe judges the tuned
// score against QualityGate.MinComposite absolutely. The strict-improvement
// check below guarantees Probe always sees post > pre, so a gate that keyed on
// the pre→post direction alone could never refuse anything here and every
// refinement would reach the live tree ungated. What the gate still catches is
// a tuned tree that improved yet stayed under the health floor — a gain too
// small to be worth committing.
//
// tree is never mutated in place — the caller commits the result.
func (ls *LocalSearcher) RefineGated(
	tree *SerializableNode,
	baseFitness float64,
	fitnessFn func(*SerializableNode) float64,
	gate *QualityGate,
	treeKey string,
) RefineResult {
	rejected := RefineResult{Tree: tree, Fitness: baseFitness, Gate: GateRejected}
	if tree == nil || fitnessFn == nil {
		return rejected
	}
	if gate != nil && gate.IsDisabledFor(treeKey) {
		return rejected
	}

	tuned, delta := ls.Search(tree, fitnessFn)
	if tuned == nil || delta <= 0 {
		return rejected
	}

	tunedFitness := fitnessFn(tuned)
	if tunedFitness <= baseFitness {
		return rejected
	}

	if gate != nil {
		if verdict := gate.Probe(baseFitness, tunedFitness); verdict != GateAccepted {
			rejected.Gate = verdict
			return rejected
		}
	}

	return RefineResult{
		Tree:     tuned,
		Fitness:  tunedFitness,
		Delta:    tunedFitness - baseFitness,
		Accepted: true,
		Gate:     GateAccepted,
	}
}

// ─── Hill Climbing ───────────────────────────────────────────────────────

// hillClimb performs steepest-ascent hill climbing on a tree.
// At each step it evaluates every neighbor of every mutable parameter (see
// mutableParam.neighbors) and commits the single best strictly-improving move.
// Stops when no improvement is found or max iterations reached.
//
// The neighborhood is a deterministic ladder spanning coarse-to-fine moves in
// both directions rather than the single ±5% random nudge earlier revisions
// used. Real fitness landscapes here are step-shaped — estimateStructuralQuality
// only credits a Retry node whose MaxRetries falls in [1,5], so climbing down
// from 8 needs one move large enough to clear the plateau in a single step.
// A ±5% nudge on an integer-backed field rounds straight back to where it
// started, which made the whole pass inert on exactly the trees it targets.
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
		bestParam := -1
		bestValue := 0.0
		bestFitness := currentFitness

		// Steepest ascent: probe every neighbor of every parameter, then
		// commit only the best one, so a coarse move that clears a plateau
		// is not masked by a fine move that merely ties.
		for i := range params {
			original := params[i].getValue()
			for _, candidate := range params[i].neighbors() {
				params[i].setValue(candidate)
				if f := fitnessFn(current); f > bestFitness {
					bestFitness = f
					bestParam = i
					bestValue = candidate
				}
			}
			params[i].setValue(original) // revert — the winner is applied below
		}

		if bestParam < 0 {
			break // local optimum reached
		}
		params[bestParam].setValue(bestValue)
		currentFitness = bestFitness
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

		for range 5 {
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
// The crossover/mutation replacement step runs inside the same
// selfHealGeneration envelope Evolve, EvolveWithExperience, EvolveQLearning,
// NSGAIIPopulation.Evolve, and EvolvePareto use, so the seeded Specialists
// registry is observed and extinct specialists get resurrected on a
// population-level crisis; the local-search refinement step above stays
// untouched by that envelope.
func (p *Population) MemeticEvolve(
	generations int,
	fitnessFn func(*SerializableNode) float64,
	searcher *LocalSearcher,
	refineTopN int, // how many top individuals to refine per generation
) *SerializableNode {
	if len(p.Individuals) == 0 {
		return nil
	}
	p.Evaluate(fitnessFn)
	// Clamp so degenerate populations (size < 2) don't overflow the elite copy
	// or the top-N refine loop.
	eliteCount := min(max(2, len(p.Individuals)/10), len(p.Individuals))
	if refineTopN <= 0 {
		refineTopN = 1
	}
	supervisor := NewLLMSupervisor()

	for range generations {
		p.Generation++

		// Sort by fitness descending
		slices.SortFunc(p.Individuals, func(a, b Individual) int {
			return cmp.Compare(b.Fitness, a.Fitness)
		})

		// --- MEMETIC: Local search on top N individuals ---
		refineCount := min(refineTopN, eliteCount)
		for i := range refineCount {
			refined, delta := searcher.Search(p.Individuals[i].Tree, fitnessFn)
			if delta > 0 {
				p.Individuals[i].Tree = refined
				p.Individuals[i].Fitness += delta
				p.Individuals[i].Genome = hashTree(refined)
			}
		}

		p.selfHealGeneration(eliteCount, supervisor, func(mutationRate float64) {
			// Keep elites
			newPop := make([]Individual, len(p.Individuals))
			copy(newPop[:eliteCount], p.Individuals[:eliteCount])

			// Fill rest with crossover + mutation
			for i := eliteCount; i < len(p.Individuals); i++ {
				parents := p.Select()
				child := Crossover(parents[0], parents[1])
				if rand.Float64() < mutationRate {
					ops := randomMutation(child)
					ApplyMutations(child, ops)
				}
				newPop[i] = Individual{Tree: child, Genome: hashTree(child)}
			}

			p.Individuals = newPop
			p.Evaluate(fitnessFn)
		})

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
//
// integral marks a parameter backed by an integer field (SerializableNode's
// MaxRetries/TimeoutMs), so neighbor generation snaps candidates to whole
// numbers instead of proposing moves the setter would silently round away.
type mutableParam struct {
	node     *SerializableNode
	integral bool
	getter   func() float64
	setter   func(float64)
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

// paramStepFactors is the multiplicative ladder hill climbing probes around a
// parameter's current value: coarse enough (÷4, ×2) to clear a step-shaped
// plateau in one move, fine enough (±5%) to settle on a continuous landscape.
var paramStepFactors = []float64{0.25, 0.5, 0.75, 0.9, 0.95, 1.05, 1.1, 1.25, 1.5, 2.0}

// neighbors returns the candidate values hill climbing should evaluate for
// this parameter, deduplicated and excluding the current value. Integral
// parameters additionally get ±1 so small counts (a MaxRetries of 2) still
// have reachable neighbors once the multiplicative ladder rounds flat.
//
// Candidates never cross zero: a zero-valued parameter cannot be perturbed
// multiplicatively at all, and driving a positive knob (a retry count, a
// timeout) to zero or below disables the behavior rather than tuning it.
func (mp *mutableParam) neighbors() []float64 {
	current := mp.getValue()
	if current == 0 {
		return nil
	}

	seen := map[float64]bool{current: true}
	out := make([]float64, 0, len(paramStepFactors)+2)
	add := func(v float64) {
		if mp.integral {
			v = math.Round(v)
		}
		if (current > 0 && v <= 0) || (current < 0 && v >= 0) {
			return
		}
		if seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	}

	for _, f := range paramStepFactors {
		add(current * f)
	}
	if mp.integral {
		add(current - 1)
		add(current + 1)
	}
	return out
}

// extractMutableParams collects tunable numeric parameters from a tree: the
// node metadata knobs (timeout_ms, threshold) and SerializableNode's own
// MaxRetries/TimeoutMs struct fields.
//
// The struct fields matter as much as the metadata: trees built by
// internal/domains and the gardener registry set MaxRetries/TimeoutMs directly
// and leave Metadata nil, so a metadata-only extractor finds nothing to tune on
// exactly the production trees local search is meant to refine.
//
// Zero-valued fields are skipped — they are indistinguishable from "unset", and
// a multiplicative perturbation of 0 can never leave 0 anyway.
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

	// Extract from the struct fields themselves.
	if node.TimeoutMs != 0 {
		n := node // capture
		params = append(params, mutableParam{
			node:     n,
			integral: true,
			getter:   func() float64 { return float64(n.TimeoutMs) },
			setter:   func(v float64) { n.TimeoutMs = int64(math.Round(v)) },
		})
	}
	if node.MaxRetries != 0 {
		n := node // capture
		params = append(params, mutableParam{
			node:     n,
			integral: true,
			getter:   func() float64 { return float64(n.MaxRetries) },
			setter:   func(v float64) { n.MaxRetries = int(math.Round(v)) },
		})
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

func toFloat64(v any) (float64, bool) {
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
