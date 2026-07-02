package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestBanditSelector_DisabledMatchesSelectorSemantics(t *testing.T) {
	t.Setenv("BT_BANDIT_DIR", t.TempDir())

	var order []string
	RegisterAction("BanditDisabledChild0", func(_ *btcore.BTContext[Blackboard]) int {
		order = append(order, "child0")
		return -1
	})
	RegisterAction("BanditDisabledChild1", func(_ *btcore.BTContext[Blackboard]) int {
		order = append(order, "child1")
		return 1
	})

	node := &evolution.SerializableNode{
		Type:     "BanditSelector",
		Name:     "BanditDisabledUnderTest",
		Metadata: map[string]any{"enabled": false},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "BanditDisabledChild0"},
			{Type: "Action", Name: "BanditDisabledChild1"},
		},
	}
	bb := newTestBlackboard()
	cmd := buildNode(node, bb, "")
	ctx := newTestBTContext(bb)

	if got := cmd.Run(ctx); got != 1 {
		t.Fatalf("want SUCCESS, got %d", got)
	}
	if want := []string{"child0", "child1"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("call order = %v, want %v (Selector semantics: earlier failure falls through)", order, want)
	}

	path := banditStatsPath("BanditDisabledUnderTest")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected stats file at %s: %v", path, err)
	}
	stats := loadBanditStats("BanditDisabledUnderTest")
	if outcomes := stats.Outcomes["BanditDisabledChild0"]; len(outcomes) != 1 || outcomes[0] != false {
		t.Fatalf("child0 outcome not recorded correctly: %v", stats.Outcomes)
	}
	if outcomes := stats.Outcomes["BanditDisabledChild1"]; len(outcomes) != 1 || outcomes[0] != true {
		t.Fatalf("child1 outcome not recorded correctly: %v", stats.Outcomes)
	}
}

func TestBanditSelector_UCB1PrefersHistoricallySuccessfulChild(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BT_BANDIT_DIR", dir)

	nodeName := "BanditUCB1UnderTest"
	seed := banditStats{Outcomes: map[string][]bool{
		// child0: 1/10 successes, child1: 9/10 successes, equal sample sizes
		// so the exploration bonus is identical and mean alone decides order.
		"BanditUCBChild0": {true, false, false, false, false, false, false, false, false, false},
		"BanditUCBChild1": {true, true, true, true, true, true, true, true, true, false},
	}}
	data, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, nodeName+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	var order []string
	RegisterAction("BanditUCBChild0", func(_ *btcore.BTContext[Blackboard]) int {
		order = append(order, "child0")
		return -1
	})
	RegisterAction("BanditUCBChild1", func(_ *btcore.BTContext[Blackboard]) int {
		order = append(order, "child1")
		return -1
	})

	node := &evolution.SerializableNode{
		Type:     "BanditSelector",
		Name:     nodeName,
		Metadata: map[string]any{"enabled": true},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "BanditUCBChild0"},
			{Type: "Action", Name: "BanditUCBChild1"},
		},
	}
	bb := newTestBlackboard()
	cmd := buildNode(node, bb, "")
	ctx := newTestBTContext(bb)

	if got := cmd.Run(ctx); got != -1 {
		t.Fatalf("want FAILURE (both children fail), got %d", got)
	}
	if want := []string{"child1", "child0"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("call order = %v, want %v (higher historical success rate tried first)", order, want)
	}
}

