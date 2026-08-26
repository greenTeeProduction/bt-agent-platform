package engine

import (
	"fmt"
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"

	"github.com/nico/go-bt-evolve/internal/blackboard"
)

// The CIRCUITPOLICY state-hash history is the sole signal the loop runner and
// circuit breaker use to detect the "Activity-Progress Confusion" cycle. Each
// cron tick builds a fresh Blackboard (RunOnce) whose ChainState dies with the
// run, so a history that only lives in ChainState resets to empty every tick and
// the bounded window can never see a repeat across ticks. The producer must
// persist the []string history to the agent-scope blackboard (mirroring
// saveSuperpowersPlanState) so it survives across cron ticks.
func TestGoapFusionStateHashes_DurableAcrossCronTicks(t *testing.T) {
	action := GetAction("PublishGoapFusionStateHash")
	if action == nil {
		t.Fatal("missing production action PublishGoapFusionStateHash")
	}

	// Two runs share the manager exactly as the agent-scope store does across
	// scheduled cron ticks: the hash published by tick 1 must be visible to tick 2.
	mgr := blackboard.NewManager(nil)
	tick1 := &Blackboard{
		BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop"),
		ChainState: map[string]any{
			"goap_fusion_goal_queue": "[P0] fix the loop runner\n[P2] add smoke tests",
		},
	}
	if status := action(&btcore.BTContext[Blackboard]{Blackboard: tick1}); status != 1 {
		t.Fatalf("expected PublishGoapFusionStateHash to return SUCCESS (1) on tick 1, got %d: %s", status, tick1.Result)
	}

	tick1Hist := goapFusionStateHashes(tick1)
	if len(tick1Hist) != 1 {
		t.Fatalf("expected exactly one hash published on tick 1, got %d: %v", len(tick1Hist), tick1Hist)
	}
	publishedHash := tick1Hist[0]

	// A completely fresh Blackboard (new ChainState) — the next cron tick — must
	// load the durable history from the agent-scope store, not start empty.
	tick2 := &Blackboard{
		BB:         blackboard.NewHandle(mgr, "run-2", "", "goap-loop"),
		ChainState: map[string]any{},
	}
	carried := goapFusionStateHashes(tick2)
	if len(carried) != 1 || carried[0] != publishedHash {
		t.Fatalf("expected tick 2 to load the durable state-hash history [%q] from the agent-scope blackboard, got %v", publishedHash, carried)
	}

	// Re-deriving the same goal queue on tick 2 appends onto the durable history,
	// so the accumulated window now holds the repeated state hash the circuit
	// breaker HALTs on — the end-to-end closure that only works if the history is
	// durable across ticks.
	tick2.ChainState["goap_fusion_goal_queue"] = "[P0] fix the loop runner\n[P2] add smoke tests"
	if status := action(&btcore.BTContext[Blackboard]{Blackboard: tick2}); status != 1 {
		t.Fatalf("expected SUCCESS (1) on tick 2 publish, got %d: %s", status, tick2.Result)
	}
	accumulated := goapFusionStateHashes(tick2)
	if len(accumulated) != 2 || accumulated[0] != accumulated[1] {
		t.Fatalf("expected two identical hashes accumulated across cron ticks for an unchanged goal queue, got %v", accumulated)
	}
}

