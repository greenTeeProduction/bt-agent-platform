package evolution

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func islandTestTree(name string) *SerializableNode {
	return &SerializableNode{
		Type: "Sequence",
		Name: name,
		Children: []SerializableNode{
			{Type: "Action", Name: "ValidateInput"},
			{Type: "Action", Name: name + "Action"},
		},
	}
}

func islandTestPopulation(names ...string) *Population {
	pop := &Population{Individuals: make([]Individual, len(names))}
	for i, name := range names {
		tree := islandTestTree(name)
		pop.Individuals[i] = Individual{Tree: tree, Genome: hashTree(tree)}
	}
	return pop
}

func TestIslandModel_AddGetAndSingleIslandMigration(t *testing.T) {
	im := NewIslandModel(3, 0.25)
	if im.MigrationInterval != 3 || im.MigrationRate != 0.25 {
		t.Fatalf("unexpected migration config: interval=%d rate=%v", im.MigrationInterval, im.MigrationRate)
	}

	goPop := islandTestPopulation("go-a", "go-b")
	im.AddIsland("go", goPop)

	if got := im.GetIsland("go"); got != goPop {
		t.Fatalf("GetIsland returned %#v, want original population", got)
	}
	if got := im.GetIsland("missing"); got != nil {
		t.Fatalf("GetIsland(missing) = %#v, want nil", got)
	}
	if migrated := im.Migrate(); migrated != 0 {
		t.Fatalf("single island migration moved %d individuals, want 0", migrated)
	}
}

func TestIslandModel_MigrateReplacesWorstWithClonedElite(t *testing.T) {
	im := NewIslandModel(10, 0.5)
	goPop := islandTestPopulation("go-elite", "go-mid", "go-low", "go-min")
	opsPop := islandTestPopulation("ops-elite", "ops-mid", "ops-low", "ops-min")
	for i := range goPop.Individuals {
		goPop.Individuals[i].Fitness = float64(100 - i*10)
	}
	for i := range opsPop.Individuals {
		opsPop.Individuals[i].Fitness = float64(40 - i*10)
	}
	im.AddIsland("go", goPop)
	im.AddIsland("ops", opsPop)

	migrated := im.Migrate()
	if migrated == 0 {
		t.Fatal("expected at least one migrated individual")
	}

	seenHighFitness := false
	seenClonedEliteInOtherIsland := false
	for domain, pop := range im.Islands {
		for _, ind := range pop.Individuals {
			if ind.Fitness >= 90 {
				seenHighFitness = true
			}
			if domain != "go" && ind.Tree != nil && ind.Tree.Name == "go-elite" {
				seenClonedEliteInOtherIsland = true
				if ind.Tree == goPop.Individuals[0].Tree {
					t.Fatal("migrated elite should be cloned, not pointer-aliased")
				}
			}
			if ind.Genome == "" {
				t.Fatal("migrated individuals must retain non-empty genomes")
			}
		}
	}
	if !seenHighFitness {
		t.Fatal("migration did not preserve/copy an elite high-fitness individual")
	}
	if !seenClonedEliteInOtherIsland {
		t.Fatal("expected go elite to migrate into another island")
	}
}

func TestIslandModel_DiversityStatsAndSummary(t *testing.T) {
	im := NewIslandModel(2, 0.5)
	shared := islandTestTree("shared")
	unique := islandTestTree("unique")
	im.AddIsland("alpha", &Population{
		Individuals: []Individual{
			{Tree: shared, Genome: hashTree(shared), Fitness: 7},
			{Tree: unique, Genome: hashTree(unique), Fitness: 9},
		},
		BestFitness: 9,
	})
	im.AddIsland("beta", &Population{
		Individuals: []Individual{
			{Tree: shared, Genome: hashTree(shared), Fitness: 5},
		},
		BestFitness: 5,
	})

	diversity := im.DiversityAcrossIslands()
	if diversity <= 0 || diversity >= 1 {
		t.Fatalf("expected partial cross-island diversity, got %.3f", diversity)
	}
	stats := im.Stats()
	if stats.Domains != 2 || stats.TotalPop != 3 {
		t.Fatalf("stats = domains %d total %d, want 2/3", stats.Domains, stats.TotalPop)
	}
	if stats.BestPerDomain["alpha"] != 9 || stats.BestPerDomain["beta"] != 5 {
		t.Fatalf("unexpected best-per-domain stats: %#v", stats.BestPerDomain)
	}
	if stats.CrossDiversity != diversity {
		t.Fatalf("Stats diversity %.3f does not match DiversityAcrossIslands %.3f", stats.CrossDiversity, diversity)
	}

	summary := im.Summary()
	for _, want := range []string{"IslandModel: 2 domains, 3 total pop", "alpha: best=9.0", "beta: best=5.0", "cross-diversity:"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary %q missing %q", summary, want)
		}
	}
}

