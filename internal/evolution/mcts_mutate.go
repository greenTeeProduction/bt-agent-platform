// Package evolution — MCTS-Guided Mutation Engine.
//
// Implements the algorithm described in:
//   - Zheng et al., "MCTS-AHD: Monte Carlo Tree Search for Automated Heuristic
//     Discovery" (ICML 2025)
//   - Lu et al., "Empirical-MCTS: Dual-Experience Memory for Evolutionary
//     Meta-Prompting" (arXiv 2602.04248)
//
// Instead of one random mutation (which regresses ~97.3% of the time), MCTS
// runs K iterations of search, pre-evaluating mutations. Only the "winner"
// of the MCTS search enters the GA population. The vast majority of regressions
// are filtered out during the mini-search, not in the main population.
package evolution

import (
	"encoding/json"
	"math"
	"math/rand"
	"sync"
)

// ─── MCTSNode ───────────────────────────────────────────────────────────────

// MCTSNode represents one state in the Monte Carlo search tree.
// Each node corresponds to a tree variant produced by a specific mutation.
type MCTSNode struct {
	Tree      *SerializableNode `json:"tree"`
	Q         float64           `json:"q"`          // cumulative reward (fitness)
	N         int               `json:"n"`          // visit count
	Children  []*MCTSNode       `json:"children"`   // child nodes
	Parent    *MCTSNode         `json:"-"`          // back-pointer (not serialized)
	MutationOp string           `json:"mutation_op"` // what mutation created this
	UntriedOps []string         `json:"-"`          // mutation ops not yet tried from this node
	IsLeaf     bool             `json:"is_leaf"`
}

// Clone returns a deep copy of this MCTSNode (tree clone, new children slice).
func (n *MCTSNode) Clone() *MCTSNode {
	c := &MCTSNode{
		Tree:       cloneTree(n.Tree),
		Q:          n.Q,
		N:          n.N,
		MutationOp: n.MutationOp,
		IsLeaf:     n.IsLeaf,
	}
	if n.UntriedOps != nil {
		c.UntriedOps = make([]string, len(n.UntriedOps))
		copy(c.UntriedOps, n.UntriedOps)
	}
	// Children and Parent are left nil — caller must re-establish linkage.
	return c
}

// UCB1 computes the Upper Confidence Bound score for child selection.
// Returns +inf for unvisited children to guarantee exploration.
func (n *MCTSNode) UCB1(C float64) float64 {
	if n.N == 0 {
		return math.Inf(1)
	}
	exploitation := n.Q / float64(n.N)
	exploration := C * math.Sqrt(math.Log(float64(n.Parent.N))/float64(n.N))
	return exploitation + exploration
}

// BestChild returns the child with the highest UCB1 score.
func (n *MCTSNode) BestChild(C float64) *MCTSNode {
	if len(n.Children) == 0 {
		return nil
	}
	best := n.Children[0]
	bestScore := best.UCB1(C)
	for _, ch := range n.Children[1:] {
		score := ch.UCB1(C)
		if score > bestScore {
			best = ch
			bestScore = score
		}
	}
	return best
}

// ─── Mutation Operation Catalog ─────────────────────────────────────────────

// AllMutationOps lists the mutation operations the MCTS expander can try.
var AllMutationOps = []string{
	"add_before",
	"add_after",
	"add_fallback",
	"replace_node",
	"replace_children",
	"reorder_children",
}

// ─── MCTSMutator ────────────────────────────────────────────────────────────

// FitnessFunc evaluates a tree and returns a composite fitness score.
type FitnessFunc func(*SerializableNode) float64

// MCTSMutator uses MCTS to find high-fitness mutation variants of a parent tree.
// Instead of applying one random mutation, it searches K iterations using
// SELECT → EXPAND → SIMULATE → BACKPROPAGATE, then returns the best variant found.
type MCTSMutator struct {
	Iterations       int         `json:"iterations"`         // K, default 10
	ExplorationConst float64     `json:"exploration_constant"` // C, default 1.4
	MaxDepth         int         `json:"max_depth"`          // search depth limit, default 3
	FitnessEvaluator FitnessFunc `json:"-"`                  // evaluates tree fitness
	Verbose          bool        `json:"verbose,omitempty"`  // enable logging

	// Experience bank warm-start — optional reference to recent successful mutations
	WarmStartHints []string `json:"warmstart_hints,omitempty"`

	mu sync.Mutex
}

