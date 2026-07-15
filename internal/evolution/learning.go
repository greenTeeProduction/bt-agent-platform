package evolution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// ─── Genetic Algorithm Engine ───

// Individual represents one tree in the population.
type Individual struct {
	Tree    *SerializableNode  `json:"tree"`
	Fitness float64            `json:"fitness"`
	Genome  string             `json:"genome"`         // SHA256 hash of serialized tree
	Meta    *EvolutionMetadata `json:"meta,omitempty"` // specialist provenance for SpecialistRegistry.Observe
}

// Population is a generation of individuals.
type Population struct {
	Individuals         []Individual      `json:"individuals"`
	Generation          int               `json:"generation"`
	BestFitness         float64           `json:"best_fitness"`
	PrevBestFitness     float64           `json:"prev_best_fitness"`
	BestTree            *SerializableNode `json:"-"`
	TotalMutations      int               `json:"total_mutations"`
	Regressions         int               `json:"regressions"`
	NicheDiversityScore float64           `json:"niche_diversity"`

	// Crisis wires proactive population-level crisis detection into the GA
	// loop. It is lazily initialized on first Evolve so death spirals
	// (diversity collapse, regression spirals, quality crashes) are detected
	// as they happen rather than silently converging. Not serialized.
	Crisis *CrisisDetector `json:"-"`
	// CrisisReasons accumulates the unique population-level crisis reasons
	// DetectPopulation surfaced across the most recent Evolve run, in
	// first-seen order (e.g. "diversity_collapse", "regression_spiral").
	CrisisReasons []string `json:"crisis_reasons,omitempty"`
	// Specialists preserves the best validated archetype per specialist family
	// across generations so a diversity-collapse or extinction can be reversed
	// by resurrecting a lost niche instead of silently converging. Validated
	// elites are Observe'd into it every generation, and when a crisis is
	// detected any extinct high-fitness specialist is Resurrect'd back into the
	// population in place of the weakest non-elite individual. Nil disables
	// specialist archiving/resurrection. Not serialized.
	Specialists *SpecialistRegistry `json:"-"`
	// LastMutationRate records the mutation rate actually applied in the most
	// recent generation of an Evolve run. It equals the supervisor's
	// recommended rate for a healthy generation, or the CrisisDetector's
	// emergency rate when proactive crisis intervention overrode it. Not
	// serialized; it exists so callers (and tests) can observe whether a
	// generation ran under emergency control.
	LastMutationRate float64 `json:"-"`
	// Resurrections counts how many extinct specialists were successfully
	// resurrected and injected back into the population across the Evolve runs
	// of this Population — the crisis-recovery half of the specialist loop.
	// Only actual injections count; a Resurrect that finds no replaceable slot
	// does not.
	Resurrections int `json:"resurrections,omitempty"`
}

// PopulationHealth is a read-only snapshot of the GA's self-healing signals —
// what crises fired, the mutation rate actually applied, where the run stands,
// and how many specialists were resurrected — so metrics and dashboard
// consumers can observe population health without reaching into Evolve
// internals.
type PopulationHealth struct {
	// CrisisReasons are the unique population-level crisis reasons surfaced
	// across the most recent Evolve run, in first-seen order.
	CrisisReasons []string `json:"crisis_reasons,omitempty"`
	// LastMutationRate is the mutation rate actually applied in the most
	// recent generation — the emergency rate if crisis intervention overrode
	// the supervisor's recommendation.
	LastMutationRate float64 `json:"last_mutation_rate"`
	// Generation is the population's post-run generation counter.
	Generation int `json:"generation"`
	// Resurrections is how many extinct specialists were resurrected and
	// injected back into the population.
	Resurrections int `json:"resurrections"`
}

// HealthSnapshot exposes the population's self-healing state as a single
// PopulationHealth value. The CrisisReasons slice is copied so consumers
// cannot mutate the population's accumulated reasons.
func (p *Population) HealthSnapshot() PopulationHealth {
	reasons := make([]string, len(p.CrisisReasons))
	copy(reasons, p.CrisisReasons)
	return PopulationHealth{
		CrisisReasons:    reasons,
		LastMutationRate: p.LastMutationRate,
		Generation:       p.Generation,
		Resurrections:    p.Resurrections,
	}
}

// NewPopulation creates an initial population by mutating a base tree.
func NewPopulation(size int, baseTree *SerializableNode) *Population {
	pop := &Population{
		Individuals: make([]Individual, size),
		Generation:  0,
	}
	pop.Individuals[0] = Individual{Tree: cloneTree(baseTree), Genome: hashTree(baseTree)}
	for i := 1; i < size; i++ {
		mutated := cloneTree(baseTree)
		// Apply random mutation
		ops := randomMutation(mutated)
		ApplyMutations(mutated, ops)
		pop.Individuals[i] = Individual{Tree: mutated, Genome: hashTree(mutated)}
	}
	return pop
}

