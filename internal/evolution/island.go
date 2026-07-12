package evolution

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// IslandModel manages domain-separated subpopulations with periodic migration.
// Prevents premature convergence by maintaining genetic diversity across domains.
type IslandModel struct {
	mu                sync.RWMutex
	Islands           map[string]*Population `json:"islands"`
	Domain            string                 `json:"-"`
	MigrationInterval int                    `json:"migration_interval"` // generations between migration
	MigrationRate     float64                `json:"migration_rate"`     // fraction of population to migrate (0-1)
	Generation        int                    `json:"generation"`
	TotalMigrations   int                    `json:"total_migrations"` // cumulative individuals moved by Migrate
	Cap               int                    `json:"cap"`              // max individuals per island after Load; 0 = unbounded
	IslandCap         int                    `json:"island_cap"`       // max distinct island keys retained after Load; 0 = unbounded
	// EvictedIndividuals is the cumulative count of individuals dropped by
	// enforceIslandCap across every Load call, mirroring how TotalMigrations
	// accumulates across Migrate calls.
	EvictedIndividuals int `json:"evicted_individuals"`
	// EvictedIslands is the cumulative count of whole islands dropped by
	// evictAdoptedIslandsBeyondCap across every Load call.
	EvictedIslands int `json:"evicted_islands"`
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
				Tree:    cloneTree(srcSorted[i].Tree),
				Fitness: srcSorted[i].Fitness,
				Genome:  hashTree(srcSorted[i].Tree),
			}
			migrated++
		}

		// Update target population
		tgtPop.Individuals = tgtSorted
		im.Islands[tgtDomain] = tgtPop
	}

	im.TotalMigrations += migrated
	return migrated
}

// EvolveAll runs one generation on all islands, with migration if due. Each
// island's generation runs inside the shared Population.selfHealGeneration
// envelope rather than a bare Evaluate, so a collapsed island seeds its
// Specialists registry, initializes its Crisis detector, and resurrects an
// extinct specialist archetype before migration — the same self-healing path
// a direct Population.Evolve call already gets. Islands added without a
// pre-seeded Specialists registry (the AddIsland default) get one here so no
// island can skip the self-healing step for lack of a registry to consult.
func (im *IslandModel) EvolveAll(fitnessFn func(*SerializableNode) float64) map[string]*SerializableNode {
	im.mu.Lock()
	defer im.mu.Unlock()

	im.Generation++
	bestTrees := make(map[string]*SerializableNode)
	supervisor := NewLLMSupervisor()

	for domain, pop := range im.Islands {
		if pop.Specialists == nil {
			pop.Specialists = NewSpecialistRegistry()
		}
		// Clamp so degenerate populations (size < 2) don't overflow the elite copy.
		eliteCount := min(max(2, len(pop.Individuals)/10), len(pop.Individuals))
		pop.Generation++
		pop.selfHealGeneration(eliteCount, supervisor, func(float64) {
			pop.Evaluate(fitnessFn)
		})
		bestTrees[domain] = pop.BestTree
	}

	// Periodic migration
	if im.Generation%im.MigrationInterval == 0 {
		im.mu.Unlock() // Migrate handles its own locking
		im.Migrate()
		im.mu.Lock()
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
	Domains        int                `json:"domains"`
	TotalPop       int                `json:"total_population"`
	BestPerDomain  map[string]float64 `json:"best_per_domain"`
	CrossDiversity float64            `json:"cross_diversity"`
	Migrations     int                `json:"migrations"`
	// Resurrections sums Population.Resurrections across every island — the
	// model-level view of how many extinct specialists EvolveAll's
	// self-healing step has resurrected fleet-wide.
	Resurrections int `json:"resurrections"`
	// EvictedIndividuals/EvictedIslands mirror IslandModel.EvictedIndividuals/
	// EvictedIslands so eviction activity is visible via Stats() alongside
	// Migrations.
	EvictedIndividuals int `json:"evicted_individuals"`
	EvictedIslands     int `json:"evicted_islands"`
}

// Stats returns aggregate statistics for the island model.
func (im *IslandModel) Stats() IslandStats {
	im.mu.RLock()
	defer im.mu.RUnlock()

	stats := IslandStats{
		Domains:            len(im.Islands),
		BestPerDomain:      make(map[string]float64),
		Migrations:         im.TotalMigrations,
		EvictedIndividuals: im.EvictedIndividuals,
		EvictedIslands:     im.EvictedIslands,
	}

	for domain, pop := range im.Islands {
		stats.TotalPop += len(pop.Individuals)
		stats.BestPerDomain[domain] = pop.BestFitness
		stats.Resurrections += pop.Resurrections
	}

	im.mu.RUnlock()
	stats.CrossDiversity = im.DiversityAcrossIslands()
	im.mu.RLock()

	return stats
}

// islandArchive is the durable JSON snapshot of an IslandModel — the model's
// serializable fields under their existing keys, so archive consumers read
// the same "islands"/"generation" shape the model itself marshals to.
type islandArchive struct {
	Islands         map[string]*Population `json:"islands"`
	Generation      int                    `json:"generation"`
	TotalMigrations int                    `json:"total_migrations"`
}

// Save persists the island model as JSON at path, creating missing parent
// directories and writing atomically (temp file + rename) under the shared
// advisory flock so concurrent writers cannot interleave partial archives
// (ADR-024).
func (im *IslandModel) Save(path string) error {
	im.mu.RLock()
	data, err := json.MarshalIndent(islandArchive{
		Islands:         im.Islands,
		Generation:      im.Generation,
		TotalMigrations: im.TotalMigrations,
	}, "", "  ")
	im.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("marshal island archive: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create island archive dir: %w", err)
	}
	release, err := acquireExperienceLock(path)
	if err != nil {
		return err
	}
	defer release()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write island archive: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("commit island archive: %w", err)
	}
	return nil
}

