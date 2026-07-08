package evolution

import (
	"crypto/sha256"
	"encoding/hex"
	"math/rand"
	"sort"
	"strconv"
)

// ─── Genetic Algorithm Engine ───

// Individual represents one tree in the population.
type Individual struct {
	Tree    *SerializableNode `json:"tree"`
	Fitness float64           `json:"fitness"`
	Genome  string            `json:"genome"` // SHA256 hash of serialized tree
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
		best := -1
		bestFit := -1.0
		for k := 0; k < 3; k++ {
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
		guidance := supervisor.Guide(BuildPopulationState(p))
		mutationRate := guidance.RecommendedRate

		// Sort by fitness descending
		sort.Slice(p.Individuals, func(i, j int) bool {
			return p.Individuals[i].Fitness > p.Individuals[j].Fitness
		})

		// Record baseline fitness of each individual BEFORE mutation
		baselineFitness := make([]float64, len(p.Individuals))
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
	if bank == nil {
		return 0
	}
	return len(bank.RetrieveByTreeType(extractTreeType(tree), experienceHintTopK))
}

// EvolveWithExperience runs the genetic algorithm like Evolve, but closes the
// EvoRepair-style learn→retrieve→mutate loop against an ExperienceBank:
// operator selection is warm-started from RetrieveByTreeType hints for the
// population's tree type, and every fitness-improving mutation is recorded
// back into the bank via AddFromMutation. A nil bank degrades to plain Evolve.
func (p *Population) EvolveWithExperience(generations int, fitnessFn func(*SerializableNode) float64, bank *ExperienceBank) *SerializableNode {
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

	// Warm-start: consult prior successes for this population's tree type and
	// bias operator selection toward them.
	treeType := extractTreeType(p.Individuals[0].Tree)
	hints := bank.RetrieveByTreeType(treeType, experienceHintTopK)
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
		guidance := supervisor.Guide(BuildPopulationState(p))
		mutationRate := guidance.RecommendedRate

		// Sort by fitness descending
		sort.Slice(p.Individuals, func(i, j int) bool {
			return p.Individuals[i].Fitness > p.Individuals[j].Fitness
		})

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
// afterwards via LearnedActions.
func (p *Population) EvolveQLearning(generations int, fitnessFn func(*SerializableNode) float64, qt *QTable, category string, epsilon, learningRate float64) *SerializableNode {
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
			newPop[i] = Individual{Tree: p.qLearnMutate(child, fitnessFn, qt, category, epsilon, learningRate, mutator)}
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
// so the table learns to avoid that category in that state.
func (p *Population) qLearnMutate(
	child *SerializableNode,
	fitnessFn func(*SerializableNode) float64,
	qt *QTable,
	category string,
	epsilon, learningRate float64,
	mutator *MCTSMutator,
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
	qt.Update(state, action, after-before, learningRate)

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
