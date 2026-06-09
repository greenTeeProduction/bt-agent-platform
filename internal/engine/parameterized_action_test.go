package engine

import (
	"testing"
)

func TestBindTemplate_Simple(t *testing.T) {
	result := bindTemplate("pick_${fruit}_from_${tree}", map[string]string{"fruit": "apple", "tree": "tree_3"})
	if result != "pick_apple_from_tree_3" {
		t.Errorf("expected 'pick_apple_from_tree_3', got '%s'", result)
	}
}

func TestBindTemplate_NoPlaceholders(t *testing.T) {
	result := bindTemplate("open_door", map[string]string{})
	if result != "open_door" {
		t.Errorf("expected 'open_door', got '%s'", result)
	}
}

func TestParamAction_Bind_FullTemplate(t *testing.T) {
	pa := ParamAction{
		GOAPAction: GOAPAction{
			Name:  "pick_${fruit}_from_${tree}",
			Cost:  1.0,
			Preconditions: map[string]bool{
				"near_${tree}": true,
			},
			Effects: map[string]bool{
				"has_${fruit}": true,
			},
			ActionFunc: "pick_${fruit}_${tree}",
		},
	}

	bound := pa.Bind(map[string]string{"fruit": "apple", "tree": "tree_3"})

	if bound.Name != "pick_apple_from_tree_3" {
		t.Errorf("expected 'pick_apple_from_tree_3', got '%s'", bound.Name)
	}
	if bound.Preconditions["near_tree_3"] != true {
		t.Error("expected precondition 'near_tree_3'")
	}
	if bound.Effects["has_apple"] != true {
		t.Error("expected effect 'has_apple'")
	}
	if bound.ActionFunc != "pick_apple_tree_3" {
		t.Errorf("expected 'pick_apple_tree_3', got '%s'", bound.ActionFunc)
	}
}

func TestParamPlannerNode_PlanWithContext(t *testing.T) {
	pp := &ParamPlannerNode{
		PlannerNode: PlannerNode{
			Goal: GOAPGoal{
				Name:       "has_fruit",
				Conditions: map[string]bool{"has_apple": true},
			},
			MaxDepth: 3,
			Mode:     "greedy",
		},
		Templates: []ParamAction{
			{
				GOAPAction: GOAPAction{
					Name:          "pick_${fruit}",
					Cost:          1.0,
					Preconditions: map[string]bool{},
					Effects:       map[string]bool{"has_${fruit}": true},
					ActionFunc:    "pick_${fruit}",
				},
			},
		},
	}

	plan := pp.PlanWithContext(
		map[string]bool{"has_apple": false},
		map[string]string{"fruit": "apple"},
	)

	if !plan.Complete {
		t.Errorf("expected complete plan, got incomplete")
	}
	if len(plan.Actions) != 1 || plan.Actions[0] != "pick_apple" {
		t.Errorf("expected [pick_apple], got %v", plan.Actions)
	}
}
