package evaluator

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestDeepSearchReplaysCompleteWinnerAndCachedEvidence(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "Root", Children: []evolution.SerializableNode{{Type: "Sequence", Name: "PreGate"}, {Type: "ChainAction", Name: "agent:Research", Metadata: map[string]any{"max_iterations": float64(3)}}}}
	records := []evolution.Record{{Outcome: evolution.Failure, DurationMs: 1000}, {Outcome: evolution.Success, DurationMs: 1000}}
	tt, err := NewTranspositionTable(t.TempDir(), 100)
	if err != nil {
		t.Fatal(err)
	}
	assertWinner := func(result DeepeningResult) {
		t.Helper()
		if result.BestFitness == nil || len(result.BestMutations) != 2 {
			t.Fatalf("two-proposal winner not exercised: %+v", result)
		}
		encoded, _ := json.Marshal(tree)
		var candidate evolution.SerializableNode
		if err := json.Unmarshal(encoded, &candidate); err != nil {
			t.Fatal(err)
		}
		for _, proposal := range result.BestMutations {
			if proposal.Source != "deep_search" {
				t.Fatalf("incorrect winning attribution: %+v", proposal)
			}
			if evolution.ApplyMutations(&candidate, []evolution.MutationOp{proposal.Op}) == 0 {
				t.Fatal("winning proposal is not replayable")
			}
		}
		got := EvaluateTree(&candidate, records)
		if math.Abs(got.Composite-result.BestFitness.Composite) > 0.0001 {
			t.Fatalf("reported fitness %.5f differs from replay %.5f", result.BestFitness.Composite, got.Composite)
		}
	}
	first := IterativeDeepening(tree, records, tt, 2)
	assertWinner(first)
	second := IterativeDeepening(tree, records, tt, 2)
	assertWinner(second)
	if second.TTProbeHits == 0 {
		t.Fatal("cached winner not exercised")
	}
	// Identical tree and proposal menu with changed durations have different
	// scoring evidence. They must not reuse the previous pruning entries.
	changed := append([]evolution.Record(nil), records...)
	for i := range changed {
		changed[i].DurationMs = 5000
	}
	changedResult := IterativeDeepening(tree, changed, tt, 2)
	if changedResult.TTProbeHits != 0 {
		t.Fatal("search reused a different reflection history")
	}
	if changedResult.BestFitness == nil {
		t.Fatal("changed evidence lost the improving proposal")
	}
}
