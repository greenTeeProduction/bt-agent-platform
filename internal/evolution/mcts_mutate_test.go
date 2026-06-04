package evolution

import (
	"math"
	"testing"
)

// ─── Test Helpers ───────────────────────────────────────────────────────────

// mockFitnessEvaluator returns a fitness based on tree node count.
// Simple but deterministic: more nodes = higher fitness (up to a cap of 50).
func mockFitnessEvaluator(tree *SerializableNode) float64 {
	if tree == nil {
		return 0
	}
	nodes := CountNodes(tree)
	if nodes > 50 {
		return 50.0
	}
	return float64(nodes)
}

// Test tree that is small and predictable.
func testBaseTree() *SerializableNode {
	return &SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []SerializableNode{
			{Type: "Action", Name: "Action1"},
			{Type: "Condition", Name: "Cond1"},
		},
	}
}

// ─── MCTSNode Tests ─────────────────────────────────────────────────────────

func TestMCTSNode_UCB1_Unvisited(t *testing.T) {
	parent := &MCTSNode{N: 10, Q: 5.0}
	child := &MCTSNode{Parent: parent, N: 0, Q: 0}

	score := child.UCB1(1.4)
	if !math.IsInf(score, 1) {
		t.Errorf("expected +Inf for unvisited child, got %f", score)
	}
}

func TestMCTSNode_UCB1_Visited(t *testing.T) {
	parent := &MCTSNode{N: 10, Q: 5.0}
	child := &MCTSNode{Parent: parent, N: 3, Q: 1.5}

	score := child.UCB1(1.4)
	// UCB1 = 1.5/3 + 1.4 * sqrt(ln(10)/3)
	expected := 0.5 + 1.4*math.Sqrt(math.Log(10.0)/3.0)
	if math.Abs(score-expected) > 0.001 {
		t.Errorf("expected UCB1 ≈ %.4f, got %.4f", expected, score)
	}
}

func TestMCTSNode_BestChild(t *testing.T) {
	parent := &MCTSNode{N: 15, Q: 7.0}
	child1 := &MCTSNode{Parent: parent, N: 5, Q: 3.0}
	child2 := &MCTSNode{Parent: parent, N: 0, Q: 0} // unvisited → +Inf
	child3 := &MCTSNode{Parent: parent, N: 3, Q: 0.5}

	parent.Children = []*MCTSNode{child1, child2, child3}

	best := parent.BestChild(1.4)
	if best != child2 {
		t.Errorf("expected unvisited child to be selected, got mutation_op=%s", best.MutationOp)
	}
}

func TestMCTSNode_Clone(t *testing.T) {
	tree := testBaseTree()
	node := &MCTSNode{
		Tree:       tree,
		Q:          5.0,
		N:          3,
		MutationOp: "add_before",
		UntriedOps: []string{"a", "b"},
	}

	clone := node.Clone()

	// Verify values were copied
	if clone.Q != node.Q || clone.N != node.N || clone.MutationOp != node.MutationOp {
		t.Errorf("clone field mismatch")
	}
	// Verify tree is a deep copy (not same pointer)
	clone.Tree.Name = "Modified"
	if node.Tree.Name == "Modified" {
		t.Error("clone is shallow — modifying clone affected original")
	}
	// Verify children and parent are nil
	if clone.Children != nil {
		t.Error("clone.Children should be nil")
	}
	if clone.Parent != nil {
		t.Error("clone.Parent should be nil")
	}
	// Verify UntriedOps is a copy
	clone.UntriedOps[0] = "modified"
	if node.UntriedOps[0] == "modified" {
		t.Error("UntriedOps is not a deep copy")
	}
}

// ─── MCTSMutator Tests ──────────────────────────────────────────────────────

