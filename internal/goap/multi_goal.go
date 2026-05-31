package goap

import (
	"container/heap"
	"fmt"
	"sort"
	"sync"
)

// goalEntry is an item in the GoalQueue heap.
type goalEntry struct {
	goal  *Goal
	index int // original insertion index for stable ordering
}

// goalHeap implements container/heap.Interface for priority-ordered goals.
// Higher priority goals sort first; ties are broken by insertion order.
type goalHeap []*goalEntry

func (h goalHeap) Len() int { return len(h) }

func (h goalHeap) Less(i, j int) bool {
	// Higher Priority first; break ties with insertion index
	if h[i].goal.Priority != h[j].goal.Priority {
		return h[i].goal.Priority > h[j].goal.Priority
	}
	return h[i].index < h[j].index
}

func (h goalHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *goalHeap) Push(x interface{}) {
	*h = append(*h, x.(*goalEntry))
}

func (h *goalHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// GoalQueue manages a priority-ordered set of goals.
// Goals are ordered by Priority (highest first), with stable insertion order for ties.
// The queue supports selecting the highest-priority unsatisfied goal,
// interleaved goal switching, and dynamic reprioritization.
type GoalQueue struct {
	mu      sync.RWMutex
	entries map[string]*goalEntry // name -> entry for O(1) lookup
	heap    goalHeap
	counter int // insertion counter for stable ordering
}

// NewGoalQueue creates an empty goal queue.
func NewGoalQueue() *GoalQueue {
	return &GoalQueue{
		entries: make(map[string]*goalEntry),
		heap:    make(goalHeap, 0),
	}
}

// NewGoalQueueFrom creates a goal queue initialized with the given goals.
func NewGoalQueueFrom(goals ...*Goal) *GoalQueue {
	gq := NewGoalQueue()
	for _, g := range goals {
		gq.Add(g)
	}
	return gq
}

// Add inserts a goal into the queue. If a goal with the same name already
// exists, it is replaced (priority updated).
func (gq *GoalQueue) Add(goal *Goal) {
	gq.mu.Lock()
	defer gq.mu.Unlock()
	gq.addLocked(goal)
}

func (gq *GoalQueue) addLocked(goal *Goal) {
	// If goal already exists, remove it first
	if existing, ok := gq.entries[goal.Name]; ok {
		gq.removeByNameLocked(goal.Name)
		_ = existing
	}

	entry := &goalEntry{
		goal:  goal,
		index: gq.counter,
	}
	gq.counter++

	gq.entries[goal.Name] = entry
	heap.Push(&gq.heap, entry)
}

// Remove removes a goal by name from the queue.
func (gq *GoalQueue) Remove(name string) bool {
	gq.mu.Lock()
	defer gq.mu.Unlock()
	return gq.removeByNameLocked(name)
}

func (gq *GoalQueue) removeByNameLocked(name string) bool {
	entry, ok := gq.entries[name]
	if !ok {
		return false
	}

	// Find the entry in the heap and remove it
	for i, e := range gq.heap {
		if e == entry {
			heap.Remove(&gq.heap, i)
			break
		}
	}
	delete(gq.entries, name)
	return true
}

// Reprioritize updates the priority of an existing goal.
// Returns an error if the goal is not found.
func (gq *GoalQueue) Reprioritize(name string, newPriority float64) error {
	gq.mu.Lock()
	defer gq.mu.Unlock()

	entry, ok := gq.entries[name]
	if !ok {
		return fmt.Errorf("goal %q not found", name)
	}

	entry.goal.Priority = newPriority

	// Reorder: remove and re-insert to fix heap position
	for i, e := range gq.heap {
		if e == entry {
			heap.Remove(&gq.heap, i)
			break
		}
	}
	heap.Push(&gq.heap, entry)
	return nil
}

// SelectGoal returns the highest-priority goal that is NOT yet satisfied
// by the given state. Returns nil if all goals are satisfied or the queue is empty.
func (gq *GoalQueue) SelectGoal(state WorldState) *Goal {
	gq.mu.RLock()
	defer gq.mu.RUnlock()

	if len(gq.heap) == 0 {
		return nil
	}

	// The heap is ordered by priority, but we need to find the first
	// unsatisfied goal, which may not be at the top if the top goal
	// is already satisfied.
	// Strategy: iterate through a sorted copy — this is O(n log n) but
	// goal queues are typically small (<100 goals).
	sorted := make([]*goalEntry, len(gq.heap))
	copy(sorted, gq.heap)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].goal.Priority != sorted[j].goal.Priority {
			return sorted[i].goal.Priority > sorted[j].goal.Priority
		}
		return sorted[i].index < sorted[j].index
	})

	for _, entry := range sorted {
		if !entry.goal.IsSatisfied(state) {
			return entry.goal
		}
	}
	return nil
}

