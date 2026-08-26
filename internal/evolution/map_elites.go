package evolution

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	Cap         int                    `json:"cap,omitzero"` // max occupied cells for Save/Load (0 = unbounded)
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
//
// Ownership: the grid stores ind as given — it does not copy the Individual or
// its Tree. A cell is therefore only a snapshot of the shape whose descriptor
// selected it if the caller hands over a tree nothing else mutates afterwards.
// Callers archiving a long-lived tree that is evolved in place across cycles
// (see gardener.recordDiversityObservation) MUST clone before inserting;
// otherwise every cell ends up aliasing that one object, holding the latest
// shape against a stale per-cell Fitness, and consumers like EliteSeed hand
// back the caller's own live tree instead of an archived alternative.
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
	slices.SortFunc(elites, func(a, b *Individual) int {
		return cmp.Compare(b.Fitness, a.Fitness)
	})

	// Truncate to EliteSize
	if len(elites) > g.EliteSize {
		elites = elites[:g.EliteSize]
	}

	return elites
}

// EliteSeed returns the fittest archived elite that occupies a niche OTHER
// than current's and whose fitness strictly beats fitnessFloor — the active-
// elitism accessor a caller uses to escape a collapsed niche by reseeding from
// the archive instead of mutating its current best again.
//
// Same-niche elites are never returned: reseeding into the cell we are already
// collapsed in is not an escape, however fit that cell's winner happens to be.
// A floor no other-niche elite clears yields nil rather than a downgrade, so
// the caller can safely treat nil as "keep the current best".
func (g *MAPElitesGrid) EliteSeed(current BehavioralDescriptor, fitnessFloor float64) *Individual {
	if g == nil || len(g.Cells) == 0 {
		return nil
	}
	currentKey := g.Key(current)
	var best *Individual
	for key, ind := range g.Cells {
		if ind == nil || ind.Tree == nil || key == currentKey {
			continue
		}
		if ind.Fitness <= fitnessFloor {
			continue
		}
		if best == nil || ind.Fitness > best.Fitness {
			best = ind
		}
	}
	return best
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
	// ExpertKnowledge is an optional, caller-owned learning archive that
	// EvolveMAPElites's mutation-application step observes every
	// genuinely-improving mutation into via Observe, mirroring the ek
	// plumbing EvolvePareto/NSGA-II Evolve/EvolveQLearning already have
	// (pareto.go:323, multi_objective.go, learning.go:845-921). A nil
	// ExpertKnowledge is a no-op.
	ExpertKnowledge *ExpertKnowledge
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
		return mp.Select()
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
// Crisis detection, emergency mutation-rate override, specialist archiving,
// and extinct-specialist resurrection all run inside the same
// Population.selfHealGeneration envelope every other production Evolve
// variant uses (learning.go, pareto.go, multi_objective.go, island.go), so a
// population-wide diversity collapse is caught even when every individual
// shares the same behavioral descriptor and the grid itself looks diverse.
func (mp *MAPElitesPopulation) EvolveMAPElites(generations int, fitnessFn func(*SerializableNode) float64) *SerializableNode {
	mp.Evaluate(fitnessFn)
	eliteCount := max(2, len(mp.Individuals)/10)
	supervisor := NewLLMSupervisor()

	for range generations {
		mp.Generation++

		// Capture the MAP-Elites grid's niche winners before the shared
		// self-healing envelope runs — it only touches Population fields, not
		// the grid — so the diversity-preserving newPop assembly below still
		// has this generation's niche winners available.
		mapElites := mp.Grid.Elites()

		mp.selfHealGeneration(eliteCount, supervisor, func(mutationRate float64) {
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
				if evoFloat64() < mutationRate {
					ops := randomMutation(child)
					if len(ops) > 0 {
						ops[0] = materializeMutationOp(ops[0])
					}
					before := fitnessFn(child)
					if applied := ApplyMutations(child, ops); applied > 0 && len(ops) > 0 {
						after := fitnessFn(child)
						mp.ExpertKnowledge.Observe(ops[0].Operation, "map_elites", after-before)
					}
				}
				newPop[i] = Individual{Tree: child, Genome: hashTree(child)}
			}

			mp.Individuals = newPop
			mp.Evaluate(fitnessFn)
		})
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

// mapElitesArchive is the durable JSON snapshot of a MAPElitesGrid — just the
// occupied cells under the same "cells" key the grid itself marshals to, so
// archive consumers read the shape they already know.
type mapElitesArchive struct {
	Cells map[string]*Individual `json:"cells"`
}

// cappedCells bounds a cell map to at most limit niches by evicting the
// lowest-fitness cells first (ties broken by key for determinism). A limit of
// zero or less means unbounded. The input map is never mutated; callers get
// either the original map or a bounded copy.
func cappedCells(cells map[string]*Individual, limit int) map[string]*Individual {
	if limit <= 0 || len(cells) <= limit {
		return cells
	}
	type niche struct {
		key string
		ind *Individual
	}
	niches := make([]niche, 0, len(cells))
	for key, ind := range cells {
		niches = append(niches, niche{key: key, ind: ind})
	}
	slices.SortFunc(niches, func(a, b niche) int {
		return cmp.Or(
			cmp.Compare(b.ind.Fitness, a.ind.Fitness),
			cmp.Compare(a.key, b.key),
		)
	})
	bounded := make(map[string]*Individual, limit)
	for _, n := range niches[:limit] {
		bounded[n.key] = n.ind
	}
	return bounded
}

// Save persists the grid's occupied cells as JSON at path, creating missing
// parent directories and writing atomically (temp file + rename) under the
// shared advisory flock so concurrent writers cannot interleave partial
// archives (ADR-024). When Cap is set, only the strongest Cap niches are
// persisted — the weakest cells are evicted from the archive first. The
// in-memory grid is left untouched.
func (g *MAPElitesGrid) Save(path string) error {
	data, err := json.MarshalIndent(mapElitesArchive{Cells: cappedCells(g.Cells, g.Cap)}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal map-elites archive: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create map-elites archive dir: %w", err)
	}
	release, err := acquireExperienceLock(path)
	if err != nil {
		return err
	}
	defer release()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write map-elites archive: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("commit map-elites archive: %w", err)
	}
	return nil
}

// Load warm-starts the grid from the archive at path by merging niches:
// disk-only niches are adopted wholesale, memory-only niches survive
// untouched, and an overlapping niche key keeps the fitter copy. After the
// merge the grid is bounded back to Cap by evicting the lowest-fitness cells
// first, so a cross-domain merge can never exceed the cap. A missing archive
// is a silent cold start; a corrupt archive is an error that leaves the
// in-memory state untouched.
func (g *MAPElitesGrid) Load(path string) error {
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
		return fmt.Errorf("read map-elites archive: %w", err)
	}
	var snap mapElitesArchive
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("parse map-elites archive %s: %w", path, err)
	}
	if g.Cells == nil {
		g.Cells = make(map[string]*Individual, len(snap.Cells))
	}
	for key, ind := range snap.Cells {
		if ind == nil {
			continue
		}
		if existing, ok := g.Cells[key]; ok && existing.Fitness >= ind.Fitness {
			continue
		}
		g.Cells[key] = ind
	}
	g.Cells = cappedCells(g.Cells, g.Cap)
	return nil
}