// The durable history must be capped so it cannot grow without bound on disk as
// cron ticks accumulate. The bound must retain at least goapFusionMaxLoopIterations
// (and therefore at least goapFusionCircuitHistoryWindow) recent hashes so the
// runaway-loop backstop and the repeated-state window both still function.
func TestGoapFusionStateHashes_HistoryCappedOnDisk(t *testing.T) {
	action := GetAction("PublishGoapFusionStateHash")
	if action == nil {
		t.Fatal("missing production action PublishGoapFusionStateHash")
	}

	mgr := blackboard.NewManager(nil)

	// Simulate many cron ticks, each re-deriving the same goal queue on a fresh
	// Blackboard (as RunOnce does). Without a cap this history would grow by one
	// entry per tick forever.
	const ticks = 500
	var last *Blackboard
	for i := range ticks {
		bb := &Blackboard{
			BB: blackboard.NewHandle(mgr, "run", "", "goap-loop"),
			ChainState: map[string]any{
				"goap_fusion_goal_queue": "[P0] the same unchanged goal queue",
			},
		}
		if status := action(&btcore.BTContext[Blackboard]{Blackboard: bb}); status != 1 {
			t.Fatalf("expected SUCCESS (1) on tick %d, got %d: %s", i, status, bb.Result)
		}
		last = bb
	}

	hist := goapFusionStateHashes(last)
	if len(hist) >= ticks {
		t.Fatalf("expected the durable state-hash history to be capped, but it grew unbounded to %d entries across %d ticks", len(hist), ticks)
	}
	if len(hist) < goapFusionMaxLoopIterations {
		t.Fatalf("expected the cap to retain at least goapFusionMaxLoopIterations (%d) recent hashes, got %d", goapFusionMaxLoopIterations, len(hist))
	}
	if goapFusionMaxLoopIterations < goapFusionCircuitHistoryWindow {
		t.Fatalf("cap invariant broken: goapFusionMaxLoopIterations (%d) must be >= goapFusionCircuitHistoryWindow (%d)", goapFusionMaxLoopIterations, goapFusionCircuitHistoryWindow)
	}
}

// PublishGoapFusionStateHash's own bb.Result must describe the DURABLE store it
// now writes to. It reports the appended hash's position as "window depth N/M".
// Now that the history is a durable, cross-tick store capped at
// goapFusionStateHashHistoryCap — not the ephemeral, per-tick ChainState the old
// contract assumed — denominating that depth against goapFusionCircuitHistoryWindow
// (the 3-hash repeated-state window) actively misleads: as soon as more than
// goapFusionCircuitHistoryWindow hashes accumulate across ticks the numerator
// exceeds its own denominator (e.g. "5/3"), reporting a depth larger than its
// stated maximum. The window-depth message must reflect the durable store by
// denominating the depth against the durable cap, not the circuit window.
func TestPublishGoapFusionStateHash_ResultReflectsDurableDepth(t *testing.T) {
	action := GetAction("PublishGoapFusionStateHash")
	if action == nil {
		t.Fatal("missing production action PublishGoapFusionStateHash")
	}

	mgr := blackboard.NewManager(nil)

	// Accumulate more than the circuit window's worth of hashes across cron ticks
	// (each a fresh Blackboard sharing the durable agent-scope store) so the durable
	// depth strictly exceeds goapFusionCircuitHistoryWindow but stays under the
	// durable cap, so no truncation confounds the reported depth.
	n := goapFusionCircuitHistoryWindow + 2
	var last *Blackboard
	for i := range n {
		bb := &Blackboard{
			BB: blackboard.NewHandle(mgr, "run", "", "goap-loop"),
			ChainState: map[string]any{
				"goap_fusion_goal_queue": fmt.Sprintf("[P0] distinct goal %d", i),
			},
		}
		if status := action(&btcore.BTContext[Blackboard]{Blackboard: bb}); status != 1 {
			t.Fatalf("expected SUCCESS (1) on tick %d, got %d: %s", i, status, bb.Result)
		}
		last = bb
	}

	depth := len(goapFusionStateHashes(last))
	if depth <= goapFusionCircuitHistoryWindow {
		t.Fatalf("test setup: expected durable depth (%d) to exceed the circuit window (%d)", depth, goapFusionCircuitHistoryWindow)
	}
	if depth > goapFusionStateHashHistoryCap {
		t.Fatalf("test setup: expected durable depth (%d) to stay under the cap (%d) so no truncation occurs", depth, goapFusionStateHashHistoryCap)
	}

	// The result must denominate the accumulated depth against the DURABLE cap it
	// is actually bounded by...
	wantDurable := fmt.Sprintf("%d/%d", depth, goapFusionStateHashHistoryCap)
	if !strings.Contains(last.Result, wantDurable) {
		t.Fatalf("expected bb.Result to report the durable history depth %q against the durable cap, got:\n%s", wantDurable, last.Result)
	}

	// ...and must NOT report the misleading circuit-window depth, where the numerator
	// exceeds its own denominator now that the history is durable across ticks.
	misleading := fmt.Sprintf("%d/%d", depth, goapFusionCircuitHistoryWindow)
	if strings.Contains(last.Result, misleading) {
		t.Fatalf("bb.Result still reports the misleading circuit-window depth %q (numerator exceeds denominator):\n%s", misleading, last.Result)
	}
}

