package evolution

import (
	"fmt"
	"math/rand"
	"sort"
)

// FeatureDimension defines a behavioral feature axis for MAP-Elites.
type FeatureDimension int

const (
	DimNodeCount FeatureDimension = iota
	DimMaxDepth
	DimDomain
)

func (d FeatureDimension) String() string {
	switch d {
	case DimNodeCount:
		return "node_count"
	case DimMaxDepth:
		return "max_depth"
	case DimDomain:
		return "domain"
	default:
		return "unknown"
	}
}

// BehavioralDescriptor maps a tree to a feature vector for MAP-Elites binning.
type BehavioralDescriptor struct {
	NodeCount int
	MaxDepth  int
	Domain    string // "" for unlabeled
}

// Descriptor computes the behavioral descriptor for a tree.
func Descriptor(tree *SerializableNode, domain string) BehavioralDescriptor {
	return BehavioralDescriptor{
		NodeCount: CountNodes(tree),
		MaxDepth:  MaxDepth(tree, 0),
		Domain:    domain,
	}
}

// Bucket bins a continuous value into discrete buckets.
func Bucket(value, bucketSize int) int {
	if bucketSize <= 0 {
		return 0
	}
	return (value / bucketSize) * bucketSize
}

// MAPElitesGrid stores the best individual per behavioral niche.
// Niche key format: "node_<bucket>|depth_<bucket>|domain_<name>"
type MAPElitesGrid struct {
	Cells       map[string]*Individual `json:"cells"`
	Dimensions  []FeatureDimension     `json:"dimensions"`
	EliteSize   int                    `json:"elite_size"`   // max elites to return
	NodeBucket  int                    `json:"node_bucket"`  // bucket size for node count (default: 10)
	DepthBucket int                    `json:"depth_bucket"` // bucket size for depth (default: 2)
}

// NewMAPElitesGrid creates an empty MAP-Elites grid.
func NewMAPElitesGrid(eliteSize int) *MAPElitesGrid {
	return &MAPElitesGrid{
		Cells:       make(map[string]*Individual),
		Dimensions:  []FeatureDimension{DimNodeCount, DimMaxDepth, DimDomain},
		EliteSize:   eliteSize,
		NodeBucket:  10,
		DepthBucket: 2,
	}
}

// Key generates the niche key for a behavioral descriptor.
func (g *MAPElitesGrid) Key(d BehavioralDescriptor) string {
	nodeBucket := Bucket(d.NodeCount, g.NodeBucket)
	depthBucket := Bucket(d.MaxDepth, g.DepthBucket)
	return fmt.Sprintf("n%d|d%d|%s", nodeBucket, depthBucket, d.Domain)
}

// Insert adds an individual to the grid, replacing only if it has higher fitness.
// Returns true if this individual won the cell.
func (g *MAPElitesGrid) Insert(desc BehavioralDescriptor, ind *Individual) bool {
	key := g.Key(desc)
	existing, ok := g.Cells[key]
	if !ok || ind.Fitness > existing.Fitness {
		g.Cells[key] = ind
		return true
	}
	return false
}

// InsertFromPopulation inserts all individuals into the grid.
// Returns the number of cells updated.
func (g *MAPElitesGrid) InsertFromPopulation(pop *Population, domain string) int {
	updated := 0
	for i := range pop.Individuals {
		desc := Descriptor(pop.Individuals[i].Tree, domain)
		// Use fitness from the individual (must be evaluated first)
		if g.Insert(desc, &pop.Individuals[i]) {
			updated++
		}
	}
	return updated
}

// Elites returns the top N elite individuals from distinct niches.
// Each niche contributes at most one individual (the best in that cell).
// If more cells exist than EliteSize, returns the top EliteSize by fitness.
func (g *MAPElitesGrid) Elites() []*Individual {
	if len(g.Cells) == 0 {
		return nil
	}

	// Collect all cell winners
	elites := make([]*Individual, 0, len(g.Cells))
	for _, ind := range g.Cells {
		elites = append(elites, ind)
	}

	// Sort by fitness descending
	sort.Slice(elites, func(i, j int) bool {
		return elites[i].Fitness > elites[j].Fitness
	})

	// Truncate to EliteSize
	if len(elites) > g.EliteSize {
		elites = elites[:g.EliteSize]
	}

	return elites
}

// DiversityScore returns the fraction of occupied cells (0-1).
// Higher = more behavioral diversity.
func (g *MAPElitesGrid) DiversityScore() float64 {
	totalCells := g.estimateTotalCells()
	if totalCells == 0 {
		return 0
	}
	return float64(len(g.Cells)) / float64(totalCells)
}

// estimateTotalCells estimates the grid capacity from occupied cells.
// This is approximate since we don't know all possible domains a priori.
func (g *MAPElitesGrid) estimateTotalCells() int {
	if len(g.Cells) == 0 {
		return 0
	}
	// Count unique domains, node buckets, depth buckets
	domains := make(map[string]bool)
	nodeBuckets := make(map[int]bool)
	depthBuckets := make(map[int]bool)

	for key := range g.Cells {
		var domain string
		var nodeB, depthB int
		_, _ = fmt.Sscanf(key, "n%d|d%d|%s", &nodeB, &depthB, &domain)
		domains[domain] = true
		nodeBuckets[nodeB] = true
		depthBuckets[depthB] = true
	}
	return len(domains) * len(nodeBuckets) * len(depthBuckets)
}

// CellCount returns the number of occupied cells.
func (g *MAPElitesGrid) CellCount() int { return len(g.Cells) }

// BestIndividual returns the overall best across all cells.
func (g *MAPElitesGrid) BestIndividual() *Individual {
	var best *Individual
	for _, ind := range g.Cells {
		if best == nil || ind.Fitness > best.Fitness {
			best = ind
		}
	}
	return best
}

