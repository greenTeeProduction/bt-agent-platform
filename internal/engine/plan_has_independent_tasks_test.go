package engine

import "testing"

func TestPlanHasIndependentTasks_DisjointFilesTrue(t *testing.T) {
	bb := newTestBlackboard()
	setSuperpowersRun(bb, &SuperpowersRun{Tasks: []SuperpowersTask{
		{Title: "t0", Status: "pending", Files: []string{"a.go"}},
		{Title: "t1", Status: "pending", Files: []string{"b.go"}},
	}})

	fn := GetCondition("PlanHasIndependentTasks")
	if fn == nil {
		t.Fatal("PlanHasIndependentTasks condition not registered")
	}
	if !fn(bb) {
		t.Fatalf("want true for two pending tasks with disjoint files")
	}
}

func TestPlanHasIndependentTasks_OverlappingFilesFalse(t *testing.T) {
	bb := newTestBlackboard()
	setSuperpowersRun(bb, &SuperpowersRun{Tasks: []SuperpowersTask{
		{Title: "t0", Status: "pending", Files: []string{"a.go", "shared.go"}},
		{Title: "t1", Status: "pending", Files: []string{"b.go", "shared.go"}},
	}})

	fn := GetCondition("PlanHasIndependentTasks")
	if fn == nil {
		t.Fatal("PlanHasIndependentTasks condition not registered")
	}
	if fn(bb) {
		t.Fatalf("want false for two pending tasks sharing a file")
	}
}

func TestPlanHasIndependentTasks_SinglePendingFalse(t *testing.T) {
	bb := newTestBlackboard()
	setSuperpowersRun(bb, &SuperpowersRun{Tasks: []SuperpowersTask{
		{Title: "t0", Status: "pending", Files: []string{"a.go"}},
		{Title: "t1", Status: "done", Files: []string{"b.go"}},
	}})

	fn := GetCondition("PlanHasIndependentTasks")
	if fn == nil {
		t.Fatal("PlanHasIndependentTasks condition not registered")
	}
	if fn(bb) {
		t.Fatalf("want false when fewer than two tasks are pending")
	}
}