// Evaluate scores every individual.
func (p *Population) Evaluate(fitnessFn func(*SerializableNode) float64) {
	best := 0.0
	for i := range p.Individuals {
		p.Individuals[i].Fitness = fitnessFn(p.Individuals[i].Tree)
		if p.Individuals[i].Fitness > best {
			best = p.Individuals[i].Fitness
			p.BestTree = p.Individuals[i].Tree
		}
	}
	p.BestFitness = best
}

// Select returns parents via tournament selection (k=3).
func (p *Population) Select() []*SerializableNode {
	parents := make([]*SerializableNode, 2)
	for j := 0; j < 2; j++ {
		// Seed best/bestFit from the first draw instead of a sentinel like
		// -1.0: fitness functions (e.g. structuralFitnessFn's unbounded
		// anti-pattern penalty) can legitimately return values <= -1.0, which
		// would otherwise leave best unset and index Individuals[-1].
		best := rand.Intn(len(p.Individuals))
		bestFit := p.Individuals[best].Fitness
		for k := 1; k < 3; k++ {
			idx := rand.Intn(len(p.Individuals))
			if p.Individuals[idx].Fitness > bestFit {
				bestFit = p.Individuals[idx].Fitness
				best = idx
			}
		}
		parents[j] = p.Individuals[best].Tree
	}
	return parents
}

// Crossover produces an offspring by swapping subtrees.
func Crossover(a, b *SerializableNode) *SerializableNode {
	child := cloneTree(a)
	// Pick a random node in child and replace with random node from b
	if len(child.Children) > 0 {
		childIdx := rand.Intn(len(child.Children))
		if len(b.Children) > 0 {
			bIdx := rand.Intn(len(b.Children))
			child.Children[childIdx] = *cloneTree(&b.Children[bIdx])
		}
	}
	return child
}

// Evolve runs the genetic algorithm for N generations with quality gate.
func (p *Population) Evolve(generations int, fitnessFn func(*SerializableNode) float64) *SerializableNode {
	if len(p.Individuals) == 0 {
		return nil
	}
	p.Evaluate(fitnessFn)
	p.PrevBestFitness = p.BestFitness
	// Clamp so degenerate populations (size < 2) don't overflow the elite copy.
	eliteCount := min(max(2, len(p.Individuals)/10), len(p.Individuals))
	supervisor := NewLLMSupervisor()

	for gen := 0; gen < generations; gen++ {
		p.Generation++

		// Record baseline fitness of each individual BEFORE mutation, and
		// keep the newly bred population, so the quality gate below can
		// compare post-reproduction fitness against it.
		var baselineFitness []float64

		p.selfHealGeneration(eliteCount, supervisor, func(mutationRate float64) {
			baselineFitness = make([]float64, len(p.Individuals))
			for i := range p.Individuals {
				baselineFitness[i] = p.Individuals[i].Fitness
			}

			// Keep elites
			newPop := make([]Individual, len(p.Individuals))
			copy(newPop[:eliteCount], p.Individuals[:eliteCount])

			// Create MCTS mutator if not already created (lazy init)
			mctsMutator := NewMCTSMutator()
			mctsMutator.Iterations = 5 // K=5 for speed; use 10 for deeper search
			mctsMutator.FitnessEvaluator = fitnessFn

			// Fill rest with crossover + MCTS-guided mutation
			for i := eliteCount; i < len(p.Individuals); i++ {
				parents := p.Select()
				child := Crossover(parents[0], parents[1])
				// Mutate with MCTS-guided search instead of random mutation.
				// The MCTS mutator pre-evaluates K=5 mutation variants and
				// returns the best one, filtering out ~97% of regressions at
				// the search level before they enter the population.
				if rand.Float64() < mutationRate {
					parentFitness := fitnessFn(child)
					mutated := mctsMutator.Mutate(child, parentFitness)
					if mutated != nil {
						child = mutated
					} else {
						// Fallback: random mutation
						ops := randomMutation(child)
						ApplyMutations(child, ops)
					}
					p.TotalMutations++
				}
				newPop[i] = Individual{Tree: child, Genome: hashTree(child)}
			}

			p.Individuals = newPop
			p.Evaluate(fitnessFn)
		})

		// Quality gate: count regressions and revert them
		for i := eliteCount; i < len(p.Individuals); i++ {
			if i < len(baselineFitness) && p.Individuals[i].Fitness < baselineFitness[i] {
				p.Regressions++
			}
		}

		// Update best fitness tracking
		if p.BestFitness > p.PrevBestFitness {
			p.PrevBestFitness = p.BestFitness
		}
	}

	return p.BestTree
}

