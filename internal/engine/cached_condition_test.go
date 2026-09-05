package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestCachedCondition_CachesWithinTTLAndExpires(t *testing.T) {
	calls := 0
	RegisterCondition("TestCachedCond", func(_ *Blackboard) bool {
		calls++
		return true
	})
	node := &evolution.SerializableNode{
		Type: "CachedCondition", Name: "CCUnderTest",
		Metadata: map[string]any{"ttl_ms": 50},
		Children: []evolution.SerializableNode{{Type: "Condition", Name: "TestCachedCond"}},
	}
	bb := newTestBlackboard()
	cmd := buildNode(node, bb, "")
	ctx := newTestBTContext(bb)

	cmd.Run(ctx)
	cmd.Run(ctx)
	if calls != 1 {
		t.Fatalf("second call within TTL must hit cache: %d calls", calls)
	}
	time.Sleep(60 * time.Millisecond)
	cmd.Run(ctx)
	if calls != 2 {
		t.Fatalf("call after TTL must re-evaluate: %d calls", calls)
	}
}

func TestCachedCondition_RefusesHITLConditions(t *testing.T) {
	node := &evolution.SerializableNode{
		Type: "CachedCondition", Name: "CCGuard",
		Children: []evolution.SerializableNode{{Type: "Condition", Name: "HITLAlreadyApproved"}},
	}
	msgs := ValidateTree(&evolution.SerializableNode{Type: "Sequence", Name: "r",
		Children: []evolution.SerializableNode{*node}})
	if len(msgs) == 0 {
		t.Fatal("caching an HITL/approval condition must produce a validation message")
	}
}

func TestCachedCondition_RefusesNestedHITLConditions(t *testing.T) {
	node := &evolution.SerializableNode{
		Type: "CachedCondition", Name: "CCNested",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence", Name: "seq",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "HITLAlreadyApproved"},
					{Type: "AlwaysSucceed"},
				},
			},
		},
	}
	msgs := ValidateTree(&evolution.SerializableNode{Type: "Sequence", Name: "r",
		Children: []evolution.SerializableNode{*node}})
	found := false
	for _, m := range msgs {
		if strings.Contains(m, "HITLAlreadyApproved") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("caching a subtree that contains a nested HITL/approval condition must produce a validation message, got: %v", msgs)
	}
}
