package evolution

import (
	"fmt"
	"math/rand"
	"sort"
	"sync"
)

// IslandModel manages domain-separated subpopulations with periodic migration.
// Prevents premature convergence by maintaining genetic diversity across domains.
type IslandModel struct {
	mu       sync.RWMutex
	Islands  map[string]*Population           `json:"islands"`
	Domain   string                            `json:"-"`
	MigrationInterval int                      `json:"migration_interval"` // generations between migration
	MigrationRate     float64                  `json:"migration_rate"`     // fraction of population to migrate (0-1)
	Generation        int                      `json:"generation"`
}

// NewIslandModel creates an island model with domain-separated populations.
func NewIslandModel(migrationInterval int, migrationRate float64) *IslandModel {
	return &IslandModel{
		Islands:           make(map[string]*Population),
		MigrationInterval: migrationInterval,
		MigrationRate:     migrationRate,
	}
}

// AddIsland adds a new domain population to the model.
func (im *IslandModel) AddIsland(domain string, pop *Population) {
	im.mu.Lock()
	defer im.mu.Unlock()
	im.Islands[domain] = pop
}

// GetIsland returns the population for a domain.
func (im *IslandModel) GetIsland(domain string) *Population {
	im.mu.RLock()
	defer im.mu.RUnlock()
	return im.Islands[domain]
}

// Migrate performs inter-island migration: top individuals from each island
// are copied to random other islands, replacing their worst individuals.
// This is the AlphaEvolve-style periodic cross-pollination.
func (im *IslandModel) Migrate() int {
	im.mu.Lock()
	defer im.mu.Unlock()

	if len(im.Islands) < 2 {
		return 0
	}

	migrated := 0
	domains := make([]string, 0, len(im.Islands))
	for d := range im.Islands {
		domains = append(domains, d)
	}

	for _, srcDomain := range domains {
		srcPop := im.Islands[srcDomain]
		if srcPop == nil || len(srcPop.Individuals) == 0 {
			continue
		}

		// Pick a random target domain (different from source)
		var tgtDomain string
		for {
			tgtDomain = domains[rand.Intn(len(domains))]
			if tgtDomain != srcDomain {
				break
			}
		}

		tgtPop := im.Islands[tgtDomain]
		if tgtPop == nil || len(tgtPop.Individuals) == 0 {
			continue
		}

		// Sort source by fitness (best first) and target (worst first)
		srcSorted := make([]Individual, len(srcPop.Individuals))
		copy(srcSorted, srcPop.Individuals)
		sort.Slice(srcSorted, func(i, j int) bool {
			return srcSorted[i].Fitness > srcSorted[j].Fitness
		})

		tgtSorted := make([]Individual, len(tgtPop.Individuals))
		copy(tgtSorted, tgtPop.Individuals)
		sort.Slice(tgtSorted, func(i, j int) bool {
			return tgtSorted[i].Fitness < tgtSorted[j].Fitness
		})

		// Migrate top individuals from source to replace worst in target
		migrateCount := max(1, int(float64(len(tgtPop.Individuals))*im.MigrationRate))
		for i := 0; i < migrateCount && i < len(srcSorted) && i < len(tgtSorted); i++ {
			// Copy the source elite to target's worst slot
			tgtSorted[i] = Individual{
				Tree:   cloneTree(srcSorted[i].Tree),
				Fitness: srcSorted[i].Fitness,
				Genome: hashTree(srcSorted[i].Tree),
			}
			migrated++
		}

		// Update target population
		tgtPop.Individuals = tgtSorted
		im.Islands[tgtDomain] = tgtPop
	}

	return migrated
}

// EvolveAll runs one generation on all islands, with migration if due.
func (im *IslandModel) EvolveAll(fitnessFn func(*SerializableNode) float64) map[string]*SerializableNode {
	im.mu.Lock()
	defer im.mu.Unlock()

	im.Generation++
	bestTrees := make(map[string]*SerializableNode)

	for domain, pop := range im.Islands {
		pop.Evaluate(fitnessFn)
		bestTrees[domain] = pop.BestTree
	}

	// Periodic migration
	if im.Generation%im.MigrationInterval == 0 {
		im.mu.Unlock() // Migrate handles its own locking
		im.Migrate()
		im.mu.Lock()
		im.Generation++
	}

	return bestTrees
}

// DiversityAcrossIslands measures genetic diversity between islands.
// 0 = all islands identical, 1 = entirely different.
func (im *IslandModel) DiversityAcrossIslands() float64 {
	im.mu.RLock()
	defer im.mu.RUnlock()

	if len(im.Islands) <= 1 {
		return 0
	}

	// Collect all genomes per island
	islandGenomes := make(map[string]map[string]bool)
	for domain, pop := range im.Islands {
		genomes := make(map[string]bool)
		for _, ind := range pop.Individuals {
			genomes[ind.Genome] = true
		}
		islandGenomes[domain] = genomes
	}

	// Jaccard distance between all pairs
	totalDist := 0.0
	pairs := 0
	domains := make([]string, 0, len(islandGenomes))
	for d := range islandGenomes {
		domains = append(domains, d)
	}

	for i := 0; i < len(domains); i++ {
		for j := i + 1; j < len(domains); j++ {
			gi := islandGenomes[domains[i]]
			gj := islandGenomes[domains[j]]

			// Intersection size
			intersection := 0
			for g := range gi {
				if gj[g] {
					intersection++
				}
			}
			union := len(gi) + len(gj) - intersection
			if union > 0 {
				totalDist += 1.0 - float64(intersection)/float64(union)
			}
			pairs++
		}
	}

	if pairs == 0 {
		return 0
	}
	return totalDist / float64(pairs)
}

// IslandStats reports per-domain and cross-island metrics.
type IslandStats struct {
	Domains       int                `json:"domains"`
	TotalPop      int                `json:"total_population"`
	BestPerDomain map[string]float64 `json:"best_per_domain"`
	CrossDiversity float64           `json:"cross_diversity"`
}

// Stats returns aggregate statistics for the island model.
func (im *IslandModel) Stats() IslandStats {
	im.mu.RLock()
	defer im.mu.RUnlock()

	stats := IslandStats{
		Domains:       len(im.Islands),
		BestPerDomain: make(map[string]float64),
	}

	for domain, pop := range im.Islands {
		stats.TotalPop += len(pop.Individuals)
		stats.BestPerDomain[domain] = pop.BestFitness
	}

	im.mu.RUnlock()
	stats.CrossDiversity = im.DiversityAcrossIslands()
	im.mu.RLock()

	return stats
}

// Summary returns a human-readable island model summary.
func (im *IslandModel) Summary() string {
	stats := im.Stats()
	s := fmt.Sprintf("IslandModel: %d domains, %d total pop, gen %d\n",
		stats.Domains, stats.TotalPop, im.Generation)
	for domain, best := range stats.BestPerDomain {
		s += fmt.Sprintf("  %s: best=%.1f\n", domain, best)
	}
	s += fmt.Sprintf("  cross-diversity: %.2f\n", stats.CrossDiversity)
	return s
}