// selfHealGeneration runs the self-healing envelope shared by every Evolve
// variant for a single generation: proactive crisis detection, emergency
// mutation-rate override, specialist-elite archiving, extinct-specialist
// resurrection on emergency, and crisis-streak reset after a spiral. The
// caller owns p.Generation (increment it before calling) and eliteCount; the
// variant-specific breeding happens inside reproduce, invoked once with the
// effective (possibly emergency-elevated) mutation rate.
func (p *Population) selfHealGeneration(eliteCount int, supervisor *LLMSupervisor, reproduce func(mutationRate float64)) {
	state := BuildPopulationState(p)
	guidance := supervisor.Guide(state)
	mutationRate := guidance.RecommendedRate

	// Proactive crisis intervention: reuse the same PopulationState built
	// for the supervisor to detect population-level death spirals this
	// generation. Reasons accumulate across generations so an early
	// diversity collapse and a later regression spiral both survive.
	if p.Crisis == nil {
		p.Crisis = NewCrisisDetector()
	}
	crisis, reasons := p.Crisis.DetectPopulation(&state)
	if len(reasons) > 0 {
		p.recordCrisisReasons(reasons)
	}

	// Act on the crisis signal instead of throwing it away: when a
	// population-level death spiral is detected — or the supervisor itself
	// flags an intervention phase — override this generation's mutation
	// rate with the detector's emergency rate so the GA breaks out of the
	// spiral rather than silently converging.
	emergency := crisis || guidance.Intervention
	if emergency {
		mutationRate = p.Crisis.GetEmergencyMutationRate()
	}
	p.LastMutationRate = mutationRate

	// A streak-based spiral (regression_spiral / quality_crash) is what
	// ResetPopulation exists to clear, so remember whether one surfaced
	// this generation. The reset runs once the emergency generation
	// completes, giving the recovered population a clean slate. A pure
	// diversity_collapse leaves the streak counters alone so a
	// still-regressing population's streak keeps accumulating toward the
	// regression_spiral threshold instead of being reset out from under it.
	resetStreaks := containsCrisisReason(reasons, "regression_spiral") ||
		containsCrisisReason(reasons, "quality_crash")

	// Sort by fitness descending
	sort.Slice(p.Individuals, func(i, j int) bool {
		return p.Individuals[i].Fitness > p.Individuals[j].Fitness
	})

	// Archive validated specialist elites every generation so that if a
	// niche later goes extinct — say during this same diversity collapse —
	// the registry still holds its best-performing archetype to resurrect.
	// Observe no-ops on individuals lacking validated specialist provenance.
	if p.Specialists != nil {
		for i := 0; i < eliteCount && i < len(p.Individuals); i++ {
			p.Specialists.Observe(p.Individuals[i].Meta, p.Individuals[i].Tree, p.Generation)
		}
	}

	reproduce(mutationRate)

	// Crisis recovery: resurrect extinct specialists. When a death spiral is
	// underway a lost high-fitness niche won't spontaneously reappear through
	// crossover of a collapsed population, so pull each extinct archetype out
	// of the registry and inject it in place of the weakest non-elite
	// individual. This re-seeds diversity with proven genomes rather than
	// letting the niche stay silently extinct.
	if emergency && p.Specialists != nil {
		p.resurrectExtinctSpecialists(eliteCount)
	}

	// After an emergency generation that tripped a streak-based spiral ran
	// under the elevated mutation rate, reset the population-level streak
	// counters so the recovered population isn't immediately re-flagged by
	// stale spiral history.
	if resetStreaks {
		p.Crisis.ResetPopulation()
	}
}

// recordCrisisReasons merges newly detected population-level crisis reasons
// into p.CrisisReasons, preserving first-seen order and skipping duplicates so
// reasons surfaced in different generations (e.g. an early diversity collapse
// and a later regression spiral) both persist to the end of the run.
func (p *Population) recordCrisisReasons(reasons []string) {
	for _, r := range reasons {
		seen := false
		for _, existing := range p.CrisisReasons {
			if existing == r {
				seen = true
				break
			}
		}
		if !seen {
			p.CrisisReasons = append(p.CrisisReasons, r)
		}
	}
}

// containsCrisisReason reports whether the given crisis reason appears in the
// slice DetectPopulation surfaced this generation.
func containsCrisisReason(reasons []string, want string) bool {
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}