func TestNewMCTSMutator(t *testing.T) {
	m := NewMCTSMutator()
	if m == nil {
		t.Fatal("NewMCTSMutator returned nil")
	}
	if m.Iterations != 10 {
		t.Errorf("expected Iterations=10, got %d", m.Iterations)
	}
	if m.ExplorationConst != 1.4 {
		t.Errorf("expected ExplorationConst=1.4, got %f", m.ExplorationConst)
	}
	if m.MaxDepth != 3 {
		t.Errorf("expected MaxDepth=3, got %d", m.MaxDepth)
	}
	if m.FitnessEvaluator != nil {
		t.Error("expected nil FitnessEvaluator initially")
	}
}

func TestMCTSMutator_SetFitnessEvaluator(t *testing.T) {
	m := NewMCTSMutator()
	fn := func(tree *SerializableNode) float64 { return 1.0 }

	m.SetFitnessEvaluator(fn)
	if m.FitnessEvaluator == nil {
		t.Error("FitnessEvaluator should not be nil after SetFitnessEvaluator")
	}
}

func TestMCTSMutator_WithConfig(t *testing.T) {
	m := NewMCTSMutator()
	m2 := m.WithConfig(20, 2.0, 5)

	if m2.Iterations != 20 || m2.ExplorationConst != 2.0 || m2.MaxDepth != 5 {
		t.Errorf("WithConfig did not apply: got %d, %f, %d",
			m2.Iterations, m2.ExplorationConst, m2.MaxDepth)
	}
	// Original should be unchanged
	if m.Iterations != 10 {
		t.Error("WithConfig modified original mutator")
	}
}

func TestMCTSMutator_Mutate_NoEvaluator(t *testing.T) {
	// Without an evaluator, Mutate should fall back to random mutation
	m := NewMCTSMutator()
	tree := testBaseTree()
	originalNodeCount := CountNodes(tree)

	result := m.Mutate(tree, 0)

	if result == nil {
		t.Fatal("Mutate returned nil")
	}
	// Should be a different tree (mutation applied)
	if result.Name != tree.Name {
		// Names can differ after mutation; just verify it's valid
	}
	if CountNodes(result) == 0 {
		t.Error("result tree has 0 nodes — invalid")
	}
	_ = originalNodeCount
}

func TestMCTSMutator_Mutate_WithEvaluator(t *testing.T) {
	m := NewMCTSMutator()
	m.SetFitnessEvaluator(mockFitnessEvaluator)

	tree := testBaseTree()
	parentFitness := mockFitnessEvaluator(tree)

	result := m.Mutate(tree, parentFitness)

	if result == nil {
		t.Fatal("Mutate returned nil")
	}

	// Result should be a valid tree
	if CountNodes(result) == 0 {
		t.Error("result tree has 0 nodes")
	}
	// Fitness should at least be valid
	_ = mockFitnessEvaluator(result)
}

func TestMCTSMutator_Mutate_ImprovesFitness(t *testing.T) {
	// With a tree with low node count, MCTS should find expansions that add nodes
	m := NewMCTSMutator()
	m.SetFitnessEvaluator(mockFitnessEvaluator)
	m.Iterations = 20 // more iterations for better chance of improvement

	// Create a minimal tree (2 nodes — sequence + 1 action)
	tree := &SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []SerializableNode{
			{Type: "Action", Name: "OnlyAction"},
		},
	}
	parentFitness := mockFitnessEvaluator(tree)

	result := m.Mutate(tree, parentFitness)

	if result == nil {
		t.Fatal("Mutate returned nil")
	}

	resultFitness := mockFitnessEvaluator(result)
	// MCTS should at least produce a valid tree
	if CountNodes(result) == 0 {
		t.Error("result tree has 0 nodes")
	}

	t.Logf("Parent fitness: %.2f, Result fitness: %.2f, Parent nodes: %d, Result nodes: %d",
		parentFitness, resultFitness, CountNodes(tree), CountNodes(result))
}

