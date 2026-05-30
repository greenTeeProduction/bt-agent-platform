package evolution

import (
	"testing"
)

func makeTestTree(name string, depth, childrenPerLevel int) *SerializableNode {
	root := &SerializableNode{Type: "Selector", Name: name}
	if depth <= 0 {
		return root
	}
	for i := 0; i < childrenPerLevel; i++ {
		child := &SerializableNode{Type: "Action", Name: name + "_leaf"}
		root.Children = append(root.Children, *child)
	}
	return root
}

func makeDeepTree(depth int) *SerializableNode {
	root := &SerializableNode{Type: "Selector", Name: "deep_root"}
	current := root
	for d := 1; d < depth; d++ {
		child := &SerializableNode{Type: "Sequence", Name: "deep_" + itoa(d)}
		child.Children = append(child.Children, SerializableNode{Type: "Action", Name: "leaf"})
		current.Children = append(current.Children, *child)
		if len(current.Children) > 0 {
			current = &current.Children[len(current.Children)-1]
		}
	}
	return root
}

func TestDescriptor(t *testing.T) {
	tree := makeTestTree("test", 2, 3)
	desc := Descriptor(tree, "godev")

	if desc.Domain != "godev" {
		t.Errorf("domain = %q, want godev", desc.Domain)
	}
	if desc.NodeCount != 4 { // root + 3 children
		t.Errorf("node count = %d, want 4", desc.NodeCount)
	}
	if desc.MaxDepth != 1 {
		t.Errorf("max depth = %d, want 1", desc.MaxDepth)
	}
}

func TestDescriptor_NilTree(t *testing.T) {
	desc := Descriptor(nil, "")
	if desc.NodeCount != 0 {
		t.Errorf("nil tree node count = %d, want 0", desc.NodeCount)
	}
}

func TestBucket(t *testing.T) {
	tests := []struct {
		value, bucketSize, want int
	}{
		{0, 10, 0},
		{5, 10, 0},
		{10, 10, 10},
		{14, 10, 10},
		{20, 10, 20},
		{7, 2, 6},
		{0, 0, 0},
	}
	for _, tt := range tests {
		got := Bucket(tt.value, tt.bucketSize)
		if got != tt.want {
			t.Errorf("Bucket(%d, %d) = %d, want %d", tt.value, tt.bucketSize, got, tt.want)
		}
	}
}

func TestMAPElitesGrid_InsertAndRetrieve(t *testing.T) {
	grid := NewMAPElitesGrid(5)
	tree1 := makeTestTree("small", 1, 2)   // 3 nodes, depth 1
	tree2 := makeDeepTree(5)               // 5 nodes, depth 4

	ind1 := &Individual{Tree: tree1, Fitness: 50, Genome: hashTree(tree1)}
	ind2 := &Individual{Tree: tree2, Fitness: 80, Genome: hashTree(tree2)}

	desc1 := Descriptor(tree1, "godev")
	desc2 := Descriptor(tree2, "research")

	// Different niches → both inserted
	if !grid.Insert(desc1, ind1) {
		t.Error("first insert should succeed")
	}
	if !grid.Insert(desc2, ind2) {
		t.Error("second insert (different niche) should succeed")
	}

	if grid.CellCount() != 2 {
		t.Errorf("cell count = %d, want 2", grid.CellCount())
	}

	// Same niche, worse fitness → should NOT replace
	ind3 := &Individual{Tree: tree1, Fitness: 30, Genome: hashTree(tree1)}
	if grid.Insert(desc1, ind3) {
		t.Error("worse fitness in same niche should NOT replace")
	}
	if grid.CellCount() != 2 {
		t.Errorf("cell count = %d, want 2 (no replacement)", grid.CellCount())
	}

	// Same niche, better fitness → should replace
	ind4 := &Individual{Tree: tree1, Fitness: 90, Genome: hashTree(tree1)}
	if !grid.Insert(desc1, ind4) {
		t.Error("better fitness in same niche should replace")
	}

	// Verify best individual
	best := grid.BestIndividual()
	if best == nil || best.Fitness != 90 {
		t.Errorf("best fitness = %.1f, want 90", best.Fitness)
	}
}

func TestMAPElitesGrid_Elites(t *testing.T) {
	grid := NewMAPElitesGrid(3) // only keep top 3

	for i := 0; i < 5; i++ {
		tree := makeTestTree("t"+itoa(i), i+1, 2)
		ind := &Individual{Tree: tree, Fitness: float64((i+1)*20), Genome: hashTree(tree)}
		desc := Descriptor(tree, "test")
		grid.Insert(desc, ind)
	}

	elites := grid.Elites()
	if len(elites) > 3 {
		t.Errorf("elites count = %d, want max 3", len(elites))
	}

	// Should be sorted by fitness descending
	for i := 1; i < len(elites); i++ {
		if elites[i-1].Fitness < elites[i].Fitness {
			t.Error("elites not sorted by fitness descending")
		}
	}
}