const (
	// specialistExtinctAfter is how many generations a specialist niche must be
	// absent from the live population before its archetype counts as extinct and
	// eligible for resurrection.
	specialistExtinctAfter = 5
	// specialistMinFitness is the minimum archived fitness worth resurrecting;
	// low-value archetypes are left extinct rather than re-seeded.
	specialistMinFitness = 0.5
)

// resurrectExtinctSpecialists replaces the weakest non-elite individuals with
// resurrected archetypes for any specialist niche that has gone extinct. It is
// the crisis-recovery half of the specialist loop: Observe archives elites each
// generation, and when a crisis fires this pulls the lost niches back in.
func (p *Population) resurrectExtinctSpecialists(eliteCount int) {
	if p.Specialists == nil {
		return
	}
	current := p.specialistNicheCounts()
	extinct := p.Specialists.ExtinctSpecialists(current, p.Generation, specialistExtinctAfter, specialistMinFitness)
	for _, archetype := range extinct {
		ind, meta, ok := p.Specialists.Resurrect(archetype.Type, p.Generation)
		if !ok {
			continue
		}
		idx := p.weakestNonEliteIndex(eliteCount)
		if idx < 0 {
			break // no replaceable slot left this generation
		}
		ind.Meta = meta
		p.Individuals[idx] = ind
		p.Resurrections++
	}
}

// specialistNicheCounts tallies how many live individuals carry each specialist
// niche, so ExtinctSpecialists can tell which archetypes are currently absent.
func (p *Population) specialistNicheCounts() map[string]int {
	counts := make(map[string]int)
	for i := range p.Individuals {
		meta := p.Individuals[i].Meta
		if meta == nil {
			continue
		}
		if t := firstSpecialistType(meta.Tags); t != "" {
			counts[t]++
		}
	}
	return counts
}

// weakestNonEliteIndex returns the index of the lowest-fitness non-elite
// individual, skipping any already resurrected this generation so a second
// extinct niche doesn't overwrite the first. Returns -1 if none is available.
func (p *Population) weakestNonEliteIndex(eliteCount int) int {
	idx := -1
	var worst float64
	for i := eliteCount; i < len(p.Individuals); i++ {
		if m := p.Individuals[i].Meta; m != nil && m.IsResurrected() {
			continue
		}
		if idx == -1 || p.Individuals[i].Fitness < worst {
			worst = p.Individuals[i].Fitness
			idx = i
		}
	}
	return idx
}

// experienceHintTopK bounds how many prior experiences the warm-start retrieves.
const experienceHintTopK = 5

// experienceHintBias is the probability that a mutation reuses a warm-start
// hint operator instead of a uniformly random one.
const experienceHintBias = 0.5

// ExperienceRetrievalHits reports how many warm-start hints EvolveWithExperience
// retrieves from the bank for the given base tree — the same RetrieveByTreeType
// query with the same topK — so callers can surface the hit count without
// duplicating the tree-type extraction. A nil bank yields 0.
func ExperienceRetrievalHits(bank *ExperienceBank, tree *SerializableNode) int {
	return len(RetrieveExperienceHints(bank, tree, experienceHintTopK))
}

// RetrieveExperienceHints returns the top-K highest-quality experience entries
// for the given tree's type — the same RetrieveByTreeType query the warm-start
// uses — so callers outside the package can retrieve hints without duplicating
// the tree-type extraction. A nil bank yields nil.
func RetrieveExperienceHints(bank *ExperienceBank, tree *SerializableNode, topK int) []ExperienceEntry {
	if bank == nil {
		return nil
	}
	return bank.RetrieveByTreeType(extractTreeType(tree), topK)
}

// EvolveWithExperience runs the genetic algorithm like Evolve, but closes the
// EvoRepair-style learn→retrieve→mutate loop against an ExperienceBank:
// operator selection is warm-started from RetrieveByTreeType hints for the
// population's tree type, and every fitness-improving mutation is recorded
// back into the bank via AddFromMutation. A nil bank degrades to plain Evolve.
func (p *Population) EvolveWithExperience(generations int, fitnessFn func(*SerializableNode) float64, bank *ExperienceBank) *SerializableNode {
	return p.EvolveWithExperienceContext(generations, fitnessFn, bank, "")
}

