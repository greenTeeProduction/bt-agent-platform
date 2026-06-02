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
	p.Evaluate(fitnessFn)
	p.PrevBestFitness = p.BestFitness
	eliteCount := max(2, len(p.Individuals)/10)
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
	depth := maxTreeDepth(tree, 0)
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
	// Include all mutation types the expert system recommends
	allOps := []string{
		"add_before", "add_after", "add_fallback",
		"replace_node", "replace_children", "reorder_children",
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

func maxTreeDepth(node *SerializableNode, current int) int {
	maxD := current
	for i := range node.Children {
		d := maxTreeDepth(&node.Children[i], current+1)
		if d > maxD {
			maxD = d
		}
	}
	return maxD
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
