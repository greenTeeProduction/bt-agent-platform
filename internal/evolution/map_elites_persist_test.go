package evolution

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// Milestone 5/5 of the durable MAP-Elites archive: the merged archive must be
// bounded by a configurable cap (MAPElitesGrid.Cap, zero = unbounded) with
// lowest-fitness niche eviction applied in both Save and Load, so an archive
// that accumulates niches across domains and runs can never grow without
// bound. The persistence API mirrors IslandModel.Save/Load in island.go:
// atomic write (tmp + rename) under the shared advisory flock (ADR-024),
// merge-on-load with the fitter copy winning an overlapping niche key, and a
// missing archive treated as a silent cold start.

// newPersistElite builds an individual carrying a small named tree so cells
// stay distinguishable across a save/load round trip.
func newPersistElite(name string, fitness float64) *Individual {
	tree := makeTestTree(name, 1, 2)
	return &Individual{Tree: tree, Fitness: fitness, Genome: hashTree(tree)}
}

// insertPersistNiche seeds one distinct behavioral niche (node-count bucket
// idx*10 under the default NodeBucket of 10) in the given domain.
func insertPersistNiche(t *testing.T, g *MAPElitesGrid, domain string, idx int, fitness float64) {
	t.Helper()
	desc := BehavioralDescriptor{NodeCount: idx * 10, MaxDepth: 1, Domain: domain}
	if !g.Insert(desc, newPersistElite(fmt.Sprintf("%s-%d", domain, idx), fitness)) {
		t.Fatalf("insert %s niche %d should win its empty cell", domain, idx)
	}
}

// cellFitnesses returns the sorted fitness of every occupied cell.
func cellFitnesses(g *MAPElitesGrid) []float64 {
	out := make([]float64, 0, len(g.Cells))
	for _, ind := range g.Cells {
		out = append(out, ind.Fitness)
	}
	sort.Float64s(out)
	return out
}

// TestMAPElitesGridSaveLoad_CrossDomainMergeNeverExceedsCap is the milestone's
// core invariant: run 1 persists godev niches, run 2 (a different domain)
// warm-starts from the same archive, and the merged grid — 6 niches against a
// cap of 4 — holds exactly Cap cells with the weakest niches evicted first,
// regardless of which domain they came from. The re-persisted archive itself
// must also never exceed the cap.
func TestMAPElitesGridSaveLoad_CrossDomainMergeNeverExceedsCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "map_elites.json")

	run1 := NewMAPElitesGrid(5)
	run1.Cap = 4
	insertPersistNiche(t, run1, "godev", 0, 1.0)
	insertPersistNiche(t, run1, "godev", 1, 2.0)
	insertPersistNiche(t, run1, "godev", 2, 3.0)
	if err := run1.Save(path); err != nil {
		t.Fatalf("run1 Save: %v", err)
	}

	run2 := NewMAPElitesGrid(5)
	run2.Cap = 4
	insertPersistNiche(t, run2, "finance", 0, 6.0)
	insertPersistNiche(t, run2, "finance", 1, 5.0)
	insertPersistNiche(t, run2, "finance", 2, 4.0)
	if err := run2.Load(path); err != nil {
		t.Fatalf("run2 Load: %v", err)
	}

	if got := run2.CellCount(); got != 4 {
		t.Fatalf("merged cell count = %d, want cap 4", got)
	}
	// The two weakest niches (godev 1.0 and 2.0) must be the ones evicted;
	// the surviving godev 3.0 cell proves eviction is per-niche
	// weakest-first, not per-domain.
	want := []float64{3.0, 4.0, 5.0, 6.0}
	if got := cellFitnesses(run2); !reflect.DeepEqual(got, want) {
		t.Fatalf("surviving cell fitnesses = %v, want %v", got, want)
	}

	// The durable archive is bounded too: re-persist and reload into an
	// unbounded grid — only the capped cells may be on disk.
	if err := run2.Save(path); err != nil {
		t.Fatalf("run2 Save: %v", err)
	}
	run3 := NewMAPElitesGrid(5)
	if err := run3.Load(path); err != nil {
		t.Fatalf("run3 Load: %v", err)
	}
	if got := cellFitnesses(run3); !reflect.DeepEqual(got, want) {
		t.Fatalf("persisted archive cell fitnesses = %v, want %v", got, want)
	}
}

// TestMAPElitesGridSave_EvictsWeakestNichesFirst pins the eviction order on
// the write path: a grid over its cap persists only the strongest niches,
// independent of insertion order.
func TestMAPElitesGridSave_EvictsWeakestNichesFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "map_elites.json")

	g := NewMAPElitesGrid(5)
	g.Cap = 2
	insertPersistNiche(t, g, "godev", 0, 10.0)
	insertPersistNiche(t, g, "godev", 1, 40.0)
	insertPersistNiche(t, g, "godev", 2, 20.0)
	insertPersistNiche(t, g, "godev", 3, 30.0)
	if err := g.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reload := NewMAPElitesGrid(5) // Cap zero: reads the archive verbatim
	if err := reload.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []float64{30.0, 40.0}
	if got := cellFitnesses(reload); !reflect.DeepEqual(got, want) {
		t.Fatalf("persisted cell fitnesses = %v, want strongest two %v", got, want)
	}
}

// TestMAPElitesGridSaveLoad_ZeroCapKeepsAllCells guards backward
// compatibility: existing callers construct grids without a cap, and a
// zero-value Cap must mean unbounded — no eviction on either path.
func TestMAPElitesGridSaveLoad_ZeroCapKeepsAllCells(t *testing.T) {
	path := filepath.Join(t.TempDir(), "map_elites.json")

	g := NewMAPElitesGrid(5)
	insertPersistNiche(t, g, "godev", 0, 1.0)
	insertPersistNiche(t, g, "godev", 1, 2.0)
	insertPersistNiche(t, g, "godev", 2, 3.0)
	if err := g.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reload := NewMAPElitesGrid(5)
	if err := reload.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := reload.CellCount(); got != 3 {
		t.Fatalf("uncapped reload cell count = %d, want 3", got)
	}
	best := reload.BestIndividual()
	if best == nil || best.Fitness != 3.0 {
		t.Fatalf("uncapped reload best = %+v, want fitness 3.0", best)
	}
}

// TestMAPElitesGridLoad_MissingFileColdStart pins the cold-start contract the
// cross-run merge flow relies on: loading a path whose parent directory does
// not even exist yet is a silent no-op, not an error (and must not try to
// create the flock sidecar there).
func TestMAPElitesGridLoad_MissingFileColdStart(t *testing.T) {
	g := NewMAPElitesGrid(5)
	if err := g.Load(filepath.Join(t.TempDir(), "absent", "map_elites.json")); err != nil {
		t.Fatalf("cold-start Load: %v", err)
	}
	if got := g.CellCount(); got != 0 {
		t.Fatalf("cold-start cell count = %d, want 0", got)
	}
}
