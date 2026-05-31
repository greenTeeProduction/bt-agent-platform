package goap

import (
	"testing"
)

func TestGoalQueueAddAndSelect(t *testing.T) {
	gq := NewGoalQueue()

	gq.Add(NewGoal("eat", 0.5, WorldState{"hungry": false}))
	gq.Add(NewGoal("sleep", 0.9, WorldState{"tired": false}))
	gq.Add(NewGoal("work", 0.3, WorldState{"has_money": true}))

	// All unsatisfied from empty state
	state := WorldState{}
	selected := gq.SelectGoal(state)
	if selected == nil {
		t.Fatal("should select a goal")
	}
	if selected.Name != "sleep" {
		t.Errorf("expected 'sleep' (priority 0.9), got %q (priority %.1f)", selected.Name, selected.Priority)
	}
}

func TestGoalQueueSelectSatisfied(t *testing.T) {
	gq := NewGoalQueue()

	gq.Add(NewGoal("eat", 0.5, WorldState{"hungry": false}))
	gq.Add(NewGoal("sleep", 0.9, WorldState{"tired": false}))

	// sleep is satisfied
	state := WorldState{"tired": false}
	selected := gq.SelectGoal(state)
	if selected == nil {
		t.Fatal("should select the unsatisfied goal")
	}
	if selected.Name != "eat" {
		t.Errorf("expected 'eat' (sleep satisfied), got %q", selected.Name)
	}
}

func TestGoalQueueAllSatisfied(t *testing.T) {
	gq := NewGoalQueue()

	gq.Add(NewGoal("eat", 0.5, WorldState{"hungry": false}))
	gq.Add(NewGoal("sleep", 0.9, WorldState{"tired": false}))

	state := WorldState{"hungry": false, "tired": false}
	selected := gq.SelectGoal(state)
	if selected != nil {
		t.Errorf("expected nil when all satisfied, got %q", selected.Name)
	}
}

func TestGoalQueueEmpty(t *testing.T) {
	gq := NewGoalQueue()

	if !gq.IsEmpty() {
		t.Error("should be empty")
	}

	selected := gq.SelectGoal(WorldState{})
	if selected != nil {
		t.Error("empty queue should return nil")
	}
}

func TestGoalQueueRemove(t *testing.T) {
	gq := NewGoalQueue()

	gq.Add(NewGoal("eat", 0.5, WorldState{"hungry": false}))
	gq.Add(NewGoal("sleep", 0.9, WorldState{"tired": false}))

	if gq.Len() != 2 {
		t.Errorf("expected 2 goals, got %d", gq.Len())
	}

	if !gq.Remove("eat") {
		t.Error("should remove existing goal")
	}
	if gq.Len() != 1 {
		t.Errorf("expected 1 goal after remove, got %d", gq.Len())
	}

	if gq.Remove("nonexistent") {
		t.Error("should return false for non-existent goal")
	}

	selected := gq.SelectGoal(WorldState{})
	if selected == nil || selected.Name != "sleep" {
		t.Errorf("expected 'sleep' remaining, got %v", selected)
	}
}

func TestGoalQueueReprioritize(t *testing.T) {
	gq := NewGoalQueue()

	gq.Add(NewGoal("eat", 0.5, WorldState{"hungry": false}))
	gq.Add(NewGoal("sleep", 0.3, WorldState{"tired": false}))

	// sleep should be lower priority
	selected := gq.SelectGoal(WorldState{})
	if selected == nil || selected.Name != "eat" {
		t.Fatalf("expected 'eat' initially, got %v", selected)
	}

	// Reprioritize sleep higher
	err := gq.Reprioritize("sleep", 0.9)
	if err != nil {
		t.Fatalf("reprioritize failed: %v", err)
	}

	selected = gq.SelectGoal(WorldState{})
	if selected == nil || selected.Name != "sleep" {
		t.Errorf("expected 'sleep' after reprioritize (priority 0.9 > 0.5), got %v", selected)
	}
}