func TestIslandModel_EvolveAllEvaluatesPopulations(t *testing.T) {
	im := NewIslandModel(10, 0.5)
	im.AddIsland("go", islandTestPopulation("short", "much-longer-tree-name"))
	im.AddIsland("ops", islandTestPopulation("ops"))

	best := im.EvolveAll(func(tree *SerializableNode) float64 {
		return float64(len(tree.Name))
	})
	if im.Generation != 1 {
		t.Fatalf("generation = %d, want 1", im.Generation)
	}
	if len(best) != 2 || best["go"] == nil || best["ops"] == nil {
		t.Fatalf("unexpected best tree map: %#v", best)
	}
	if best["go"].Name != "much-longer-tree-name" {
		t.Fatalf("go best = %q, want longest-name tree", best["go"].Name)
	}
	if im.GetIsland("go").BestFitness != float64(len("much-longer-tree-name")) {
		t.Fatalf("population best fitness was not updated")
	}
}

func TestIslandModelTracksMigrations(t *testing.T) {
	im := NewIslandModel(1, 0.5)
	im.AddIsland("go", islandTestPopulation("go-short", "go-much-longer-name"))
	im.AddIsland("ops", islandTestPopulation("ops-short", "ops-much-longer-name"))

	const evolveCalls = 3
	for i := 0; i < evolveCalls; i++ {
		im.EvolveAll(func(tree *SerializableNode) float64 {
			return float64(len(tree.Name))
		})
	}

	// EvolveAll must advance Generation exactly once per call, even when
	// migration fires (MigrationInterval=1 → every call migrates).
	if im.Generation != evolveCalls {
		t.Fatalf("generation = %d after %d EvolveAll calls, want %d", im.Generation, evolveCalls, evolveCalls)
	}

	stats := im.Stats()
	if stats.Migrations <= 0 {
		t.Fatalf("Stats().Migrations = %d after %d migrating generations, want > 0", stats.Migrations, evolveCalls)
	}
	if stats.Migrations != im.TotalMigrations {
		t.Fatalf("Stats().Migrations = %d, want TotalMigrations %d", stats.Migrations, im.TotalMigrations)
	}

	// A direct Migrate() call must accumulate exactly its returned count.
	before := stats.Migrations
	moved := im.Migrate()
	if moved <= 0 {
		t.Fatal("expected direct Migrate() to move at least one individual")
	}
	if got := im.Stats().Migrations; got != before+moved {
		t.Fatalf("Stats().Migrations = %d after Migrate() moved %d, want %d", got, moved, before+moved)
	}

	if summary := im.Summary(); !strings.Contains(summary, "migrations") {
		t.Fatalf("summary %q missing migrations count", summary)
	}
}

func TestIslandStatsJSONIncludesMigrations(t *testing.T) {
	data, err := json.Marshal(IslandStats{})
	if err != nil {
		t.Fatalf("marshal IslandStats: %v", err)
	}
	if !strings.Contains(string(data), `"migrations"`) {
		t.Fatalf("IslandStats JSON %s missing \"migrations\" key", data)
	}
}

