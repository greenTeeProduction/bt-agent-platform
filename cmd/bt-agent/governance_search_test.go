package main

import (
	"encoding/json"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestGovernedSearchCanImproveBuiltin(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	base := resolveTree("godev")
	t.Logf("base %+v", evolution.AssessGovernance(base))
	var walk func(*evolution.SerializableNode)
	count := 0
	walk = func(n *evolution.SerializableNode) {
		if evolution.IsTaskWork(n) {
			data, _ := json.Marshal(base)
			candidate := new(evolution.SerializableNode)
			_ = json.Unmarshal(data, candidate)
			applied := evolution.ApplyMutations(candidate, []evolution.MutationOp{{Operation: "wrap_quality_gate", Target: n.Name}})
			score := governedStructuralFitness(base)(candidate)
			t.Logf("%s applied=%d score=%.2f", n.Name, applied, score)
			if applied > 0 && score > structuralFitnessFn(base) {
				count++
			}
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(base)
	if count == 0 {
		t.Fatal("no governance-improving mutation can be applied to builtin task work")
	}
}