// NewMCTSMutator creates an MCTS mutator with sensible defaults.
// The fitness evaluator must be set separately via SetFitnessEvaluator or
// passed implicitly through configuration.
func NewMCTSMutator() *MCTSMutator {
	return &MCTSMutator{
		Iterations:       10,
		ExplorationConst: 1.4,
		MaxDepth:         3,
		FitnessEvaluator: nil, // must be set before Mutate() is called
	}
}

// SetFitnessEvaluator sets the functio that evaluates tree fitness.
func (m *MCTSMutator) SetFitnessEvaluator(fn FitnessFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FitnessEvaluator = fn
}

// WithConfig returns a copy with overridden parameters.
func (m *MCTSMutator) WithConfig(iterations int, explorationConst float64, maxDepth int) *MCTSMutator {
	m.mu.Lock()
	defer m.mu.Unlock()
	return &MCTSMutator{
		Iterations:       iterations,
		ExplorationConst: explorationConst,
		MaxDepth:         maxDepth,
		FitnessEvaluator: m.FitnessEvaluator,
		Verbose:          m.Verbose,
		WarmStartHints:   append([]string{}, m.WarmStartHints...),
	}
}

// ─── Core Algorithm ─────────────────────────────────────────────────────────

// Mutate runs the MCTS-guided mutation search and returns the best variant found.
// Uses the parent tree as the root state and searches for promising mutations
// across K iterations. Returns the parent tree (unmutated) if no improvement found.
func (m *MCTSMutator) Mutate(parent *SerializableNode, parentFitness float64) *SerializableNode {
	if parent == nil {
		return nil
	}

	m.mu.Lock()
	fn := m.FitnessEvaluator
	m.mu.Unlock()

	if fn == nil {
		// No evaluator configured — fall back to a single random mutation.
		clone := cloneTree(parent)
		ops := randomMutation(clone)
		ApplyMutations(clone, ops)
		return clone
	}

	// 1. CREATE root node
	root := &MCTSNode{
		Tree:       cloneTree(parent),
		Q:          parentFitness,
		N:          1,
		UntriedOps: m.buildMutationOps(parent),
	}

	bestNode := root
	bestFitness := parentFitness

	// 2. MAIN LOOP: K iterations
	for i := 0; i < m.Iterations; i++ {
		// SELECT: traverse using UCB1 until we hit a leaf or an unexpanded node
		selected := m.selectNode(root, 0)

		// EXPAND: generate a new mutation variant
		leaf := m.expandNode(selected)
		if leaf == nil {
			continue // no new mutations to try from this node
		}

		// SIMULATE: evaluate the variant's fitness
		fitness := fn(leaf.Tree)

		// BACKPROPAGATE: update scores up the tree
		m.backpropagate(leaf, fitness)

		// Track best
		if fitness > bestFitness {
			bestFitness = fitness
			bestNode = leaf
		}
	}

	// 3. RETURN the best variant found
	if bestNode == root || bestNode.Tree == nil {
		// No improvement found — return a single random mutation as fallback
		clone := cloneTree(parent)
		ops := randomMutation(clone)
		ApplyMutations(clone, ops)
		return clone
	}

	return cloneTree(bestNode.Tree)
}

// selectNode traverses from root using UCB1 until reaching an expandable leaf.
// The maxDepth parameter bounds tree traversal depth for performance.
func (m *MCTSMutator) selectNode(node *MCTSNode, depth int) *MCTSNode {
	if node == nil {
		return nil
	}
	// If node has unexpanded ops, it's a leaf for selection purposes
	if len(node.UntriedOps) > 0 {
		return node
	}
	// If node is a leaf with no children, return it for expansion
	if len(node.Children) == 0 {
		return node
	}
	// If at max depth, return this node
	if depth >= m.MaxDepth {
		return node
	}
	// Recurse into best child
	best := node.BestChild(m.ExplorationConst)
	if best == nil {
		return node
	}
	return m.selectNode(best, depth+1)
}