// TestIslandModel_SaveLoadMergesPerDomainSubpopulations pins the durable
// island archive (milestone 3/5 of the durable quality-diversity program):
// Save must persist the model to disk (creating missing parent directories),
// and Load must warm-start a model from that archive by merging per-domain
// subpopulations — disk-only domains are adopted wholesale, memory-only
// domains survive untouched, and an overlapping domain unions its individuals
// deduped by genome with the fitter copy winning. Progress counters
// (Generation, TotalMigrations) resume from the persisted high-water mark, a
// missing archive is a silent cold start rather than an error, and a corrupt
// archive surfaces an error instead of silently wiping accumulated state.
func TestIslandModel_SaveLoadMergesPerDomainSubpopulations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "island_archive.json")

	// Cold start: a missing archive leaves the model unchanged and is not an error.
	cold := NewIslandModel(3, 0.25)
	if err := cold.Load(path); err != nil {
		t.Fatalf("Load(missing archive) = %v, want nil cold start", err)
	}
	if len(cold.Islands) != 0 {
		t.Fatalf("cold-start Load must leave the model empty; got %d islands", len(cold.Islands))
	}

	// Persist a model holding two domains and non-zero progress counters.
	saved := NewIslandModel(3, 0.25)
	goPop := islandTestPopulation("go-a", "go-b")
	goPop.Individuals[0].Fitness = 80 // go-a: only on disk
	goPop.Individuals[1].Fitness = 10 // go-b: disk copy is weaker than memory's
	opsPop := islandTestPopulation("ops-a")
	opsPop.Individuals[0].Fitness = 55
	saved.AddIsland("go", goPop)
	saved.AddIsland("ops", opsPop)
	saved.Generation = 7
	saved.TotalMigrations = 3
	if err := saved.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Warm start into a model that already holds an overlapping domain ("go",
	// sharing the go-b genome with the archive) and a disjoint one ("fin").
	loaded := NewIslandModel(3, 0.25)
	memGo := islandTestPopulation("go-b", "go-mem")
	memGo.Individuals[0].Fitness = 90 // fitter in-memory copy of the shared go-b genome
	memGo.Individuals[1].Fitness = 20
	loaded.AddIsland("go", memGo)
	loaded.AddIsland("fin", islandTestPopulation("fin-a"))
	if err := loaded.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(loaded.Islands) != 3 {
		domains := make([]string, 0, len(loaded.Islands))
		for d := range loaded.Islands {
			domains = append(domains, d)
		}
		t.Fatalf("merged model has %d islands %v, want 3 (go, ops, fin)", len(domains), domains)
	}
	if ops := loaded.GetIsland("ops"); ops == nil || len(ops.Individuals) != 1 || ops.Individuals[0].Fitness != 55 {
		t.Fatalf("disk-only ops island must be adopted wholesale; got %#v", ops)
	}
	if fin := loaded.GetIsland("fin"); fin == nil || len(fin.Individuals) != 1 {
		t.Fatalf("memory-only fin island must survive Load; got %#v", fin)
	}

	merged := loaded.GetIsland("go")
	if merged == nil {
		t.Fatal("Load lost the overlapping go island")
	}
	byName := make(map[string]float64, len(merged.Individuals))
	for _, ind := range merged.Individuals {
		if ind.Tree == nil || ind.Genome == "" {
			t.Fatalf("merged individual lost its tree or genome: %#v", ind)
		}
		if _, dup := byName[ind.Tree.Name]; dup {
			t.Fatalf("individual %q duplicated after merge; want the union deduped by genome", ind.Tree.Name)
		}
		byName[ind.Tree.Name] = ind.Fitness
	}
	want := map[string]float64{"go-a": 80, "go-b": 90, "go-mem": 20}
	if len(byName) != len(want) {
		t.Fatalf("merged go island = %v, want exactly %v", byName, want)
	}
	for name, fitness := range want {
		if got, present := byName[name]; !present || got != fitness {
			t.Errorf("merged go island[%q] = %v (present=%v), want fitness %v", name, got, present, fitness)
		}
	}

	if loaded.Generation != 7 {
		t.Errorf("Generation = %d after Load, want persisted high-water mark 7", loaded.Generation)
	}
	if loaded.TotalMigrations != 3 {
		t.Errorf("TotalMigrations = %d after Load, want persisted high-water mark 3", loaded.TotalMigrations)
	}

	// A corrupt archive must fail loudly, not zero out accumulated state.
	corrupt := filepath.Join(t.TempDir(), "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt archive: %v", err)
	}
	if err := NewIslandModel(3, 0.25).Load(corrupt); err == nil {
		t.Error("Load(corrupt archive) = nil, want error")
	}
}

