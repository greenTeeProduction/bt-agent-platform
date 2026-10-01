package evolution

import (
	"math/rand"
	"slices"
	"testing"
)

func TestRegisteredBlocksDoNotStarveGovernanceMutations(t *testing.T) {
	previous := blockRandomMutatorFn
	t.Cleanup(func() { blockRandomMutatorFn = previous })
	RegisterBlockRandomMutator(func(*SerializableNode) []MutationOp {
		return []MutationOp{{Operation: "insert_block_before"}}
	})
	t.Cleanup(SetEvolutionRand(rand.New(rand.NewSource(42))))
	seen := map[string]bool{}
	for range 200 {
		seen[randomMutation(GoDeveloperTree())[0].Operation] = true
	}
	for _, operation := range []string{"insert_block_before", "wrap_quality_gate", "guard_task"} {
		if !seen[operation] {
			t.Errorf("registered block provider starved %s", operation)
		}
	}
}

func TestEvolutionArchivesRetainEvaluatedTreesAfterPopulationSort(t *testing.T) {
	base := &SerializableNode{Type: "ChainAction", Name: "llm_call:Explain the supplied task clearly"}
	guarded := cloneTree(base)
	ApplyMutations(guarded, []MutationOp{{Operation: "wrap_quality_gate", Target: base.Name}})
	fitness := func(tree *SerializableNode) float64 { return AssessGovernance(tree).Score }
	pop := &Population{Individuals: []Individual{{Tree: base, Genome: "base", Fitness: fitness(base)}, {Tree: guarded, Genome: "guarded", Fitness: fitness(guarded)}}}
	grid := NewMAPElitesGrid(2)
	grid.InsertFromPopulation(pop, "test")
	front := NewParetoFront(GovernanceDimensions())
	front.AddFromPopulation(pop, StructuralMultiFitness)
	pp := &ParetoPopulation{Population: pop, Front: NewParetoFront(GovernanceDimensions())}
	pp.Evaluate(StructuralMultiFitness)
	slices.Reverse(pop.Individuals)
	pop.Individuals[0] = Individual{Tree: &SerializableNode{Type: "AlwaysSucceed", Name: "replacement"}, Fitness: 999}
	for _, archived := range []*Individual{grid.BestIndividual(), front.Best(1)[0].Individual, pp.Front.Best(1)[0].Individual} {
		if archived.Genome != "guarded" || !PreservesGovernance(guarded, archived.Tree) {
			t.Errorf("archive was corrupted by population movement: %+v", archived)
		}
	}
}

func TestQTableExploresGovernanceMutations(t *testing.T) {
	t.Cleanup(SetEvolutionRand(rand.New(rand.NewSource(42))))
	q := NewQTable()
	seen := map[string]bool{}
	for range 200 {
		seen[q.SelectAction("task", 1)] = true
	}
	for _, operation := range []string{"wrap_quality_gate", "guard_task"} {
		if !seen[operation] {
			t.Errorf("Q-learning cannot explore %s", operation)
		}
	}
}
