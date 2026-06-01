package engine

import (
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// ParallelMode defines the execution mode for ReactiveParallel.
type ParallelMode string

const (
	ParallelAll     ParallelMode = "all"     // All succeed → success; any fail → failure
	ParallelAny     ParallelMode = "any"     // First success → success; cancel rest
	ParallelRace    ParallelMode = "race"    // First terminal → return that; cancel rest
	ParallelMonitor ParallelMode = "monitor" // Monitors cancel actions on failure
)

// BuildReactiveParallel builds a ReactiveParallel composite node.
// Accepts "AbortOnEvent" and "ReactiveParallel" types.
func BuildReactiveParallel(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	// Read mode from metadata
	mode := ParallelAll
	monitorIndices := []int{}
	actionIndices := []int{}
	cancelOnMonitorFailure := true

	if node.Metadata != nil {
		if m, ok := node.Metadata["mode"].(string); ok {
			mode = ParallelMode(m)
		}
		if mi, ok := node.Metadata["monitor_indices"]; ok {
			monitorIndices = intSliceFromInterface(mi)
		}
		if ai, ok := node.Metadata["action_indices"]; ok {
			actionIndices = intSliceFromInterface(ai)
		}
		if c, ok := node.Metadata["cancel_on_monitor_failure"].(bool); ok {
			cancelOnMonitorFailure = c
		}
	}

	// Handle "AbortOnEvent" type — treat as a single-monitor ReactiveParallel
	if node.Type == "AbortOnEvent" {
		return BuildEventDrivenAbort(node, bb)
	}

	// Pre-build all children
	children := make([]btcore.Command[Blackboard], len(node.Children))
	for i := range node.Children {
		children[i] = buildNode(&node.Children[i], bb, node.Name)
	}

	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		return runReactiveParallel(children, mode, monitorIndices, actionIndices, cancelOnMonitorFailure, ctx)
	})
}

// runReactiveParallel executes children according to the parallel mode.
// Handles all 4 modes: all, any, race, monitor.
func runReactiveParallel(
	children []btcore.Command[Blackboard],
	mode ParallelMode,
	monitorIdx, actionIdx []int,
	cancelOnMonitorFailure bool,
	ctx *btcore.BTContext[Blackboard],
) int {
	switch mode {
	case ParallelAll:
		allSuccess := true
		for _, child := range children {
			result := child.Run(ctx)
			if result == -1 {
				allSuccess = false
			}
		}
		if allSuccess {
			return 1
		}
		return -1

	case ParallelAny:
		for _, child := range children {
			result := child.Run(ctx)
			if result == 1 {
				return 1 // First success wins
			}
		}
		return -1

	case ParallelRace:
		for _, child := range children {
			result := child.Run(ctx)
			if result != 0 { // Terminal state (success or failure)
				return result
			}
		}
		return 0 // Still running

	case ParallelMonitor:
		// Run monitors first
		for _, idx := range monitorIdx {
			if idx < len(children) {
				result := children[idx].Run(ctx)
				if result == -1 {
					// Monitor failed — cancel all actions
					return -1
				}
			}
		}
		// Run action children
		allSuccess := true
		for _, idx := range actionIdx {
			if idx < len(children) {
				result := children[idx].Run(ctx)
				if result == -1 {
					if cancelOnMonitorFailure {
						return -1
					}
					allSuccess = false
				}
			}
		}
		if allSuccess {
			return 1
		}
		return -1

	default:
		// Fallback: sequential execution (like Sequence)
		for _, child := range children {
			result := child.Run(ctx)
			if result == -1 {
				return -1
			}
		}
		return 1
	}
}

// intSliceFromInterface converts an interface{} to an []int slice.
// Handles both []interface{} and []float64 (common from JSON unmarshalling).
func intSliceFromInterface(v interface{}) []int {
	switch items := v.(type) {
	case []interface{}:
		result := make([]int, 0, len(items))
		for _, item := range items {
			switch n := item.(type) {
			case float64:
				result = append(result, int(n))
			case int:
				result = append(result, n)
			}
		}
		return result
	case []float64:
		result := make([]int, len(items))
		for i, n := range items {
			result[i] = int(n)
		}
		return result
	}
	return nil
}