// EvolveWithExperienceContext runs the genetic algorithm like
// EvolveWithExperience, but conditions the warm-start hints on the failing
// task's semantics rather than just the population's tree type: when query is
// non-empty, hints come from bank.Retrieve(query, experienceHintTopK) —
// Retrieve's Jaccard-similarity ranking is not filtered by tree type, so it
// can surface relevant prior mutations regardless of what tree type recorded
// them. An empty query keeps calling RetrieveByTreeType exactly like
// EvolveWithExperience, so existing callers see no behavior change. A nil
// bank degrades to plain Evolve.
func (p *Population) EvolveWithExperienceContext(generations int, fitnessFn func(*SerializableNode) float64, bank *ExperienceBank, query string) *SerializableNode {
	if bank == nil {
		return p.Evolve(generations, fitnessFn)
	}
	if len(p.Individuals) == 0 {
		return nil
	}

	p.Evaluate(fitnessFn)
	p.PrevBestFitness = p.BestFitness
	// Clamp so degenerate populations (size < 2) don't overflow the elite copy.
	eliteCount := min(max(2, len(p.Individuals)/10), len(p.Individuals))
	supervisor := NewLLMSupervisor()

	// Warm-start: consult prior successes and bias operator selection toward
	// them. A non-empty query conditions retrieval on the failing task's
	// semantics via Retrieve's similarity ranking; an empty query falls back
	// to the population's tree type via RetrieveByTreeType, unchanged from
	// EvolveWithExperience's original behavior.
	var hints []ExperienceEntry
	if query != "" {
		hints = bank.Retrieve(query, experienceHintTopK)
	} else {
		treeType := extractTreeType(p.Individuals[0].Tree)
		hints = bank.RetrieveByTreeType(treeType, experienceHintTopK)
	}
	hintOps := make([]string, 0, len(hints))
	hintIDs := make([]string, 0, len(hints))
	for _, h := range hints {
		hintOps = append(hintOps, h.MutationOp)
		hintIDs = append(hintIDs, h.ID)
	}
	_ = bank.MarkReused(hintIDs)

	mutator := NewMCTSMutator()
	mutator.WarmStartHints = hintOps

	for gen := 0; gen < generations; gen++ {
		p.Generation++

		p.selfHealGeneration(eliteCount, supervisor, func(mutationRate float64) {
			// Keep elites
			newPop := make([]Individual, len(p.Individuals))
			copy(newPop[:eliteCount], p.Individuals[:eliteCount])

			// Fill rest with crossover + experience-guided mutation
			for i := eliteCount; i < len(p.Individuals); i++ {
				parents := p.Select()
				child := Crossover(parents[0], parents[1])
				if rand.Float64() < mutationRate {
					child = p.mutateAndRecord(child, hintOps, fitnessFn, bank, mutator)
					p.TotalMutations++
				}
				newPop[i] = Individual{Tree: child, Genome: hashTree(child)}
			}

			p.Individuals = newPop
			p.Evaluate(fitnessFn)
		})

		if p.BestFitness > p.PrevBestFitness {
			p.PrevBestFitness = p.BestFitness
		}
	}

	return p.BestTree
}

// mutateAndRecord applies one experience-biased mutation to child. Improving
// mutations are recorded in the bank; regressions are discarded so the
// quality gate holds.
func (p *Population) mutateAndRecord(
	child *SerializableNode,
	hintOps []string,
	fitnessFn func(*SerializableNode) float64,
	bank *ExperienceBank,
	mutator *MCTSMutator,
) *SerializableNode {
	before := fitnessFn(child)

	opName := AllMutationOps[rand.Intn(len(AllMutationOps))]
	if len(hintOps) > 0 && rand.Float64() < experienceHintBias {
		opName = hintOps[rand.Intn(len(hintOps))]
	}

	mutated := cloneTree(child)
	op := mutator.concreteMutationOp(opName, mutated)
	if ApplyMutations(mutated, []MutationOp{op}) == 0 {
		return child
	}

	after := fitnessFn(mutated)
	if after <= before {
		p.Regressions++
		return child
	}
	_ = bank.AddFromMutation(mutated, op, before, after, nil)
	return mutated
}

// Diversity measures population uniqueness.
func (p *Population) Diversity() float64 {
	seen := make(map[string]bool)
	for _, ind := range p.Individuals {
		seen[ind.Genome] = true
	}
	return float64(len(seen)) / float64(len(p.Individuals))
}

// ConvergenceRate returns fitness improvement per generation.
func (p *Population) ConvergenceRate() float64 {
	if p.Generation == 0 {
		return 0
	}
	return p.BestFitness / float64(p.Generation)
}

// RegressionRate returns the percentage of mutations that caused fitness regressions.
func (p *Population) RegressionRate() float64 {
	if p.TotalMutations == 0 {
		return 0
	}
	return float64(p.Regressions) / float64(p.TotalMutations) * 100
}

