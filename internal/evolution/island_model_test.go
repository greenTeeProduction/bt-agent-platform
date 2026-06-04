package evolution

import (
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
