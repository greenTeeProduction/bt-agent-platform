package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
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

// TestBanditSelector_ResumesRunningChildDespiteReordering (Finding 3): an
// enabled BanditSelector must resume an in-flight RUNNING child on the next
// tick even if a UCB1 recompute would otherwise rank a different arm first.
// childA has the better seeded stats so it is tried (and ticked) first;
// childA reports RUNNING (not recorded, so its own tally is untouched)
// between the two ticks the on-disk stats file is rewritten so that, if
// re-read, childB would now rank first — reproducing the "another actor
// changed the ranking while we were mid-flight" scenario from the review
// finding. The fix (a running-child cursor in ChainState) must resume
// childA regardless; childB must never be touched.
func TestBanditSelector_ResumesRunningChildDespiteReordering(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BT_BANDIT_DIR", dir)

	nodeName := "BanditResumeUnderTest"
	seed := banditStats{Outcomes: map[string][]bool{
		"BanditResumeChildA": {true, true, true, true, true},      // mean 1.0: tried first
		"BanditResumeChildB": {false, false, false, false, false}, // mean 0.0
	}}
	data, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, nodeName+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	callsA, callsB := 0, 0
	RegisterAction("BanditResumeChildA", func(_ *btcore.BTContext[Blackboard]) int {
		callsA++
		if callsA == 1 {
			return 0 // RUNNING on the first tick, not recorded
		}
		return 1 // SUCCESS on resume
	})
	RegisterAction("BanditResumeChildB", func(_ *btcore.BTContext[Blackboard]) int {
		callsB++
		return 1
	})

	node := &evolution.SerializableNode{
		Type:     "BanditSelector",
		Name:     nodeName,
		Metadata: map[string]any{"enabled": true},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "BanditResumeChildA"},
			{Type: "Action", Name: "BanditResumeChildB"},
		},
	}
	bb := newTestBlackboard()
	cmd := buildNode(node, bb, "")
	ctx := newTestBTContext(bb)

	if got := cmd.Run(ctx); got != 0 {
		t.Fatalf("tick 1: want RUNNING, got %d", got)
	}
	if callsA != 1 || callsB != 0 {
		t.Fatalf("tick 1: want childA tried first and childB untouched, got callsA=%d callsB=%d", callsA, callsB)
	}

	// Simulate the ranking-changing write a concurrent actor (or a stale
	// per-tick reload) could observe between ticks: childB now dominates.
	mutated := banditStats{Outcomes: map[string][]bool{
		"BanditResumeChildA": {false, false, false, false, false},
		"BanditResumeChildB": {true, true, true, true, true, true, true, true, true, true},
	}}
	mutatedData, err := json.Marshal(mutated)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, nodeName+".json"), mutatedData, 0o644); err != nil {
		t.Fatal(err)
	}

	if got := cmd.Run(ctx); got != 1 {
		t.Fatalf("tick 2: want SUCCESS, got %d", got)
	}
	if callsA != 2 {
		t.Fatalf("tick 2: want childA resumed (2nd call), got callsA=%d", callsA)
	}
	if callsB != 0 {
		t.Fatalf("tick 2: want childB never tried — the running-child cursor must pin childA, got callsB=%d", callsB)
	}
}

// TestBanditSelector_DisabledRunningChildRestartsAtZero (Important
// re-review finding): disabled mode is contractually "behaves EXACTLY like
// Selector" — restart at child 0 every tick, zero cross-tick memory; the
// vendored composite.Selector has no cursor at all. The running-child
// resume cursor (added for Finding 3) was not gated by `enabled`, so a
// disabled BanditSelector resumed a previously-RUNNING child on the next
// tick instead of restarting at child 0 — skipping earlier children and
// leaving a "bandit/<name>/running" ChainState key that plain Selector
// semantics never produce.
func TestBanditSelector_DisabledRunningChildRestartsAtZero(t *testing.T) {
	t.Setenv("BT_BANDIT_DIR", t.TempDir())

	nodeName := "BanditDisabledRestartUnderTest"
	runningKey := "bandit/" + nodeName + "/running"

	callsA, callsB := 0, 0
	RegisterAction("BanditDisabledRestartChildA", func(_ *btcore.BTContext[Blackboard]) int {
		callsA++
		return -1 // always fails
	})
	RegisterAction("BanditDisabledRestartChildB", func(_ *btcore.BTContext[Blackboard]) int {
		callsB++
		if callsB == 1 {
			return 0 // RUNNING on first tick
		}
		return 1 // SUCCESS on second tick
	})

	node := &evolution.SerializableNode{
		Type:     "BanditSelector",
		Name:     nodeName,
		Metadata: map[string]any{"enabled": false},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "BanditDisabledRestartChildA"},
			{Type: "Action", Name: "BanditDisabledRestartChildB"},
		},
	}
	bb := newTestBlackboard()
	cmd := buildNode(node, bb, "")
	ctx := newTestBTContext(bb)

	if got := cmd.Run(ctx); got != 0 {
		t.Fatalf("tick 1: want RUNNING, got %d", got)
	}
	if callsA != 1 || callsB != 1 {
		t.Fatalf("tick 1: want childA and childB each tried once (Selector semantics: A fails, falls through to B), got callsA=%d callsB=%d", callsA, callsB)
	}
	if _, ok := bb.ChainState[runningKey]; ok {
		t.Fatalf("tick 1: disabled BanditSelector must never write a resume cursor, found %q in ChainState", runningKey)
	}

	if got := cmd.Run(ctx); got != 1 {
		t.Fatalf("tick 2: want SUCCESS, got %d", got)
	}
	if callsA != 2 {
		t.Fatalf("tick 2: want childA tried again before reaching childB (disabled mode restarts at child 0 every tick), got callsA=%d", callsA)
	}
	if callsB != 2 {
		t.Fatalf("tick 2: want childB tried again, got callsB=%d", callsB)
	}
	if _, ok := bb.ChainState[runningKey]; ok {
		t.Fatalf("tick 2: disabled BanditSelector must never write a resume cursor, found %q in ChainState", runningKey)
	}
}

