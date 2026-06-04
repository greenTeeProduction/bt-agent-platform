package engine

import (
	"sync"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// BuildReactiveParallel builds a go-bt Command for a ReactiveParallel node.
// Plan #3: Parallel execution with monitoring — monitor children can cancel
// action children when they detect failure conditions.
//
// Modes:
//   - "all": Success when ALL children succeed. Failure when ANY fails.
//   - "any": Success when any ONE child succeeds. Cancel remaining.
//   - "race": Return first terminal (Success/Failure) state. Cancel rest.
//   - "monitor": Monitor children run continuously; if any monitor fails,
//     all action children are cancelled and overall fails.
//     If all monitors succeed and all actions succeed, overall succeeds.
func BuildReactiveParallel(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	if len(node.Children) == 0 {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int { return 1 })
	}

	children := make([]btcore.Command[Blackboard], len(node.Children))
	for i := range node.Children {
		children[i] = buildNode(&node.Children[i], bb, node.Name)
	}

	mode := "all"
	monitorIndices := make(map[int]bool)
	if node.Metadata != nil {
		if m, ok := node.Metadata["mode"].(string); ok {
			mode = m
		}
		if raw, ok := node.Metadata["monitor_indices"]; ok {
			if indices, ok := raw.([]interface{}); ok {
				for _, idx := range indices {
					if i, ok := idx.(float64); ok {
						monitorIndices[int(i)] = true
					}
				}
			}
		}
	}

	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		n := len(children)
		resultCh := make(chan int, n)
		stopCh := make(chan struct{}, n)
		var wg sync.WaitGroup

		switch mode {
		case "monitor":
			// Run monitors and actions in parallel. Monitors that fail signal
			// cancellation of action children.
			for i, child := range children {
				wg.Add(1)
				go func(idx int, cmd btcore.Command[Blackboard], isMonitor bool) {
					defer wg.Done()
					// Create a context copy for the goroutine
					localCtx := &btcore.BTContext[Blackboard]{Blackboard: bb}
					result := cmd.Run(localCtx)
					if isMonitor && result <= 0 {
						// Monitor failed/signalled — cancel actions
						select {
						case stopCh <- struct{}{}:
						default:
						}
					}
					resultCh <- result
				}(i, child, monitorIndices[i])
			}
			go func() {
				wg.Wait()
				close(resultCh)
			}()

			results := make([]int, n)
			for i := range results {
				results[i] = 1
			}
			idx := 0
			for r := range resultCh {
				results[idx] = r
				idx++
			}
			// Check monitors
			for i := range results {
				if monitorIndices[i] && results[i] <= 0 {
					return -1 // Monitor failed → overall failure
				}
			}
			// Check actions
			for i, r := range results {
				if !monitorIndices[i] && r <= 0 {
					return -1 // Action failed
				}
			}
			return 1

		case "race":
			// Run all, return first terminal result, cancel rest
			for i, child := range children {
				wg.Add(1)
				go func(idx int, cmd btcore.Command[Blackboard]) {
					defer wg.Done()
					localCtx := &btcore.BTContext[Blackboard]{Blackboard: bb}
					result := cmd.Run(localCtx)
					resultCh <- result
				}(i, child)
			}
			// Wait for first terminal result
			var firstResult int
			select {
			case r := <-resultCh:
				if r != 0 {
					firstResult = r
					close(stopCh) // Cancel remaining goroutines
					go func() { wg.Wait(); close(resultCh) }()
					return firstResult
				}
			}
			// If first was Running, wait for next
			select {
			case r := <-resultCh:
				firstResult = r
			}
			close(stopCh)
			go func() { wg.Wait(); close(resultCh) }()
			return firstResult

		case "any":
			// Success when any ONE succeeds. Cancel remaining.
			for i, child := range children {
				wg.Add(1)
				go func(idx int, cmd btcore.Command[Blackboard]) {
					defer wg.Done()
					localCtx := &btcore.BTContext[Blackboard]{Blackboard: bb}
					result := cmd.Run(localCtx)
					// Send result before stop signal to avoid Go select
					// randomly picking the stopCh read and discarding result.
					resultCh <- result
					if result == 1 {
						select {
						case stopCh <- struct{}{}:
						default:
						}
					}
				}(i, child)
			}
			go func() {
				wg.Wait()
				close(resultCh)
			}()

			allFail := true
			for r := range resultCh {
				if r == 1 {
					return 1
				}
				if r == 0 {
					allFail = false
				}
			}
			if allFail {
				return -1
			}
			return 0

		default:
			// "all" mode: Success when ALL succeed, failure when ANY fails
			for i, child := range children {
				wg.Add(1)
				go func(idx int, cmd btcore.Command[Blackboard]) {
					defer wg.Done()
					localCtx := &btcore.BTContext[Blackboard]{Blackboard: bb}
					result := cmd.Run(localCtx)
					resultCh <- result
				}(i, child)
			}
			go func() {
				wg.Wait()
				close(resultCh)
			}()

			var failureCount int
			for r := range resultCh {
				if r == -1 || r == StatusAborted {
					failureCount++
				}
			}
			if failureCount > 0 {
				return -1
			}
			return 1
		}
	})
}

