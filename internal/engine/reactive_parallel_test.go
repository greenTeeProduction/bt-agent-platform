package engine

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// panicCommand is a btcore.Command[Blackboard] whose Run always panics,
// simulating a child action that blows up mid-execution.
type panicCommand struct{}

func (panicCommand) Run(_ *btcore.BTContext[Blackboard]) int {
	panic("reactive_parallel_test: child command exploded")
}

// okCommand always returns a fixed terminal result.
type okCommand struct{ result int }

func (c okCommand) Run(_ *btcore.BTContext[Blackboard]) int {
	return c.result
}

// reactiveParallelPanicActionName is registered in init() below so
// BuildReactiveParallel's tree-based path (Action nodes resolved through the
// registry) can be driven into a panicking child the same way production
// trees are built, not just via a raw btcore.Command.
const reactiveParallelPanicActionName = "TestReactiveParallelPanicAction"

func init() {
	RegisterAction(reactiveParallelPanicActionName, func(_ *btcore.BTContext[Blackboard]) int {
		panic("reactive_parallel_test: registered action exploded")
	})
}

// Both subtests below re-exec the test binary because an unrecovered panic
// inside a `go func(...)` goroutine crashes the entire process — it cannot be
// caught by the parent test's own recover(). Today (before reliability.SafeGo
// wraps the spawns in reactive_parallel.go) the child process crashes with an
// unhandled panic instead of exiting cleanly with the branch surfaced as a
// Failure result.
const reactiveParallelPanicSubprocessEnv = "BT_REACTIVE_PARALLEL_PANIC_SUBPROCESS"

func TestRunReactiveParallel_ChildPanicRecoveredAsFailure(t *testing.T) {
	if os.Getenv(reactiveParallelPanicSubprocessEnv) == "1" {
		bb := &Blackboard{Task: "test", LLM: &MockLLM{}}
		ctx := btcore.NewBTContext(context.TODO(), bb)
		children := []btcore.Command[Blackboard]{panicCommand{}, okCommand{result: 1}}
		result := runReactiveParallel(children, ParallelAll, nil, nil, false, ctx)
		if result != -1 {
			os.Exit(3)
		}
		os.Exit(0)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestRunReactiveParallel_ChildPanicRecoveredAsFailure")
	cmd.Env = append(os.Environ(), reactiveParallelPanicSubprocessEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("runReactiveParallel: a panicking child crashed the process instead of "+
			"being recovered as a Failure result via reliability.SafeGo; exit error=%v output=%s", err, out)
	}
}

func TestBuildReactiveParallel_ChildPanicRecoveredAsFailure(t *testing.T) {
	if os.Getenv(reactiveParallelPanicSubprocessEnv) == "1" {
		bb := &Blackboard{Task: "test", LLM: &MockLLM{}}
		node := &evolution.SerializableNode{
			Type: "ReactiveParallel",
			Name: "test_parallel",
			Children: []evolution.SerializableNode{
				{Type: "Action", Name: reactiveParallelPanicActionName},
				{Type: "Succeeder", Children: []evolution.SerializableNode{
					{Type: "Action", Name: "MarkSuccessful"},
				}},
			},
		}
		cmd := BuildReactiveParallel(node, bb)
		ctx := btcore.NewBTContext(context.TODO(), bb)
		result := cmd.Run(ctx)
		if result != -1 {
			os.Exit(3)
		}
		os.Exit(0)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestBuildReactiveParallel_ChildPanicRecoveredAsFailure")
	cmd.Env = append(os.Environ(), reactiveParallelPanicSubprocessEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("BuildReactiveParallel: a panicking child crashed the process instead of "+
			"being recovered as a Failure result via reliability.SafeGo; exit error=%v output=%s", err, out)
	}
}
