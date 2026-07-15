package domains

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/knowledge"
)

// =============================================================================
// NotebookLMFitness wiring into the knowledge graph
//
// NotebookLMFitness computes a 0.0-1.0 fitness score with an anti-fabrication
// term, but nothing in the repo ever calls it — the gardener's per-tree
// fitness update for the "notebooklm"/"notebooklm_consumer" trees only ever
// sees the knowledge graph's generic runtime-success EMA. RegisterNotebookLMFitness
// must wire NotebookLMFitness into the knowledge graph's RegisterDomainFitness
// hook for both tree IDs, so real run history for those trees drives Fitness
// through the domain-aware, anti-fabrication-penalizing score instead.
// =============================================================================

func TestRegisterNotebookLMFitness_WiresIntoKnowledgeGraph(t *testing.T) {
	for _, treeID := range []string{"notebooklm", "notebooklm_consumer"} {
		t.Run(treeID, func(t *testing.T) {
			kg := knowledge.NewKnowledgeGraph()
			kg.Register(&knowledge.TreeMeta{ID: treeID, Name: treeID, Category: "domain"})

			RegisterNotebookLMFitness(kg)

			kg.RecordRun(knowledge.RunRecord{TreeID: treeID, Outcome: "chain_success", Quality: 0.9})
			kg.RecordRun(knowledge.RunRecord{TreeID: treeID, Outcome: "failure", Quality: 0.1})

			want := NotebookLMFitness([]NotebookLMRunSummary{
				{Outcome: "chain_success", Quality: 0.9},
				{Outcome: "failure", Quality: 0.1},
			}) * 100

			got := kg.Trees[treeID].Fitness
			if got != want {
				t.Errorf("expected Fitness=%.4f (NotebookLMFitness output *100), got %.4f — RecordRun is not using the wired domain fitness function for %q", want, got, treeID)
			}
		})
	}
}

// A tree ID other than the two NotebookLM trees must be unaffected by
// RegisterNotebookLMFitness — the wiring must be scoped to those tree IDs only.
func TestRegisterNotebookLMFitness_DoesNotAffectOtherTrees(t *testing.T) {
	kg := knowledge.NewKnowledgeGraph()
	kg.Register(&knowledge.TreeMeta{ID: "domain:unrelated", Name: "Unrelated", Category: "domain", Fitness: 50.0})

	RegisterNotebookLMFitness(kg)

	kg.RecordRun(knowledge.RunRecord{TreeID: "domain:unrelated", Outcome: "success"})

	// Generic EMA: 0.9*50 + 0.1*100 = 55.0 — unchanged by the NotebookLM wiring.
	if got := kg.Trees["domain:unrelated"].Fitness; got != 55.0 {
		t.Errorf("expected unrelated tree to keep the generic EMA (55.0), got %.4f", got)
	}
}