// NicheDiversity returns the diversity index across niches (0-1).
// Uses the Herfindahl-Hirschman Index (HHI) inverted: 0 = single niche, 1 = perfectly distributed.
func (p *Population) NicheDiversity() float64 {
	// Count individuals per niche based on genome prefix (first 8 chars = niche fingerprint)
	niches := make(map[string]int)
	for _, ind := range p.Individuals {
		prefix := ind.Genome
		if len(prefix) > 8 {
			prefix = prefix[:8]
		}
		niches[prefix]++
	}
	if len(niches) <= 1 {
		return 0
	}
	total := float64(len(p.Individuals))
	hhi := 0.0
	for _, count := range niches {
		share := float64(count) / total
		hhi += share * share
	}
	// Invert HHI so 0 = concentrated, 1 = perfectly diverse
	n := float64(len(niches))
	if n <= 1 {
		return 0
	}
	normalized := (1 - hhi) / (1 - 1/n)
	if normalized > 1 {
		normalized = 1
	}
	return normalized
}

// ─── Reinforcement Learning Engine ───

// QTable maps state→action→value for reinforcement learning.
type QTable struct {
	Values map[string]map[string]float64 `json:"values"` // state → action → Q-value
	// Cap bounds the number of distinct states Update retains; 0 = unbounded,
	// mirroring IslandModel.Cap.
	Cap int `json:"cap"`
	// EvictedStates is the cumulative count of states dropped by Update's
	// eviction across every call, mirroring IslandModel.EvictedIndividuals.
	EvictedStates int `json:"evicted_states"`
	// updateSeq and lastUpdated track per-state recency so Update can evict
	// the least-recently-updated state first once Cap is exceeded.
	updateSeq   uint64
	lastUpdated map[string]uint64
}

// NewQTable creates an empty Q-table.
func NewQTable() *QTable {
	return &QTable{Values: make(map[string]map[string]float64)}
}

// GetState encodes a tree's state for Q-table lookup.
func (qt *QTable) GetState(tree *SerializableNode, category string) string {
	nodes := CountNodes(tree)
	depth := MaxDepth(tree, 0)
	bucket := "low"
	if nodes > 20 {
		bucket = "med"
	}
	if nodes > 35 {
		bucket = "high"
	}
	return category + ":" + bucket + ":" + strconv.Itoa(depth)
}

// SelectAction returns best action via epsilon-greedy.
func (qt *QTable) SelectAction(state string, epsilon float64) string {
	actions, ok := qt.Values[state]
	if !ok || rand.Float64() < epsilon {
		allMutations := []string{"add_before", "add_after", "add_fallback", "replace_node", "remove_node"}
		return allMutations[rand.Intn(len(allMutations))]
	}
	best := ""
	bestVal := -1e9
	for action, val := range actions {
		if val > bestVal {
			bestVal = val
			best = action
		}
	}
	return best
}

// Update applies Q-learning update: Q(s,a) += α * (reward - Q(s,a))
func (qt *QTable) Update(state, action string, reward, learningRate float64) {
	if _, ok := qt.Values[state]; !ok {
		qt.Values[state] = make(map[string]float64)
	}
	qt.Values[state][action] += learningRate * (reward - qt.Values[state][action])

	if qt.lastUpdated == nil {
		qt.lastUpdated = make(map[string]uint64)
	}
	qt.updateSeq++
	qt.lastUpdated[state] = qt.updateSeq
	qt.enforceCap()
}

// enforceCap evicts the least-recently-updated states from Values until at
// most Cap states remain; Cap <= 0 leaves Values unbounded, mirroring
// enforceIslandCap in island.go. Each eviction accumulates onto EvictedStates.
func (qt *QTable) enforceCap() {
	for qt.Cap > 0 && len(qt.Values) > qt.Cap {
		var oldest string
		var oldestSeq uint64
		found := false
		for state := range qt.Values {
			seq := qt.lastUpdated[state]
			if !found || seq < oldestSeq {
				oldest, oldestSeq, found = state, seq, true
			}
		}
		delete(qt.Values, oldest)
		delete(qt.lastUpdated, oldest)
		qt.EvictedStates++
	}
}

// Save persists the Q-table as JSON at path, creating missing parent
// directories and writing atomically (temp file + rename) under the shared
// advisory flock so concurrent writers cannot interleave partial archives,
// mirroring IslandModel.Save (ADR-024).
func (qt *QTable) Save(path string) error {
	data, err := json.MarshalIndent(qt.Values, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal qtable archive: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create qtable archive dir: %w", err)
	}
	release, err := acquireExperienceLock(path)
	if err != nil {
		return err
	}
	defer release()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write qtable archive: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("commit qtable archive: %w", err)
	}
	return nil
}

