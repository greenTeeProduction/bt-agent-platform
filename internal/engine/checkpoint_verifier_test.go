package engine

import (
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

func TestCheckpointVerifier_SuccessFirstTry(t *testing.T) {
	// Set up a world state that already satisfies postconditions.
	bb := &Blackboard{
		ChainState: map[string]any{
			"world_state": map[string]bool{
				"door_open": true,
				"light_on":  false,
			},
		},
	}

	// Child that immediately returns success (1) — it's a no-op.
	child := btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		return 1
	})

	verifier := NewCheckpointVerifier(
		child,
		3,
		map[string]bool{
			"door_open": true,
			"light_on":  false,
		},
	)

	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	status := verifier.Run(ctx)

	if status != 1 {
		t.Fatalf("expected success (1), got %d", status)
	}
}

func TestCheckpointVerifier_RetryOnPostconditionMismatch(t *testing.T) {
	bb := &Blackboard{
		ChainState: map[string]any{
			"world_state": map[string]bool{
				"door_open": false,
			},
		},
	}

	callCount := 0

	// Child that returns success but only sets the correct
	// postcondition on the second call.
	child := btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		callCount++
		if callCount == 1 {
			// First attempt: leave world_state as-is (doesn't match postcondition).
			return 1
		}
		// Second attempt: fix the world state to match postconditions.
		ctx.Blackboard.ChainState["world_state"] = map[string]bool{
			"door_open": true,
		}
		return 1
	})

	verifier := NewCheckpointVerifier(
		child,
		3,
		map[string]bool{"door_open": true},
	)

	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	status := verifier.Run(ctx)

	if status != 1 {
		t.Fatalf("expected success (1) after retry, got %d", status)
	}
	if callCount != 2 {
		t.Fatalf("expected child to be called 2 times, got %d", callCount)
	}

	// Verify final world state matches postconditions.
	ws := extractWorldState(bb)
	if !ws["door_open"] {
		t.Fatal("expected door_open to be true after retry")
	}
}

func TestCheckpointVerifier_ExhaustsRetriesReturnsFailure(t *testing.T) {
	bb := &Blackboard{
		ChainState: map[string]any{
			"world_state": map[string]bool{
				"task_done": false,
			},
		},
	}

	callCount := 0

	// Child that always returns success but never sets the
	// required postcondition.
	child := btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		callCount++
		return 1
	})

	verifier := NewCheckpointVerifier(
		child,
		2, // 2 retries → 3 total attempts (0, 1, 2)
		map[string]bool{"task_done": true},
	)

	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	status := verifier.Run(ctx)

	if status != -1 {
		t.Fatalf("expected failure (-1) after exhausting retries, got %d", status)
	}
	// MaxRetries=2, loop runs attempt 0..2 (inclusive) → 3 calls
	if callCount != 3 {
		t.Fatalf("expected child to be called 3 times, got %d", callCount)
	}
}

func TestCheckpointVerifier_StateRestorationOnChildFailure(t *testing.T) {
	// Set up initial world state that should be restored after child failure.
	initialState := map[string]bool{
		"balance":       true,
		"task_complete": false,
	}
	bb := &Blackboard{
		ChainState: map[string]any{
			"world_state": initialState,
		},
	}

	callCount := 0

	// Child that fails on first attempt (corrupting state),
	// and succeeds on retry.
	child := btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		callCount++
		if callCount == 1 {
			// Corrupt world state then return failure.
			ctx.Blackboard.ChainState["world_state"] = map[string]bool{
				"balance":       false,
				"task_complete": false,
				"garbage":       true,
			}
			return -1
		}
		// Second attempt: set correct postconditions and return success.
		ctx.Blackboard.ChainState["world_state"] = map[string]bool{
			"balance":       true,
			"task_complete": true,
		}
		return 1
	})

	verifier := NewCheckpointVerifier(
		child,
		3,
		map[string]bool{
			"balance":       true,
			"task_complete": true,
		},
	)

	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	status := verifier.Run(ctx)

	if status != 1 {
		t.Fatalf("expected success (1) after retry from failure, got %d", status)
	}
	if callCount != 2 {
		t.Fatalf("expected child to be called 2 times, got %d", callCount)
	}

	// Verify final world state matches postconditions.
	ws := extractWorldState(bb)
	if !ws["balance"] {
		t.Fatal("expected balance to be true after retry")
	}
	if !ws["task_complete"] {
		t.Fatal("expected task_complete to be true after retry")
	}
	// Garbage from the corrupted state should NOT be present.
	if ws["garbage"] {
		t.Fatal("expected garbage key to NOT be present after state restoration")
	}
}

func TestCheckpointVerifier_RunningPropagation(t *testing.T) {
	// When child returns Running (0), the verifier should propagate it.
	bb := &Blackboard{
		ChainState: map[string]any{
			"world_state": map[string]bool{},
		},
	}

	child := btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		return 0 // Running
	})

	verifier := NewCheckpointVerifier(
		child,
		3,
		map[string]bool{},
	)

	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	status := verifier.Run(ctx)

	if status != 0 {
		t.Fatalf("expected running (0), got %d", status)
	}
}

func TestCheckpointVerifier_DefaultMaxRetries(t *testing.T) {
	// NewCheckpointVerifier should default to 3 when maxRetries <= 0.
	verifier := NewCheckpointVerifier(
		btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int { return 1 }),
		0,
		map[string]bool{},
	)
	if verifier.MaxRetries != 3 {
		t.Fatalf("expected MaxRetries=3 as default, got %d", verifier.MaxRetries)
	}
}

func TestCheckpointVerifier_NilPostconditions(t *testing.T) {
	// Nil postconditions should be converted to an empty map.
	verifier := NewCheckpointVerifier(
		btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int { return 1 }),
		1,
		nil,
	)
	if verifier.Postconditions == nil {
		t.Fatal("expected non-nil Postconditions after constructor")
	}
	if len(verifier.Postconditions) != 0 {
		t.Fatalf("expected empty postconditions, got %d entries", len(verifier.Postconditions))
	}

	// With empty postconditions, verifyPostconditions should always return true.
	if !verifier.verifyPostconditions(map[string]bool{}) {
		t.Fatal("expected empty postconditions to verify successfully")
	}
}