func TestBanditSelector_ColdStartTriesEveryArmOnce(t *testing.T) {
	t.Setenv("BT_BANDIT_DIR", t.TempDir())

	names := []string{"BanditColdChild0", "BanditColdChild1", "BanditColdChild2"}
	calls := map[string]int{}
	for _, name := range names {
		name := name
		RegisterAction(name, func(_ *btcore.BTContext[Blackboard]) int {
			calls[name]++
			return -1
		})
	}

	children := make([]evolution.SerializableNode, len(names))
	for i, name := range names {
		children[i] = evolution.SerializableNode{Type: "Action", Name: name}
	}
	node := &evolution.SerializableNode{
		Type:     "BanditSelector",
		Name:     "BanditColdStartUnderTest",
		Metadata: map[string]any{"enabled": true},
		Children: children,
	}
	bb := newTestBlackboard()
	cmd := buildNode(node, bb, "")
	ctx := newTestBTContext(bb)

	if got := cmd.Run(ctx); got != -1 {
		t.Fatalf("want FAILURE (all children fail), got %d", got)
	}
	if len(calls) != len(names) {
		t.Fatalf("expected all %d children tried, got %v", len(names), calls)
	}
	for _, name := range names {
		if calls[name] != 1 {
			t.Fatalf("child %s called %d times, want exactly 1", name, calls[name])
		}
	}
}

func TestBanditSelector_WindowTrimsOutcomes(t *testing.T) {
	t.Setenv("BT_BANDIT_DIR", t.TempDir())

	RegisterAction("BanditWindowChild", func(_ *btcore.BTContext[Blackboard]) int {
		return 1
	})

	node := &evolution.SerializableNode{
		Type:     "BanditSelector",
		Name:     "BanditWindowUnderTest",
		Metadata: map[string]any{"enabled": false, "window": 3},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "BanditWindowChild"},
		},
	}

	for i := 0; i < 5; i++ {
		bb := newTestBlackboard()
		cmd := buildNode(node, bb, "")
		ctx := newTestBTContext(bb)
		if got := cmd.Run(ctx); got != 1 {
			t.Fatalf("run %d: want SUCCESS, got %d", i, got)
		}
	}

	stats := loadBanditStats("BanditWindowUnderTest")
	outcomes := stats.Outcomes["BanditWindowChild"]
	if len(outcomes) > 3 {
		t.Fatalf("stored outcomes not trimmed: len=%d, want <=3 (%v)", len(outcomes), outcomes)
	}
}

// Extra coverage beyond the four mandated tests: contract also requires a
// malformed config (zero children) to fail cleanly instead of panicking, and
// the unique-name validation to cover BanditSelector like the other memory
// nodes (stats files key on node name).

func TestBanditSelector_ZeroChildrenFailsWithoutPanic(t *testing.T) {
	t.Setenv("BT_BANDIT_DIR", t.TempDir())

	node := &evolution.SerializableNode{Type: "BanditSelector", Name: "BanditEmptyUnderTest"}
	bb := newTestBlackboard()
	cmd := buildNode(node, bb, "")
	ctx := newTestBTContext(bb)

	if got := cmd.Run(ctx); got != -1 {
		t.Fatalf("want FAILURE for zero-child BanditSelector, got %d", got)
	}
}

func TestValidate_BanditSelectorRequiresUniqueName(t *testing.T) {
	root := &evolution.SerializableNode{
		Type: "Sequence", Name: "root",
		Children: []evolution.SerializableNode{
			{Type: "BanditSelector", Name: "", Children: []evolution.SerializableNode{{Type: "AlwaysSucceed"}}},
		},
	}
	msgs := ValidateTree(root)
	if len(msgs) == 0 {
		t.Fatal("expected validation message for unnamed BanditSelector")
	}

	dup := &evolution.SerializableNode{
		Type: "Sequence", Name: "root",
		Children: []evolution.SerializableNode{
			{Type: "BanditSelector", Name: "DupBandit", Children: []evolution.SerializableNode{{Type: "AlwaysSucceed"}}},
			{Type: "BanditSelector", Name: "DupBandit", Children: []evolution.SerializableNode{{Type: "AlwaysSucceed"}}},
		},
	}
	msgs = ValidateTree(dup)
	found := false
	for _, msg := range msgs {
		if strings.Contains(msg, "duplicate") && strings.Contains(msg, "DupBandit") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected message containing both 'duplicate' and 'DupBandit', got: %v", msgs)
	}
}
