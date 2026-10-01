package engine

import (
	"errors"
	"sync"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

// executionStop is shared by parallel branch blackboards. Already admitted
// branches may finish, but no later node may replay or replace terminal work.
type executionStop struct {
	mu     sync.Mutex
	err    error
	output string
}

// ExecutionError preserves a delegation diagnostic as a typed error across
// the tree's integer status interface. A string in Result is not sufficient
// evidence for a scheduler to decide whether repeating work is safe.
func (bb *Blackboard) ExecutionError() error {
	if bb == nil || bb.executionStop == nil {
		return nil
	}
	bb.executionStop.mu.Lock()
	defer bb.executionStop.mu.Unlock()
	return bb.executionStop.err
}

func (bb *Blackboard) stopExecution(output string, err error) {
	if !reliability.IsExecutionTerminalError(err) {
		return
	}
	if bb.executionStop == nil {
		bb.executionStop = &executionStop{}
	}
	bb.executionLocalError = err
	stop := bb.executionStop
	stop.mu.Lock()
	defer stop.mu.Unlock()
	joined := errors.Join(stop.err, err)
	if stop.err == nil || reliability.ExecutionStopOutcome(joined) != reliability.ExecutionStopOutcome(stop.err) || reliability.IsExecutionUncertainError(err) {
		stop.output = output
	}
	stop.err = joined
}

func (bb *Blackboard) applyExecutionStop() bool {
	if bb == nil || bb.executionStop == nil {
		return false
	}
	stop := bb.executionStop
	stop.mu.Lock()
	defer stop.mu.Unlock()
	if stop.err == nil {
		return false
	}
	bb.Result = stop.output
	if bb.Result == "" {
		bb.Result = stop.err.Error()
	}
	// A completed child does not mean the remaining tree ran. Preserve its
	// output, but record the surrounding workflow as aborted.
	bb.Outcome = reliability.ExecutionStopOutcome(stop.err)
	return true
}