func TestGoalQueueReprioritizeNotFound(t *testing.T) {
	gq := NewGoalQueue()
	err := gq.Reprioritize("nonexistent", 1.0)
	if err == nil {
		t.Error("should error on non-existent goal")
	}
}

func TestGoalQueueAddReplace(t *testing.T) {
	gq := NewGoalQueue()

	gq.Add(NewGoal("task", 0.5, WorldState{"done": false}))
	gq.Add(NewGoal("task", 0.9, WorldState{"done": false}))

	if gq.Len() != 1 {
		t.Errorf("re-adding same name should replace, got %d goals", gq.Len())
	}

	g := gq.Get("task")
	if g == nil || g.Priority != 0.9 {
		t.Errorf("expected priority 0.9 after replace, got %v", g)
	}
}

func TestGoalQueueGet(t *testing.T) {
	gq := NewGoalQueue()

	gq.Add(NewGoal("eat", 0.5, WorldState{"hungry": false}))

	g := gq.Get("eat")
	if g == nil || g.Name != "eat" {
		t.Error("should retrieve existing goal")
	}

	if gq.Get("nonexistent") != nil {
		t.Error("should return nil for missing goal")
	}
}

func TestGoalQueueAll(t *testing.T) {
	gq := NewGoalQueue()

	gq.Add(NewGoal("c", 0.1, WorldState{"c": true}))
	gq.Add(NewGoal("a", 0.9, WorldState{"a": true}))
	gq.Add(NewGoal("b", 0.5, WorldState{"b": true}))

	all := gq.All()
	if len(all) != 3 {
		t.Fatalf("expected 3 goals, got %d", len(all))
	}

	if all[0].Name != "a" || all[1].Name != "b" || all[2].Name != "c" {
		t.Errorf("expected priority order a(0.9) > b(0.5) > c(0.1), got %v", names(all))
	}
}

func TestGoalQueueInterleaveCheck(t *testing.T) {
	gq := NewGoalQueue()

	gq.Add(NewGoal("eat", 0.3, WorldState{"hungry": false}))
	gq.Add(NewGoal("sleep", 0.9, WorldState{"tired": false}))
	gq.Add(NewGoal("work", 0.5, WorldState{"busy": false}))

	// Currently working on low-priority "eat"
	currentGoal := gq.Get("eat")
	state := WorldState{"hungry": true, "tired": true, "busy": true}

	// InterleaveCheck should suggest switching to sleep (higher priority)
	newGoal := gq.InterleaveCheck(state, currentGoal)
	if newGoal == nil {
		t.Fatal("should suggest switching to higher-priority goal")
	}
	if newGoal.Name != "sleep" {
		t.Errorf("expected 'sleep' (priority 0.9 > 0.3), got %q", newGoal.Name)
	}
}

func TestGoalQueueInterleaveCurrentSatisfied(t *testing.T) {
	gq := NewGoalQueue()

	gq.Add(NewGoal("eat", 0.5, WorldState{"hungry": false}))
	gq.Add(NewGoal("work", 0.8, WorldState{"busy": false}))

	currentGoal := gq.Get("eat")
	state := WorldState{"hungry": false, "busy": true}

	// eat is now satisfied, should pick next goal
	newGoal := gq.InterleaveCheck(state, currentGoal)
	if newGoal == nil || newGoal.Name != "work" {
		t.Errorf("expected 'work' after eat satisfied, got %v", newGoal)
	}
}

func TestGoalQueueInterleaveNoSwitch(t *testing.T) {
	gq := NewGoalQueue()

	gq.Add(NewGoal("sleep", 0.9, WorldState{"tired": false}))
	gq.Add(NewGoal("work", 0.5, WorldState{"busy": false}))

	currentGoal := gq.Get("sleep")
	state := WorldState{"tired": true, "busy": true}

	// No higher-priority goal than sleep
	newGoal := gq.InterleaveCheck(state, currentGoal)
	if newGoal != nil {
		t.Errorf("should not switch when current is highest priority, got %q", newGoal.Name)
	}
}