// SpecialistDistribution returns occupied MAP-Elites cells grouped by domain.
// The resulting map feeds the LLM supervisor's structured population state.
func (g *MAPElitesGrid) SpecialistDistribution() map[string]int {
	domains := make(map[string]int)
	if g == nil {
		return domains
	}
	for key := range g.Cells {
		var domain string
		var nb, db int
		if _, err := fmt.Sscanf(key, "n%d|d%d|%s", &nb, &db, &domain); err != nil || domain == "" {
			domain = "unknown"
		}
		domains[domain]++
	}
	return domains
}

// MAPElitesPopulation wraps a Population with a MAP-Elites grid for diversity-preserving evolution.
type MAPElitesPopulation struct {
	*Population
	Grid   *MAPElitesGrid
	Domain string
}

// NewMAPElitesPopulation creates a population with MAP-Elites diversity tracking.
func NewMAPElitesPopulation(size int, baseTree *SerializableNode, domain string) *MAPElitesPopulation {
	pop := NewPopulation(size, baseTree)
	return &MAPElitesPopulation{
		Population: pop,
		Grid:       NewMAPElitesGrid(size / 2),
		Domain:     domain,
	}
}

// Evaluate scores all individuals and updates the MAP-Elites grid.
func (mp *MAPElitesPopulation) Evaluate(fitnessFn func(*SerializableNode) float64) {
	mp.Population.Evaluate(fitnessFn)
	mp.Grid.InsertFromPopulation(mp.Population, mp.Domain)
}

// SelectElites selects parents using MAP-Elites diversity + fitness.
// Picks from distinct niches first, falls back to fitness-based selection
// if grid is sparse. This prevents premature convergence.
func (mp *MAPElitesPopulation) SelectElites() []*SerializableNode {
	elites := mp.Grid.Elites()

	// If grid is too sparse for selection, fall back to standard tournament
	if len(elites) < 2 {
		return mp.Population.Select()
	}

	// Pick two parents from different niches (if possible)
	parents := make([]*SerializableNode, 2)
	parents[0] = elites[0].Tree

	// Try to find a second parent from a different niche
	if len(elites) > 1 {
		parents[1] = elites[1].Tree
	} else {
		parents[1] = elites[0].Tree
	}

	return parents
}

// EvolveMAPElites runs the genetic algorithm with MAP-Elites diversity preservation.
// Each generation selects from diverse niches instead of pure fitness ranking.
func (mp *MAPElitesPopulation) EvolveMAPElites(generations int, fitnessFn func(*SerializableNode) float64) *SerializableNode {
	mp.Evaluate(fitnessFn)
	eliteCount := max(2, len(mp.Individuals)/10)
	supervisor := NewLLMSupervisor()

	for gen := 0; gen < generations; gen++ {
		mp.Generation++
		guidance := supervisor.Guide(BuildPopulationStateWithGrid(mp.Population, mp.Grid, mp.Domain))
		mutationRate := guidance.RecommendedRate

		// Get diverse elite parents from MAP-Elites grid
		mapElites := mp.Grid.Elites()

		// Sort regular population by fitness for fallback
		sort.Slice(mp.Individuals, func(i, j int) bool {
			return mp.Individuals[i].Fitness > mp.Individuals[j].Fitness
		})

		// Keep elites from MAP-Elites grid (diverse) + top fitness (quality)
		newPop := make([]Individual, len(mp.Individuals))
		copied := 0

		// 1. First, preserve MAP-Elites niche winners (diversity)
		for i := 0; i < len(mapElites) && copied < eliteCount; i++ {
			newPop[copied] = *mapElites[i]
			copied++
		}

		// 2. Fill remaining elite slots with top fitness (quality)
		for i := 0; copied < eliteCount && i < len(mp.Individuals); i++ {
			newPop[copied] = mp.Individuals[i]
			copied++
		}

		// 3. Fill rest with crossover + mutation from diverse parents
		for i := eliteCount; i < len(mp.Individuals); i++ {
			parents := mp.SelectElites()
			child := Crossover(parents[0], parents[1])
			if rand.Float64() < mutationRate {
				ops := randomMutation(child)
				ApplyMutations(child, ops)
			}
			newPop[i] = Individual{Tree: child, Genome: hashTree(child)}
		}

		mp.Individuals = newPop
		mp.Evaluate(fitnessFn)
	}

	return mp.BestTree
}

// MAPElitesStats reports diversity and coverage metrics for the grid.
type MAPElitesStats struct {
	TotalCells     int     `json:"total_cells"`
	OccupiedCells  int     `json:"occupied_cells"`
	DiversityScore float64 `json:"diversity_score"`
	BestFitness    float64 `json:"best_fitness"`
	MeanFitness    float64 `json:"mean_fitness"`
	Domains        int     `json:"domains"`
}

// Stats returns aggregate statistics for the MAP-Elites grid.
func (g *MAPElitesGrid) Stats() MAPElitesStats {
	stats := MAPElitesStats{
		OccupiedCells: len(g.Cells),
	}

	domains := make(map[string]bool)
	totalFit := 0.0
	for key, ind := range g.Cells {
		var domain string
		var nb, db int
		_, _ = fmt.Sscanf(key, "n%d|d%d|%s", &nb, &db, &domain)
		domains[domain] = true
		if ind.Fitness > stats.BestFitness {
			stats.BestFitness = ind.Fitness
		}
		totalFit += ind.Fitness
	}

	if stats.OccupiedCells > 0 {
		stats.MeanFitness = totalFit / float64(stats.OccupiedCells)
	}
	stats.Domains = len(domains)
	stats.TotalCells = g.estimateTotalCells()
	stats.DiversityScore = g.DiversityScore()

	return stats
}
