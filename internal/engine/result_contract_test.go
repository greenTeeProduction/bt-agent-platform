package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestQualityGateEnforcesContractOnPrimaryAndRecovery(t *testing.T) {
	RegisterAction("ContractWrongKey", func(ctx *btcore.BTContext[Blackboard]) int {
		ctx.Blackboard.Result = `{"result":42,"approved":true,"message":"enough content for a quality heuristic"}`
		return 1
	})
	RegisterAction("ContractWrongValue", func(ctx *btcore.BTContext[Blackboard]) int {
		ctx.Blackboard.Result = `{"total":17,"approved":true,"message":"enough content for a quality heuristic"}`
		return 1
	})
	RegisterAction("ContractCorrect", func(ctx *btcore.BTContext[Blackboard]) int {
		ctx.Blackboard.Result = `{"total":42,"approved":true,"message":"verified correct task result"}`
		return 1
	})
	for _, test := range []struct {
		recovery string
		success  bool
	}{{"ContractWrongValue", false}, {"ContractCorrect", true}} {
		t.Run(test.recovery, func(t *testing.T) {
			store, err := evolution.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			tree := &evolution.SerializableNode{Type: "QualityGate", Name: "AnswerContract", Metadata: map[string]any{"result_contract": map[string]any{"json_fields": map[string]any{"total": 42, "approved": true}}}, Children: []evolution.SerializableNode{{Type: "Action", Name: "ContractWrongKey"}, {Type: "Action", Name: test.recovery}}}
			bb := &Blackboard{Task: "verify answer", Reflections: store}
			RunTask(bb, BuildTree(tree, bb))
			if (bb.Outcome == "success") != test.success {
				t.Fatalf("outcome=%s result=%s", bb.Outcome, bb.Result)
			}
			records, err := store.LoadAll()
			if err != nil || len(records) != 1 {
				t.Fatalf("records=%+v err=%v", records, err)
			}
			checks := records[0].ResultChecks
			if len(checks) != 2 || checks[0].Passed || checks[1].Passed != test.success || checks[0].OutputDigest == checks[1].OutputDigest {
				t.Fatalf("missing contract evidence: %+v", checks)
			}
		})
	}
}

func TestMalformedResultContractCannotExecuteWork(t *testing.T) {
	calls := 0
	RegisterAction("MalformedContractWork", func(_ *btcore.BTContext[Blackboard]) int { calls++; return 1 })
	tree := &evolution.SerializableNode{Type: "QualityGate", Name: "BadContract", Metadata: map[string]any{"result_contract": map[string]any{"typo": "ignored?"}}, Children: []evolution.SerializableNode{{Type: "Action", Name: "MalformedContractWork"}}}
	if ValidateTreeFull(tree).Valid() || len(ValidateTree(tree)) == 0 {
		t.Fatal("malformed contract passes validation")
	}
	bb := &Blackboard{Task: "must not execute"}
	RunTask(bb, BuildTree(tree, bb))
	if calls != 0 || bb.Outcome == "success" {
		t.Fatalf("invalid gate ran: calls=%d outcome=%s", calls, bb.Outcome)
	}
}