// TestBanditSelector_ConcurrentTicksNoLostOutcomes (Finding 1): two
// goroutines race a fresh BanditSelector command instance each tick against
// the same node name (hence the same stats file). Without a per-path mutex
// serializing each load-modify-save cycle, concurrent reads/writes to the
// same tmp file (path+".tmp") and stale-read races drop outcomes. Run with
// -race to also catch any in-memory race in the lock registry itself.
func TestBanditSelector_ConcurrentTicksNoLostOutcomes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BT_BANDIT_DIR", dir)

	const nodeName = "BanditConcurrentUnderTest"
	const perGoroutine = 150

	RegisterAction("BanditConcurrentChild", func(_ *btcore.BTContext[Blackboard]) int {
		return 1
	})

	node := &evolution.SerializableNode{
		Type:     "BanditSelector",
		Name:     nodeName,
		Metadata: map[string]any{"enabled": false, "window": 1000}, // window large enough to hold every outcome
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "BanditConcurrentChild"},
		},
	}

	tick := func() {
		bb := newTestBlackboard()
		cmd := buildNode(node, bb, "")
		ctx := newTestBTContext(bb)
		if got := cmd.Run(ctx); got != 1 {
			t.Errorf("want SUCCESS, got %d", got)
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	for g := 0; g < 2; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				tick()
			}
		}()
	}
	wg.Wait()

	stats := loadBanditStats(nodeName)
	got := len(stats.Outcomes["BanditConcurrentChild"])
	want := 2 * perGoroutine
	if got != want {
		t.Fatalf("recorded outcomes = %d, want %d (lost updates from an unsynchronized load-modify-save cycle)", got, want)
	}
}

// TestBanditSelector_CachesStatsAcrossTicksNoPerTickReload (Finding 2): a
// cheap proxy for "no reload on every tick" — corrupt the on-disk stats file
// after the first tick. loadBanditStats tolerates corrupt JSON by starting
// empty, so a node that still reloads every tick would silently lose tick
// 1's recorded outcome (the final file would hold only tick 2's). A node
// that caches after the first load carries tick 1's outcome forward
// regardless of what's on disk.
func TestBanditSelector_CachesStatsAcrossTicksNoPerTickReload(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BT_BANDIT_DIR", dir)

	nodeName := "BanditCacheUnderTest"
	RegisterAction("BanditCacheChild", func(_ *btcore.BTContext[Blackboard]) int {
		return 1
	})
	node := &evolution.SerializableNode{
		Type:     "BanditSelector",
		Name:     nodeName,
		Metadata: map[string]any{"enabled": false, "window": 10},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "BanditCacheChild"},
		},
	}
	bb := newTestBlackboard()
	cmd := buildNode(node, bb, "")
	ctx := newTestBTContext(bb)

	if got := cmd.Run(ctx); got != 1 {
		t.Fatalf("tick 1: want SUCCESS, got %d", got)
	}

	if err := os.WriteFile(banditStatsPath(nodeName), []byte("not valid json{{{"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := cmd.Run(ctx); got != 1 {
		t.Fatalf("tick 2: want SUCCESS, got %d", got)
	}

	stats := loadBanditStats(nodeName)
	outcomes := stats.Outcomes["BanditCacheChild"]
	if len(outcomes) != 2 {
		t.Fatalf("recorded outcomes = %d, want 2 (cache must survive a corrupted on-disk file between ticks)", len(outcomes))
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