// runReactiveParallel executes child commands in parallel according to the given mode.
// This is the standalone implementation used by both BuildReactiveParallel and tests.
func runReactiveParallel(children []btcore.Command[Blackboard], mode ParallelMode, monitorIndices, actionIndices []int, cancelOnMonitor bool, ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	n := len(children)
	resultCh := make(chan int, n)
	stopCh := make(chan struct{}, n)
	var wg sync.WaitGroup

	// Build monitor set from indices
	isMonitor := make(map[int]bool)
	for _, idx := range monitorIndices {
		if idx >= 0 && idx < n {
			isMonitor[idx] = true
		}
	}
	isAction := make(map[int]bool)
	for _, idx := range actionIndices {
		if idx >= 0 && idx < n {
			isAction[idx] = true
		}
	}

	switch mode {
	case ParallelMonitor:
		for i, child := range children {
			wg.Add(1)
			go func(idx int, cmd btcore.Command[Blackboard], monitor bool) {
				defer wg.Done()
				localCtx := &btcore.BTContext[Blackboard]{Blackboard: bb}
				result := cmd.Run(localCtx)
				if monitor && result <= 0 && cancelOnMonitor {
					select {
					case stopCh <- struct{}{}:
					default:
					}
				}
				resultCh <- result
			}(i, child, isMonitor[i])
		}
		go func() { wg.Wait(); close(resultCh) }()
		results := make([]int, n)
		idx := 0
		for r := range resultCh {
			if idx < n {
				results[idx] = r
				idx++
			}
		}
		for i := range results {
			if isMonitor[i] && results[i] <= 0 {
				return -1
			}
		}
		for i, r := range results {
			if isAction[i] && r <= 0 {
				return -1
			}
		}
		return 1

	case ParallelRace:
		for i, child := range children {
			wg.Add(1)
			go func(idx int, cmd btcore.Command[Blackboard]) {
				defer wg.Done()
				localCtx := &btcore.BTContext[Blackboard]{Blackboard: bb}
				result := cmd.Run(localCtx)
				resultCh <- result
			}(i, child)
		}
		var firstResult int
		select {
		case r := <-resultCh:
			if r != 0 {
				firstResult = r
				close(stopCh)
				go func() { wg.Wait(); close(resultCh) }()
				return firstResult
			}
		}
		select {
		case r := <-resultCh:
			firstResult = r
		}
		close(stopCh)
		go func() { wg.Wait(); close(resultCh) }()
		return firstResult

	case ParallelAny:
		for i, child := range children {
			wg.Add(1)
			go func(idx int, cmd btcore.Command[Blackboard]) {
				defer wg.Done()
				localCtx := &btcore.BTContext[Blackboard]{Blackboard: bb}
				result := cmd.Run(localCtx)
				// Send result before stop signal to avoid Go select
				// randomly picking the stopCh read and discarding result.
				resultCh <- result
				if result == 1 {
					select {
					case stopCh <- struct{}{}:
					default:
					}
				}
			}(i, child)
		}
		go func() { wg.Wait(); close(resultCh) }()
		allFail := true
		for r := range resultCh {
			if r == 1 {
				return 1
			}
			if r == 0 {
				allFail = false
			}
		}
		if allFail {
			return -1
		}
		return 0

	default:
		// ParallelAll
		for i, child := range children {
			wg.Add(1)
			go func(idx int, cmd btcore.Command[Blackboard]) {
				defer wg.Done()
				localCtx := &btcore.BTContext[Blackboard]{Blackboard: bb}
				result := cmd.Run(localCtx)
				resultCh <- result
			}(i, child)
		}
		go func() { wg.Wait(); close(resultCh) }()
		failureCount := 0
		for r := range resultCh {
			if r == -1 || r == StatusAborted {
				failureCount++
			}
		}
		if failureCount > 0 {
			return -1
		}
		return 1
	}
}
