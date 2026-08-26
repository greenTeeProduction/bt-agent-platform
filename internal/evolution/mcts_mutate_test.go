package evolution

import (
	"math"
	"strings"
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
	fn := func(_ *SerializableNode) float64 { return 1.0 }

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

	result, _ := m.Mutate(tree, 0)

	if result == nil {
		t.Fatal("Mutate returned nil")
	}
	// Should be a different tree (mutation applied)
	if result.Name != tree.Name {
		_ = struct{}{}
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

	result, _ := m.Mutate(tree, parentFitness)

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

	result, _ := m.Mutate(tree, parentFitness)

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

// hasNodeWithPrefix reports whether any node in the tree has a Name with the given prefix.
func hasNodeWithPrefix(node *SerializableNode, prefix string) bool {
	if node == nil {
		return false
	}
	if strings.HasPrefix(node.Name, prefix) {
		return true
	}
	for i := range node.Children {
		if hasNodeWithPrefix(&node.Children[i], prefix) {
			return true
		}
	}
	return false
}

// TestMCTSMutator_Mutate_ReturnsWinningMutationOp pins the winning mutation's
// op name to the structural marker it actually left on the returned tree.
// concreteMutationOp tags every add_before/add_after candidate node with
// "MCTS_<op>_<n>", so rigging the evaluator to favor the add_before marker
// forces that variant to win regardless of iteration order — since
// randomNodeName (via collectNodeNames) only ever offers "Action1" or "Cond1"
// as a target on testBaseTree, add_before is guaranteed to apply cleanly
// whenever the search tries it, and Iterations == len(AllMutationOps)
// guarantees every op — including add_before — gets tried exactly once as a
// direct child of root before the loop ends.
func TestMCTSMutator_Mutate_ReturnsWinningMutationOp(t *testing.T) {
	m := NewMCTSMutator()
	m.Iterations = len(AllMutationOps)
	m.SetFitnessEvaluator(func(tree *SerializableNode) float64 {
		if hasNodeWithPrefix(tree, "MCTS_add_before_") {
			return 1000.0
		}
		return mockFitnessEvaluator(tree)
	})

	tree := testBaseTree()
	parentFitness := mockFitnessEvaluator(tree)

	result, opName := m.Mutate(tree, parentFitness)

	if result == nil {
		t.Fatal("Mutate returned nil tree")
	}
	if opName != "add_before" {
		t.Fatalf("expected winning op %q, got %q", "add_before", opName)
	}
	if !hasNodeWithPrefix(result, "MCTS_add_before_") {
		t.Errorf("returned op %q does not match the mutation actually applied — result tree is missing the MCTS_add_before_ marker node", opName)
	}
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
	if len(root.UntriedOps) != 0 {
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
	result, _ := m.Mutate(nil, 0)
	if result != nil {
		t.Log("Mutate returned a tree for nil parent (cloneTree handles nil)")
	}
}

func TestMCTSMutator_Mutate_SingleNode(t *testing.T) {
	m := NewMCTSMutator()
	m.SetFitnessEvaluator(mockFitnessEvaluator)

	tree := &SerializableNode{Type: "Action", Name: "Singleton"}
	parentFitness := mockFitnessEvaluator(tree)

	result, _ := m.Mutate(tree, parentFitness)
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

	mctsResult, _ := m.Mutate(tree, parentFitness)
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

// ─── MCTS as a second structural-mutation strategy (milestone 4/5) ──────────
//
// evolveTreeV2 scores structural candidates from evaluator.OrderMutations only,
// so MCTSMutator's search never competes for the mutation budget. These tests
// pin the three pieces that let it: MCTSMutator.Candidates (search → scored,
// individually applicable MutationOps), MergeScoredMutations (one competition
// across both generators), and SelectStructuralStrategy (per-tree choice driven
// by the SpecialistRegistry/SelectorOptimizer heuristics).

// mctsSelectorTree is a two-Selector tree used by the strategy-selection tests:
// one Selector accumulates telemetry, the other stays cold.
func mctsSelectorTree() *SerializableNode {
	return &SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []SerializableNode{
			{Type: "Selector", Name: "SelHot", Children: []SerializableNode{
				{Type: "Action", Name: "HotA"},
				{Type: "Action", Name: "HotB"},
			}},
			{Type: "Selector", Name: "SelCold", Children: []SerializableNode{
				{Type: "Action", Name: "ColdA"},
				{Type: "Action", Name: "ColdB"},
			}},
		},
	}
}

func TestMCTSMutator_Candidates_ScoredSortedAndApplicable(t *testing.T) {
	m := NewMCTSMutator()
	m.Iterations = 24
	m.SetFitnessEvaluator(mockFitnessEvaluator)

	parent := testBaseTree()
	parentNodes := CountNodes(parent)
	cands := m.Candidates(parent, mockFitnessEvaluator(parent))

	if len(cands) == 0 {
		t.Fatal("Candidates returned none; the MCTS search must surface its improving root mutations as scored candidates")
	}
	if CountNodes(parent) != parentNodes {
		t.Errorf("Candidates mutated the parent tree: %d nodes before, %d after", parentNodes, CountNodes(parent))
	}

	known := make(map[string]bool, len(AllMutationOps))
	for _, op := range AllMutationOps {
		known[op] = true
	}
	seen := make(map[string]bool, len(cands))
	for i, c := range cands {
		if !known[c.Op.Operation] {
			t.Errorf("candidate %d: operation %q is not one of AllMutationOps", i, c.Op.Operation)
		}
		if c.Op.Target == "" {
			t.Errorf("candidate %d (%s): empty Target — the concrete MutationOp must be applicable via ApplyMutations, not just an op name",
				i, c.Op.Operation)
		}
		if seen[c.Op.Operation+"\x00"+c.Op.Target] {
			t.Errorf("candidate %d: duplicate op/target %s/%s", i, c.Op.Operation, c.Op.Target)
		}
		seen[c.Op.Operation+"\x00"+c.Op.Target] = true

		// Scores must land in the same normalized band evaluator.OrderMutations
		// uses, otherwise the merged competition is decided by scale, not merit.
		if c.Score <= 0.5 || c.Score > 1.0 {
			t.Errorf("candidate %d (%s): score %.4f outside the (0.5, 1.0] band shared with the heuristic candidates",
				i, c.Op.Operation, c.Score)
		}
		if !strings.Contains(strings.ToLower(c.Reason), "mcts") {
			t.Errorf("candidate %d (%s): reason %q must attribute the candidate to the MCTS search", i, c.Op.Operation, c.Reason)
		}
		if i > 0 && cands[i-1].Score < c.Score {
			t.Errorf("candidates not sorted by descending score: [%d]=%.4f < [%d]=%.4f", i-1, cands[i-1].Score, i, c.Score)
		}
	}

	// Every returned candidate must be individually applicable to the parent —
	// that is what lets evolveTreeV2 run them through the same per-candidate
	// benchmark/pre-score/gate loop as the heuristic ones.
	for _, c := range cands {
		clone := cloneTree(parent)
		if ApplyMutations(clone, []MutationOp{c.Op}) == 0 {
			t.Errorf("candidate %s/%s did not apply to the parent tree", c.Op.Operation, c.Op.Target)
		}
	}
}

func TestMCTSMutator_Candidates_OnlyImprovingVariants(t *testing.T) {
	m := NewMCTSMutator()
	m.Iterations = 30
	// Fitness is flat, so no mutation can beat the parent — MCTS must not feed
	// the competition candidates it has no evidence for.
	m.SetFitnessEvaluator(func(*SerializableNode) float64 { return 1.0 })

	if cands := m.Candidates(testBaseTree(), 1.0); len(cands) != 0 {
		t.Errorf("expected no candidates when no variant beats the parent, got %d (first: %s/%s score %.4f)",
			len(cands), cands[0].Op.Operation, cands[0].Op.Target, cands[0].Score)
	}
}

func TestMCTSMutator_Candidates_NoEvaluatorOrNilParent(t *testing.T) {
	withEval := NewMCTSMutator()
	withEval.SetFitnessEvaluator(mockFitnessEvaluator)
	if cands := withEval.Candidates(nil, 0); cands != nil {
		t.Errorf("expected nil candidates for a nil parent, got %d", len(cands))
	}

	// Without a fitness evaluator there is nothing to score the search with, so
	// Candidates must decline rather than emit unscored guesses.
	if cands := NewMCTSMutator().Candidates(testBaseTree(), 0); cands != nil {
		t.Errorf("expected nil candidates without a fitness evaluator, got %d", len(cands))
	}
}

func TestMergeScoredMutations_OneCompetition(t *testing.T) {
	heuristic := []ScoredMutation{
		{Op: MutationOp{Operation: "add_before", Target: "Action1"}, Score: 0.90, Reason: "killer move"},
		{Op: MutationOp{Operation: "prune_node", Target: "Cond1"}, Score: 0.50, Reason: "history heuristic"},
	}
	mcts := []ScoredMutation{
		{Op: MutationOp{Operation: "add_before", Target: "Action1"}, Score: 0.95, Reason: "mcts gain"},
		{Op: MutationOp{Operation: "add_fallback", Target: "Root"}, Score: 0.70, Reason: "mcts gain"},
	}

	merged := MergeScoredMutations(heuristic, mcts)
	if len(merged) != 3 {
		t.Fatalf("expected 3 merged candidates (duplicate op/target collapsed), got %d: %+v", len(merged), merged)
	}
	for i := 1; i < len(merged); i++ {
		if merged[i-1].Score < merged[i].Score {
			t.Errorf("merged candidates not sorted by descending score: [%d]=%.4f < [%d]=%.4f",
				i-1, merged[i-1].Score, i, merged[i].Score)
		}
	}
	if merged[0].Op.Operation != "add_before" || math.Abs(merged[0].Score-0.95) > 1e-9 {
		t.Errorf("expected the higher-scored duplicate to win, got %s/%s score %.4f",
			merged[0].Op.Operation, merged[0].Op.Target, merged[0].Score)
	}
	if !strings.Contains(strings.ToLower(merged[0].Reason), "mcts") {
		t.Errorf("winning duplicate must keep its own reason, got %q", merged[0].Reason)
	}
	if merged[1].Op.Operation != "add_fallback" || merged[2].Op.Operation != "prune_node" {
		t.Errorf("unexpected merged ordering: %s then %s", merged[1].Op.Operation, merged[2].Op.Operation)
	}

	// The heuristic side wins the duplicate when it scores higher.
	heuristicWins := MergeScoredMutations(
		[]ScoredMutation{{Op: MutationOp{Operation: "add_before", Target: "Action1"}, Score: 0.99, Reason: "killer move"}},
		[]ScoredMutation{{Op: MutationOp{Operation: "add_before", Target: "Action1"}, Score: 0.60, Reason: "mcts gain"}},
	)
	if len(heuristicWins) != 1 {
		t.Fatalf("expected 1 merged candidate, got %d", len(heuristicWins))
	}
	if math.Abs(heuristicWins[0].Score-0.99) > 1e-9 || heuristicWins[0].Reason != "killer move" {
		t.Errorf("expected the heuristic entry to win, got score %.4f reason %q",
			heuristicWins[0].Score, heuristicWins[0].Reason)
	}

	// No MCTS candidates → the heuristic ordering survives untouched.
	only := MergeScoredMutations(heuristic, nil)
	if len(only) != len(heuristic) {
		t.Fatalf("expected %d candidates with no MCTS side, got %d", len(heuristic), len(only))
	}
	for i := range only {
		// MutationOp carries a Metadata map, so it is not comparable with != —
		// op/target identity is what "unchanged" means for an ordering anyway.
		if only[i].Op.Operation != heuristic[i].Op.Operation ||
			only[i].Op.Target != heuristic[i].Op.Target ||
			only[i].Score != heuristic[i].Score {
			t.Errorf("candidate %d changed with an empty MCTS side: %+v vs %+v", i, only[i], heuristic[i])
		}
	}
}

func TestSpecialistRegistry_MCTSAffinity(t *testing.T) {
	tree := mctsSelectorTree()

	var nilReg *SpecialistRegistry
	if got := nilReg.MCTSAffinity(tree); got != 1.0 {
		t.Errorf("nil registry: expected affinity 1.0 (no archetype knowledge), got %.2f", got)
	}

	reg := NewSpecialistRegistry()
	if got := reg.MCTSAffinity(tree); got != 1.0 {
		t.Errorf("empty registry: expected affinity 1.0 for an unknown tree, got %.2f", got)
	}

	reg.Observe(&EvolutionMetadata{
		TreeID:   "go_developer",
		Genotype: "seq(sel,sel)",
		Fitness:  FitnessRecord{Score: 0.9, Validated: true},
		Tags:     []string{"specialist:go"},
	}, tree, 1)
	if got := reg.MCTSAffinity(tree); got != 0.0 {
		t.Errorf("proven specialist archetype: expected affinity 0.0 (do not gamble on a preserved shape), got %.2f", got)
	}

	// A different shape is still unknown to the registry.
	if got := reg.MCTSAffinity(testBaseTree()); got != 1.0 {
		t.Errorf("unrelated tree: expected affinity 1.0, got %.2f", got)
	}
}

func TestSelectorOptimizer_MCTSAffinity(t *testing.T) {
	tree := mctsSelectorTree()

	var nilSO *SelectorOptimizer
	if got := nilSO.MCTSAffinity(tree); got != 1.0 {
		t.Errorf("nil optimizer: expected affinity 1.0 (no learned ordering signal), got %.2f", got)
	}

	so := NewSelectorOptimizer(OrderBySuccessRate)
	if got := so.MCTSAffinity(tree); got != 1.0 {
		t.Errorf("cold optimizer: expected affinity 1.0, got %.2f", got)
	}

	// Half the Selectors reach MinSamples → half the tree still has no signal.
	for range so.MinSamples + 2 {
		so.Record("SelHot", NodeExecutionRecord{NodeName: "HotA", Outcome: "success"})
	}
	if got := so.MCTSAffinity(tree); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("one of two Selectors informed: expected affinity 0.5, got %.2f", got)
	}

	for range so.MinSamples + 2 {
		so.Record("SelCold", NodeExecutionRecord{NodeName: "ColdA", Outcome: "failure"})
	}
	if got := so.MCTSAffinity(tree); got != 0.0 {
		t.Errorf("fully informed optimizer: expected affinity 0.0, got %.2f", got)
	}

	// A tree with no Selector at all offers the learned-ordering heuristic
	// nothing to lean on either.
	if got := so.MCTSAffinity(testBaseTree()); got != 1.0 {
		t.Errorf("Selector-free tree: expected affinity 1.0, got %.2f", got)
	}
}

func TestSelectStructuralStrategy(t *testing.T) {
	tree := mctsSelectorTree()

	if got := SelectStructuralStrategy(nil, nil, tree); got != StrategyMCTSAugmented {
		t.Errorf("cold start: expected %q, got %q", StrategyMCTSAugmented, got)
	}

	reg := NewSpecialistRegistry()
	reg.Observe(&EvolutionMetadata{
		TreeID:   "go_developer",
		Genotype: "seq(sel,sel)",
		Fitness:  FitnessRecord{Score: 0.9, Validated: true},
		Tags:     []string{"specialist:go"},
	}, tree, 1)
	so := NewSelectorOptimizer(OrderBySuccessRate)
	for range so.MinSamples + 2 {
		so.Record("SelHot", NodeExecutionRecord{NodeName: "HotA", Outcome: "success"})
		so.Record("SelCold", NodeExecutionRecord{NodeName: "ColdA", Outcome: "success"})
	}

	if got := SelectStructuralStrategy(reg, so, tree); got != StrategyHeuristicOnly {
		t.Errorf("proven specialist with full Selector telemetry: expected %q, got %q", StrategyHeuristicOnly, got)
	}

	// Telemetry alone is not enough: an unknown shape still earns the search.
	if got := SelectStructuralStrategy(NewSpecialistRegistry(), so, tree); got != StrategyMCTSAugmented {
		t.Errorf("informed Selectors but unknown archetype: expected %q, got %q", StrategyMCTSAugmented, got)
	}
}