// SelectAllUnsatisfied returns all goals that are not yet satisfied,
// ordered by priority (highest first).
func (gq *GoalQueue) SelectAllUnsatisfied(state WorldState) []*Goal {
	gq.mu.RLock()
	defer gq.mu.RUnlock()

	var result []*Goal
	for _, entry := range gq.heap {
		if !entry.goal.IsSatisfied(state) {
			result = append(result, entry.goal)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Priority > result[j].Priority
	})
	return result
}

// Get returns a goal by name, or nil if not found.
func (gq *GoalQueue) Get(name string) *Goal {
	gq.mu.RLock()
	defer gq.mu.RUnlock()

	if entry, ok := gq.entries[name]; ok {
		return entry.goal
	}
	return nil
}

// Len returns the number of goals in the queue.
func (gq *GoalQueue) Len() int {
	gq.mu.RLock()
	defer gq.mu.RUnlock()
	return len(gq.entries)
}

// IsEmpty returns true if the queue has no goals.
func (gq *GoalQueue) IsEmpty() bool {
	return gq.Len() == 0
}

// All returns all goals in priority order (highest first).
func (gq *GoalQueue) All() []*Goal {
	gq.mu.RLock()
	defer gq.mu.RUnlock()

	result := make([]*Goal, 0, len(gq.entries))
	for _, entry := range gq.heap {
		result = append(result, entry.goal)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Priority > result[j].Priority
	})
	return result
}

// SatisfiedCount returns the number of goals satisfied by the given state.
func (gq *GoalQueue) SatisfiedCount(state WorldState) int {
	gq.mu.RLock()
	defer gq.mu.RUnlock()

	count := 0
	for _, entry := range gq.heap {
		if entry.goal.IsSatisfied(state) {
			count++
		}
	}
	return count
}

// InterleaveCheck determines if the agent should switch to a higher-priority
// goal that has emerged. Returns the new goal if switching is warranted,
// or nil to continue with the current goal.
//
// This implements GOBT-style goal interleaving: if a higher-priority goal
// becomes active during execution of a lower-priority goal, the agent can
// preempt and switch.
func (gq *GoalQueue) InterleaveCheck(state WorldState, currentGoal *Goal) *Goal {
	gq.mu.RLock()
	defer gq.mu.RUnlock()

	if currentGoal == nil {
		return gq.selectLocked(state)
	}

	// If current goal is now satisfied, pick the next one
	if currentGoal.IsSatisfied(state) {
		return gq.selectLocked(state)
	}

	// Check if any higher-priority unsatisfied goal exists
	for _, entry := range gq.heap {
		if entry.goal.Priority > currentGoal.Priority && !entry.goal.IsSatisfied(state) {
			return entry.goal
		}
	}
	return nil // stick with current goal
}

// selectLocked is the internal (non-locking) highest-priority unsatisfied goal selector.
func (gq *GoalQueue) selectLocked(state WorldState) *Goal {
	for _, entry := range gq.heap {
		if !entry.goal.IsSatisfied(state) {
			return entry.goal
		}
	}
	return nil
}

// Clear removes all goals from the queue.
func (gq *GoalQueue) Clear() {
	gq.mu.Lock()
	defer gq.mu.Unlock()
	gq.entries = make(map[string]*goalEntry)
	gq.heap = make(goalHeap, 0)
	gq.counter = 0
}