// When the scheduled loop is IDLE — no active program milestone AND no
// prioritized goals to work on — PublishGoapFusionStateHash must NOT accumulate a
// fresh state hash every tick. An idle loop re-derives the identical empty goal
// queue on each cron tick; appending that identical hash every tick piles up
// goapFusionCircuitHistoryWindow repeats of the very same idle hash and falsely
// trips the repeated-state circuit breaker even though the loop is merely waiting
// for work, not stuck in an "Activity-Progress Confusion" cycle. Cross-tick
// accumulation of a repeated hash is the intended signal ONLY when there is real
// work (a populated goal queue / an active milestone); idle ticks must leave the
// durable history unchanged rather than manufacturing a phantom repeated state.
func TestPublishGoapFusionStateHash_IdleDoesNotAccumulate(t *testing.T) {
	action := GetAction("PublishGoapFusionStateHash")
	if action == nil {
		t.Fatal("missing production action PublishGoapFusionStateHash")
	}

	mgr := blackboard.NewManager(nil)

	// Simulate several idle cron ticks: no goap_fusion_program_milestone and an
	// empty prioritized goal queue (nothing to work on). Each tick is a fresh
	// Blackboard sharing the durable agent-scope store, exactly as RunOnce builds.
	const idleTicks = goapFusionCircuitHistoryWindow + 2
	var last *Blackboard
	for i := range idleTicks {
		bb := &Blackboard{
			BB: blackboard.NewHandle(mgr, "run", "", "goap-loop"),
			ChainState: map[string]any{
				"goap_fusion_goal_queue": "",
			},
		}
		if status := action(&btcore.BTContext[Blackboard]{Blackboard: bb}); status != 1 {
			t.Fatalf("expected SUCCESS (1) on idle tick %d, got %d: %s", i, status, bb.Result)
		}
		last = bb
	}

	hist := goapFusionStateHashes(last)
	if len(hist) > 1 {
		t.Fatalf("expected idle cron ticks (no active program milestone, empty goal queue) NOT to accumulate identical idle hashes, but the durable history grew to %d entries: %v", len(hist), hist)
	}
}

// The idle guard must NOT suppress genuine repeated-state detection. When the
// loop has real work — a populated goal queue — re-deriving the identical queue
// across cron ticks must still accumulate the repeated hash the circuit breaker
// HALTs on. This pins the idle guard to the truly-idle case (empty queue, no
// milestone) so it cannot regress the "Activity-Progress Confusion" cycle signal.
func TestPublishGoapFusionStateHash_ActiveWorkStillAccumulates(t *testing.T) {
	action := GetAction("PublishGoapFusionStateHash")
	if action == nil {
		t.Fatal("missing production action PublishGoapFusionStateHash")
	}

	mgr := blackboard.NewManager(nil)

	var last *Blackboard
	for i := range 2 {
		bb := &Blackboard{
			BB: blackboard.NewHandle(mgr, "run", "", "goap-loop"),
			ChainState: map[string]any{
				"goap_fusion_goal_queue": "[P0] fix the loop runner\n[P2] add smoke tests",
			},
		}
		if status := action(&btcore.BTContext[Blackboard]{Blackboard: bb}); status != 1 {
			t.Fatalf("expected SUCCESS (1) on active-work tick %d, got %d: %s", i, status, bb.Result)
		}
		last = bb
	}

	hist := goapFusionStateHashes(last)
	if len(hist) != 2 || hist[0] != hist[1] {
		t.Fatalf("expected two identical hashes accumulated for a repeated populated goal queue (the intended cycle signal), got %v", hist)
	}
}

