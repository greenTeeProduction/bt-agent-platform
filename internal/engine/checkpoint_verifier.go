package engine

import (
	"encoding/json"
	"fmt"
	"maps"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/goap"
	"github.com/nico/go-bt-evolve/internal/reliability"
	btcore "github.com/rvitorper/go-bt/core"
)

// CheckpointVerifier checks typed facts in its selected state map. Retries
// restore the attempt's in-memory snapshot, never an external side effect.
// The snapshot and retry budget survive Running ticks; a terminal run is cached.
type CheckpointVerifier struct {
	child          btcore.Command[Blackboard]
	MaxRetries     int
	Postconditions any
	StateKey       string
	contract       *evolution.CheckpointContract
	configErr      error
	bb             *Blackboard
	runID          string
	attempt        int
	terminal       int
	prepared       bool
	snapshot       any
	hadState       bool
	effectStart    int
}

// NewCheckpointVerifier accepts boolean or typed postcondition maps. Invalid
// and empty declarations fail before child execution instead of weakening a gate.
func NewCheckpointVerifier(child btcore.Command[Blackboard], maxRetries int, postconditions any) *CheckpointVerifier {
	return newCheckpointVerifier(child, &evolution.SerializableNode{
		MaxRetries: maxRetries,
		Metadata:   map[string]any{"postconditions": postconditions},
		Children:   []evolution.SerializableNode{{}},
	})
}

func newCheckpointVerifier(child btcore.Command[Blackboard], node *evolution.SerializableNode) *CheckpointVerifier {
	maxRetries := node.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	contract, err := evolution.ParseCheckpointContract(node)
	key := "world_state"
	if contract != nil {
		key = contract.StateKey
	}
	return &CheckpointVerifier{child: child, MaxRetries: maxRetries, StateKey: key,
		Postconditions: node.Metadata["postconditions"], contract: contract, configErr: err}
}

// cloneCheckpointState detaches maps across Running ticks and retries while
// preserving the map types used by legacy boolean actions and GOAP actions.
func cloneCheckpointState(value any) (any, error) {
	switch state := value.(type) {
	case nil, bool, string, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return state, nil
	case map[string]bool:
		return maps.Clone(state), nil
	case goap.WorldState:
		cloned, err := cloneCheckpointState(map[string]any(state))
		if err != nil {
			return nil, err
		}
		return goap.WorldState(cloned.(map[string]any)), nil
	case map[string]any:
		cloned := make(map[string]any, len(state))
		for key, value := range state {
			var err error
			cloned[key], err = cloneCheckpointState(value)
			if err != nil {
				return nil, err
			}
		}
		return cloned, nil
	case []any:
		cloned := make([]any, len(state))
		for i, value := range state {
			var err error
			cloned[i], err = cloneCheckpointState(value)
			if err != nil {
				return nil, err
			}
		}
		return cloned, nil
	default:
		return nil, fmt.Errorf("unsupported checkpoint state value %T", value)
	}
}

func (c *CheckpointVerifier) verifyPostconditions(state any) bool {
	return c.configErr == nil && c.contract != nil && c.contract.Verify(state) == nil
}

func (c *CheckpointVerifier) restore(bb *Blackboard) error {
	if !c.hadState {
		delete(bb.ChainState, c.StateKey)
		return nil
	}
	value, err := cloneCheckpointState(c.snapshot)
	if err == nil {
		bb.ChainState[c.StateKey] = value
	}
	return err
}

func (c *CheckpointVerifier) fail(bb *Blackboard, err error) int {
	c.terminal = -1
	bb.Outcome = "checkpoint_failed"
	bb.ChainState["checkpoint_error"] = err.Error()
	return c.terminal
}

// Run returns 1 for success, 0 for Running and -1 for failure.
func (c *CheckpointVerifier) Run(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	runID := ""
	if bb.runEvidence != nil {
		runID = bb.runEvidence.id
	}
	if c.bb != bb || c.runID != runID {
		c.bb, c.runID = bb, runID
		c.attempt, c.terminal, c.prepared = 0, 0, false
	}
	if bb.applyExecutionStop() {
		return -1
	}
	if c.terminal != 0 {
		return c.terminal
	}
	if bb.ChainState == nil {
		bb.ChainState = make(map[string]any)
	}
	if c.configErr != nil {
		return c.fail(bb, c.configErr)
	}
	if c.child == nil {
		return c.fail(bb, fmt.Errorf("checkpoint child unavailable"))
	}
	for c.attempt <= c.MaxRetries {
		if !c.prepared {
			value, exists := bb.ChainState[c.StateKey]
			var err error
			c.snapshot, err = cloneCheckpointState(value)
			if err != nil {
				return c.fail(bb, err)
			}
			c.hadState, c.prepared = exists, true
			c.effectStart = len(bb.EvidenceEffects())
		}
		status := c.child.Run(ctx)
		if bb.applyExecutionStop() {
			return -1
		}
		if status == 0 {
			return 0
		}
		err := c.contract.Verify(bb.ChainState[c.StateKey])
		if status == 1 && err == nil {
			delete(bb.ChainState, "checkpoint_error")
			c.terminal = 1
			return 1
		}
		if status != 1 {
			err = fmt.Errorf("checkpoint child failed with status %d", status)
		}
		// A state snapshot is not a filesystem rollback. A committed effect
		// followed by a failed gate requires reconciliation, never blind replay.
		for _, effect := range bb.EvidenceEffects()[c.effectStart:] {
			if effect.WriteCommitted {
				bb.stopExecution(err.Error(), &reliability.ExecutionUncertainError{Err: err})
				bb.applyExecutionStop()
				c.terminal = -1
				return -1
			}
		}
		if restoreErr := c.restore(bb); restoreErr != nil {
			return c.fail(bb, restoreErr)
		}
		c.attempt++
		c.prepared = false
		if c.attempt > c.MaxRetries {
			return c.fail(bb, err)
		}
		bb.ChainState["checkpoint_retry_reason"] = err.Error()
	}
	return c.fail(bb, fmt.Errorf("checkpoint retry budget exhausted"))
}
