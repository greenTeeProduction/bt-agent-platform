package evolution

import (
	"math/rand"
	"sync"
)

// Injectable randomness for the shared evolution primitives.
//
// Under Go 1.26 the top-level math/rand.Seed is a no-op, so seeding the global
// source no longer makes an evolution run reproducible. SetEvolutionRand is the
// only supported way to obtain a reproducible evolution run: it swaps the
// package-level source every draw on the breeding path reads from.
//
// Three families of draw go through the seam. CMA-ES samples its candidate
// population through sampleStdNormal, so a seeded source makes a parameter
// tuning run replay exactly. The mutation primitives —
// randomMutation, randomNodeName, materializeMutationOp and
// applyReorderChildren — decide WHAT a mutation does. The breeding loops decide
// WHETHER and TO WHOM it happens: the `< mutationRate` gate in each production
// Evolve variant (map_elites.go, island.go, pareto.go, multi_objective.go), the
// tournament draws in Population.Select and NSGAIIPopulation.TournamentSelect,
// the subtree pick in Crossover, and IslandModel.Migrate's target choice.
// Both families must stay on the seam for a run to replay: an injected source
// that only covers the first family reproduces the mutation menu but not the
// parents it is applied to, which is precisely the gap
// TestEvolveMAPElites_SameSeedSameArchive guards.
//
// Production callers never call SetEvolutionRand. With no source installed the
// helpers delegate to the top-level math/rand functions, so the global source —
// and therefore current production behavior — is unchanged.
var (
	evolutionRandMu sync.Mutex
	// evolutionRand is nil unless a test has installed a source.
	evolutionRand *rand.Rand
)

// SetEvolutionRand installs r as the package's mutation randomness source and
// returns a restore func that puts the previous source back. The restore func is
// idempotent — calling it more than once (for example from both an explicit call
// and a t.Cleanup) restores exactly once and never re-pins a later test to a
// stale source.
//
// Evolution loops mutate concurrently under selfHealGeneration callbacks, so the
// swap and every draw are serialized under a mutex to stay race-free.
func SetEvolutionRand(r *rand.Rand) (restore func()) {
	evolutionRandMu.Lock()
	prev := evolutionRand
	evolutionRand = r
	evolutionRandMu.Unlock()

	return sync.OnceFunc(func() {
		evolutionRandMu.Lock()
		evolutionRand = prev
		evolutionRandMu.Unlock()
	})
}

// evoIntn returns a random int in [0,n) from the injected source, or from the
// global math/rand source when none is installed.
func evoIntn(n int) int {
	evolutionRandMu.Lock()
	r := evolutionRand
	if r == nil {
		evolutionRandMu.Unlock()
		return rand.Intn(n) //#nosec G404 -- non-crypto PRNG for evolution heuristics
	}
	v := r.Intn(n)
	evolutionRandMu.Unlock()
	return v
}

// evoFloat64 returns a random float64 in [0.0,1.0) from the injected source, or
// from the global math/rand source when none is installed. The mutation-rate
// gates in every production Evolve variant draw through here so an injected
// source makes the whole mutation schedule reproducible.
func evoFloat64() float64 {
	evolutionRandMu.Lock()
	r := evolutionRand
	if r == nil {
		evolutionRandMu.Unlock()
		return rand.Float64() //#nosec G404 -- non-crypto PRNG for evolution heuristics
	}
	v := r.Float64()
	evolutionRandMu.Unlock()
	return v
}
