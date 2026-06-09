package engine

import (
	"fmt"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// CheckpointVerifier is a decorator node that validates world state against
// expected postconditions after child execution. If postconditions aren't met,
// it restores the pre-execution snapshot and retries (up to MaxRetries).
//
// World state is stored in bb.ChainState["world_state"] as map[string]bool.
// Postconditions specify which facts must be true/false after child success.
type CheckpointVerifier struct {
	child          btcore.Command[Blackboard]
	MaxRetries     int
	Postconditions map[string]bool // expected world state facts after child success
	CheckInterval  int             // verify every N actions (0 = only at end)
}

// NewCheckpointVerifier creates a CheckpointVerifier decorator wrapping a child command.
func NewCheckpointVerifier(child btcore.Command[Blackboard], maxRetries int, postconditions map[string]bool) *CheckpointVerifier {
	if maxRetries <= 0 {
		maxRetries = 3
	}
	if postconditions == nil {
		postconditions = make(map[string]bool)
	}
	return &CheckpointVerifier{
		child:          child,
		MaxRetries:     maxRetries,
		Postconditions: postconditions,
	}
}

// readPostconditions extracts postconditions from a SerializableNode's metadata.
// Metadata["postconditions"] should be a map[string]any with boolean values.
func readPostconditions(node *evolution.SerializableNode) map[string]bool {
	pc := make(map[string]bool)
	if node.Metadata == nil {
		return pc
	}
	raw, ok := node.Metadata["postconditions"]
	if !ok {
		return pc
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return pc
	}
	for k, v := range m {
		if b, ok := v.(bool); ok {
			pc[k] = b
		}
	}
	return pc
}

// snapshotState deep-copies the current world state from ChainState.
func snapshotState(bb *Blackboard) map[string]bool {
	snap := make(map[string]bool)
	if bb.ChainState != nil {
		if ws, ok := bb.ChainState["world_state"].(map[string]bool); ok {
			for k, v := range ws {
				snap[k] = v
			}
		}
	}
	return snap
}

// restoreState writes a snapshot back to ChainState as the current world state.
func restoreState(bb *Blackboard, snap map[string]bool) {
	if bb.ChainState == nil {
		bb.ChainState = make(map[string]any)
	}
	ws := make(map[string]bool, len(snap))
	for k, v := range snap {
		ws[k] = v
	}
	bb.ChainState["world_state"] = ws
}

// extractWorldState reads the current world state from ChainState.
// Returns an empty map if no world state has been set yet.
func extractWorldState(bb *Blackboard) map[string]bool {
	state := make(map[string]bool)
	if bb.ChainState != nil {
		if ws, ok := bb.ChainState["world_state"].(map[string]bool); ok {
			for k, v := range ws {
				state[k] = v
			}
		}
	}
	return state
}

// verifyPostconditions checks whether all expected postconditions are satisfied
// by the given world state.
func (c *CheckpointVerifier) verifyPostconditions(state map[string]bool) bool {
	for fact, expected := range c.Postconditions {
		actual, exists := state[fact]
		if !exists || actual != expected {
			return false
		}
	}
	return true
}

// Run executes the child subtree with checkpoint verification.
// On child success, it verifies postconditions. On mismatch or child failure,
// it restores the pre-execution snapshot and retries up to MaxRetries.
//
// Status codes follow go-bt convention: 1 = Success, 0 = Running, -1 = Failure.
func (c *CheckpointVerifier) Run(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard

	if c.MaxRetries <= 0 {
		c.MaxRetries = 3
	}

	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		// Take a snapshot before executing the child
		snap := snapshotState(bb)

		status := c.child.Run(ctx)

		// Child failure → restore and retry
		if status == -1 {
			restoreState(bb, snap)
			if attempt < c.MaxRetries {
				bb.ChainState["checkpoint_retry_reason"] = fmt.Sprintf("child_failure_attempt_%d", attempt+1)
				continue
			}
			return -1
		}

		// Child still running → propagate
		if status == 0 {
			return 0
		}

		// Child succeeded → verify postconditions
		currentState := extractWorldState(bb)
		if c.verifyPostconditions(currentState) {
			return 1
		}

		// Postcondition mismatch → restore and retry
		restoreState(bb, snap)
		if attempt < c.MaxRetries {
			bb.ChainState["checkpoint_retry_reason"] = fmt.Sprintf("postcondition_mismatch_attempt_%d", attempt+1)
			continue
		}
	}

	return -1
}
