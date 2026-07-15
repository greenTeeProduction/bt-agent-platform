package evolution

import (
	"strconv"
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
		child := &SerializableNode{Type: "Sequence", Name: "deep_" + strconv.Itoa(d)}
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
	tree1 := makeTestTree("small", 1, 2) // 3 nodes, depth 1
	tree2 := makeDeepTree(5)             // 5 nodes, depth 4

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
		tree := makeTestTree("t"+strconv.Itoa(i), i+1, 2)
		ind := &Individual{Tree: tree, Fitness: float64((i + 1) * 20), Genome: hashTree(tree)}
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
	fitnessFn := StructuralQuickEval

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
	maxDepth := MaxDepth(tree, 0)

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
	baseTree.Children = append(baseTree.Children,
		SerializableNode{Type: "Condition", Name: "test_cond"},
		SerializableNode{Type: "Action", Name: "test_action"},
	)

	mp := NewMAPElitesPopulation(8, baseTree, "godev")

	fitnessFn := StructuralQuickEval

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

// TestMAPElitesPopulation_EvolveMAPElites_ResurrectsExtinctSpecialist verifies
// milestone 2/3 of the "Close the self-healing envelope gap across the
// remaining GA variants" program: EvolveMAPElites must delegate crisis
// detection, emergency mutation-rate override, specialist archiving, and
// extinct-specialist resurrection to the shared Population.selfHealGeneration
// envelope via its embedded *Population — exactly like MemeticEvolve, Evolve,
// EvolveWithExperience, EvolveQLearning, NSGAIIPopulation.Evolve, and
// EvolvePareto already do — instead of hand-inlining its own
// mp.Crisis/recordCrisisReasons/resurrectExtinctSpecialists/
// GetEmergencyMutationRate copy driven by a MAP-Elites-grid-aware
// PopulationState.
//
// Setup mirrors TestMemeticEvolve_ResurrectsExtinctSpecialist: a
// SpecialistRegistry pre-loaded with a validated, high-fitness "goap"
// archetype last seen at generation 0, and a live population of identical,
// non-specialist individuals. Because every individual shares the same
// genome AND the same behavioral descriptor, they collapse into a single
// occupied MAP-Elites cell — under the grid-aware PopulationState the
// hand-inlined code builds today, MAPElitesGrid.DiversityScore() reads 1.0
// (a single occupied cell against an estimated total of 1), so no crisis
// ever fires and the extinct goap niche is never resurrected. Only the
// shared envelope's plain population-level Diversity() (distinct genomes /
// population size == 1/size == 0.1, below the 0.2 threshold) correctly
// detects this as a diversity collapse. This isolates the exact behavior the
// consolidation is supposed to fix: identical resurrection behavior to every
// other selfHealGeneration-wrapped GA variant, not a grid-only signal that
// misses population-wide collapse.
func TestMAPElitesPopulation_EvolveMAPElites_ResurrectsExtinctSpecialist(t *testing.T) {
	base := DefaultTree()

	// Archive a high-fitness specialist that is missing from the live population.
	registry := NewSpecialistRegistry()
	archetype := &SerializableNode{
		Type:     "Sequence",
		Name:     "GoapSpecialist",
		Children: []SerializableNode{{Type: "Action", Name: "PlanGoap"}},
	}
	registry.Observe(&EvolutionMetadata{
		TreeID:  "goap-archetype",
		Tags:    []string{"specialist:goap"},
		Fitness: FitnessRecord{Score: 0.95, Validated: true},
	}, archetype, 0)

	const size = 10
	const domain = "godev"
	individuals := make([]Individual, size)
	for i := 0; i < size; i++ {
		// Identical, non-specialist genomes → Population.Diversity() ==
		// 1/size == 0.1 (< 0.2 threshold) trips diversity_collapse under the
		// shared envelope, while the single shared behavioral descriptor
		// collapses the grid to one cell (DiversityScore == 1.0), which
		// would NOT trip the old grid-aware detection.
		individuals[i] = Individual{Tree: cloneTree(base), Genome: "identical-genome"}
	}

	mp := &MAPElitesPopulation{
		Population: &Population{
			Individuals: individuals,
			// Far past the archetype's last-seen generation so any reasonable
			// extinctAfter window has elapsed once Evolve bumps the counter.
			Generation:  500,
			Specialists: registry,
		},
		Grid:   NewMAPElitesGrid(3),
		Domain: domain,
	}

	if d := mp.Diversity(); d <= 0 || d >= 0.2 {
		t.Fatalf("test setup: want collapsed population diversity in (0, 0.2), got %.3f", d)
	}
	mp.Grid.InsertFromPopulation(mp.Population, domain)
	if div := mp.Grid.DiversityScore(); div != 1.0 {
		t.Fatalf("test setup: want a single-cell grid diversity of 1.0 (no grid-level collapse signal), got %.3f", div)
	}

	// Constant fitness keeps every individual "working" so quality_crash stays
	// quiet and diversity_collapse is the isolated crisis signal.
	mp.EvolveMAPElites(1, func(*SerializableNode) float64 { return 1.0 })

	emergency := NewCrisisDetector().GetEmergencyMutationRate()
	if mp.LastMutationRate < emergency {
		t.Errorf("collapsed-diversity generation mutation rate = %.3f, want >= EmergencyRate %.3f",
			mp.LastMutationRate, emergency)
	}

	var resurrected bool
	for _, ind := range mp.Individuals {
		if ind.Meta != nil && ind.Meta.IsResurrected() {
			resurrected = true
			break
		}
	}
	if !resurrected {
		t.Fatal("expected EvolveMAPElites to resurrect the extinct goap specialist via the shared selfHealGeneration envelope")
	}
}