// expandNode picks an untried mutation op and creates a child node.
// Returns the new child, or nil if no expansion possible.
func (m *MCTSMutator) expandNode(node *MCTSNode) *MCTSNode {
	if node == nil {
		return nil
	}

	var op string
	m.mu.Lock()
	if len(node.UntriedOps) == 0 {
		m.mu.Unlock()
		return nil
	}
	// Pick a random untried op
	idx := rand.Intn(len(node.UntriedOps))
	op = node.UntriedOps[idx]
	// Remove from untried list
	node.UntriedOps = append(node.UntriedOps[:idx], node.UntriedOps[idx+1:]...)
	m.mu.Unlock()

	// Apply mutation to a clone of the node's tree
	childTree := cloneTree(node.Tree)
	ops := []MutationOp{{Operation: op, Target: randomNodeName(childTree, childTree.Name)}}
	applied := ApplyMutations(childTree, ops)
	if applied == 0 {
		return nil // mutation didn't apply cleanly
	}

	child := &MCTSNode{
		Tree:       childTree,
		Q:          0,
		N:          0,
		Parent:     node,
		MutationOp: op,
		UntriedOps: m.buildMutationOps(childTree),
	}

	m.mu.Lock()
	node.Children = append(node.Children, child)
	m.mu.Unlock()

	return child
}

// backpropagate propagates the fitness reward from the leaf up to the root.
func (m *MCTSMutator) backpropagate(node *MCTSNode, fitness float64) {
	for n := node; n != nil; n = n.Parent {
		n.N++
		n.Q += fitness
	}
}

// buildMutationOps creates the set of mutation operations to try from a given tree.
// Includes warm-start hints from the experience bank if available.
func (m *MCTSMutator) buildMutationOps(tree *SerializableNode) []string {
	ops := make([]string, len(AllMutationOps))
	copy(ops, AllMutationOps)

	// Prepend warm-start hints if any
	if len(m.WarmStartHints) > 0 {
		// Filter hints to only include valid ops
		for _, hint := range m.WarmStartHints {
			for _, valid := range AllMutationOps {
				if hint == valid {
					ops = append([]string{hint}, ops...)
					break
				}
			}
		}
	}

	return ops
}

// ─── Statistics / Introspection ─────────────────────────────────────────────

// MCTSMetrics captures the result of an MCTS search for inspection/auditing.
type MCTSMetrics struct {
	Iterations        int     `json:"iterations"`
	TotalNodes        int     `json:"total_nodes"`
	RootFitness       float64 `json:"root_fitness"`
	BestFitness       float64 `json:"best_fitness"`
	Improvement       float64 `json:"improvement"`
	ExplorationConst  float64 `json:"exploration_constant"`
	SearchDepth       int     `json:"search_depth"`
	NodesExpanded     int     `json:"nodes_expanded"`
}

// Metrics returns a snapshot of the MCTS tree rooted at the given node.
func (m *MCTSMutator) Metrics(root *MCTSNode) *MCTSMetrics {
	if root == nil {
		return nil
	}
	totalNodes := m.countNodes(root)
	rootFitness := root.Q / float64(maxInt(root.N, 1))
	bestFitness := rootFitness
	m.findBest(root, &bestFitness)

	return &MCTSMetrics{
		Iterations:       m.Iterations,
		TotalNodes:       totalNodes,
		RootFitness:      rootFitness,
		BestFitness:      bestFitness,
		Improvement:      bestFitness - rootFitness,
		ExplorationConst: m.ExplorationConst,
		SearchDepth:      m.treeDepth(root),
		NodesExpanded:    len(root.Children),
	}
}

// countNodes recursively counts all MCTS nodes in the tree.
func (m *MCTSMutator) countNodes(node *MCTSNode) int {
	if node == nil {
		return 0
	}
	count := 1
	for _, ch := range node.Children {
		count += m.countNodes(ch)
	}
	return count
}

// treeDepth computes the maximum depth of the MCTS tree.
func (m *MCTSMutator) treeDepth(node *MCTSNode) int {
	if node == nil || len(node.Children) == 0 {
		return 0
	}
	maxD := 0
	for _, ch := range node.Children {
		d := 1 + m.treeDepth(ch)
		if d > maxD {
			maxD = d
		}
	}
	return maxD
}

// findBest traverses the MCTS tree to find the best fitness value.
func (m *MCTSMutator) findBest(node *MCTSNode, best *float64) {
	if node == nil {
		return
	}
	if node.N > 0 {
		f := node.Q / float64(node.N)
		if f > *best {
			*best = f
		}
	}
	for _, ch := range node.Children {
		m.findBest(ch, best)
	}
}

// ─── Serialization ──────────────────────────────────────────────────────────

// MarshalJSON serializes the MCTS tree rooted at the given node.
func MCTSTreeToJSON(root *MCTSNode) ([]byte, error) {
	return json.MarshalIndent(root, "", "  ")
}

// ─── Internal helpers ───────────────────────────────────────────────────────

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Ensure cloneTree, randomMutation, ApplyMutations, randomNodeName, 
// and CountNodes are accessible (same package).