// Load warm-starts the model from the archive at path by merging per-domain
// subpopulations: disk-only domains are adopted wholesale, memory-only domains
// survive untouched, and an overlapping domain unions its individuals deduped
// by genome with the fitter copy winning. Progress counters (Generation,
// TotalMigrations) resume from the persisted high-water mark. A missing
// archive is a silent cold start; a corrupt archive is an error that leaves
// the in-memory state untouched.
func (im *IslandModel) Load(path string) error {
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
		return fmt.Errorf("read island archive: %w", err)
	}
	var snap islandArchive
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("parse island archive %s: %w", path, err)
	}

	im.mu.Lock()
	defer im.mu.Unlock()
	seeded := make(map[string]bool, len(im.Islands))
	for domain := range im.Islands {
		seeded[domain] = true
	}
	var adopted []string
	for domain, diskPop := range snap.Islands {
		if diskPop == nil {
			continue
		}
		if mem := im.Islands[domain]; mem != nil {
			im.EvictedIndividuals += mergeIslandPopulation(mem, diskPop, im.Cap)
			continue
		}
		im.EvictedIndividuals += enforceIslandCap(diskPop, im.Cap)
		im.Islands[domain] = diskPop
		if !seeded[domain] {
			adopted = append(adopted, domain)
		}
	}
	im.evictAdoptedIslandsBeyondCap(adopted)
	if snap.Generation > im.Generation {
		im.Generation = snap.Generation
	}
	if snap.TotalMigrations > im.TotalMigrations {
		im.TotalMigrations = snap.TotalMigrations
	}
	return nil
}

// evictAdoptedIslandsBeyondCap enforces IslandCap on the distinct island keys
// held by im, evicting whole islands by lowest BestFitness until the count is
// within IslandCap or no more adopted candidates remain. Only domains in
// adopted are eviction candidates — islands the current run seeded before
// Load are never evicted, regardless of their BestFitness. IslandCap <= 0
// leaves the island count unbounded.
func (im *IslandModel) evictAdoptedIslandsBeyondCap(adopted []string) {
	if im.IslandCap <= 0 {
		return
	}
	for len(im.Islands) > im.IslandCap && len(adopted) > 0 {
		worst := 0
		for i := 1; i < len(adopted); i++ {
			if im.Islands[adopted[i]].BestFitness < im.Islands[adopted[worst]].BestFitness {
				worst = i
			}
		}
		delete(im.Islands, adopted[worst])
		im.EvictedIslands++
		adopted = append(adopted[:worst], adopted[worst+1:]...)
	}
}

// mergeIslandPopulation unions the archived individuals into the in-memory
// population, deduped by genome with the fitter copy winning, then enforces
// islandCap (if non-zero) so the merged island never exceeds it. It returns
// the number of individuals enforceIslandCap evicted.
func mergeIslandPopulation(mem, disk *Population, islandCap int) int {
	byGenome := make(map[string]int, len(mem.Individuals))
	for i, ind := range mem.Individuals {
		byGenome[ind.Genome] = i
	}
	for _, ind := range disk.Individuals {
		if i, seen := byGenome[ind.Genome]; seen {
			if ind.Fitness > mem.Individuals[i].Fitness {
				mem.Individuals[i] = ind
			}
			continue
		}
		byGenome[ind.Genome] = len(mem.Individuals)
		mem.Individuals = append(mem.Individuals, ind)
	}
	if disk.BestFitness > mem.BestFitness {
		mem.BestFitness = disk.BestFitness
	}
	return enforceIslandCap(mem, islandCap)
}

// enforceIslandCap evicts the lowest-fitness individuals from pop so it holds
// at most islandCap individuals; islandCap <= 0 leaves pop unbounded. It
// returns the number of individuals evicted.
func enforceIslandCap(pop *Population, islandCap int) int {
	if islandCap <= 0 || len(pop.Individuals) <= islandCap {
		return 0
	}
	sort.Slice(pop.Individuals, func(i, j int) bool {
		return pop.Individuals[i].Fitness > pop.Individuals[j].Fitness
	})
	evicted := len(pop.Individuals) - islandCap
	pop.Individuals = pop.Individuals[:islandCap]
	return evicted
}

// Summary returns a human-readable island model summary.
func (im *IslandModel) Summary() string {
	stats := im.Stats()
	s := fmt.Sprintf("IslandModel: %d domains, %d total pop, gen %d, migrations %d\n",
		stats.Domains, stats.TotalPop, im.Generation, stats.Migrations)
	for domain, best := range stats.BestPerDomain {
		s += fmt.Sprintf("  %s: best=%.1f\n", domain, best)
	}
	s += fmt.Sprintf("  cross-diversity: %.2f\n", stats.CrossDiversity)
	s += fmt.Sprintf("  evicted: %d individuals, %d islands\n", stats.EvictedIndividuals, stats.EvictedIslands)
	return s
}
