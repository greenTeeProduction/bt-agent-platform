package evolution

import (
	"fmt"
	"testing"
)

// Regression tests for degenerate populations (Q3 Reliability): Evolve,
// EvolveWithExperience, and MemeticEvolve all compute
// eliteCount := max(2, len/10) and then copy(newPop[:eliteCount], ...),
// which panics with an out-of-range slice for populations of size 0 or 1
// (first surfaced by an MCP bt_evolve_genetic call with {"population":1}).
// Required behavior: no panic — return nil for an empty population and the
// sole individual's tree for a single-individual population.

// degeneratePopulation builds a population of exactly size individuals.
// NewPopulation cannot be used here: it writes Individuals[0]
// unconditionally, so it cannot construct a size-0 population.
func degeneratePopulation(size int) *Population {
	pop := &Population{Individuals: make([]Individual, size)}
	base := DefaultTree()
	for i := 0; i < size; i++ {
		pop.Individuals[i] = Individual{Tree: cloneTree(base), Genome: hashTree(base)}
	}
	return pop
}

func nodeCountFitness(tree *SerializableNode) float64 {
	return float64(CountNodes(tree))
}

// runWithoutPanic invokes fn and fails the test if it panics instead of
// degrading gracefully.
func runWithoutPanic(t *testing.T, name string, fn func() *SerializableNode) (result *SerializableNode) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s panicked on degenerate population: %v", name, r)
		}
	}()
	return fn()
}

var degenerateSizeCases = []struct {
	size    int
	wantNil bool
}{
	{size: 0, wantNil: true},
	{size: 1, wantNil: false},
	{size: 2, wantNil: false},
}

func TestEvolve_DegeneratePopulations(t *testing.T) {
	for _, tc := range degenerateSizeCases {
		t.Run(fmt.Sprintf("size_%d", tc.size), func(t *testing.T) {
			pop := degeneratePopulation(tc.size)
			got := runWithoutPanic(t, "Evolve", func() *SerializableNode {
				return pop.Evolve(2, nodeCountFitness)
			})
			if tc.wantNil && got != nil {
				t.Fatalf("Evolve on empty population: want nil, got %+v", got)
			}
			if !tc.wantNil && got == nil {
				t.Fatalf("Evolve on population of size %d returned nil, want a best tree", tc.size)
			}
		})
	}
}

func TestEvolveWithExperience_DegeneratePopulations(t *testing.T) {
	for _, tc := range degenerateSizeCases {
		t.Run(fmt.Sprintf("size_%d", tc.size), func(t *testing.T) {
			eb, err := NewExperienceBank(t.TempDir())
			if err != nil {
				t.Fatalf("NewExperienceBank: %v", err)
			}
			pop := degeneratePopulation(tc.size)
			got := runWithoutPanic(t, "EvolveWithExperience", func() *SerializableNode {
				return pop.EvolveWithExperience(2, nodeCountFitness, eb)
			})
			if tc.wantNil && got != nil {
				t.Fatalf("EvolveWithExperience on empty population: want nil, got %+v", got)
			}
			if !tc.wantNil && got == nil {
				t.Fatalf("EvolveWithExperience on population of size %d returned nil, want a best tree", tc.size)
			}
		})
	}
}

func TestMemeticEvolve_DegeneratePopulations(t *testing.T) {
	for _, tc := range degenerateSizeCases {
		t.Run(fmt.Sprintf("size_%d", tc.size), func(t *testing.T) {
			pop := degeneratePopulation(tc.size)
			searcher := NewLocalSearcher(HillClimbSearch)
			got := runWithoutPanic(t, "MemeticEvolve", func() *SerializableNode {
				return pop.MemeticEvolve(2, nodeCountFitness, searcher, 1)
			})
			if tc.wantNil && got != nil {
				t.Fatalf("MemeticEvolve on empty population: want nil, got %+v", got)
			}
			if !tc.wantNil && got == nil {
				t.Fatalf("MemeticEvolve on population of size %d returned nil, want a best tree", tc.size)
			}
		})
	}
}