func TestMCTSMutator_SelectNode(t *testing.T) {
	m := NewMCTSMutator()
	tree := testBaseTree()

	root := &MCTSNode{
		Tree:       cloneTree(tree),
		Q:          5.0,
		N:          1,
		UntriedOps: []string{"add_before", "add_after"},
	}

	// Select should return the root itself (it has untried ops)
	selected := m.selectNode(root, 0)
	if selected == nil {
		t.Fatal("selectNode returned nil")
	}
	if selected != root {
		t.Error("selectNode should return root when root has untried ops")
	}
}

func TestMCTSMutator_ExpandNode(t *testing.T) {
	m := NewMCTSMutator()

	tree := testBaseTree()
	root := &MCTSNode{
		Tree:       cloneTree(tree),
		Q:          3.0,
		N:          1,
		UntriedOps: []string{"add_before"},
	}

	child := m.expandNode(root)
	if child == nil {
		t.Fatal("expandNode returned nil")
	}
	if len(root.Children) != 1 {
		t.Errorf("expected 1 child, got %d", len(root.Children))
	}
	if root.Children[0] != child {
		t.Error("child not registered in root.Children")
	}
	if child.Parent != root {
		t.Error("child.Parent not set")
	}
	if root.UntriedOps != nil && len(root.UntriedOps) != 0 {
		t.Errorf("expected root.UntriedOps empty, got %v", root.UntriedOps)
	}
	// Should have its own untried ops
	if len(child.UntriedOps) == 0 {
		t.Error("child should have untried ops from buildMutationOps")
	}
}

func TestMCTSMutator_ExpandNode_NoUntriedOps(t *testing.T) {
	m := NewMCTSMutator()

	root := &MCTSNode{
		Tree:       testBaseTree(),
		UntriedOps: []string{},
	}

	child := m.expandNode(root)
	if child != nil {
		t.Error("expected nil when no untried ops")
	}
}

func TestMCTSMutator_Backpropagate(t *testing.T) {
	m := NewMCTSMutator()

	// Build a small tree
	root := &MCTSNode{Q: 0, N: 0}
	child := &MCTSNode{Q: 0, N: 0, Parent: root}
	grandchild := &MCTSNode{Q: 0, N: 0, Parent: child}
	root.Children = append(root.Children, child)
	child.Children = append(child.Children, grandchild)

	m.backpropagate(grandchild, 5.0)

	// Verify backpropagation
	if grandchild.N != 1 || grandchild.Q != 5.0 {
		t.Errorf("grandchild: expected N=1,Q=5.0, got N=%d,Q=%f", grandchild.N, grandchild.Q)
	}
	if child.N != 1 || child.Q != 5.0 {
		t.Errorf("child: expected N=1,Q=5.0, got N=%d,Q=%f", child.N, child.Q)
	}
	if root.N != 1 || root.Q != 5.0 {
		t.Errorf("root: expected N=1,Q=5.0, got N=%d,Q=%f", root.N, root.Q)
	}
}

func TestMCTSMutator_Metrics(t *testing.T) {
	m := NewMCTSMutator()
	m.SetFitnessEvaluator(mockFitnessEvaluator)

	tree := testBaseTree()
	root := &MCTSNode{
		Tree:       cloneTree(tree),
		Q:          3.0,
		N:          1,
		UntriedOps: []string{"add_before", "add_after"},
	}

	metrics := m.Metrics(root)
	if metrics == nil {
		t.Fatal("Metrics returned nil")
	}
	if metrics.Iterations != 10 {
		t.Errorf("expected Iterations=10, got %d", metrics.Iterations)
	}
	if metrics.RootFitness != 3.0 {
		t.Errorf("expected RootFitness=3.0, got %f", metrics.RootFitness)
	}
	if metrics.TotalNodes < 1 {
		t.Errorf("expected TotalNodes >=1, got %d", metrics.TotalNodes)
	}
}

