package engine

import (
	"testing"
)

func TestPlannerNode_Greedy_DirectPath(t *testing.T) {
	p := &PlannerNode{
		Goal: GOAPGoal{
			Name:       "door_open",
			Conditions: map[string]bool{"door_open": true},
		},
		Actions: []GOAPAction{
			{
				Name: "open_door", Cost: 1.0,
				Preconditions: map[string]bool{},
				Effects:       map[string]bool{"door_open": true},
				ActionFunc:    "open_door",
			},
		},
		MaxDepth: 3,
		Mode:     "greedy",
	}

	plan := p.Plan(map[string]bool{"door_open": false})
	if !plan.Complete {
		t.Errorf("expected complete plan, got incomplete")
	}
	if len(plan.Actions) != 1 || plan.Actions[0] != "open_door" {
		t.Errorf("expected [open_door], got %v", plan.Actions)
	}
}

func TestPlannerNode_AStar_MultiStep(t *testing.T) {
	p := &PlannerNode{
		Goal: GOAPGoal{
			Name:       "room_entered",
			Conditions: map[string]bool{"in_room": true},
		},
		Actions: []GOAPAction{
			{
				Name: "unlock_door", Cost: 1.0,
				Preconditions: map[string]bool{"has_key": true},
				Effects:       map[string]bool{"door_unlocked": true},
				ActionFunc:    "unlock_door",
			},
			{
				Name: "pickup_key", Cost: 1.0,
				Preconditions: map[string]bool{},
				Effects:       map[string]bool{"has_key": true},
				ActionFunc:    "pickup_key",
			},
			{
				Name: "enter_room", Cost: 1.0,
				Preconditions: map[string]bool{"door_unlocked": true},
				Effects:       map[string]bool{"in_room": true},
				ActionFunc:    "enter_room",
			},
		},
		MaxDepth: 5,
		Mode:     "search",
	}

	plan := p.Plan(map[string]bool{})
	if !plan.Complete {
		t.Errorf("expected complete plan, got incomplete")
	}
	if len(plan.Actions) != 3 {
		t.Errorf("expected 3 actions, got %d: %v", len(plan.Actions), plan.Actions)
	}
}

func TestPlannerNode_MaxDepth_Exceeded(t *testing.T) {
	p := &PlannerNode{
		Goal: GOAPGoal{
			Name:       "impossible",
			Conditions: map[string]bool{"done": true},
		},
		Actions: []GOAPAction{
			{
				Name: "do_nothing", Cost: 1.0,
				Preconditions: map[string]bool{},
				Effects:       map[string]bool{},
				ActionFunc:    "do_nothing",
			},
		},
		MaxDepth: 2,
		Mode:     "search",
	}

	plan := p.Plan(map[string]bool{})
	if plan.Complete {
		t.Errorf("expected incomplete plan (goal unreachable), got complete")
	}
	if plan.Depth > 2 {
		t.Errorf("expected max depth 2, got %d", plan.Depth)
	}
}

func TestPlannerNode_Unsatisfiable_NoActions(t *testing.T) {
	p := &PlannerNode{
		Goal: GOAPGoal{
			Name:       "impossible",
			Conditions: map[string]bool{"magic": true},
		},
		Actions:  []GOAPAction{},
		MaxDepth: 5,
		Mode:     "search",
	}

	plan := p.Plan(map[string]bool{})
	if plan.Complete {
		t.Errorf("expected incomplete plan")
	}
}
