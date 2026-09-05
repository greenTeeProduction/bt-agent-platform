package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestForEachTask_IteratesPendingTasksAndFailsFast(t *testing.T) {
	executed := []int{}
	RegisterAction("TestFETBody", func(ctx *btcore.BTContext[Blackboard]) int {
		idx, _ := chainStateInt(ctx.Blackboard, "superpowers_task_index")
		executed = append(executed, idx)
		if idx == 1 {
			return -1 // second task fails
		}
		run, _ := getSuperpowersRun(ctx.Blackboard)
		run.Tasks[idx].Status = "done" // real done-constant from superpowers_runtime_types.go
		setSuperpowersRun(ctx.Blackboard, run)
		return 1
	})
	bb := newTestBlackboard()
	setSuperpowersRun(bb, &SuperpowersRun{Tasks: []SuperpowersTask{
		{Title: "t0"}, {Title: "t1"}, {Title: "t2"},
	}})
	cmd := buildNode(&evolution.SerializableNode{
		Type: "ForEachTask", Name: "FETUnderTest",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "TestFETBody"}},
	}, bb, "")
	ctx := newTestBTContext(bb)

	if got := cmd.Run(ctx); got != -1 {
		t.Fatalf("want FAILURE on task1, got %d", got)
	}
	if len(executed) != 2 || executed[0] != 0 || executed[1] != 1 {
		t.Fatalf("fail-fast violated: executed=%v", executed)
	}
	// Re-run resumes at the failed task (index persisted), not at 0.
	executed = nil
	if got := cmd.Run(ctx); got != -1 {
		t.Fatalf("resume run: want FAILURE again, got %d", got)
	}
	if len(executed) != 1 || executed[0] != 1 {
		t.Fatalf("resume must start at failed task 1: executed=%v", executed)
	}
}