// Load warm-starts the table by merging the archive at path into the
// in-memory Values, state-by-action, so previously learned values not present
// on disk survive. A missing archive is a silent cold start; a corrupt
// archive is an error that leaves the in-memory state untouched, mirroring
// IslandModel.Load.
func (qt *QTable) Load(path string) error {
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
		return fmt.Errorf("read qtable archive: %w", err)
	}
	var values map[string]map[string]float64
	if err := json.Unmarshal(data, &values); err != nil {
		return fmt.Errorf("parse qtable archive %s: %w", path, err)
	}
	if qt.Values == nil {
		qt.Values = make(map[string]map[string]float64)
	}
	for state, actions := range values {
		if qt.Values[state] == nil {
			qt.Values[state] = make(map[string]float64)
		}
		for action, val := range actions {
			qt.Values[state][action] = val
		}
	}
	return nil
}

// BestAction returns the highest-value action for a state.
func (qt *QTable) BestAction(state string) string {
	actions := qt.Values[state]
	best := ""
	bestVal := -1e9
	for action, val := range actions {
		if val > bestVal {
			bestVal = val
			best = action
		}
	}
	return best
}

// ReinforcementLearner wraps a Q-table with hyperparameters.
type ReinforcementLearner struct {
	QTable         *QTable `json:"qtable"`
	Epsilon        float64 `json:"epsilon"`
	LearningRate   float64 `json:"learning_rate"`
	DiscountFactor float64 `json:"discount_factor"`
	EpsilonDecay   float64 `json:"epsilon_decay"` // multiplicative decay per episode (0 < decay ≤ 1)
	MinEpsilon     float64 `json:"min_epsilon"`   // floor for epsilon after decay
}

// NewReinforcementLearner creates a new RL agent.
func NewReinforcementLearner() *ReinforcementLearner {
	return &ReinforcementLearner{
		QTable:         NewQTable(),
		Epsilon:        0.2,
		LearningRate:   0.1,
		DiscountFactor: 0.9,
		EpsilonDecay:   0.995,
		MinEpsilon:     0.01,
	}
}

// DecayEpsilon reduces epsilon by the decay factor, clamped to MinEpsilon.
func (rl *ReinforcementLearner) DecayEpsilon() {
	rl.Epsilon *= rl.EpsilonDecay
	if rl.Epsilon < rl.MinEpsilon {
		rl.Epsilon = rl.MinEpsilon
	}
}

// ConfigureEpsilonSchedule sets a custom decay schedule.
func (rl *ReinforcementLearner) ConfigureEpsilonSchedule(initial, decay, minEpsilon float64) {
	rl.Epsilon = initial
	rl.EpsilonDecay = decay
	rl.MinEpsilon = minEpsilon
}

// Learn updates the Q-table based on the outcome of a mutation.
func (rl *ReinforcementLearner) Learn(tree *SerializableNode, category, action string, beforeFitness, afterFitness float64) {
	state := rl.QTable.GetState(tree, category)
	reward := afterFitness - beforeFitness // fitness delta as reward
	rl.QTable.Update(state, action, reward, rl.LearningRate)
}

// Suggest returns the best action for the current tree state.
func (rl *ReinforcementLearner) Suggest(tree *SerializableNode, category string) string {
	state := rl.QTable.GetState(tree, category)
	return rl.QTable.SelectAction(state, rl.Epsilon)
}

// LearnedActions extracts the per-state best actions from the table — the
// greedy policy learned so far. States without Q-values are omitted.
func (qt *QTable) LearnedActions() map[string]string {
	learned := make(map[string]string, len(qt.Values))
	for state := range qt.Values {
		if best := qt.BestAction(state); best != "" {
			learned[state] = best
		}
	}
	return learned
}

// EvolveQLearning runs the genetic algorithm like Evolve, but drives
// mutation-category selection through the QTable's reinforcement loop: each
// offspring mutation encodes the child via GetState, picks its mutation
// category via epsilon-greedy SelectAction, and feeds the fitness delta back
// through Update. With epsilon=0 selection is pure greedy once a state has
// Q-values. The caller owns the QTable and reads the learned policy
// afterwards via LearnedActions. ek is an optional *ExpertKnowledge that
// observes every genuinely-improving mutation via Observe, growing its
// learned archive from this run; a nil ek is a no-op.
func (p *Population) EvolveQLearning(generations int, fitnessFn func(*SerializableNode) float64, qt *QTable, category string, epsilon, learningRate float64, ek *ExpertKnowledge) *SerializableNode {
	if len(p.Individuals) == 0 || qt == nil {
		return nil
	}
	p.Evaluate(fitnessFn)
	p.PrevBestFitness = p.BestFitness
	// Clamp so degenerate populations (size < 2) don't overflow the elite copy.
	eliteCount := min(max(2, len(p.Individuals)/10), len(p.Individuals))
	// The MCTS mutator only materializes category names into concrete ops
	// (target selection + payload nodes); no tree search runs here.
	mutator := NewMCTSMutator()

	for gen := 0; gen < generations; gen++ {
		p.Generation++
		sort.Slice(p.Individuals, func(i, j int) bool {
			return p.Individuals[i].Fitness > p.Individuals[j].Fitness
		})

		newPop := make([]Individual, len(p.Individuals))
		copy(newPop[:eliteCount], p.Individuals[:eliteCount])

		for i := eliteCount; i < len(p.Individuals); i++ {
			parents := p.Select()
			child := Crossover(parents[0], parents[1])
			newPop[i] = Individual{Tree: p.qLearnMutate(child, fitnessFn, qt, category, epsilon, learningRate, mutator, ek)}
			newPop[i].Genome = hashTree(newPop[i].Tree)
		}

		p.Individuals = newPop
		p.Evaluate(fitnessFn)
		if p.BestFitness > p.PrevBestFitness {
			p.PrevBestFitness = p.BestFitness
		}
	}
	return p.BestTree
}