func TestMAPElitesGrid_DiversityScore(t *testing.T) {
	grid := NewMAPElitesGrid(10)
	// Empty grid
	if grid.DiversityScore() != 0 {
		t.Error("diversity of empty grid should be 0")
	}

	// One insertion
	tree := makeTestTree("t1", 1, 2)
	ind := &Individual{Tree: tree, Fitness: 50, Genome: hashTree(tree)}
	grid.Insert(Descriptor(tree, "godev"), ind)

	if grid.DiversityScore() <= 0 {
		t.Error("diversity of single cell should be > 0")
	}
}

func TestMAPElitesGrid_Stats(t *testing.T) {
	grid := NewMAPElitesGrid(10)

	tree := makeTestTree("t1", 1, 2)
	ind := &Individual{Tree: tree, Fitness: 75, Genome: hashTree(tree)}
	grid.Insert(Descriptor(tree, "godev"), ind)
	grid.Insert(Descriptor(tree, "research"), ind)

	stats := grid.Stats()
	if stats.OccupiedCells != 2 {
		t.Errorf("occupied cells = %d, want 2", stats.OccupiedCells)
	}
	if stats.BestFitness != 75 {
		t.Errorf("best fitness = %.1f, want 75", stats.BestFitness)
	}
	if stats.MeanFitness <= 0 {
		t.Error("mean fitness should be > 0")
	}
}

func TestMAPElitesPopulation_BasicFlow(t *testing.T) {
	baseTree := makeTestTree("base", 2, 3)
	mp := NewMAPElitesPopulation(10, baseTree, "godev")

	// Simple structural fitness function (no LLM needed)
	fitnessFn := func(tree *SerializableNode) float64 {
		return StructuralQuickEval(tree)
	}

	mp.Evaluate(fitnessFn)

	if mp.Grid.CellCount() == 0 {
		t.Error("MAP-Elites grid should have entries after evaluation")
	}

	parents := mp.SelectElites()
	if len(parents) != 2 {
		t.Errorf("SelectElites() returned %d parents, want 2", len(parents))
	}
	if parents[0] == nil || parents[1] == nil {
		t.Error("parents should not be nil")
	}
}

// StructuralQuickEval is duplicated here for testing without import cycles.
func StructuralQuickEval(tree *SerializableNode) float64 {
	if tree == nil {
		return 0
	}
	nodeCount := CountNodes(tree)
	maxDepth := maxTreeDepthEvo(tree, 0)

	score := 0.0
	if nodeCount >= 15 && nodeCount <= 40 {
		score += 25
	} else if nodeCount >= 5 && nodeCount <= 60 {
		score += 15
	} else {
		score += 5
	}
	if maxDepth >= 3 && maxDepth <= 6 {
		score += 25
	} else if maxDepth >= 2 && maxDepth <= 8 {
		score += 15
	} else {
		score += 5
	}

	// Count conditions and actions
	conds, acts := 0, 0
	countCondsActs(tree, &conds, &acts)

	condScore := float64(conds)
	if condScore > 10 {
		condScore = 10
	}
	score += condScore * 2.5

	actScore := float64(acts)
	if actScore > 10 {
		actScore = 10
	}
	score += actScore * 2.5

	return score
}

func countCondsActs(node *SerializableNode, conds, acts *int) {
	if node == nil {
		return
	}
	if node.Type == "Condition" {
		*conds++
	}
	if node.Type == "Action" {
		*acts++
	}
	for i := range node.Children {
		countCondsActs(&node.Children[i], conds, acts)
	}
}

func TestMAPElitesPopulation_EvolveMAPElites(t *testing.T) {
	baseTree := makeTestTree("base", 2, 4)
	// Add conditions and actions for structural scoring
	baseTree.Children = append(baseTree.Children, SerializableNode{Type: "Condition", Name: "test_cond"})
	baseTree.Children = append(baseTree.Children, SerializableNode{Type: "Action", Name: "test_action"})

	mp := NewMAPElitesPopulation(8, baseTree, "godev")

	fitnessFn := func(tree *SerializableNode) float64 {
		return StructuralQuickEval(tree)
	}

	result := mp.EvolveMAPElites(3, fitnessFn)
	if result == nil {
		t.Error("EvolveMAPElites returned nil tree")
	}

	// Grid should have entries after evolution
	if mp.Grid.CellCount() == 0 {
		t.Error("grid should have entries after evolution")
	}

	// Diversity should increase over generations
	div := mp.Grid.DiversityScore()
	if div <= 0 {
		t.Error("diversity score should be > 0")
	}
}

func TestMAPElitesGrid_EmptyElites(t *testing.T) {
	grid := NewMAPElitesGrid(5)
	elites := grid.Elites()
	if len(elites) != 0 {
		t.Errorf("empty grid Elites() = %d, want 0", len(elites))
	}
	if grid.BestIndividual() != nil {
		t.Error("empty grid BestIndividual should be nil")
	}
}
