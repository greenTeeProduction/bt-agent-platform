package evolution

import (
	"math/rand"
	"strings"
	"testing"
)

// drawMutations records the mutation stream produced by n successive
// randomMutation calls as "operation|target" keys, so two runs can be compared
// element-by-element.
func drawMutations(t *testing.T, n int) []string {
	t.Helper()
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		ops := randomMutation(DefaultTree())
		if len(ops) == 0 {
			t.Fatalf("randomMutation returned no ops on draw %d", i)
		}
		out = append(out, ops[0].Operation+"|"+ops[0].Target)
	}
	return out
}

// distinctOperations counts how many unique Operation values a mutation stream
// contains. A non-deterministic source must produce more than one.
func distinctOperations(draws []string) int {
	seen := map[string]struct{}{}
	for _, d := range draws {
		op, _, _ := strings.Cut(d, "|")
		seen[op] = struct{}{}
	}
	return len(seen)
}

// observeSeeds is the seed budget the ExpertKnowledge "Observe" tests retry
// across. Those tests need a run that happens to draw at least one improving
// mutation; each seed is now genuinely reproducible (see withEvolutionSeed), so
// the list is a fixed, replayable sample rather than a re-roll of the same
// unseeded dice. 16 seeds rather than the historical 3: the per-seed miss rate
// was measured at a few percent, so 16 independent samples drive the
// all-seeds-missed branch far below any rate CI could notice.
var observeSeeds = []int64{
	42, 43, 44, 45, 46, 47, 48, 49,
	50, 51, 52, 53, 54, 55, 56, 57,
}

// withEvolutionSeed runs fn with a fresh source seeded at seed installed as the
// package's evolution randomness, restoring the previous source before it
// returns. It replaces the `rand.Seed(seed)` calls these tests used to make:
// under Go 1.26 the top-level rand.Seed is a no-op, so those calls bought no
// reproducibility at all. Taking fn as a func makes the restore fire per
// iteration when called from inside a seed loop.
func withEvolutionSeed(seed int64, fn func()) {
	restore := SetEvolutionRand(rand.New(rand.NewSource(seed)))
	defer restore()
	fn()
}

// isolateBlockMutator neutralizes the blocks-package random mutator hook so
// these tests exercise the shared mutation primitives rather than a registered
// block mutator.
func isolateBlockMutator(t *testing.T) {
	t.Helper()
	prev := blockRandomMutatorFn
	blockRandomMutatorFn = nil
	t.Cleanup(func() { blockRandomMutatorFn = prev })
}

func TestEvolutionRand_SeededIsReproducible(t *testing.T) {
	isolateBlockMutator(t)

	restore := SetEvolutionRand(rand.New(rand.NewSource(42)))
	first := drawMutations(t, 200)
	restore()

	restore = SetEvolutionRand(rand.New(rand.NewSource(42)))
	second := drawMutations(t, 200)
	restore()

	if len(first) != len(second) {
		t.Fatalf("draw count mismatch: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("mutation stream diverged at draw %d: %q vs %q", i, first[i], second[i])
		}
	}
}

func TestEvolutionRand_DefaultIsNonDeterministic(t *testing.T) {
	isolateBlockMutator(t)

	draws := drawMutations(t, 200)
	if got := distinctOperations(draws); got < 2 {
		t.Fatalf("default source produced %d distinct operations across 200 draws, want >= 2 "+
			"(production randomness must not be frozen)", got)
	}
}

func TestEvolutionRand_RestoreIsIdempotent(t *testing.T) {
	isolateBlockMutator(t)

	restore := SetEvolutionRand(rand.New(rand.NewSource(7)))
	restore()
	restore()

	draws := drawMutations(t, 200)
	if got := distinctOperations(draws); got < 2 {
		t.Fatalf("after a double restore the package produced %d distinct operations across 200 draws, "+
			"want >= 2 (restore must not leave a fixed source installed)", got)
	}
}