// TestIslandModel_LoadEnforcesCapWithLowestFitnessEviction pins milestone
// 1/4 of the durable island-archive-bound program: a non-zero Cap must bound
// every warm-started island to at most Cap individuals, evicting the
// lowest-fitness survivors first when the disk/memory merge would otherwise
// overflow it.
func TestIslandModel_LoadEnforcesCapWithLowestFitnessEviction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capped_merge_archive.json")

	saved := NewIslandModel(3, 0.25)
	goPop := islandTestPopulation("go-a", "go-b", "go-c")
	goPop.Individuals[0].Fitness = 10
	goPop.Individuals[1].Fitness = 90
	goPop.Individuals[2].Fitness = 50
	saved.AddIsland("go", goPop)
	if err := saved.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded := NewIslandModel(3, 0.25)
	loaded.Cap = 2
	memGo := islandTestPopulation("go-mem")
	memGo.Individuals[0].Fitness = 5
	loaded.AddIsland("go", memGo)
	if err := loaded.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	merged := loaded.GetIsland("go")
	if merged == nil {
		t.Fatal("Load lost the go island")
	}
	if len(merged.Individuals) != 2 {
		t.Fatalf("merged island has %d individuals, want Cap=2", len(merged.Individuals))
	}
	byName := make(map[string]bool, len(merged.Individuals))
	for _, ind := range merged.Individuals {
		byName[ind.Tree.Name] = true
	}
	if !byName["go-b"] || !byName["go-c"] {
		t.Fatalf("expected the two highest-fitness survivors go-b(90)/go-c(50); got %v", byName)
	}
	if byName["go-mem"] || byName["go-a"] {
		t.Fatalf("expected lowest-fitness individuals go-mem(5)/go-a(10) evicted; got %v", byName)
	}
}

// TestIslandModel_LoadCapsDiskOnlyIslandWholesaleAdoption pins that a
// disk-only domain (adopted wholesale, never touching mergeIslandPopulation)
// is still bounded by a non-zero Cap.
func TestIslandModel_LoadCapsDiskOnlyIslandWholesaleAdoption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capped_wholesale_archive.json")

	saved := NewIslandModel(3, 0.25)
	opsPop := islandTestPopulation("ops-a", "ops-b", "ops-c")
	opsPop.Individuals[0].Fitness = 1
	opsPop.Individuals[1].Fitness = 100
	opsPop.Individuals[2].Fitness = 3
	saved.AddIsland("ops", opsPop)
	if err := saved.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded := NewIslandModel(3, 0.25)
	loaded.Cap = 1
	if err := loaded.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ops := loaded.GetIsland("ops")
	if ops == nil || len(ops.Individuals) != 1 {
		t.Fatalf("disk-only island not capped to 1: %#v", ops)
	}
	if ops.Individuals[0].Tree.Name != "ops-b" {
		t.Fatalf("expected highest-fitness survivor ops-b(100), got %q", ops.Individuals[0].Tree.Name)
	}
}

// TestIslandModel_LoadCapZeroPreservesUnboundedBehavior pins that the
// default zero-value Cap keeps Load's merge unbounded — the pre-existing
// behavior TestIslandModel_SaveLoadMergesPerDomainSubpopulations also
// exercises, restated here explicitly against the new Cap field.
func TestIslandModel_LoadCapZeroPreservesUnboundedBehavior(t *testing.T) {
	path := filepath.Join(t.TempDir(), "uncapped_archive.json")

	saved := NewIslandModel(3, 0.25)
	saved.AddIsland("go", islandTestPopulation("go-a", "go-b", "go-c"))
	if err := saved.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded := NewIslandModel(3, 0.25)
	if loaded.Cap != 0 {
		t.Fatalf("Cap zero value = %d, want 0", loaded.Cap)
	}
	loaded.AddIsland("go", islandTestPopulation("go-mem"))
	if err := loaded.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(loaded.GetIsland("go").Individuals); got != 4 {
		t.Fatalf("Cap=0 merged island has %d individuals, want unbounded 4", got)
	}
}