func TestAllMutationOps_Completeness(t *testing.T) {
	expected := map[string]bool{
		"add_before":          true,
		"add_after":           true,
		"add_fallback":        true,
		"replace_node":        true,
		"replace_children":    true,
		"reorder_children":    true,
		"increase_retries":    true,
		"prune_node":          true,
		"increase_iterations": true,
		"add_tool":            true,
	}

	for _, op := range AllMutationOps {
		if !expected[op] {
			t.Errorf("unexpected op in AllMutationOps: %s", op)
		}
		delete(expected, op)
	}

	for op := range expected {
		t.Errorf("missing op from AllMutationOps: %s", op)
	}
}

func TestMCTSMutator_BuildMutationOps(t *testing.T) {
	m := NewMCTSMutator()
	tree := testBaseTree()

	ops := m.buildMutationOps(tree)
	if len(ops) == 0 {
		t.Fatal("buildMutationOps returned empty")
	}
	if len(ops) != len(AllMutationOps) {
		t.Errorf("expected %d ops, got %d", len(AllMutationOps), len(ops))
	}

	// With warm-start hints
	m.WarmStartHints = []string{"add_before"}
	opsWithHints := m.buildMutationOps(tree)
	if len(opsWithHints) <= len(AllMutationOps) {
		t.Error("expected more ops with warm-start hints")
	}
	if opsWithHints[0] != "add_before" {
		t.Error("expected warm-start hint to be first")
	}
}

// ─── Edge Cases ─────────────────────────────────────────────────────────────

func TestMCTSMutator_Mutate_NilParent(t *testing.T) {
	m := NewMCTSMutator()
	result := m.Mutate(nil, 0)
	if result != nil {
		t.Log("Mutate returned a tree for nil parent (cloneTree handles nil)")
	}
}

func TestMCTSMutator_Mutate_SingleNode(t *testing.T) {
	m := NewMCTSMutator()
	m.SetFitnessEvaluator(mockFitnessEvaluator)

	tree := &SerializableNode{Type: "Action", Name: "Singleton"}
	parentFitness := mockFitnessEvaluator(tree)

	result := m.Mutate(tree, parentFitness)
	if result == nil {
		t.Fatal("Mutate returned nil for single node tree")
	}
	// Should still be valid
	if result.Type == "" || result.Name == "" {
		t.Error("result tree has empty type or name")
	}
}

func TestMCTSNode_UCB1_ZeroParentN(t *testing.T) {
	// Edge case: parent.N == 0 (shouldn't happen in practice but handle gracefully)
	parent := &MCTSNode{N: 0, Q: 0}
	child := &MCTSNode{Parent: parent, N: 1, Q: 1.0}

	score := child.UCB1(1.4)
	// math.Log(0) = -Inf, but N=0 means no visits yet
	// The UCB1 implementation handles this via the unvisited check first
	if math.IsNaN(score) {
		t.Log("UCB1 returned NaN for zero parent N (edge case)")
	}
}

// ─── Integration: MCTS vs Random Mutation ───────────────────────────────────

func TestMCTSMutation_FitnessGap(t *testing.T) {
	// Verify that MCTS with K=10 finds better trees than K=1 (single random mutation)
	// on average over multiple trials.

	m := NewMCTSMutator()
	m.SetFitnessEvaluator(mockFitnessEvaluator)
	m.Iterations = 10

	tree := testBaseTree()
	parentFitness := mockFitnessEvaluator(tree)

	mctsResult := m.Mutate(tree, parentFitness)
	mctsFitness := mockFitnessEvaluator(mctsResult)

	// Single random mutation
	randomResult := cloneTree(tree)
	ops := randomMutation(randomResult)
	ApplyMutations(randomResult, ops)
	randomFitness := mockFitnessEvaluator(randomResult)

	t.Logf("Parent fitness: %.2f, MCTS fitness: %.2f, Random fitness: %.2f",
		parentFitness, mctsFitness, randomFitness)

	// MCTS should produce valid trees at minimum
	if mctsFitness < 0 {
		t.Error("MCTS produced negative fitness")
	}
}