func TestGoalQueueInterleaveNilCurrent(t *testing.T) {
	gq := NewGoalQueue()

	gq.Add(NewGoal("eat", 0.5, WorldState{"hungry": false}))

	state := WorldState{}
	newGoal := gq.InterleaveCheck(state, nil)
	if newGoal == nil || newGoal.Name != "eat" {
		t.Errorf("nil current should pick the first goal, got %v", newGoal)
	}
}

func TestGoalQueueSatisfiedCount(t *testing.T) {
	gq := NewGoalQueue()

	gq.Add(NewGoal("eat", 0.5, WorldState{"hungry": false}))
	gq.Add(NewGoal("sleep", 0.9, WorldState{"tired": false}))
	gq.Add(NewGoal("work", 0.3, WorldState{"busy": false}))

	state := WorldState{"hungry": false}
	if count := gq.SatisfiedCount(state); count != 1 {
		t.Errorf("expected 1 satisfied, got %d", count)
	}

	state = WorldState{"hungry": false, "tired": false}
	if count := gq.SatisfiedCount(state); count != 2 {
		t.Errorf("expected 2 satisfied, got %d", count)
	}
}

func TestGoalQueueClear(t *testing.T) {
	gq := NewGoalQueue()

	gq.Add(NewGoal("eat", 0.5, WorldState{"hungry": false}))
	gq.Add(NewGoal("sleep", 0.9, WorldState{"tired": false}))

	gq.Clear()

	if !gq.IsEmpty() {
		t.Error("should be empty after clear")
	}
	if gq.Len() != 0 {
		t.Errorf("expected 0 after clear, got %d", gq.Len())
	}
}

func TestGoalQueueSelectAllUnsatisfied(t *testing.T) {
	gq := NewGoalQueue()

	gq.Add(NewGoal("eat", 0.5, WorldState{"hungry": false}))
	gq.Add(NewGoal("sleep", 0.9, WorldState{"tired": false}))
	gq.Add(NewGoal("work", 0.3, WorldState{"busy": false}))

	state := WorldState{"hungry": false} // only eat satisfied
	unsatisfied := gq.SelectAllUnsatisfied(state)

	if len(unsatisfied) != 2 {
		t.Fatalf("expected 2 unsatisfied, got %d: %v", len(unsatisfied), names2(unsatisfied))
	}
	if unsatisfied[0].Name != "sleep" {
		t.Errorf("expected 'sleep' first (priority 0.9), got %q", unsatisfied[0].Name)
	}
	if unsatisfied[1].Name != "work" {
		t.Errorf("expected 'work' second (priority 0.3), got %q", unsatisfied[1].Name)
	}
}

func TestNewGoalQueueFrom(t *testing.T) {
	goals := []*Goal{
		NewGoal("eat", 0.5, WorldState{"hungry": false}),
		NewGoal("sleep", 0.9, WorldState{"tired": false}),
	}
	gq := NewGoalQueueFrom(goals...)

	if gq.Len() != 2 {
		t.Errorf("expected 2 goals, got %d", gq.Len())
	}
}

func TestGoalIsSatisfied(t *testing.T) {
	g := NewGoal("test", 1.0, WorldState{"x": true, "y": "done"})

	if !g.IsSatisfied(WorldState{"x": true, "y": "done"}) {
		t.Error("should be satisfied when all conditions match")
	}
	if g.IsSatisfied(WorldState{"x": false, "y": "done"}) {
		t.Error("should not be satisfied when condition mismatches")
	}
	if g.IsSatisfied(WorldState{"x": true}) {
		t.Error("should not be satisfied when key is missing")
	}
}

// helpers
func names(goals []*Goal) []string {
	n := make([]string, len(goals))
	for i, g := range goals {
		n[i] = g.Name
	}
	return n
}

func names2(goals []*Goal) []string { return names(goals) }