// The durable state-hash history cap must be STRICTLY greater than the
// runaway-loop backstop threshold. When the cap equals the threshold, once a
// runaway loop accumulates goapFusionMaxLoopIterations distinct hashes across
// cron ticks the persisted history is pinned at exactly the trip point: every
// subsequent tick appends one hash, the cap truncates it straight back to the
// threshold, and RunScheduledGoapFusionLoop re-HALTs on the runaway backstop
// forever — a permanent wedge that dead-letters the goap-fusion-loop-runner. A
// cap strictly above the threshold leaves headroom so the persisted history is
// not held at the trip point by the cap alone.
func TestGoapFusionStateHashHistoryCap_StrictlyExceedsBackstop(t *testing.T) {
	if goapFusionStateHashHistoryCap <= goapFusionMaxLoopIterations {
		t.Fatalf("durable history cap (%d) must be STRICTLY greater than the runaway-loop backstop threshold (%d): a cap equal to the threshold pins the persisted history at exactly the trip point, so every cron tick re-loads a >=threshold history and re-HALTs forever", goapFusionStateHashHistoryCap, goapFusionMaxLoopIterations)
	}
}

// The runaway-loop backstop must be HALF-OPEN: when it trips (HALT) it must
// self-clear the durable state-hash history so the NEXT cron tick starts from a
// fresh window. Without this, the persisted history stays at >= the backstop
// threshold across ticks, so RunScheduledGoapFusionLoop re-HALTs on the runaway
// backstop every subsequent tick — a permanent wedge (the recurring
// goap-fusion-loop-runner dead-letter). Tripping HALTs the current cycle but
// must give the next cycle a clean slate to make genuine progress.
func TestRunScheduledGoapFusionLoop_RunawayBackstopIsHalfOpen(t *testing.T) {
	loop := GetAction("RunScheduledGoapFusionLoop")
	if loop == nil {
		t.Fatal("missing production action RunScheduledGoapFusionLoop")
	}

	bb := newLoopBB(t)

	// Fill the durable history with goapFusionMaxLoopIterations DISTINCT hashes:
	// the bounded window sees no repeat and there is no no-op streak, so it is
	// the runaway-loop backstop — not the repeated-hash breaker — that trips.
	hashes := make([]string, goapFusionMaxLoopIterations)
	for i := range hashes {
		hashes[i] = fmt.Sprintf("distinct-runaway-hash-%d", i)
	}
	saveGoapFusionStateHashes(bb, hashes)
	if got := len(goapFusionStateHashes(bb)); got < goapFusionMaxLoopIterations {
		t.Fatalf("setup: expected the durable history to hold at least the backstop threshold (%d), got %d", goapFusionMaxLoopIterations, got)
	}

	// First tick: the runaway-loop backstop trips → HALT.
	if got := loop(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != -1 {
		t.Fatalf("expected the runaway-loop backstop to HALT (-1) at the finite backstop, got %d: %s", got, bb.Result)
	}
	if !strings.Contains(bb.Result, "Runaway-loop backstop") {
		t.Fatalf("expected the HALT to be attributed to the runaway-loop backstop, got:\n%s", bb.Result)
	}

	// Half-open: on trip the backstop must SELF-CLEAR the durable history so the
	// history no longer sits at the trip point.
	if remaining := goapFusionStateHashes(bb); len(remaining) != 0 {
		t.Fatalf("half-open backstop must clear the durable state-hash history on trip, got %d entries: %v", len(remaining), remaining)
	}

	// End-to-end: the very next tick (fresh window) must be able to CONTINUE
	// rather than re-HALT on the same runaway backstop — proving the wedge is
	// broken.
	if got := loop(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("expected the next tick to CONTINUE (1) after the half-open clear, got %d: %s", got, bb.Result)
	}
}
