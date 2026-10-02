package evolution

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestMCTSCandidatesSeededConcurrentReplay(t *testing.T) {
	parent := DefaultTree()
	before, _ := json.Marshal(parent)
	m := NewMCTSMutator().WithConfig(64, 1.4, 3)
	m.WarmStartHints = []string{"add_before", "add_tool"}
	m.SetFitnessEvaluator(func(tree *SerializableNode) float64 { return float64(CountNodes(tree)) })
	const seed int64 = 42
	baseline := float64(CountNodes(parent))
	want := m.CandidatesWithSeed(parent, baseline, seed)
	if len(want) == 0 {
		t.Fatal("seed produced no proposals; replay assertion would be vacuous")
	}
	for _, proposal := range want {
		if proposal.Source != "mcts" || proposal.Search == nil || proposal.Search.Seed != seed || proposal.Search.Iterations != 64 {
			t.Fatalf("missing replay evidence: %+v", proposal)
		}
	}
	search := want[0].Search
	replayed := NewMCTSMutator().WithConfig(search.Iterations, search.ExplorationConst, search.MaxDepth)
	replayed.WarmStartHints = search.WarmStartHints
	replayed.SetFitnessEvaluator(func(tree *SerializableNode) float64 { return float64(CountNodes(tree)) })
	if got := replayed.CandidatesWithSeed(parent, baseline, search.Seed); !reflect.DeepEqual(got, want) {
		t.Fatal("recorded search configuration does not reconstruct proposals")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			got := m.CandidatesWithSeed(parent, baseline, seed)
			if !reflect.DeepEqual(got, want) {
				t.Error("concurrent seeded proposals differ")
			}
		})
	}
	wg.Wait()
	after, _ := json.Marshal(parent)
	if string(before) != string(after) {
		t.Fatal("proposal search mutated its input")
	}
}

func TestExperienceProposalSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	bank, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatal(err)
	}
	proposal := ScoredMutation{Op: MutationOp{Operation: "add_before", Target: "ExecutePlan", Node: &SerializableNode{Type: "Condition", Name: "HasClearTask"}}, Score: 0.8, Reason: "accepted search guard", Source: "mcts", Search: &MutationSearchEvidence{Seed: 0, Iterations: 12, ExplorationConst: 1.4, MaxDepth: 3}}
	if err := bank.AddFromProposal(DefaultTree(), proposal, 1, 2, nil, "missing task context"); err != nil {
		t.Fatal(err)
	}
	loaded, err := NewExperienceBank(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Count() != 1 || loaded.Entries[0].Proposal == nil {
		t.Fatal("accepted proposal was not persisted")
	}
	// JSON normalizes nested numeric payloads, so compare persisted representations.
	want, _ := json.Marshal(proposal)
	got, _ := json.Marshal(loaded.Entries[0].Proposal)
	if string(want) != string(got) {
		t.Fatalf("proposal changed on reload: %s", got)
	}
}

func TestRetrievedProposalCannotMutateExperienceBank(t *testing.T) {
	bank, err := NewExperienceBank(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	proposal := ScoredMutation{Op: MutationOp{Operation: "add_before", Target: "ExecutePlan", Node: &SerializableNode{Type: "Condition", Name: "HasClearTask", Metadata: map[string]any{"nested": map[string]any{"value": "original"}}}, Metadata: map[string]any{"nested": []any{map[string]any{"value": "original"}}}}, Score: 0.8, Source: "mcts", Search: &MutationSearchEvidence{Seed: 42, Iterations: 12}}
	if err := bank.AddFromProposal(DefaultTree(), proposal, 1, 2, nil); err != nil {
		t.Fatal(err)
	}
	// Caller-owned input, lookup results and domain-transfer results all need
	// independent nested payloads; only explicit bank writes may change them.
	proposal.Search.Seed = 99
	proposal.Op.Node.Name = "caller change"
	for _, lookup := range []func() []ExperienceEntry{
		func() []ExperienceEntry { return bank.Retrieve("ExecutePlan", 1) },
		func() []ExperienceEntry { return bank.RetrieveByTreeType("Default", 1) },
		func() []ExperienceEntry { return bank.TransferExperiences("Default", "GoDev") },
	} {
		found := lookup()
		if len(found) != 1 || found[0].Proposal == nil {
			t.Fatal("proposal missing")
		}
		found[0].Proposal.Source = "changed"
		found[0].Proposal.Search.Seed = 123
		found[0].Proposal.Op.Node.Metadata["nested"].(map[string]any)["value"] = "changed"
		found[0].Proposal.Op.Metadata["nested"].([]any)[0].(map[string]any)["value"] = "changed"
	}
	if err := bank.Persist(); err != nil {
		t.Fatal(err)
	}
	loaded, err := NewExperienceBank(filepath.Dir(bank.PersistPath))
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.Entries[0].Proposal
	if got.Source != "mcts" || got.Search.Seed != 42 || got.Op.Node.Name != "HasClearTask" || got.Op.Node.Metadata["nested"].(map[string]any)["value"] != "original" || got.Op.Metadata["nested"].([]any)[0].(map[string]any)["value"] != "original" {
		t.Fatalf("consumer edits leaked into persisted proposal: %+v", got)
	}
}