// TestIslandModel_LoadEnforcesIslandCapByEvictingLowestBestFitnessAdoptedIslands
// pins milestone 2/4 of the durable island-archive-bound program: a non-zero
// IslandCap bounds the number of distinct island keys retained across merges.
// When Load adopts archived domains the current run didn't seed, whole
// islands beyond the cap are evicted by lowest BestFitness — the current
// run's own seeded islands are never evicted, only islands newly adopted
// from disk are eviction candidates.
func TestIslandModel_LoadEnforcesIslandCapByEvictingLowestBestFitnessAdoptedIslands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "island_cap_archive.json")

	saved := NewIslandModel(3, 0.25)
	goPop := islandTestPopulation("go-a")
	goPop.BestFitness = 20
	opsPop := islandTestPopulation("ops-a")
	opsPop.BestFitness = 90
	finPop := islandTestPopulation("fin-a")
	finPop.BestFitness = 10
	saved.AddIsland("go", goPop)
	saved.AddIsland("ops", opsPop)
	saved.AddIsland("fin", finPop)
	if err := saved.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded := NewIslandModel(3, 0.25)
	loaded.IslandCap = 2
	seededGo := islandTestPopulation("go-mem")
	seededGo.BestFitness = 1 // lowest BestFitness, but seeded so must never be evicted
	loaded.AddIsland("go", seededGo)
	if err := loaded.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := len(loaded.Islands); got != 2 {
		domains := make([]string, 0, len(loaded.Islands))
		for d := range loaded.Islands {
			domains = append(domains, d)
		}
		t.Fatalf("island count = %d %v, want IslandCap=2", got, domains)
	}
	if loaded.GetIsland("go") == nil {
		t.Fatal("seeded go island must never be evicted regardless of BestFitness")
	}
	if loaded.GetIsland("fin") != nil {
		t.Fatalf("expected lowest-BestFitness adopted island fin(10) evicted, got %#v", loaded.GetIsland("fin"))
	}
	if loaded.GetIsland("ops") == nil {
		t.Fatal("expected higher-BestFitness adopted island ops(90) retained")
	}
}

// TestIslandModel_LoadIslandCapAppliesWhenNoIslandsSeeded pins that IslandCap
// bounds a cold-start Load too: when the current run seeded nothing, all
// disk domains are adopted candidates and the cap keeps only the
// highest-BestFitness ones.
func TestIslandModel_LoadIslandCapAppliesWhenNoIslandsSeeded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "island_cap_no_seed_archive.json")

	saved := NewIslandModel(3, 0.25)
	for i, domain := range []string{"go", "ops", "fin"} {
		pop := islandTestPopulation(domain + "-a")
		pop.BestFitness = float64((i + 1) * 10) // go=10, ops=20, fin=30
		saved.AddIsland(domain, pop)
	}
	if err := saved.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded := NewIslandModel(3, 0.25)
	loaded.IslandCap = 2
	if err := loaded.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := len(loaded.Islands); got != 2 {
		t.Fatalf("island count = %d, want IslandCap=2", got)
	}
	if loaded.GetIsland("go") != nil {
		t.Fatal("expected lowest-BestFitness island go(10) evicted")
	}
	if loaded.GetIsland("ops") == nil || loaded.GetIsland("fin") == nil {
		t.Fatal("expected higher-BestFitness islands ops(20)/fin(30) retained")
	}
}

// TestIslandModel_LoadIslandCapZeroPreservesUnboundedBehavior pins that the
// default zero-value IslandCap keeps Load's island-key growth unbounded,
// mirroring the existing per-individual Cap zero-value guarantee.
func TestIslandModel_LoadIslandCapZeroPreservesUnboundedBehavior(t *testing.T) {
	path := filepath.Join(t.TempDir(), "island_cap_zero_archive.json")

	saved := NewIslandModel(3, 0.25)
	saved.AddIsland("go", islandTestPopulation("go-a"))
	saved.AddIsland("ops", islandTestPopulation("ops-a"))
	saved.AddIsland("fin", islandTestPopulation("fin-a"))
	if err := saved.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded := NewIslandModel(3, 0.25)
	if loaded.IslandCap != 0 {
		t.Fatalf("IslandCap zero value = %d, want 0", loaded.IslandCap)
	}
	if err := loaded.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(loaded.Islands); got != 3 {
		t.Fatalf("IslandCap=0 merged model has %d islands, want unbounded 3", got)
	}
}

func TestIslandModel_DiversityEdgeCases(t *testing.T) {
	if got := NewIslandModel(1, 0.5).DiversityAcrossIslands(); got != 0 {
		t.Fatalf("empty model diversity = %.3f, want 0", got)
	}

	im := NewIslandModel(1, 0.5)
	im.AddIsland("empty-a", &Population{})
	im.AddIsland("empty-b", &Population{})
	if got := im.DiversityAcrossIslands(); got != 0 {
		t.Fatalf("empty-island diversity = %.3f, want 0", got)
	}
}
