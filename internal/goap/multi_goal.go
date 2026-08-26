package goap

import (
	"cmp"
	"container/heap"
	"fmt"
	"slices"
	"sync"
)

// goalEntry is an item in the GoalQueue heap.
type goalEntry struct {
	goal  *Goal
	index int // original insertion index for stable ordering
}

// goalHeap implements container/heap.Interface for priority-ordered goals.
// Higher priority goals sort first; priority ties are broken by earlier
// deadline (0 = no deadline, sorts last), then by insertion order.
type goalHeap []*goalEntry

func (h goalHeap) Len() int { return len(h) }

func (h goalHeap) Less(i, j int) bool {
	return entryLess(h[i], h[j])
}

// entryLess is the single ordering rule for goal entries: Priority desc,
// then Deadline asc with 0 (none) last, then insertion order.
func entryLess(a, b *goalEntry) bool {
	if a.goal.Priority != b.goal.Priority {
		return a.goal.Priority > b.goal.Priority
	}
	if a.goal.Deadline != b.goal.Deadline {
		if a.goal.Deadline == 0 {
			return false
		}
		if b.goal.Deadline == 0 {
			return true
		}
		return a.goal.Deadline < b.goal.Deadline
	}
	return a.index < b.index
}

func (h goalHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *goalHeap) Push(x any) {
	*h = append(*h, x.(*goalEntry))
}

func (h *goalHeap) Pop() any {
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
	sorted := slices.Clone(gq.heap)
	slices.SortFunc(sorted, func(a, b *goalEntry) int {
		if entryLess(a, b) {
			return -1
		}
		if entryLess(b, a) {
			return 1
		}
		return 0
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

	slices.SortFunc(result, func(a, b *Goal) int {
		return cmp.Compare(b.Priority, a.Priority)
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

	slices.SortFunc(result, func(a, b *Goal) int {
		return cmp.Compare(b.Priority, a.Priority)
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