// qLearnMutate applies one Q-table-selected mutation to child and rewards the
// (state, action) pair with the fitness delta. A mutation that fails to apply
// earns reward 0; a regression is discarded (quality gate) but still recorded
// so the table learns to avoid that category in that state. ek is an optional
// *ExpertKnowledge observing the same (action, category, gain) via Observe so
// a genuine improvement grows its learned archive; a nil ek is a no-op.
func (p *Population) qLearnMutate(
	child *SerializableNode,
	fitnessFn func(*SerializableNode) float64,
	qt *QTable,
	category string,
	epsilon, learningRate float64,
	mutator *MCTSMutator,
	ek *ExpertKnowledge,
) *SerializableNode {
	before := fitnessFn(child)
	state := qt.GetState(child, category)
	action := qt.SelectAction(state, epsilon)

	// The QTable vocabulary's "remove_node" maps to the mutation engine's
	// "prune_node" op — the engine has no literal remove_node operation.
	opName := action
	if opName == "remove_node" {
		opName = "prune_node"
	}
	mutated := cloneTree(child)
	op := mutator.concreteMutationOp(opName, mutated)
	applied := ApplyMutations(mutated, []MutationOp{op})
	p.TotalMutations++

	after := before
	if applied > 0 {
		after = fitnessFn(mutated)
	}
	delta := after - before
	qt.Update(state, action, delta, learningRate)
	ek.Observe(action, category, delta)

	if applied == 0 {
		return child
	}
	if after < before {
		p.Regressions++
		return child
	}
	return mutated
}

// ─── Helpers ───

func cloneTree(t *SerializableNode) *SerializableNode {
	if t == nil {
		return nil
	}
	c := &SerializableNode{
		Type:        t.Type,
		Name:        t.Name,
		Description: t.Description,
		MaxRetries:  t.MaxRetries,
		TimeoutMs:   t.TimeoutMs,
	}
	if t.Metadata != nil {
		c.Metadata = make(map[string]any)
		for k, v := range t.Metadata {
			c.Metadata[k] = v
		}
	}
	if t.Edges != nil {
		c.Edges = make([]TypedEdge, len(t.Edges))
		copy(c.Edges, t.Edges)
	}
	for _, ch := range t.Children {
		c.Children = append(c.Children, *cloneTree(&ch))
	}
	return c
}

func hashTree(t *SerializableNode) string {
	h := sha256.Sum256([]byte(t.Name + t.Type + strconv.Itoa(len(t.Children))))
	return hex.EncodeToString(h[:])[:16]
}

func randomMutation(tree *SerializableNode) []MutationOp {
	if ops := tryBlockRandomMutation(tree); len(ops) > 0 {
		return ops
	}
	// Include all mutation types the expert system recommends
	allOps := []string{
		"add_before", "add_after", "add_fallback",
		"replace_node", "replace_children", "reorder_children",
		"increase_retries", "prune_node", "increase_iterations", "add_tool",
	}
	op := allOps[rand.Intn(len(allOps))]
	// Find a random target node
	target := randomNodeName(tree, tree.Name)
	if target == "" {
		target = tree.Name
	}
	return []MutationOp{{Operation: op, Target: target}}
}

func randomNodeName(node *SerializableNode, fallback string) string {
	names := collectNodeNames(node)
	if len(names) == 0 {
		return fallback
	}
	return names[rand.Intn(len(names))]
}

func collectNodeNames(node *SerializableNode) []string {
	var names []string
	if node.Name != "" && node.Type != "Sequence" && node.Type != "Selector" {
		names = append(names, node.Name)
	}
	for i := range node.Children {
		names = append(names, collectNodeNames(&node.Children[i])...)
	}
	return names
}
